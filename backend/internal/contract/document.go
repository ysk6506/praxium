package contract

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Document is a parsed OpenAPI document, read-only. Numbers are kept as
// json.Number so the document reads back exactly as written.
type Document struct {
	// Title and Version come from info.
	Title   string
	Version string
	root    map[string]any
}

// ParseDocument reads an assembled contract.
func ParseDocument(data []byte) (*Document, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "the contract is not a JSON object")
	}
	info, _ := root["info"].(map[string]any)
	title, _ := info["title"].(string)
	version, _ := info["version"].(string)
	return &Document{Title: title, Version: version, root: root}, nil
}

// Operation is what the server and the checks need to know of one operation.
type Operation struct {
	Method      string
	Path        string
	OperationID string
	// Roles is x-required-role: who may call (anonymous, authenticated, read, or role names).
	Roles []string
	// Safe is x-safe: the call changes no state.
	Safe bool
}

var methods = []string{"get", "put", "post", "delete", "patch"}

// Operations lists every operation of the document sorted by path and then method.
func (d *Document) Operations() []Operation {
	paths, _ := d.root["paths"].(map[string]any)
	var out []Operation
	for path, item := range paths {
		entry, _ := item.(map[string]any)
		for _, m := range methods {
			op, ok := entry[m].(map[string]any)
			if !ok {
				continue
			}
			o := Operation{Method: strings.ToUpper(m), Path: path}
			o.OperationID, _ = op["operationId"].(string)
			o.Safe, _ = op["x-safe"].(bool)
			if roles, ok := op["x-required-role"].([]any); ok {
				for _, r := range roles {
					if s, ok := r.(string); ok {
						o.Roles = append(o.Roles, s)
					}
				}
			}
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// operation returns the raw operation of a method and a path template.
func (d *Document) operation(method, path string) (map[string]any, bool) {
	paths, _ := d.root["paths"].(map[string]any)
	entry, _ := paths[path].(map[string]any)
	op, ok := entry[strings.ToLower(method)].(map[string]any)
	return op, ok
}

// Schemas returns the names of the schemas under components.
func (d *Document) Schemas() []string {
	components, _ := d.root["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	out := make([]string, 0, len(schemas))
	for name := range schemas {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Schema returns a schema of components by name.
func (d *Document) Schema(name string) (any, bool) {
	components, _ := d.root["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	s, ok := schemas[name]
	return s, ok
}

// Resolve follows a local reference such as #/components/schemas/Problem.
func (d *Document) Resolve(ref string) (any, error) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "reference %q is not local", ref)
	}
	var cur any = d.root
	for _, part := range strings.Split(ref[2:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, kernel.Errorf(kernel.CodeNotFound, "reference %q does not resolve", ref)
		}
		if cur, ok = obj[part]; !ok {
			return nil, kernel.Errorf(kernel.CodeNotFound, "reference %q does not resolve", ref)
		}
	}
	return cur, nil
}
