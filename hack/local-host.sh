#!/usr/bin/env bash
# One-time, root-only preparation of the WSL host for the local cell (never used on production VMs):
#   local-host.sh setup              kernel modules at boot (WireGuard for flannel), lvm2 + xfsprogs, lvmd binary
#   local-host.sh lvm-up <node>...   per tenant node: sparse loop file -> VG tenantvg-<node> -> lvmd (systemd unit)
#   local-host.sh lvm-down <node>... stop lvmd, remove the VG, detach and delete the loop file
#   local-host.sh status
# Why LVM lives on the host: k3d nodes are containers without LVM tools, so, like TopoLVM's own kind demo
# (third_party/topolvm/example), lvmd runs on the host and each node container mounts its lvmd socket and /dev.
# Run `setup` once from Windows: wsl -d Ubuntu-24.04 -u root -- bash hack/local-host.sh setup [user]
# setup installs a root-owned copy at /usr/local/sbin/dbcloud-local-host and a sudoers rule that lets <user>
# (default dev) run only its lvm-up, lvm-down and status commands without a password; hack/cell.sh uses that.
# The repo copy is never run through sudo, so editing it can't gain root.
# Requires: root, systemd, network for apt. Idempotent: every step checks before acting.
set -Eeuo pipefail
trap 'echo "local-host: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
state=/var/lib/dbcloud-local
# 100 GiB sparse per tenant node: room for the 10-tenant capacity test (10 × 5 GB thick volumes) plus the
# platform's volumes; sparse, so only written data uses real disk.
loop_size=100G
lvmd_bin=/usr/local/sbin/lvmd

die() { echo "local-host: $*" >&2; exit 2; }

# check_node <name>: node names come from hack/cell.sh (k3d-<cell>-<role>-<n>); allow-list them.
check_node() { [[ "$1" =~ ^k3d-[a-z0-9-]{1,40}$ ]] || die "invalid node name: $1"; }

# vg_of <node>: VG names are host-global, so each node gets its own.
vg_of() { printf 'tenantvg-%s' "${1#k3d-}"; }

setup() {
  [[ $EUID -eq 0 ]] || die "run as root (wsl -u root)"
  local user="${1:-dev}"
  if [[ ! "$user" =~ ^[a-z_][a-z0-9_-]{0,31}$ ]] || ! id "$user" >/dev/null 2>&1; then die "unknown user: $user"; fi
  printf '%s\n' '# dbcloud local cell: flannel wireguard-native, TopoLVM thin support' wireguard dm_thin_pool \
    >/etc/modules-load.d/dbcloud-local.conf
  modprobe wireguard
  modprobe dm_thin_pool
  if ! command -v lvcreate >/dev/null || ! command -v mkfs.xfs >/dev/null; then
    DEBIAN_FRONTEND=noninteractive apt-get install -y -q lvm2 xfsprogs >/dev/null
  fi
  # lvmd at the version of the pinned TopoLVM chart, built by the dev user (verified by sum.golang.org).
  local built
  built="$(sudo -u dev -H bash -lc 'cd "$1" && bash hack/install-tools.sh lvmd >/dev/null && command -v lvmd' _ "$root")"
  install -m 0755 "$built" "$lvmd_bin"
  install -d -m 0755 "$state/lvm" "$state/run" "$state/etc"
  write_unit
  # Root-owned copy + a sudoers rule limited to its data-path commands (node names are validated inside).
  install -m 0755 -o root -g root "${BASH_SOURCE[0]}" /usr/local/sbin/dbcloud-local-host
  local rule=/etc/sudoers.d/dbcloud-local tmp
  tmp="$(mktemp)"
  printf '%s ALL=(root) NOPASSWD: /usr/local/sbin/dbcloud-local-host lvm-up *, /usr/local/sbin/dbcloud-local-host lvm-down *, /usr/local/sbin/dbcloud-local-host status\n' \
    "$user" >"$tmp"
  visudo -cq -f "$tmp" || { rm -f "$tmp"; die "generated sudoers rule is invalid"; }
  install -m 0440 -o root -g root "$tmp" "$rule"
  rm -f "$tmp"
  echo "setup: modules load at boot, lvm2/xfsprogs present, lvmd installed, $user may run dbcloud-local-host lvm-up/lvm-down/status"
}

# write_unit: one templated unit per node; ExecStartPre re-attaches the loop file after a WSL restart.
write_unit() {
  cat >/etc/systemd/system/dbcloud-lvmd@.service <<EOF
[Unit]
Description=TopoLVM lvmd for local k3d node %i (dbcloud)
After=local-fs.target

[Service]
Type=simple
ExecStartPre=/bin/bash -c 'losetup -j $state/lvm/%i.img | grep -q . || losetup -f $state/lvm/%i.img; vgchange -q -ay tenantvg-\$(echo %i | sed s/^k3d-//)'
ExecStart=$lvmd_bin --config=$state/etc/%i.yaml
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
}

lvm_up() {
  [[ $EUID -eq 0 ]] || die "run as root (wsl -u root)"
  [[ -x "$lvmd_bin" ]] || die "run 'local-host.sh setup' first"
  local node vg img loop
  for node in "$@"; do
    check_node "$node"
    vg="$(vg_of "$node")"; img="$state/lvm/$node.img"
    [[ -f "$img" ]] || truncate -s "$loop_size" "$img"
    loop="$(losetup -j "$img" | cut -d: -f1)"
    [[ -n "$loop" ]] || loop="$(losetup -f --show "$img")"
    if ! vgs "$vg" >/dev/null 2>&1; then
      # The loop file was just created by this script, so it holds no data; refuse anything else.
      if blkid "$loop" >/dev/null 2>&1; then die "$loop already has a signature; not touching it"; fi
      vgcreate -q "$vg" "$loop" >/dev/null
    fi
    install -d -m 0755 "$state/run/$node"
    # spare-gb 1: keep 1 GiB of the VG unallocated (TopoLVM's default of 10 is sized for real disks).
    cat >"$state/etc/$node.yaml" <<EOF
socket-name: $state/run/$node/lvmd.sock
device-classes:
  - name: tenant
    volume-group: $vg
    default: true
    spare-gb: 1
EOF
    systemctl enable --now "dbcloud-lvmd@$node.service" >/dev/null 2>&1
    systemctl restart "dbcloud-lvmd@$node.service"
    echo "lvm-up: $node -> $vg on $loop, socket $state/run/$node/lvmd.sock"
  done
}

lvm_down() {
  [[ $EUID -eq 0 ]] || die "run as root (wsl -u root)"
  local node vg img loop
  for node in "$@"; do
    check_node "$node"
    vg="$(vg_of "$node")"; img="$state/lvm/$node.img"
    systemctl disable --now "dbcloud-lvmd@$node.service" >/dev/null 2>&1 || true
    if vgs "$vg" >/dev/null 2>&1; then vgremove -q -ff -y "$vg" >/dev/null; fi
    loop="$(losetup -j "$img" 2>/dev/null | cut -d: -f1)"
    if [[ -n "$loop" ]]; then pvremove -q -ff -y "$loop" >/dev/null 2>&1 || true; losetup -d "$loop"; fi
    rm -f "$img" "$state/etc/$node.yaml"
    rm -rf "${state:?}/run/$node"
    echo "lvm-down: $node removed"
  done
}

status() {
  vgs --noheadings -o vg_name,vg_size,vg_free 2>/dev/null | grep tenantvg- || echo "no tenant VGs"
  systemctl list-units --no-legend 'dbcloud-lvmd@*' 2>/dev/null || true
  losetup -a | grep "$state" || echo "no loop devices"
}

main() {
  local cmd="${1:-}"
  shift || true
  case "$cmd" in
    setup) setup "$@" ;;
    lvm-up) lvm_up "$@" ;;
    lvm-down) lvm_down "$@" ;;
    status) status ;;
    *) die "usage: local-host.sh setup | lvm-up <node>... | lvm-down <node>... | status" ;;
  esac
}

main "$@"
