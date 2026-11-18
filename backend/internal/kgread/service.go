package kgread

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// KGSource is the part of the storage port the KG resource reads.
type KGSource interface {
	ListKGs(ctx context.Context) ([]graph.KG, error)
	GetKG(ctx context.Context, ref string) (graph.KG, error)
	Head(ctx context.Context, kg graph.KGID) (graph.Head, error)
}

// Service answers the questions over a KG. It is safe for concurrent use.
type Service struct {
	// Registry is the type registry the read filter is built on.
	Registry *model.Registry
	// Queries and KGs read the KGs; either nil means the server has no store (see Ready).
	Queries *query.Engine
	KGs     KGSource
	// Log receives the failures of a read that no code of the catalogue names.
	Log *slog.Logger
}

// Request is who asks and what they name: the subject and the id of the call (for the log), the KG as the caller wrote
// it (an id, a name or the alias main), and, for the typed forms, the object the call names outside its input (the id
// in the path of a GET), which an answer of "no such object" repeats.
type Request struct {
	Subject access.Subject
	Trace   string
	KG      string
	Object  string
}

// Result is an answer that was read: the data, and the meta in the shape of the HTTP answer (rest-api.md 3-2) without
// the trace id, which the adapter adds.
type Result[D any] struct {
	Data D
	Meta map[string]any
}

// Ready says whether there is a store to read; without one every read is server.unavailable.
func (s *Service) Ready() *Problem {
	if s.Queries == nil || s.KGs == nil {
		return &Problem{Code: "server.unavailable", Params: map[string]any{"reason": "no_store"}}
	}
	return nil
}

// ReadFilter maps the caller to the read filter of the queries. Until grants are stored, every caller who may read
// holds the default grant of access-control.md 5-3: every type, sensitivity `open`, the whole scope. The role check
// itself is the route's (RoleRead).
func (s *Service) ReadFilter(_ access.Subject) query.ReadFilter {
	return query.NewGrants(s.Registry, true, query.Grant{Max: model.SensitivityOpen})
}

// Find asks find with the typed input.
func (s *Service) Find(ctx context.Context, req Request, in query.FindInput) (Result[query.FindData], *Problem) {
	ans, err := s.Queries.Find(ctx, in, s.ReadFilter(req.Subject))
	if err != nil {
		return Result[query.FindData]{}, s.Refuse(req, err)
	}
	return Result[query.FindData]{Data: ans.Data, Meta: MetaOf(ans.Meta)}, nil
}

// Get asks get with the typed input.
func (s *Service) Get(ctx context.Context, req Request, in query.GetInput) (Result[query.GetData], *Problem) {
	if req.Object == "" {
		req.Object = in.ObjectID
		if in.Identifier != nil {
			req.Object = in.Identifier.Scheme + ":" + in.Identifier.Value
		}
	}
	ans, err := s.Queries.Get(ctx, in, s.ReadFilter(req.Subject))
	if err != nil {
		return Result[query.GetData]{}, s.Refuse(req, err)
	}
	return Result[query.GetData]{Data: ans.Data, Meta: MetaOf(ans.Meta)}, nil
}

// Neighbors asks neighbors with the typed input.
func (s *Service) Neighbors(ctx context.Context, req Request, in query.NeighborsInput) (Result[query.NeighborsData], *Problem) {
	ans, err := s.Queries.Neighbors(ctx, in, s.ReadFilter(req.Subject))
	if err != nil {
		return Result[query.NeighborsData]{}, s.Refuse(req, err)
	}
	return Result[query.NeighborsData]{Data: ans.Data, Meta: MetaOf(ans.Meta)}, nil
}

// Refuse maps an error of the storage or of the queries to the problem of the catalogue. An error that names no code
// is logged with the trace id and becomes server.internal.
func (s *Service) Refuse(req Request, err error) *Problem {
	code := kernel.CodeOf(err)
	if p, ok := queryProblems[code]; ok {
		return &Problem{Code: p.code, Params: p.params(err)}
	}
	switch code {
	case query.CodeObjectNotFound:
		return NotFound("object", req.Object)
	case graph.CodeKGNotFound:
		return NotFound("kg", req.KG)
	case graph.CodeGenerationChanged:
		return &Problem{Code: "conflict.generation_changed"}
	case graph.CodeWaitTimeout, graph.CodeNotReadable:
		return &Problem{Code: "stale.min_seq_not_reached", Params: detail(err)}
	case kernel.CodeInvalidValue:
		return &Problem{Code: "invalid_input.field", Params: fieldParams(err)}
	case kernel.CodeUnsupported:
		return &Problem{Code: "server.unavailable", Params: detail(err)}
	}
	s.Log.Error("a read of the KG failed", "error", err, "trace_id", req.Trace)
	return &Problem{Code: "server.internal"}
}

// NotFound is the answer for a missing or invisible resource (both look alike). id is what the request asked for; it
// is left out when the request named nothing.
func NotFound(resource, id string) *Problem {
	params := map[string]any{"resource": resource}
	if id != "" {
		params["id"] = id
	}
	return &Problem{Code: "not_found.resource", Params: params}
}

func detail(err error) map[string]any { return map[string]any{"detail": err.Error()} }

// fieldParams are the params of invalid_input.field: detail is the error with its code, message the plain sentence
// and field, when the error knows it, the field of the request it is about (the notes of the error shape in
// rest-api.md; the command line and the web UI show them).
func fieldParams(err error) map[string]any {
	params := detail(err)
	params["message"] = PlainMessage(err)
	if field := query.FieldOf(err); field != "" {
		params["field"] = field
	}
	return params
}

// PlainMessage is the text of an error without its code.
func PlainMessage(err error) string {
	var ke *kernel.Error
	if !errors.As(err, &ke) {
		return err.Error()
	}
	if ke.Cause != nil {
		return ke.Message + ": " + ke.Cause.Error()
	}
	return ke.Message
}

// queryProblem is the code of the catalogue a refusal of the queries becomes, and its parameters.
type queryProblem struct {
	code   string
	params func(error) map[string]any
}

var queryProblems = map[kernel.Code]queryProblem{
	query.CodeHiddenFilter:   {"permission.hidden_attribute", detail},
	query.CodeVolatileFilter: {"invalid_input.filter_field_unknown", detail},
	query.CodeFilterField:    {"invalid_input.filter_field_unknown", detail},
	query.CodeFilterOp:       {"invalid_input.filter_op_unknown", detail},
	query.CodeIncludeUnknown: {"invalid_input.include_unknown", detail},
	query.CodeCursorInvalid:  {"invalid_input.cursor_invalid", func(error) map[string]any { return nil }},
	query.CodeLimitInvalid:   {"invalid_input.limit_invalid", func(error) map[string]any { return map[string]any{"min": 1, "max": query.MaxLimit} }},
	query.CodeScopeUnsupported: {"invalid_input.param_invalid", func(err error) map[string]any {
		return map[string]any{"param": "scope_id", "detail": err.Error()}
	}},
}

// MetaOf turns an envelope of the queries into the meta of an answer (rest-api.md 3-2): the KG id is `kg`,
// volatile_as_of lies in as_of, and hidden says whether anything was hidden.
func MetaOf(env query.Envelope) map[string]any {
	raw, _ := json.Marshal(env)
	var meta map[string]any
	_ = json.Unmarshal(raw, &meta)
	asOf, _ := meta["as_of"].(map[string]any)
	asOf["kg"] = asOf["kg_id"]
	delete(asOf, "kg_id")
	asOf["volatile_as_of"] = meta["volatile_as_of"]
	delete(meta, "volatile_as_of")
	hidden, _ := meta["hidden"].(map[string]any)
	hidden["has_hidden"] = env.Hidden.Passed || env.Hidden.Reached
	return meta
}

// OptInt reads an optional change number: empty is nil, a negative or non-number is an error.
func OptInt(text string) (*int64, error) {
	if text == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "not a change number")
	}
	return &n, nil
}

// ParseDuration reads the ISO 8601 durations a client needs: P[nD][T[nH][nM][nS]].
func ParseDuration(text string) (time.Duration, error) {
	rest, ok := strings.CutPrefix(text, "P")
	if !ok || rest == "" {
		return 0, kernel.Errorf(kernel.CodeInvalidValue, "not a duration")
	}
	var total time.Duration
	inTime := false
	num := ""
	for _, r := range rest {
		switch {
		case r == 'T':
			inTime = true
		case r >= '0' && r <= '9' || r == '.':
			num += string(r)
		default:
			n, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, kernel.Errorf(kernel.CodeInvalidValue, "not a duration")
			}
			unit, ok := durationUnit(r, inTime)
			if !ok {
				return 0, kernel.Errorf(kernel.CodeInvalidValue, "not a duration")
			}
			total += time.Duration(n * float64(unit))
			num = ""
		}
	}
	if num != "" {
		return 0, kernel.Errorf(kernel.CodeInvalidValue, "not a duration")
	}
	return total, nil
}

func durationUnit(r rune, inTime bool) (time.Duration, bool) {
	switch {
	case r == 'D' && !inTime:
		return 24 * time.Hour, true
	case r == 'H' && inTime:
		return time.Hour, true
	case r == 'M' && inTime:
		return time.Minute, true
	case r == 'S' && inTime:
		return time.Second, true
	}
	return 0, false
}
