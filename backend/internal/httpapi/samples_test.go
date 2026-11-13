package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/contract"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
)

// The conformance tests exercise every operation of the contract, and the set
// of operations is derived: it is the route table of the server and the
// contract together, not a list written here. A request for each operation is
// built from data, so a handler that adds a route adds data and no code here:
//
//   - pathSamples gives a value for each path parameter by its name;
//   - requestSamples overrides, per "METHOD /path", the query string, the
//     body or the status the request is expected to answer with;
//   - without an override the body is the example the contract itself gives
//     for the request, and the status is 200.
//
// A route that needs more than the contract gives is added to these two tables. An
// operation whose request cannot be built, or that does not answer as its
// sample says, fails the conformance tests: there is no way to leave a route unexercised.
var pathSamples = map[string]string{
	"type_id": "ot.site", "link_type_id": "lt.located_in", "action_type_id": "ac.acknowledge_alarm",
	"trait_id": "tr.common", "scheme_id": "is.array_system_id", "code": "auth.token_missing",
	"kg": "main", "object_id": sampleRackID,
}

// sampleSpec overrides what a sample would otherwise be.
type sampleSpec struct {
	// Query is added to the target, after "?".
	Query string
	// Body replaces the contract's example of the request body.
	Body string
	// Status is the answer the request is expected to get; 0 means 200.
	Status int
}

var requestSamples = map[string]sampleSpec{
	"POST /v1/kgs/{kg}/questions/get":       {Body: `{"object_id":"` + sampleRackID + `","include":["attributes","link_counts"]}`},
	"POST /v1/kgs/{kg}/questions/neighbors": {Body: `{"object_id":"` + sampleRackID + `","direction":"in","max_steps":2,"shape":"items"}`},
	"POST /v1/kgs/{kg}/questions/find":      {Body: `{"object_type":"Rack","limit":2,"shape":"items"}`},
}

// sample is one request for an operation.
type sample struct {
	method, target, body string
	status               int
}

var pathParam = regexp.MustCompile(`\{([a-z_]+)\}`)

// derivedOperations is the set of operations the tests exercise: the contract's,
// checked against the server's route table in both directions.
func derivedOperations(t *testing.T) []contract.Operation {
	t.Helper()
	doc, err := contract.ParseDocument(contract.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	ops := doc.Operations()
	known := map[string]bool{}
	for _, op := range ops {
		known[op.Method+" "+op.Path] = true
	}
	for _, r := range httpapi.Routes() {
		if !known[r.Method+" "+r.Path] {
			t.Errorf("the server has %s %s and the contract does not", r.Method, r.Path)
		}
		delete(known, r.Method+" "+r.Path)
	}
	for key := range known {
		t.Errorf("the contract has %s and the server does not", key)
	}
	return ops
}

// sampleOf builds the request for an operation, or says what is missing.
func sampleOf(op contract.Operation) (sample, error) {
	key := op.Method + " " + op.Path
	spec := requestSamples[key]
	var missing []string
	target := pathParam.ReplaceAllStringFunc(op.Path, func(token string) string {
		name := strings.Trim(token, "{}")
		value, ok := pathSamples[name]
		if !ok {
			missing = append(missing, name)
		}
		return value
	})
	if len(missing) > 0 {
		return sample{}, fmt.Errorf("%s: no sample value for the path parameter %s; add it to pathSamples in the test file of the handler", key, strings.Join(missing, ", "))
	}
	if spec.Query != "" {
		target += "?" + spec.Query
	}
	smp := sample{method: op.Method, target: target, body: spec.Body, status: spec.Status}
	if smp.status == 0 {
		smp.status = http.StatusOK
	}
	if hasRequestBody(op) && smp.body == "" {
		example, ok := requestExample(op)
		if !ok {
			return sample{}, fmt.Errorf("%s takes a body and has neither an example in the contract nor an entry in requestSamples", key)
		}
		smp.body = example
	}
	return smp, nil
}

func contractTree() map[string]any {
	var root map[string]any
	if err := json.Unmarshal(contract.OpenAPI(), &root); err != nil {
		panic(err)
	}
	return root
}

func operationNode(op contract.Operation) map[string]any {
	paths, _ := contractTree()["paths"].(map[string]any)
	item, _ := paths[op.Path].(map[string]any)
	node, _ := item[strings.ToLower(op.Method)].(map[string]any)
	return node
}

func hasRequestBody(op contract.Operation) bool {
	_, ok := operationNode(op)["requestBody"]
	return ok
}

// requestExample is the example the contract gives for the JSON body of an operation.
func requestExample(op contract.Operation) (string, bool) {
	body, _ := operationNode(op)["requestBody"].(map[string]any)
	content, _ := body["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	example, ok := media["example"]
	if !ok {
		return "", false
	}
	raw, err := json.Marshal(example)
	return string(raw), err == nil
}

// send sends a sample; withToken adds the fake token.
func (h *harness) send(smp sample, withToken bool, headers ...string) reply {
	h.t.Helper()
	req := httptest.NewRequest(smp.method, smp.target, strings.NewReader(smp.body))
	if smp.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if withToken {
		req.Header.Set("Authorization", "Bearer "+testToken)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	flat := map[string]string{}
	for k, v := range rec.Header() {
		flat[strings.ToLower(k)] = v[0]
	}
	return reply{status: rec.Code, header: rec.Header(), body: rec.Body.Bytes(), headers: flat}
}

// newSampleHarness is the server the samples run against: the standard model
// and a store that holds a small world, so that the KG routes give real answers.
func newSampleHarness(t *testing.T, roles ...access.Role) *harness {
	t.Helper()
	return newKGHarness(t, roles...).harness
}
