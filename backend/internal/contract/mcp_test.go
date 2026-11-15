package contract

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var toolName = regexp.MustCompile(`^kg_[a-z][a-z0-9_]*$`)

func TestMCPToolContractIsWellFormed(t *testing.T) {
	doc, err := MCPTools()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Endpoint != "POST /mcp" || doc.Surface != "kg_read" {
		t.Fatalf("header = %d %q %q", doc.Version, doc.Endpoint, doc.Surface)
	}
	if len(doc.Tools) == 0 {
		t.Fatal("the contract lists no tool")
	}
	names := make([]string, 0, len(doc.Tools))
	for _, tool := range doc.Tools {
		names = append(names, tool.Name)
		if !toolName.MatchString(tool.Name) {
			t.Errorf("tool %q: the name must start with kg_ (the prefix is for the tools that read the KG)", tool.Name)
		}
		if strings.TrimSpace(tool.Description) == "" || strings.ContainsAny(tool.Description, "\n\r") {
			t.Errorf("tool %q: the description must be one fixed line of text", tool.Name)
		}
		if !tool.Annotations.ReadOnly || tool.Annotations.Destructive || tool.Annotations.OpenWorld {
			t.Errorf("tool %q: every tool of this interface is read-only", tool.Name)
		}
		for what, schema := range map[string]json.RawMessage{"inputSchema": tool.InputSchema, "outputSchema": tool.OutputSchema} {
			var s struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(schema, &s); err != nil || s.Type != "object" {
				t.Errorf("tool %q: %s must be a schema of type object (the structured value is always an object)", tool.Name, what)
			}
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("the tools must be ordered by name: %v", names)
	}
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			t.Errorf("tool %q is listed twice", names[i])
		}
	}
}

func TestMCPTokenRefusalCodeIsInTheCatalogue(t *testing.T) {
	p, ok := LookupProblem("permission.mcp_not_allowed")
	if !ok || p.Status != 403 || p.Group != "permission" {
		t.Fatalf("permission.mcp_not_allowed = %+v, %v; want a 403 of the permission group", p, ok)
	}
}
