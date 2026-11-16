package mcpapi

import (
	"encoding/json"
	"strings"
)

// The codes of JSON-RPC errors the server uses: the standard ones, and the three of the protocol that the 2026-07-28
// revision allots (HeaderMismatch, the missing capability that the server never asks for, and UnsupportedProtocolVersion).
const (
	codeParse           = -32700
	codeInvalidRequest  = -32600
	codeMethodNotFound  = -32601
	codeInvalidParams   = -32602
	codeInternal        = -32603
	codeHeaderMismatch  = -32020
	codeUnsupportedVers = -32022
)

// rpcError is the error member of a response.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// rpcRequest is a request or a notification read from a body. hasID is false for a notification.
type rpcRequest struct {
	id     json.RawMessage
	hasID  bool
	method string
	// params is the params member when it is an object, else nil.
	params map[string]any
}

// The members of a message, which a key must match letter for letter.
var rpcMembers = []string{"jsonrpc", "id", "method", "params"}

// readRequest turns the parsed body into a request, or says why it is not one. A key that is a known member in other
// letters ("Method") is refused, not read as the member: two readers that disagree on it are what the attack needs.
func readRequest(tree any) (*rpcRequest, *rpcError) {
	obj, ok := tree.(map[string]any)
	if !ok {
		return nil, &rpcError{Code: codeInvalidRequest, Message: "The body must be one JSON-RPC message, not a batch or a bare value."}
	}
	if e := checkMembers(obj); e != nil {
		return nil, e
	}
	method, _ := obj["method"].(string)
	req := &rpcRequest{method: method}
	if raw, present := obj["id"]; present {
		id, err := idOf(raw)
		if err != nil {
			return nil, err
		}
		req.id, req.hasID = id, true
	}
	if raw, present := obj["params"]; present {
		params, ok := raw.(map[string]any)
		if !ok {
			return nil, &rpcError{Code: codeInvalidParams, Message: `The member "params" must be an object.`}
		}
		req.params = params
	}
	return req, nil
}

// checkMembers refuses a member name in the wrong letter case, and a message without "jsonrpc": "2.0" or a method.
func checkMembers(obj map[string]any) *rpcError {
	for key := range obj {
		for _, member := range rpcMembers {
			if key != member && strings.EqualFold(key, member) {
				return &rpcError{Code: codeInvalidRequest, Message: "A member name has the wrong letter case; names are matched exactly."}
			}
		}
	}
	if v, ok := obj["jsonrpc"].(string); !ok || v != "2.0" {
		return &rpcError{Code: codeInvalidRequest, Message: `The member "jsonrpc" must be "2.0".`}
	}
	if method, ok := obj["method"].(string); !ok || method == "" {
		return &rpcError{Code: codeInvalidRequest, Message: `The member "method" must be a non-empty string.`}
	}
	return nil
}

// idOf checks an id (a string or a number; null is not one) and gives it back as it is to be echoed.
func idOf(raw any) (json.RawMessage, *rpcError) {
	switch v := raw.(type) {
	case string:
		b, _ := json.Marshal(v)
		return b, nil
	case json.Number:
		return json.RawMessage(v), nil
	}
	return nil, &rpcError{Code: codeInvalidRequest, Message: `The member "id" must be a string or a number.`}
}

type okMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
}

type errMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Error   *rpcError       `json:"error"`
}

// objectAt gives the object under the keys of a tree, and false when a step is missing or not an object.
func objectAt(m map[string]any, keys ...string) (map[string]any, bool) {
	for _, k := range keys {
		next, ok := m[k].(map[string]any)
		if !ok {
			return nil, false
		}
		m = next
	}
	return m, true
}
