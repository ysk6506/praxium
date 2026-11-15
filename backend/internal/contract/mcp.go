package contract

import (
	"bytes"
	"encoding/json"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// MCPToolsPath is where the contract of the tools of the MCP interface is, from the root of the unit's folder.
const MCPToolsPath = "mcp/tools.json"

// MCPTool is one tool of the contract of the MCP interface (ADR-0062 12): its name, its fixed description, the JSON
// Schemas of what it takes and answers, and the annotations that say it only reads.
type MCPTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	Annotations  MCPAnnotations  `json:"annotations"`
}

// MCPAnnotations are the hints a client gets of what a tool does to the world. They are a convenience for the client
// and no ground for any control of ours.
type MCPAnnotations struct {
	ReadOnly    bool `json:"readOnlyHint"`
	Destructive bool `json:"destructiveHint"`
	Idempotent  bool `json:"idempotentHint"`
	OpenWorld   bool `json:"openWorldHint"`
}

// MCPToolsDocument is the file: the tools of the surface that POST /mcp lists, and nothing else.
type MCPToolsDocument struct {
	Version  int       `json:"version"`
	Endpoint string    `json:"endpoint"`
	Surface  string    `json:"surface"`
	Rule     string    `json:"rule"`
	Tools    []MCPTool `json:"tools"`
}

// MCPTools reads the contract of the tools of the MCP interface.
func MCPTools() (MCPToolsDocument, error) {
	data, err := files.ReadFile(MCPToolsPath)
	if err != nil {
		return MCPToolsDocument{}, kernel.Wrap(kernel.CodeInternal, err, "the MCP tool contract is missing")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc MCPToolsDocument
	if err := dec.Decode(&doc); err != nil {
		return MCPToolsDocument{}, kernel.Wrap(kernel.CodeInvalidValue, err, "the MCP tool contract is not valid")
	}
	return doc, nil
}
