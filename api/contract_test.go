// Package api_test enforces the API rules of 10-engineering-standards.md §7 and 15-security-and-reliability.md
// §5 on api/openapi.yaml itself, so a new endpoint that breaks them fails CI before any code is written:
// operation ids, RFC 9457 errors, Idempotency-Key on mutations, 202 + Operation for async changes, closed
// request/response objects, bounded strings and lists, pagination, and the security of every operation.
package api_test

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// node is a decoded YAML mapping; the spec is walked generically so the test doesn't depend on any one
// OpenAPI library's interpretation.
type node = map[string]any

// unauthenticated lists the only operations allowed to set `security: []`: the public price list and the
// Razorpay webhook, which is authenticated by its HMAC signature instead.
var unauthenticated = map[string]bool{"listPlans": true, "razorpayWebhook": true}

// operation is one path + method with its decoded definition.
type operation struct {
	path, method string
	def          node
}

// loadSpec reads and decodes openapi.yaml (tests run in api/) and fails the test unless it is OpenAPI 3.1.
func loadSpec(t *testing.T) node {
	t.Helper()
	b, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec node
	if err := yaml.Unmarshal(b, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if v, _ := spec["openapi"].(string); !strings.HasPrefix(v, "3.1.") {
		t.Fatalf("openapi = %q; want 3.1.x", v)
	}
	return spec
}

// operations lists every path + method of spec, sorted so failures are reported in a stable order.
// Path-level `parameters` entries are skipped because they aren't operations.
func operations(spec node) []operation {
	var ops []operation
	paths, _ := spec["paths"].(node)
	for p, item := range paths {
		for m, def := range item.(node) {
			if d, ok := def.(node); ok && m != "parameters" {
				ops = append(ops, operation{path: p, method: strings.ToUpper(m), def: d})
			}
		}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].path+ops[i].method < ops[j].path+ops[j].method })
	return ops
}

// refs returns the $ref strings of a parameter or response list/map.
func refs(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			out = append(out, refs(e)...)
		}
	case node:
		if r, ok := x["$ref"].(string); ok {
			return []string{r}
		}
		for _, e := range x {
			out = append(out, refs(e)...)
		}
	}
	return out
}

// id returns the operationId, or "" when it is missing.
func (o operation) id() string { s, _ := o.def["operationId"].(string); return s }

// hasParam reports whether the operation references the shared parameter ref.
func (o operation) hasParam(ref string) bool {
	for _, r := range refs(o.def["parameters"]) {
		if r == ref {
			return true
		}
	}
	return false
}

// responses returns the operation's responses by status code (nil when missing).
func (o operation) responses() node { r, _ := o.def["responses"].(node); return r }

// isPublic reports whether the operation is on the customer API (/v1) rather than /internal/v1.
func (o operation) isPublic() bool { return strings.HasPrefix(o.path, "/v1/") }

// TestTagsAreWellFormed catches flow-style YAML mistakes such as an unquoted comma splitting a tag
// description into a bogus extra key (found by the OpenAPI linter on 2026-09-30).
func TestTagsAreWellFormed(t *testing.T) {
	tags, _ := loadSpec(t)["tags"].([]any)
	if len(tags) == 0 {
		t.Fatal("tags missing")
	}
	for i, tag := range tags {
		m, _ := tag.(node)
		for k := range m {
			if k != "name" && k != "description" {
				t.Errorf("tags[%d]: unexpected key %q (quote values that contain commas)", i, k)
			}
		}
		if s, _ := m["description"].(string); s == "" {
			t.Errorf("tags[%d] (%v): description missing", i, m["name"])
		}
	}
}

// TestOperationsAreIdentifiedAndDocumented: every operation has a unique operationId (the generated
// handler name), a summary and at least one tag.
func TestOperationsAreIdentifiedAndDocumented(t *testing.T) {
	seen := map[string]string{}
	for _, o := range operations(loadSpec(t)) {
		where := o.method + " " + o.path
		if o.id() == "" {
			t.Errorf("%s: operationId missing", where)
		} else if prev, dup := seen[o.id()]; dup {
			t.Errorf("%s: operationId %q already used by %s", where, o.id(), prev)
		}
		seen[o.id()] = where
		if s, _ := o.def["summary"].(string); s == "" {
			t.Errorf("%s: summary missing", where)
		}
		if tags, _ := o.def["tags"].([]any); len(tags) == 0 {
			t.Errorf("%s: tags missing", where)
		}
	}
}

// TestMutationsRequireIdempotencyKey: every public POST/PUT/PATCH/DELETE takes Idempotency-Key
// (10-engineering-standards.md §4), except the webhook.
func TestMutationsRequireIdempotencyKey(t *testing.T) {
	for _, o := range operations(loadSpec(t)) {
		mutating := o.method == "POST" || o.method == "PUT" || o.method == "PATCH" || o.method == "DELETE"
		if !mutating || !o.isPublic() || o.id() == "razorpayWebhook" {
			continue // webhooks dedupe by event id; internal reports are idempotent by generation
		}
		if !o.hasParam("#/components/parameters/IdempotencyKey") {
			t.Errorf("%s %s (%s): mutating operation without Idempotency-Key", o.method, o.path, o.id())
		}
	}
}

// TestDatabaseChangesAreAsynchronous: database creates and deletes answer 202 with the shared Accepted
// response (Operation body + Location), and no 202 uses anything else (10-engineering-standards.md §7).
func TestDatabaseChangesAreAsynchronous(t *testing.T) {
	for _, o := range operations(loadSpec(t)) {
		resp := o.responses()
		if accepted, ok := resp["202"]; ok {
			if r := refs(accepted); len(r) != 1 || r[0] != "#/components/responses/Accepted" {
				t.Errorf("%s: 202 must use #/components/responses/Accepted (Operation + Location)", o.id())
			}
		}
		dbMutation := strings.HasPrefix(o.path, "/v1/databases") && (o.method == "POST" || o.method == "DELETE")
		if _, ok := resp["202"]; dbMutation && !ok {
			t.Errorf("%s %s (%s): database change must return 202 + Operation", o.method, o.path, o.id())
		}
	}
}

// TestErrorsAreProblemDetails: shared error responses are application/problem+json only, every 4xx/5xx
// references one of them, and public operations declare 429 and 503 (load shedding, dependency down).
func TestErrorsAreProblemDetails(t *testing.T) {
	spec := loadSpec(t)
	components := spec["components"].(node)["responses"].(node)
	for name, r := range components {
		content, _ := r.(node)["content"].(node)
		if name == "Accepted" {
			continue
		}
		if _, ok := content["application/problem+json"]; !ok || len(content) != 1 {
			t.Errorf("components.responses.%s: errors must be application/problem+json only", name)
		}
	}
	for _, o := range operations(spec) {
		for code, r := range o.responses() {
			if code[0] != '4' && code[0] != '5' {
				continue
			}
			if rr := refs(r); len(rr) != 1 || !strings.HasPrefix(rr[0], "#/components/responses/") {
				t.Errorf("%s %s: error response must reference a shared problem response", o.id(), code)
			}
		}
		if !o.isPublic() || o.id() == "razorpayWebhook" {
			continue
		}
		for _, code := range []string{"429", "503"} {
			if _, ok := o.responses()[code]; !ok {
				t.Errorf("%s: public operation must declare %s (rate limit / dependency down)", o.id(), code)
			}
		}
	}
}

// TestSecurityOfEveryOperation: internal operations require agent mTLS only, and only allow-listed
// operations may opt out of authentication with `security: []`.
func TestSecurityOfEveryOperation(t *testing.T) {
	for _, o := range operations(loadSpec(t)) {
		sec, explicit := o.def["security"].([]any)
		switch {
		case strings.HasPrefix(o.path, "/internal/"):
			if !explicit || len(sec) != 1 || sec[0].(node)["agentMTLS"] == nil {
				t.Errorf("%s: internal operation must require agentMTLS only", o.id())
			}
		case explicit && len(sec) == 0 && !unauthenticated[o.id()]:
			t.Errorf("%s: unauthenticated operation not in the allow-list", o.id())
		case unauthenticated[o.id()] && (!explicit || len(sec) != 0):
			t.Errorf("%s: expected security: [] (allow-listed public operation)", o.id())
		}
	}
}

// TestListsArePaginated: a 200 body with next_cursor implies the shared Cursor and Limit parameters
// (cursor pagination with a max page size, 10-engineering-standards.md §3).
func TestListsArePaginated(t *testing.T) {
	for _, o := range operations(loadSpec(t)) {
		ok200, _ := o.responses()["200"].(node)
		schema, _ := dig(ok200, "content", "application/json", "schema").(node)
		props, _ := schema["properties"].(node)
		if _, paged := props["next_cursor"]; !paged {
			continue
		}
		if !o.hasParam("#/components/parameters/Cursor") || !o.hasParam("#/components/parameters/Limit") {
			t.Errorf("%s: paginated list must accept cursor and limit", o.id())
		}
	}
}

// TestSchemasAreClosedAndBounded walks every schema in the spec: objects with properties reject unknown
// fields, strings have a length bound (maxLength, enum, const or pattern), arrays have maxItems.
func TestSchemasAreClosedAndBounded(t *testing.T) {
	spec := loadSpec(t)
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case node:
			if _, isRef := x["$ref"]; isRef {
				return
			}
			typ, _ := x["type"].(string)
			if _, hasProps := x["properties"]; hasProps && x["additionalProperties"] != false {
				t.Errorf("%s: object with properties must set additionalProperties: false", path)
			}
			if typ == "string" && x["maxLength"] == nil && x["enum"] == nil && x["const"] == nil && x["pattern"] == nil {
				t.Errorf("%s: string without maxLength/enum/const/pattern", path)
			}
			if typ == "array" && x["maxItems"] == nil {
				t.Errorf("%s: array without maxItems", path)
			}
			for k, e := range x {
				walk(path+"."+k, e)
			}
		case []any:
			for i, e := range x {
				walk(path+"["+strconv.Itoa(i)+"]", e)
			}
		}
	}
	walk("components.schemas", spec["components"].(node)["schemas"])
	for _, o := range operations(spec) {
		walk(o.id()+".requestBody", o.def["requestBody"])
		walk(o.id()+".responses", o.responses())
		walk(o.id()+".parameters", o.def["parameters"])
	}
}

// dig follows keys through nested mappings, returning nil when any step is missing.
func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(node)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}
