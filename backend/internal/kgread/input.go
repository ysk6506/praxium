package kgread

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
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

type impactBody struct {
	commonBody
	Direction      string   `json:"direction"`
	Dead           []string `json:"dead"`
	DeadLinks      []string `json:"dead_links"`
	SiteFailure    []string `json:"site_failure"`
	DeadScopeID    string   `json:"dead_scope_id"`
	TargetObjectID string   `json:"target_object_id"`
	Mode           string   `json:"mode"`
	MaxHops        int      `json:"max_hops"`
	MaxItems       int      `json:"max_items"`
	TimeBudget     string   `json:"time_budget"`
}

// DecodeBody decodes a JSON body into dst. An empty body is the empty object. A field that dst does not know, a value
// of the wrong type and more than one JSON value are refused with the problem; a reader that fails with a
// *BodyTooLargeError is a body over the limit.
func DecodeBody(body io.Reader, dst any) *Problem {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil && dec.More() {
		err = errors.New("more than one JSON value")
	}
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	var tooBig *BodyTooLargeError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooBig):
		return &Problem{Code: "invalid_input.body_too_large", Params: map[string]any{"max_bytes": tooBig.Limit}}
	case errors.As(err, &typeErr):
		return fieldProblem(field("/"+strings.ReplaceAll(typeErr.Field, ".", "/"), "invalid_input.field_format",
			map[string]any{"expected": expectedOf(typeErr.Type)}))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		name := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return fieldProblem(field("/"+name, "invalid_input.field", map[string]any{"reason": "unknown_field"}))
	}
	return fieldProblem(field("", "invalid_input.field_format", map[string]any{"expected": "a JSON object"}))
}

// expectedOf names the type a field should have for the answer. A struct is a JSON object; the name of a Go type
// never reaches the caller.
func expectedOf(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		return "object"
	}
	return t.String()
}

// common turns the shared body fields into the input of the queries.
func (b commonBody) common(kg string) (query.Common, *Problem) {
	in := query.Common{KG: kg, KGVersionID: b.KGVersionID, ChangeSeq: b.ChangeSeq, View: b.View,
		MinSeq: b.MinSeq, Limit: b.Limit, Cursor: b.Cursor, Shape: b.Shape}
	if b.MinSeq < 0 || (b.ChangeSeq != nil && *b.ChangeSeq < 0) {
		return in, fieldProblem(field("/min_seq", "invalid_input.field_format", map[string]any{"expected": "a change number, 0 or more"}))
	}
	for name, spec := range map[string]struct {
		text string
		dst  *time.Duration
	}{"wait_timeout": {b.WaitTimeout, &in.WaitTimeout}, "max_staleness": {b.MaxStaleness, &in.MaxStaleness}} {
		if spec.text == "" {
			continue
		}
		d, err := ParseDuration(spec.text)
		if err != nil {
			return in, fieldProblem(field("/"+name, "invalid_input.field_format", map[string]any{"expected": "iso8601_duration"}))
		}
		*spec.dst = d
	}
	return in, nil
}

func identOf(b *identBody) *query.IdentifierFilter {
	if b == nil {
		return nil
	}
	return &query.IdentifierFilter{Scheme: b.Scheme, Value: b.Value}
}

// missing is the problem of a required field that is not there.
func missing(name string) *Problem {
	return &Problem{Code: "invalid_input.field_required", Fields: []FieldError{field("/"+name, "invalid_input.field_required", nil)}}
}

// AskFind reads the JSON body of a find question (kg-queries.md 3) and asks it.
func (s *Service) AskFind(ctx context.Context, req Request, body io.Reader) (Result[query.FindData], *Problem) {
	var none Result[query.FindData]
	var b findBody
	if p := s.Ready(); p != nil {
		return none, p
	}
	if p := DecodeBody(body, &b); p != nil {
		return none, p
	}
	common, p := b.common(req.KG)
	if p != nil {
		return none, p
	}
	in := query.FindInput{Common: common, ObjectType: b.ObjectType, IncludeSubtypes: b.IncludeSubtypes, Trait: b.Trait,
		Identifier: identOf(b.Identifier), Text: b.Text, ScopeID: b.ScopeID}
	for _, a := range b.Attributes {
		in.Attributes = append(in.Attributes, query.AttrFilter{Name: a.Name, Op: a.Op, Values: a.Values})
	}
	return s.Find(ctx, req, in)
}

// AskGet reads the JSON body of a get question and asks it.
func (s *Service) AskGet(ctx context.Context, req Request, body io.Reader) (Result[query.GetData], *Problem) {
	var none Result[query.GetData]
	var b getBody
	if p := s.Ready(); p != nil {
		return none, p
	}
	if p := DecodeBody(body, &b); p != nil {
		return none, p
	}
	if b.ObjectID == "" && b.Identifier == nil {
		return none, missing("object_id")
	}
	common, p := b.common(req.KG)
	if p != nil {
		return none, p
	}
	return s.Get(ctx, req, query.GetInput{Common: common, ObjectID: b.ObjectID, Identifier: identOf(b.Identifier), Include: b.Include})
}

// AskNeighbors reads the JSON body of a neighbors question and asks it.
func (s *Service) AskNeighbors(ctx context.Context, req Request, body io.Reader) (Result[query.NeighborsData], *Problem) {
	var none Result[query.NeighborsData]
	var b neighborsBody
	if p := s.Ready(); p != nil {
		return none, p
	}
	if p := DecodeBody(body, &b); p != nil {
		return none, p
	}
	if b.ObjectID == "" {
		return none, missing("object_id")
	}
	common, p := b.common(req.KG)
	if p != nil {
		return none, p
	}
	return s.Neighbors(ctx, req, query.NeighborsInput{Common: common, ObjectID: b.ObjectID, LinkTypes: b.LinkTypes,
		Meanings: b.Meanings, Direction: b.Direction, MaxSteps: b.MaxSteps, PeerTypes: b.PeerTypes})
}

// AskImpact reads the JSON body of an impact question and asks it.
func (s *Service) AskImpact(ctx context.Context, req Request, body io.Reader) (Result[query.ImpactData], *Problem) {
	var none Result[query.ImpactData]
	var b impactBody
	if p := s.Ready(); p != nil {
		return none, p
	}
	if p := DecodeBody(body, &b); p != nil {
		return none, p
	}
	common, p := b.common(req.KG)
	if p != nil {
		return none, p
	}
	in := query.ImpactInput{Common: common, Direction: b.Direction, Dead: b.Dead, DeadLinks: b.DeadLinks, SiteFailure: b.SiteFailure,
		DeadScopeID: b.DeadScopeID, TargetObjectID: b.TargetObjectID, Mode: b.Mode, MaxHops: b.MaxHops, MaxItems: b.MaxItems}
	if b.TimeBudget != "" {
		d, err := ParseDuration(b.TimeBudget)
		if err != nil {
			return none, fieldProblem(field("/time_budget", "invalid_input.field_format", map[string]any{"expected": "iso8601_duration"}))
		}
		in.TimeBudget = d
	}
	return s.Impact(ctx, req, in)
}
