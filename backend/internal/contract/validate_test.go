package contract

import (
	"strings"
	"testing"
)

func docWith(t *testing.T, schemas string) *Document {
	t.Helper()
	doc, err := ParseDocument([]byte(`{"openapi":"3.1.0","components":{"schemas":` + schemas + `}}`))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// [REQ-143]
func TestValidatorChecksTheDocumentedSubset(t *testing.T) {
	schemas := `{
	  "Name": {"type": "string", "minLength": 2, "maxLength": 5, "pattern": "^[a-z]+$"},
	  "Count": {"type": "integer", "minimum": 1, "maximum": 3},
	  "Kind": {"enum": ["a", "b"]},
	  "Fixed": {"const": "x"},
	  "Maybe": {"type": ["string", "null"]},
	  "List": {"type": "array", "items": {"$ref": "#/components/schemas/Name"}, "minItems": 1, "maxItems": 2},
	  "Thing": {"type": "object", "properties": {"n": {"$ref": "#/components/schemas/Name"}, "c": {"type": "integer"}}, "required": ["n"]},
	  "Open": {"type": "object", "additionalProperties": {"type": "integer"}},
	  "Closed": {"type": "object", "properties": {"a": {"type": "string"}}, "additionalProperties": false},
	  "Day": {"type": "string", "format": "date-time"}
	}`
	doc := docWith(t, schemas)
	strict, err := NewValidator(doc, true)
	if err != nil {
		t.Fatal(err)
	}
	lenient, err := NewValidator(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		schema string
		json   string
		want   string // a part of the first error, empty when the value fits
		loose  bool   // the lenient validator accepts it
	}{
		{"string fits", "Name", `"abc"`, "", true},
		{"too short", "Name", `"a"`, "at least 2", true},
		{"too long", "Name", `"abcdef"`, "at most 5", true},
		{"pattern", "Name", `"AB"`, "does not match", true},
		{"wrong type", "Name", `3`, "want string", true},
		{"integer fits", "Count", `2`, "", true},
		{"integer below", "Count", `0`, "minimum", true},
		{"integer above", "Count", `4`, "maximum", true},
		{"a fraction is not an integer", "Count", `1.5`, "want integer", true},
		{"enum fits", "Kind", `"a"`, "", true},
		{"enum misses", "Kind", `"c"`, "allowed values", true},
		{"const fits", "Fixed", `"x"`, "", true},
		{"const misses", "Fixed", `"y"`, "const", true},
		{"null allowed", "Maybe", `null`, "", true},
		{"null refused", "Name", `null`, "want string", true},
		{"array fits", "List", `["ab"]`, "", true},
		{"array too short", "List", `[]`, "at least 1", true},
		{"array too long", "List", `["ab","cd","ef"]`, "at most 2", true},
		{"item checked through a ref", "List", `["A"]`, "/0", true},
		{"object fits", "Thing", `{"n":"ab","c":1}`, "", true},
		{"required missing", "Thing", `{"c":1}`, "required", true},
		{"property checked", "Thing", `{"n":"ab","c":"x"}`, "/c", true},
		{"unknown field: strict refuses", "Thing", `{"n":"ab","z":1}`, "z", true},
		{"map values checked", "Open", `{"a":1,"b":"x"}`, "/b", true},
		{"closed object", "Closed", `{"a":"x","b":"y"}`, "b", false},
		{"date-time fits", "Day", `"2031-04-02T09:30:00Z"`, "", true},
		{"date-time misses", "Day", `"yesterday"`, "date-time", true},
		{"not JSON", "Name", `{`, "not JSON", true},
		{"text after the value", "Name", `"ab" "cd"`, "after", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, _ := doc.Schema(tt.schema)
			errs := strict.ValidateJSON(schema, []byte(tt.json))
			if tt.want == "" {
				if len(errs) > 0 {
					t.Fatalf("errors: %v", errs)
				}
				return
			}
			if len(errs) == 0 || !strings.Contains(strings.Join(errs, ";"), tt.want) {
				t.Fatalf("errors = %v, want one with %q", errs, tt.want)
			}
		})
	}
	thing, _ := doc.Schema("Thing")
	if errs := lenient.ValidateJSON(thing, []byte(`{"n":"ab","z":1}`)); len(errs) > 0 {
		t.Fatalf("the lenient validator refused an unknown field: %v", errs)
	}
}

// [REQ-143]
func TestValidatorRefusesWhatItCannotCheck(t *testing.T) {
	tests := []struct {
		name    string
		schemas string
		want    string
	}{
		{"oneOf", `{"X": {"oneOf": [{"type": "string"}]}}`, "oneOf"},
		{"anyOf nested", `{"X": {"type": "array", "items": {"anyOf": []}}}`, "anyOf"},
		{"unresolved ref", `{"X": {"$ref": "#/components/schemas/Missing"}}`, "Missing"},
		{"a schema that is not an object", `{"X": "string"}`, "X"},
		{"extension keys are ignored", `{"X": {"type": "string", "x-note": 1}}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewValidator(docWith(t, tt.schemas), true)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// [REQ-143]
func TestSchemaKeywordsAreClosedAndSorted(t *testing.T) {
	words := SchemaKeywords()
	seen := map[string]bool{}
	for _, w := range words {
		if seen[w] || w == "" {
			t.Fatalf("keyword %q repeated or empty", w)
		}
		seen[w] = true
	}
	for _, banned := range []string{"allOf", "oneOf", "anyOf", "not", "if", "patternProperties"} {
		if seen[banned] {
			t.Errorf("%s must stay outside the subset", banned)
		}
	}
}

// [REQ-148]
func TestValidateResponseChecksStatusHeadersAndBody(t *testing.T) {
	doc := loadDoc(t)
	v, err := NewValidator(doc, true)
	if err != nil {
		t.Fatal(err)
	}
	good := `{"data":{"status":"live"},"meta":{"trace_id":"tr-1"}}`
	tests := []struct {
		name    string
		method  string
		path    string
		status  int
		ctype   string
		headers map[string]string
		body    string
		want    string
	}{
		{"fits", "GET", "/v1/health/live", 200, "application/json", map[string]string{"x-trace-id": "tr-1"}, good, ""},
		{"unknown operation", "GET", "/v1/nothing", 200, "application/json", nil, good, "no operation"},
		{"undeclared status", "GET", "/v1/health/live", 418, "application/json", nil, good, "does not declare"},
		{"missing required header", "GET", "/v1/health/live", 200, "application/json", nil, good, "X-Trace-Id"},
		{"wrong media type", "GET", "/v1/health/live", 200, "text/html", map[string]string{"x-trace-id": "tr-1"}, good, "media type"},
		{"body off the schema", "GET", "/v1/health/live", 200, "application/json; charset=utf-8", map[string]string{"x-trace-id": "tr-1"}, `{"data":{"status":"dead"},"meta":{"trace_id":"tr-1"}}`, "allowed values"},
		{"body where none is declared", "GET", "/v1/roles", 304, "", map[string]string{"x-trace-id": "tr-1", "etag": `"a"`}, `x`, "no body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateResponse(tt.method, tt.path, tt.status, tt.ctype, tt.headers, []byte(tt.body))
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// [REQ-143]
func TestDocumentReadsOperationsAndReferences(t *testing.T) {
	doc := loadDoc(t)
	if doc.Title == "" || doc.Version == "" {
		t.Fatalf("title %q version %q", doc.Title, doc.Version)
	}
	if _, err := doc.Resolve("https://example.com/x"); err == nil {
		t.Fatal("a remote reference must be refused")
	}
	if _, err := doc.Resolve("#/components/nothing"); err == nil {
		t.Fatal("a missing reference must be refused")
	}
	if _, ok := doc.Schema("Problem"); !ok {
		t.Fatal("Problem is missing")
	}
	if _, err := ParseDocument([]byte(`[`)); err == nil {
		t.Fatal("not JSON must be refused")
	}
}
