//go:build cell

// Package cell_test is the acceptance suite for the local cell (plan/04 Step 0.3 "Done when"): Pod Security,
// the audit log and secrets encryption, tenant isolation and egress rules enforced by K3s's network-policy
// controller, TopoLVM size limits and online expansion, WAL archiving to S3 with a point-in-time restore,
// and, on the reference shape, the control plane surviving the loss of a server.
//
// It drives a running cell (`task cell:up`) through kubectl, docker and hack/tenant.sh, the same tools an
// operator uses; it creates its own namespaces with random names and deletes them afterwards. It doesn't run
// in GitHub CI (no k3d or LVM there): run it with `task cell:test`.
package cell_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// defaultContext is the kubeconfig context hack/cell.sh creates.
	defaultContext = "k3d-local"
	// shortTimeout bounds a single kubectl/docker call; longer waits use kubectl wait with its own timeout.
	shortTimeout = 2 * time.Minute
	// readyTimeout bounds a tenant database becoming Ready (measured ~80 s on the local cell).
	readyTimeout = 10 * time.Minute
	// pollEvery is the interval between checks of an asynchronous condition.
	pollEvery = 3 * time.Second
	// alpineImage runs the storage probe; pinned by digest like every image in the cell.
	alpineImage = "docker.io/library/alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8"
)

// repoRoot is the repository root; tests run in test/cell.
var repoRoot = filepath.Join("..", "..")

// kubeContext returns the kubeconfig context under test: $KUBE_CONTEXT, or the one hack/cell.sh creates.
func kubeContext() string {
	if c := os.Getenv("KUBE_CONTEXT"); c != "" {
		return c
	}
	return defaultContext
}

// run executes a command with a deadline and returns its trimmed combined output.
func run(t *testing.T, timeout time.Duration, stdin string, name string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- test harness: kubectl/docker/bash/yq with generated arguments
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// kubectl runs kubectl against the cell's context with shortTimeout and returns its output and error.
func kubectl(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return run(t, shortTimeout, "", "kubectl", append([]string{"--context", kubeContext()}, args...)...)
}

// mustKubectl is kubectl that fails the test on error.
func mustKubectl(t *testing.T, args ...string) string {
	t.Helper()
	out, err := kubectl(t, args...)
	if err != nil {
		t.Fatalf("kubectl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// applyYAML applies manifest through stdin (server-side validation and admission apply) or fails the test.
func applyYAML(t *testing.T, manifest string) {
	t.Helper()
	out, err := run(t, shortTimeout, manifest, "kubectl", "--context", kubeContext(), "apply", "-f", "-")
	if err != nil {
		t.Fatalf("kubectl apply: %v\n%s", err, out)
	}
}

// eventually polls check until it returns true or timeout passes.
func eventually(t *testing.T, timeout time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		select {
		case <-t.Context().Done():
			t.Fatalf("cancelled while waiting for %s", what)
		case <-time.After(pollEvery):
		}
	}
}

// randomName returns prefix + 8 characters from [a-z2-7], matching tenant refs (hack/tenant.sh).
func randomName(t *testing.T, prefix string) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return prefix + string(b)
}

// tenant creates a `base` tenant database (the smallest plan, so the tests fit the starter cell) with
// hack/tenant.sh, waits until it serves, and deletes it at the end.
func tenant(t *testing.T) string {
	t.Helper()
	ref := randomName(t, "t-")
	if out, err := run(t, shortTimeout, "", "bash", filepath.Join(repoRoot, "hack", "tenant.sh"), "create", ref, "base"); err != nil {
		t.Fatalf("tenant.sh create %s: %v\n%s", ref, err, out)
	}
	t.Cleanup(func() { deleteNamespace(t, ref); deleteBackups(t, ref) })
	waitTenant(t, ref)
	return ref
}

// deleteBackups removes a test tenant's prefix from the local S3 bucket so repeated runs don't fill its
// 10 GiB volume. In production, backup retention after tenant deletion is the worker's job (plan/01 §14).
func deleteBackups(t *testing.T, ref string) {
	if os.Getenv("KEEP_TEST_NAMESPACES") == "1" {
		return
	}
	// ref was generated by randomName ([a-z2-7] only), so it can't inject another weed shell command.
	if out, err := runCleanup("fs.rm -r /buckets/dbcloud-backups/"+ref+"\n", "kubectl", "--context", kubeContext(),
		"-n", "dbcloud-system", "exec", "-i", "deploy/seaweedfs", "--", "weed", "shell"); err != nil {
		t.Errorf("delete backups of %s: %v\n%s", ref, err, out)
	}
}

// runCleanup runs a command from t.Cleanup. t.Context() is already cancelled there, so the command gets its
// own shortTimeout deadline instead of being killed at once.
func runCleanup(stdin, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- test harness: kubectl/docker with generated arguments
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// waitTenant blocks until the Cluster in ns is Ready or readyTimeout passes (then the test fails).
func waitTenant(t *testing.T, ns string) {
	t.Helper()
	if out, err := run(t, readyTimeout+time.Minute, "", "kubectl", "--context", kubeContext(), "-n", ns,
		"wait", "cluster/db", "--for=condition=Ready", "--timeout="+readyTimeout.String()); err != nil {
		t.Fatalf("%s: database not Ready: %v\n%s", ns, err, out)
	}
}

// deleteNamespace removes a test namespace without waiting (KEEP_TEST_NAMESPACES=1 keeps them for debugging).
func deleteNamespace(t *testing.T, ns string) {
	if os.Getenv("KEEP_TEST_NAMESPACES") == "1" {
		t.Logf("keeping namespace %s", ns)
		return
	}
	if out, err := runCleanup("", "kubectl", "--context", kubeContext(), "delete", "namespace", ns, "--wait=false"); err != nil {
		t.Errorf("delete namespace %s: %v\n%s", ns, err, out)
	}
}

// sql runs a statement as the database superuser over the pod's local socket (the only way in without a
// superuser password; customers never get this), returning the unaligned result.
func sql(t *testing.T, ns, statement string) string {
	t.Helper()
	return mustKubectl(t, "-n", ns, "exec", "db-1", "-c", "postgres", "--", "psql", "-d", "app", "-v", "ON_ERROR_STOP=1",
		"-qtAc", statement)
}

// tcpFrom reports whether a TCP connection from the tenant's database pod to host:port succeeds within 5 s.
func tcpFrom(t *testing.T, ns, host string, port int) bool {
	t.Helper()
	probe := fmt.Sprintf("timeout 5 bash -c '</dev/tcp/%s/%d'", host, port)
	_, err := kubectl(t, "-n", ns, "exec", "db-1", "-c", "postgres", "--", "bash", "-c", probe)
	return err == nil
}

// TestPodSecurityRestrictedByDefault: a namespace without PSA labels still enforces `restricted` (psa.yaml).
func TestPodSecurityRestrictedByDefault(t *testing.T) {
	ns := randomName(t, "psa-")
	mustKubectl(t, "create", "namespace", ns)
	t.Cleanup(func() { deleteNamespace(t, ns) })
	privileged := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata: {name: privileged, namespace: %s}
spec:
  containers:
    - {name: c, image: %q, command: [sleep, "60"], securityContext: {privileged: true}}
`, ns, alpineImage)
	out, err := run(t, shortTimeout, privileged, "kubectl", "--context", kubeContext(), "apply", "--dry-run=server", "-f", "-")
	if err == nil || !strings.Contains(out, `violates PodSecurity "restricted`) {
		t.Errorf("privileged pod in an unlabelled namespace: err=%v out=%q; want a PodSecurity restricted violation", err, out)
	}
}

// TestAuditLogAndSecretsEncryption: the API audit log is written at Metadata level only, and K3s encrypts
// Secrets at rest.
func TestAuditLogAndSecretsEncryption(t *testing.T) {
	node := "k3d-local-server-0"
	out, err := run(t, shortTimeout, "", "docker", "exec", node, "sh", "-c",
		"test -s /var/lib/rancher/k3s/server/logs/audit.log && tail -n 200 /var/lib/rancher/k3s/server/logs/audit.log")
	if err != nil {
		t.Fatalf("audit log missing on %s: %v\n%s", node, err, out)
	}
	if !strings.Contains(out, `"level":"Metadata"`) {
		t.Errorf("audit log has no Metadata events")
	}
	if strings.Contains(out, `"requestObject"`) || strings.Contains(out, `"responseObject"`) {
		t.Errorf("audit log contains request/response bodies; the policy must log metadata only")
	}
	status, err := run(t, shortTimeout, "", "docker", "exec", node, "k3s", "secrets-encrypt", "status")
	if err != nil || !strings.Contains(status, "Encryption Status: Enabled") {
		t.Errorf("secrets encryption status: err=%v\n%s", err, status)
	}
}

// TestTenantNetworkIsolation: default-deny both ways per tenant, enforced by K3s's kube-router controller.
func TestTenantNetworkIsolation(t *testing.T) {
	a, b := tenant(t), tenant(t)
	tests := []struct {
		name string
		host string
		port int
		want bool
	}{
		{"own pooler", "pooler." + a + ".svc", 5432, true},
		{"other tenant's pooler", "pooler." + b + ".svc", 5432, false},
		{"other tenant's database", "db-rw." + b + ".svc", 5432, false},
		{"local S3 (backups)", "seaweedfs-s3.dbcloud-system.svc", 8333, true},
		{"internet on 443 (R2 path)", "1.1.1.1", 443, true},
		{"internet on another port", "1.1.1.1", 80, false},
		{"platform service not allowed", "victoria-metrics-victoria-metrics-single-server.observability.svc", 8428, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tcpFrom(t, a, tt.host, tt.port); got != tt.want {
				t.Errorf("connect %s:%d from %s = %v; want %v", tt.host, tt.port, a, got, tt.want)
			}
		})
	}
}

// TestStorageLimitAndOnlineExpansion: a TopoLVM volume stops writes at its size and grows while mounted.
func TestStorageLimitAndOnlineExpansion(t *testing.T) {
	ns := randomName(t, "st-")
	mustKubectl(t, "create", "namespace", ns)
	t.Cleanup(func() { deleteNamespace(t, ns) })
	applyYAML(t, fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: data, namespace: %[1]s}
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: tenant-local
  resources: {requests: {storage: 1Gi}}
---
apiVersion: v1
kind: Pod
metadata: {name: probe, namespace: %[1]s}
spec:
  nodeSelector: {dbcloud.io/pool: tenant}
  securityContext: {runAsNonRoot: true, runAsUser: 1000, runAsGroup: 1000, fsGroup: 1000, seccompProfile: {type: RuntimeDefault}}
  containers:
    - name: probe
      image: %[2]q
      command: [sleep, "3600"]
      resources: {requests: {cpu: 10m, memory: 32Mi}, limits: {memory: 32Mi}}
      securityContext: {allowPrivilegeEscalation: false, capabilities: {drop: [ALL]}}
      volumeMounts: [{name: data, mountPath: /data}]
  volumes: [{name: data, persistentVolumeClaim: {claimName: data}}]
`, ns, alpineImage))
	mustKubectl(t, "-n", ns, "wait", "pod/probe", "--for=condition=Ready", "--timeout=180s")
	uid := mustKubectl(t, "-n", ns, "get", "pod", "probe", "-o", "jsonpath={.metadata.uid}")

	sizeMB := func() int {
		out := mustKubectl(t, "-n", ns, "exec", "probe", "--", "sh", "-c", "df -Pm /data | awk 'NR==2 {print $2}'")
		n, err := strconv.Atoi(out)
		if err != nil {
			t.Fatalf("df output %q: %v", out, err)
		}
		return n
	}
	if got := sizeMB(); got > 1100 {
		t.Fatalf("filesystem size = %d MB; want ≤ 1100 for a 1 GiB volume", got)
	}
	// oflag=direct: buffered writes would fill the probe's 32 MiB cgroup with dirty page cache and get it
	// OOM-killed before the volume is full.
	out, err := kubectl(t, "-n", ns, "exec", "probe", "--", "dd", "if=/dev/zero", "of=/data/fill", "bs=1M", "count=1500", "oflag=direct")
	if err == nil || !strings.Contains(out, "No space left") {
		t.Errorf("writing 1.5 GB into a 1 GiB volume: err=%v out=%q; want ENOSPC", err, out)
	}

	mustKubectl(t, "-n", ns, "patch", "pvc", "data", "--type=merge", "-p", `{"spec":{"resources":{"requests":{"storage":"2Gi"}}}}`)
	eventually(t, 3*time.Minute, "the filesystem to grow online", func() bool { return sizeMB() > 1900 })
	if got := mustKubectl(t, "-n", ns, "get", "pod", "probe", "-o", "jsonpath={.metadata.uid}/{.status.containerStatuses[0].restartCount}"); got != uid+"/0" {
		t.Errorf("pod after expansion = %s; want the same pod, no restarts (%s/0)", got, uid)
	}
	if out, err := kubectl(t, "-n", ns, "exec", "probe", "--", "sh", "-c", "rm -f /data/fill && dd if=/dev/zero of=/data/fill bs=1M count=1500 oflag=direct"); err != nil {
		t.Errorf("writing 1.5 GB after growing to 2 GiB: %v\n%s", err, out)
	}
}

// TestWALArchiveAndPointInTimeRestore: WAL reaches the local S3 and a restore to T1 returns exactly T1's rows.
func TestWALArchiveAndPointInTimeRestore(t *testing.T) {
	src := tenant(t)
	eventually(t, 3*time.Minute, "continuous archiving", func() bool {
		out, err := kubectl(t, "-n", src, "get", "cluster", "db", "-o", `jsonpath={.status.conditions[?(@.type=="ContinuousArchiving")].status}`)
		return err == nil && out == "True"
	})

	// A base backup is the starting point of every point-in-time restore.
	applyYAML(t, fmt.Sprintf(`apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata: {name: base-1, namespace: %s}
spec:
  cluster: {name: db}
  method: plugin
  pluginConfiguration: {name: barman-cloud.cloudnative-pg.io}
`, src))
	eventually(t, 5*time.Minute, "the base backup", func() bool {
		out, err := kubectl(t, "-n", src, "get", "backup", "base-1", "-o", "jsonpath={.status.phase}")
		if err != nil {
			return false // not visible yet; keep polling
		}
		if out == "failed" {
			t.Fatalf("base backup failed: %s", mustKubectl(t, "-n", src, "get", "backup", "base-1", "-o", "jsonpath={.status.error}"))
		}
		return out == "completed"
	})

	sql(t, src, "create table sentinel (id int primary key)")
	sql(t, src, "insert into sentinel select generate_series(1, 100)")
	t1 := sql(t, src, "select clock_timestamp()::text")
	sql(t, src, "select pg_sleep(1)") // commits after T1 get a strictly later timestamp
	sql(t, src, "insert into sentinel select generate_series(101, 150)")
	segment := sql(t, src, "select pg_walfile_name(pg_current_wal_lsn())")
	sql(t, src, "select pg_switch_wal()")
	eventually(t, 3*time.Minute, "WAL segment "+segment+" in S3", func() bool {
		last := sql(t, src, "select coalesce(last_archived_wal, '') from pg_stat_archiver")
		return last >= segment
	})

	// Restore into a new tenant namespace: the same template, but bootstrapped from src's backups at T1.
	dst := randomName(t, "t-")
	t.Cleanup(func() { deleteNamespace(t, dst); deleteBackups(t, dst) })
	rendered, err := run(t, shortTimeout, "", "bash", filepath.Join(repoRoot, "hack", "tenant.sh"), "render", dst, "base")
	if err != nil {
		t.Fatalf("render %s: %v\n%s", dst, err, rendered)
	}
	source := fmt.Sprintf(`---
apiVersion: barmancloud.cnpg.io/v1
kind: ObjectStore
metadata: {name: source, namespace: %s}
spec:
  configuration:
    destinationPath: s3://dbcloud-backups/%s/
    endpointURL: http://seaweedfs-s3.dbcloud-system.svc:8333
    s3Credentials:
      accessKeyId: {name: backup-s3, key: ACCESS_KEY_ID}
      secretAccessKey: {name: backup-s3, key: ACCESS_SECRET_KEY}
    wal: {compression: zstd}
`, dst, src)
	mustKubectl(t, "create", "namespace", dst)
	mustKubectl(t, "label", "namespace", dst, "dbcloud.io/tenant="+dst, "pod-security.kubernetes.io/enforce=restricted")
	mustKubectl(t, "-n", dst, "create", "secret", "generic", "backup-s3", "--from-env-file="+filepath.Join(repoRoot, ".local", "s3.env"))
	// yq reads the target time from the environment so the timestamp is never spliced into the expression.
	t.Setenv("T1", t1)
	restore, err := run(t, shortTimeout, rendered, "yq", "e",
		`(select(.kind == "Cluster") | .spec.bootstrap) = {"recovery": {"source": "origin", "recoveryTarget": {"targetTime": strenv(T1)}}} |
		 (select(.kind == "Cluster") | .spec.externalClusters) = [{"name": "origin", "plugin": {"name": "barman-cloud.cloudnative-pg.io", "parameters": {"barmanObjectName": "source", "serverName": "db"}}}]`, "-")
	if err != nil {
		t.Fatalf("yq: %v\n%s", err, restore)
	}
	applyYAML(t, restore+"\n"+source)
	waitTenant(t, dst)

	if got := sql(t, dst, "select count(*) || ':' || max(id) from sentinel"); got != "100:100" {
		t.Errorf("restored rows (count:max) = %s; want 100:100 (only the rows committed before T1 %s)", got, t1)
	}
}

// TestControlPlaneSurvivesServerLoss runs on the reference shape only: stopping cp-1 (server-0) keeps the
// API and a tenant database serving, and the server rejoins etcd when it starts again.
func TestControlPlaneSurvivesServerLoss(t *testing.T) {
	servers := mustKubectl(t, "get", "nodes", "-l", "node-role.kubernetes.io/etcd=true", "-o", "name")
	if strings.Count(servers, "\n")+1 < 3 {
		t.Skip("starter shape (1 server); run `task cell:up PROFILE=reference` for this test")
	}
	ref := tenant(t)
	sql(t, ref, "create table heartbeat (at timestamptz)")

	if out, err := run(t, shortTimeout, "", "docker", "stop", "k3d-local-server-0"); err != nil {
		t.Fatalf("stop server-0: %v\n%s", err, out)
	}
	// Idempotent: starting a running container is a no-op, so this is safe after the start below too.
	t.Cleanup(func() {
		if out, err := runCleanup("", "docker", "start", "k3d-local-server-0"); err != nil {
			t.Errorf("start server-0: %v\n%s", err, out)
		}
	})
	eventually(t, 2*time.Minute, "the API through the remaining servers", func() bool {
		_, err := kubectl(t, "get", "--raw", "/readyz")
		return err == nil
	})
	sql(t, ref, "insert into heartbeat values (now())") // the tenant keeps accepting writes

	if out, err := run(t, shortTimeout, "", "docker", "start", "k3d-local-server-0"); err != nil {
		t.Fatalf("start server-0: %v\n%s", err, out)
	}
	eventually(t, 5*time.Minute, "server-0 to rejoin", func() bool {
		out, err := kubectl(t, "get", "node", "k3d-local-server-0", "-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`)
		return err == nil && out == "True"
	})
	// Rejoined means more than node Ready: server-0's own API server sees a healthy etcd (its member is back in
	// the quorum) and flannel has rebuilt its WireGuard link, which happens a few seconds after Ready.
	eventually(t, 2*time.Minute, "server-0's etcd member and WireGuard link", func() bool {
		if _, err := run(t, shortTimeout, "", "docker", "exec", "k3d-local-server-0", "kubectl", "get", "--raw", "/readyz/etcd"); err != nil {
			return false
		}
		_, err := run(t, shortTimeout, "", "docker", "exec", "k3d-local-server-0", "ip", "link", "show", "flannel-wg")
		return err == nil
	})
	if got := sql(t, ref, "select count(*) from heartbeat"); got != "1" {
		t.Errorf("heartbeat rows after the server came back = %s; want 1", got)
	}
}

// TestWireGuardBetweenNodes: flannel's wireguard-native interface exists on every node (plan/01 §4.2).
func TestWireGuardBetweenNodes(t *testing.T) {
	nodes := strings.Fields(mustKubectl(t, "get", "nodes", "-o", "jsonpath={.items[*].metadata.name}"))
	for _, n := range nodes {
		out, err := run(t, shortTimeout, "", "docker", "exec", n, "ip", "-d", "link", "show", "flannel-wg")
		if err != nil || !strings.Contains(out, "wireguard") {
			t.Errorf("%s: flannel-wg is not a WireGuard link: err=%v\n%s", n, err, out)
		}
	}
}
