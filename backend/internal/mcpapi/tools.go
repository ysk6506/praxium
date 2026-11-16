package mcpapi

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// The registry of tools knows no transport: a tool is a name, a fixed description, the schemas of what it takes and
// answers, and a function from "the subject, the name and the raw arguments" to a neutral result (blocks of text, a
// structured value, a mark of error). The JSON-RPC and HTTP of what carries it are in the other files (ADR-0062 9).

// KindKGRead is the kind of the tools that read the KG. /mcp lists the tools of this kind and no other: a tool of
// another kind (a gateway tool, an operations tool) in the same registry never shows there (kg-mcp.md 1).
const KindKGRead = "kg_read"

// Call is what a tool is run with. Args is the arguments object as the caller wrote it, read by the strict parser and
// written out again (nil when there were none).
type Call struct {
	Subject access.Subject
	Name    string
	Args    json.RawMessage
	Trace   string
}

// Result is what a tool answers. A successful result has the Structured value (an object) and no IsError; an error
// result has the code of the catalogue in Problem and carries no Structured value (kg-mcp.md 4.4). Read is what the
// read record notes of the call.
type Result struct {
	Structured any
	Problem    *kgread.Problem
	Read       ReadNote
}

// ReadNote is what the read record of a call says about what was read (REQ-514): the KG asked, the object types
// touched, the ids the caller named, the number of results and whether the answer was cut.
type ReadNote struct {
	KG          string
	ObjectTypes []string
	ObjectIDs   []string
	Count       int
	Truncated   bool
}

// Tool is one entry of the registry.
type Tool struct {
	Name        string
	Description string
	Kind        string
	// Input and Output are JSON Schemas as JSON text.
	Input, Output json.RawMessage
	// Run answers a call. It must not change anything outside the read record: every tool of /mcp is read-only.
	Run func(ctx context.Context, c Call) Result
}

// Registry holds the tools by name. Build it with NewRegistry and Add.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry builds a registry of the given tools.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range tools {
		r.Add(t)
	}
	return r
}

// Add puts a tool in the registry; a second tool of the same name replaces the first.
func (r *Registry) Add(t Tool) { r.tools[t.Name] = t }

// Get finds a tool by its exact name among the tools of a kind.
func (r *Registry) Get(kind, name string) (Tool, bool) {
	t, ok := r.tools[name]
	if !ok || t.Kind != kind {
		return Tool{}, false
	}
	return t, true
}

// List gives the tools of a kind in a fixed order: by name.
func (r *Registry) List(kind string) []Tool {
	var out []Tool
	for _, t := range r.tools {
		if t.Kind == kind {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names gives the names of the tools of a kind, in the order of List.
func (r *Registry) Names(kind string) []string {
	var out []string
	for _, t := range r.List(kind) {
		out = append(out, t.Name)
	}
	return out
}

// definition is the tools/list entry of a tool. The annotations say what the tool does to the world; every tool here
// reads only. They are a convenience for a client and no ground for any control of ours (ADR-0062 2).
type definition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Output      json.RawMessage `json:"outputSchema"`
	Annotations annotations     `json:"annotations"`
}

type annotations struct {
	ReadOnly    bool `json:"readOnlyHint"`
	Destructive bool `json:"destructiveHint"`
	Idempotent  bool `json:"idempotentHint"`
	OpenWorld   bool `json:"openWorldHint"`
}

// Definition is the entry of tools/list for a tool, as JSON.
func Definition(t Tool) ([]byte, error) { return json.Marshal(toDefinition(t)) }

func toDefinition(t Tool) definition {
	return definition{
		Name: t.Name, Description: t.Description, InputSchema: t.Input, Output: t.Output,
		Annotations: annotations{ReadOnly: true, Idempotent: true},
	}
}
