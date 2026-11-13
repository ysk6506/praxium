package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func loadDoc(t *testing.T) *Document {
	t.Helper()
	doc, err := ParseDocument(OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func rawRoot(t *testing.T) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(OpenAPI()))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		t.Fatal(err)
	}
	return root
}

// [REQ-143]
func TestCommittedContractIsWhatThePartsAssembleTo(t *testing.T) {
	got, err := Assemble(Files(), PartsDir)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(OpenAPI()) {
		t.Fatal("openapi/openapi.json is not what the parts assemble to; run `praxium contract assemble`")
	}
}

// [REQ-143]
func TestContractUsesOnlyTheDocumentedSchemaSubset(t *testing.T) {
	doc := loadDoc(t)
	if _, err := NewValidator(doc, true); err != nil {
		t.Fatal(err)
	}
	bad := `{"openapi":"3.1.0","components":{"schemas":{"X":{"allOf":[{"type":"string"}]}}}}`
	badDoc, err := ParseDocument([]byte(bad))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewValidator(badDoc, true); err == nil || !strings.Contains(err.Error(), "allOf") {
		t.Fatalf("err = %v, want a refusal of allOf", err)
	}
}

// [REQ-148]
func TestEveryOperationDeclaresTheConventions(t *testing.T) {
	doc := loadDoc(t)
	ops := doc.Operations()
	if len(ops) == 0 {
		t.Fatal("the contract has no operation")
	}
	roles := map[string]bool{"anonymous": true, "authenticated": true, "read": true}
	for _, r := range []string{"viewer", "editor", "approver", "operator", "model_steward", "auditor", "admin", "ingester"} {
		roles[r] = true
	}
	pathRule := regexp.MustCompile(`^/v1(/[a-z0-9]+(-[a-z0-9]+)*|/\{[a-z_]+\})+$`)
	for _, op := range ops {
		name := op.Method + " " + op.Path
		if !pathRule.MatchString(op.Path) {
			t.Errorf("%s: the path breaks the naming rule", name)
		}
		if op.OperationID == "" || len(op.Roles) == 0 {
			t.Errorf("%s: operationId and x-required-role are required", name)
		}
		for _, r := range op.Roles {
			if !roles[r] {
				t.Errorf("%s: unknown role %q", name, r)
			}
		}
		if op.Method == "GET" && !op.Safe {
			t.Errorf("%s: a GET must be x-safe", name)
		}
	}
}

func walkResponses(t *testing.T, root map[string]any, doc *Document, visit func(where string, resp map[string]any)) {
	t.Helper()
	paths, _ := root["paths"].(map[string]any)
	for path, item := range paths {
		for method, rawOp := range item.(map[string]any) {
			op, _ := rawOp.(map[string]any)
			responses, _ := op["responses"].(map[string]any)
			for status, raw := range responses {
				resp, _ := raw.(map[string]any)
				if ref, ok := resp["$ref"].(string); ok {
					target, err := doc.Resolve(ref)
					if err != nil {
						t.Fatal(err)
					}
					resp, _ = target.(map[string]any)
				}
				visit(fmt.Sprintf("%s %s %s", strings.ToUpper(method), path, status), resp)
			}
		}
	}
}

// [REQ-148]
func TestEveryResponseHasAnExampleThatFitsItsSchema(t *testing.T) {
	doc := loadDoc(t)
	v, err := NewValidator(doc, true)
	if err != nil {
		t.Fatal(err)
	}
	walkResponses(t, rawRoot(t), doc, func(where string, resp map[string]any) {
		content, _ := resp["content"].(map[string]any)
		if len(content) == 0 && !strings.HasSuffix(where, " 304") {
			t.Errorf("%s: a response without a body must be a 304", where)
		}
		for media, raw := range content {
			entry, _ := raw.(map[string]any)
			example, ok := entry["example"]
			if !ok {
				t.Errorf("%s %s: no example", where, media)
				continue
			}
			if errs := v.Validate(entry["schema"], example); len(errs) > 0 {
				t.Errorf("%s %s: the example does not fit: %s", where, media, strings.Join(errs, "; "))
			}
		}
	})
}

// [REQ-148]
func TestErrorExamplesUseOnlyCataloguedCodes(t *testing.T) {
	doc := loadDoc(t)
	root := rawRoot(t)
	seen := map[string]bool{}
	check := func(where string, resp map[string]any) {
		content, _ := resp["content"].(map[string]any)
		entry, _ := content["application/problem+json"].(map[string]any)
		if entry == nil {
			return
		}
		example, _ := entry["example"].(map[string]any)
		code, _ := example["code"].(string)
		problem, ok := LookupProblem(code)
		if !ok {
			t.Errorf("%s: code %q is not in the catalogue", where, code)
			return
		}
		status := fmt.Sprint(example["status"])
		if status != fmt.Sprint(problem.Status) {
			t.Errorf("%s: example status %s, catalogue %d", where, status, problem.Status)
		}
		if example["type"] != "/v1/problems/"+code {
			t.Errorf("%s: type %v does not point at the code", where, example["type"])
		}
		seen[code] = true
	}
	walkResponses(t, root, doc, check)
	components, _ := root["components"].(map[string]any)
	for name, raw := range components["responses"].(map[string]any) {
		check("components/responses/"+name, raw.(map[string]any))
	}
	if len(seen) < 8 {
		t.Fatalf("only %d error codes are exemplified", len(seen))
	}
}

// [REQ-148]
func TestProblemCatalogueIsClosedAndWellFormed(t *testing.T) {
	groups := map[string]bool{}
	for _, g := range ProblemGroups() {
		groups[g] = true
	}
	seen := map[string]bool{}
	for _, p := range ProblemCodes() {
		if seen[p.Code] {
			t.Errorf("code %s twice", p.Code)
		}
		seen[p.Code] = true
		group, detail, ok := strings.Cut(p.Code, ".")
		if !ok || detail == "" || group != p.Group || !groups[group] {
			t.Errorf("code %q does not follow <group>.<detail> of group %q", p.Code, p.Group)
		}
		if p.Status < 400 || p.Status > 599 || p.Meaning == "" {
			t.Errorf("code %s: status %d, meaning %q", p.Code, p.Status, p.Meaning)
		}
	}
	if _, ok := LookupProblem("no.such_code"); ok {
		t.Fatal("an unknown code was found")
	}
	if len(ReasonCodes()) != 6 {
		t.Fatalf("reason codes = %d, want the six of rest-api.md section 4", len(ReasonCodes()))
	}
}

// [REQ-143]
func TestPlannedRoutesAreRegisteredAndNotInTheContract(t *testing.T) {
	planned, err := PlannedRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(planned) == 0 {
		t.Fatal("the planned route registry is empty")
	}
	specified := map[string]bool{}
	for _, op := range loadDoc(t).Operations() {
		specified[op.Method+" "+op.Path] = true
	}
	phases := map[string]bool{"alpha-1": true, "alpha-2": true, "closed-beta": true, "open-beta": true}
	seen := map[string]bool{}
	for _, r := range planned {
		key := r.Method + " " + r.Path
		switch {
		case seen[key]:
			t.Errorf("%s is registered twice", key)
		case specified[key]:
			t.Errorf("%s is both specified and planned", key)
		case r.Status != "planned" || !phases[r.Phase] || r.Feature == "" || !strings.HasPrefix(r.ADR, "ADR-"):
			t.Errorf("%s: incomplete entry %+v", key, r)
		}
		seen[key] = true
	}
}

// [REQ-148]
func TestContractListsExactlyTheCataloguedProblemCodes(t *testing.T) {
	doc := loadDoc(t)
	raw, ok := doc.Schema("ProblemCode")
	if !ok {
		t.Fatal("the contract has no ProblemCode schema")
	}
	schema := raw.(map[string]any)
	enum, _ := schema["enum"].([]any)
	catalogue, _ := schema["x-catalogue"].(map[string]any)
	codes := ProblemCodes()
	if len(enum) != len(codes) || len(catalogue) != len(codes) {
		t.Fatalf("the contract lists %d codes (%d with a status), the catalogue has %d", len(enum), len(catalogue), len(codes))
	}
	for _, p := range codes {
		entry, _ := catalogue[p.Code].(map[string]any)
		if entry == nil || entry["group"] != p.Group || fmt.Sprint(entry["status"]) != fmt.Sprint(p.Status) {
			t.Errorf("the contract's entry for %s is %v, the catalogue says %s/%d", p.Code, entry, p.Group, p.Status)
		}
		found := false
		for _, e := range enum {
			found = found || e == p.Code
		}
		if !found {
			t.Errorf("%s is not in the enum", p.Code)
		}
	}
}
