package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/query"
)

// commonBody is the input every question shares (kg-queries.md 3), in the
// body of a POST. Durations are ISO 8601 texts, as in the query of a GET.
type commonBody struct {
	KGVersionID  string `json:"kg_version_id"`
	ChangeSeq    *int64 `json:"change_seq"`
	View         string `json:"view"`
	MinSeq       int64  `json:"min_seq"`
	WaitTimeout  string `json:"wait_timeout"`
	MaxStaleness string `json:"max_staleness"`
	Shape        string `json:"shape"`
	Limit        int    `json:"limit"`
	Cursor       string `json:"cursor"`
}

type attrBody struct {
	Name   string   `json:"name"`
	Op     string   `json:"op"`
	Values []string `json:"values"`
}

type identBody struct {
	Scheme string `json:"scheme"`
	Value  string `json:"value"`
}

type findBody struct {
	commonBody
	ObjectType      string     `json:"object_type"`
	IncludeSubtypes *bool      `json:"include_subtypes"`
	Trait           string     `json:"trait"`
	Attributes      []attrBody `json:"attributes"`
	Identifier      *identBody `json:"identifier"`
	Text            string     `json:"text"`
	ScopeID         string     `json:"scope_id"`
}

type getBody struct {
	commonBody
	ObjectID   string     `json:"object_id"`
	Identifier *identBody `json:"identifier"`
	Include    []string   `json:"include"`
}

type neighborsBody struct {
	commonBody
	ObjectID  string   `json:"object_id"`
	LinkTypes []string `json:"link_types"`
	Meanings  []string `json:"meanings"`
	Direction string   `json:"direction"`
	MaxSteps  int      `json:"max_steps"`
	PeerTypes []string `json:"peer_types"`
}

// field is the problem entry of one wrong field of a body.
func field(pointer, code string, params map[string]any) fieldError {
	return fieldError{Pointer: pointer, Code: code, Params: params}
}

// readBody decodes the JSON body into dst. An empty body is the empty object.
// It answers the problem itself and returns false when the body is not usable.
func (c *call) readBody(dst any) bool {
	dec := json.NewDecoder(c.r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil && dec.More() {
		err = errors.New("more than one JSON value")
	}
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	var tooBig *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooBig):
		c.fail("invalid_input.body_too_large", map[string]any{"max_bytes": c.s.maxBody})
	case errors.As(err, &typeErr):
		c.fail("invalid_input.field", nil, field("/"+strings.ReplaceAll(typeErr.Field, ".", "/"), "invalid_input.field_format",
			map[string]any{"expected": typeErr.Type.String()}))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		name := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		c.fail("invalid_input.field", nil, field("/"+name, "invalid_input.field", map[string]any{"reason": "unknown_field"}))
	default:
		c.fail("invalid_input.field", nil, field("", "invalid_input.field_format", map[string]any{"expected": "a JSON object"}))
	}
	return false
}

// common turns the shared body fields into the input of the queries.
func (b commonBody) common(c *call) (query.Common, bool) {
	in := query.Common{KG: c.r.PathValue("kg"), KGVersionID: b.KGVersionID, ChangeSeq: b.ChangeSeq, View: b.View,
		MinSeq: b.MinSeq, Limit: b.Limit, Cursor: b.Cursor, Shape: b.Shape}
	if b.MinSeq < 0 || (b.ChangeSeq != nil && *b.ChangeSeq < 0) {
		c.fail("invalid_input.field", nil, field("/min_seq", "invalid_input.field_format", map[string]any{"expected": "a change number, 0 or more"}))
		return in, false
	}
	for name, spec := range map[string]struct {
		text string
		dst  *time.Duration
	}{"wait_timeout": {b.WaitTimeout, &in.WaitTimeout}, "max_staleness": {b.MaxStaleness, &in.MaxStaleness}} {
		if spec.text == "" {
			continue
		}
		d, err := parseDuration(spec.text)
		if err != nil {
			c.fail("invalid_input.field", nil, field("/"+name, "invalid_input.field_format", map[string]any{"expected": "iso8601_duration"}))
			return in, false
		}
		*spec.dst = d
	}
	return in, true
}

func identOf(b *identBody) *query.IdentifierFilter {
	if b == nil {
		return nil
	}
	return &query.IdentifierFilter{Scheme: b.Scheme, Value: b.Value}
}

// missing answers a required field that is not there.
func (c *call) missing(name string) {
	c.fail("invalid_input.field_required", nil, field("/"+name, "invalid_input.field_required", nil))
}

// askFind serves POST /v1/kgs/{kg}/queries:find.
func (c *call) askFind() {
	var b findBody
	if !c.graphReady() || !c.readBody(&b) {
		return
	}
	common, ok := b.common(c)
	if !ok {
		return
	}
	in := query.FindInput{Common: common, ObjectType: b.ObjectType, IncludeSubtypes: b.IncludeSubtypes, Trait: b.Trait,
		Identifier: identOf(b.Identifier), Text: b.Text, ScopeID: b.ScopeID}
	for _, a := range b.Attributes {
		in.Attributes = append(in.Attributes, query.AttrFilter{Name: a.Name, Op: a.Op, Values: a.Values})
	}
	c.runFind(in)
}

// askGet serves POST /v1/kgs/{kg}/queries:get.
func (c *call) askGet() {
	var b getBody
	if !c.graphReady() || !c.readBody(&b) {
		return
	}
	if b.ObjectID == "" && b.Identifier == nil {
		c.missing("object_id")
		return
	}
	common, ok := b.common(c)
	if !ok {
		return
	}
	c.runGet(query.GetInput{Common: common, ObjectID: b.ObjectID, Identifier: identOf(b.Identifier), Include: b.Include})
}

// askNeighbors serves POST /v1/kgs/{kg}/queries:neighbors.
func (c *call) askNeighbors() {
	var b neighborsBody
	if !c.graphReady() || !c.readBody(&b) {
		return
	}
	if b.ObjectID == "" {
		c.missing("object_id")
		return
	}
	common, ok := b.common(c)
	if !ok {
		return
	}
	c.runNeighbors(query.NeighborsInput{Common: common, ObjectID: b.ObjectID, LinkTypes: b.LinkTypes, Meanings: b.Meanings,
		Direction: b.Direction, MaxSteps: b.MaxSteps, PeerTypes: b.PeerTypes})
}
