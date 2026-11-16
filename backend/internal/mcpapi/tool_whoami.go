package mcpapi

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// Info is what the server tells of itself: the name and the version of the program, and the version and the content
// hash of the model it serves. None of it is a secret and none of it comes from the KG.
type Info struct {
	Name, Version, ModelVersion, ModelHash string
}

// ToolDeps is what the tools of the registry need from outside.
type ToolDeps struct {
	Info Info
	// Directory is where the expiry of a token is read; nil means the server has none (the development authenticator),
	// and the expiry is left empty.
	Directory access.Directory
	// Counts gives the numbers of requests by protocol version since the start; nil means none.
	Counts func() map[string]int64
}

// whoamiDescription is a fixed text: no value of the KG is in it (ADR-0062 7).
const whoamiDescription = "Show the caller's account, the roles of the token, token expiry, limits, and the server and model versions."

const whoamiInput = `{"type":"object","properties":{},"additionalProperties":false}`

const whoamiOutput = `{"type":"object","properties":{` +
	`"meta":{"type":"object","properties":{"result_complete":{"type":"boolean"},"truncated":{"type":"boolean"}},"required":["result_complete","truncated"],"additionalProperties":false},` +
	`"next":{"type":"string"},` +
	`"data":{"type":"object","properties":{` +
	`"account":{"type":"object","properties":{"id":{"type":"string"},"kind":{"type":"string"},"display_name":{"type":"string"}},"required":["id","kind","display_name"],"additionalProperties":false},` +
	`"roles":{"type":"array","items":{"type":"string"}},` +
	`"token":{"type":"object","properties":{"id":{"type":["string","null"]},"expires_at":{"type":["string","null"]}},"required":["id","expires_at"],"additionalProperties":false},` +
	`"limits":{"type":"object","properties":{"enforced":{"type":"boolean"},"note":{"type":"string"}},"required":["enforced","note"],"additionalProperties":false},` +
	`"server":{"type":"object","properties":{"name":{"type":"string"},"version":{"type":"string"},"protocol_versions":{"type":"array","items":{"type":"string"}},"read_only":{"type":"boolean"},"requests_by_version":{"type":"object","additionalProperties":{"type":"integer"}}},"required":["name","version","protocol_versions","read_only","requests_by_version"],"additionalProperties":false},` +
	`"model":{"type":"object","properties":{"version":{"type":"string"},"hash":{"type":"string"}},"required":["version","hash"],"additionalProperties":false}` +
	`},"required":["account","roles","token","limits","server","model"],"additionalProperties":false}` +
	`},"required":["meta","next","data"],"additionalProperties":false}`

// The answer of kg_whoami, in the order of every result: meta, next, data (kg-mcp.md 4.1). There are no limits in this
// version of the server, and the answer says so as a fact: enforced false, with a note, and no numbers that would
// look like a limit.
type whoamiAnswer struct {
	Meta whoamiMeta `json:"meta"`
	Next string     `json:"next"`
	Data whoamiData `json:"data"`
}

type whoamiMeta struct {
	ResultComplete bool `json:"result_complete"`
	Truncated      bool `json:"truncated"`
}

type whoamiData struct {
	Account whoamiAccount `json:"account"`
	Roles   []string      `json:"roles"`
	Token   whoamiToken   `json:"token"`
	Limits  whoamiLimits  `json:"limits"`
	Server  whoamiServer  `json:"server"`
	Model   whoamiModel   `json:"model"`
}

type whoamiAccount struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
}

type whoamiToken struct {
	ID        *string `json:"id"`
	ExpiresAt *string `json:"expires_at"`
}

type whoamiLimits struct {
	Enforced bool   `json:"enforced"`
	Note     string `json:"note"`
}

type whoamiServer struct {
	Name              string           `json:"name"`
	Version           string           `json:"version"`
	ProtocolVersions  []string         `json:"protocol_versions"`
	ReadOnly          bool             `json:"read_only"`
	RequestsByVersion map[string]int64 `json:"requests_by_version"`
}

type whoamiModel struct {
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

const (
	whoamiNext       = "This is the whole answer. This version of the server has no tool that reads the KG yet."
	whoamiLimitsNote = "No limit on requests or on reading is enforced for this account in this version of the server."
)

// WhoamiTool is kg_whoami (REQ-518): who the caller is and what the server is. It answers about the token only its id
// and its expiry, never the token itself.
func WhoamiTool(deps ToolDeps) Tool {
	return Tool{
		Name: "kg_whoami", Description: whoamiDescription, Kind: KindKGRead,
		Input: json.RawMessage(whoamiInput), Output: json.RawMessage(whoamiOutput),
		Run: func(ctx context.Context, c Call) Result { return runWhoami(ctx, deps, c) },
	}
}

func runWhoami(ctx context.Context, deps ToolDeps, c Call) Result {
	if p := noArguments(c.Args); p != nil {
		return Result{Problem: p}
	}
	s := c.Subject
	roles := make([]string, len(s.Roles))
	for i, r := range s.Roles {
		roles[i] = string(r)
	}
	counts := map[string]int64{}
	if deps.Counts != nil {
		counts = deps.Counts()
	}
	return Result{Structured: whoamiAnswer{
		Meta: whoamiMeta{ResultComplete: true},
		Next: whoamiNext,
		Data: whoamiData{
			Account: whoamiAccount{ID: cleanShort(s.ID), Kind: string(s.Kind), DisplayName: cleanName(s.DisplayName)},
			Roles:   roles,
			Token:   tokenOf(ctx, deps.Directory, s),
			Limits:  whoamiLimits{Enforced: false, Note: whoamiLimitsNote},
			Server: whoamiServer{Name: deps.Info.Name, Version: deps.Info.Version, ProtocolVersions: supportedVersions,
				ReadOnly: true, RequestsByVersion: counts},
			Model: whoamiModel{Version: deps.Info.ModelVersion, Hash: deps.Info.ModelHash},
		},
	}}
}

func cleanName(s string) string {
	out, _ := kgread.CleanText(s, clientText)
	return out
}

// tokenOf is the id and the expiry of the token the caller presented. A token of the development authenticator has no
// id in the directory and no expiry: both stay empty.
func tokenOf(ctx context.Context, dir access.Directory, s access.Subject) whoamiToken {
	if s.TokenID == "" {
		return whoamiToken{}
	}
	id := s.TokenID
	out := whoamiToken{ID: &id}
	if dir == nil {
		return out
	}
	info, err := dir.GetToken(ctx, s.TokenID)
	if err != nil || info.ExpiresAt.IsZero() {
		return out
	}
	at := info.ExpiresAt.UTC().Format(time.RFC3339)
	out.ExpiresAt = &at
	return out
}

// noArguments refuses any argument to a tool that takes none; the key is named (cleaned) so the caller sees which.
func noArguments(args json.RawMessage) *kgread.Problem {
	if len(args) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil || len(obj) == 0 {
		return nil
	}
	var fields []kgread.FieldError
	for key := range obj {
		fields = append(fields, kgread.FieldError{Pointer: "/" + cleanShort(key), Code: "invalid_input.field", Params: map[string]any{"reason": "unknown_field"}})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Pointer < fields[j].Pointer })
	return &kgread.Problem{Code: "invalid_input.field", Fields: fields}
}

// DefaultRegistry is the registry of the tools of this version of the server.
func DefaultRegistry(deps ToolDeps) *Registry {
	return NewRegistry(WhoamiTool(deps))
}
