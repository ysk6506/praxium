package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// readFilter maps the caller to the read filter of the queries. Until grants
// are stored, every caller who may read holds the default grant of
// access-control.md 5-3: every type, sensitivity `open`, the whole scope. The
// role check itself is the route's (RoleRead).
func (c *call) readFilter() query.ReadFilter {
	return query.NewGrants(c.s.reg, true, query.Grant{Max: model.SensitivityOpen})
}

// fail maps an error of the storage or of the queries to a code of the catalogue.
func (c *call) failErr(err error) {
	switch kernel.CodeOf(err) {
	case query.CodeHiddenFilter:
		c.fail("permission.hidden_attribute", map[string]any{"detail": err.Error()})
	case query.CodeVolatileFilter, query.CodeFilterField:
		c.fail("invalid_input.filter_field_unknown", map[string]any{"detail": err.Error()})
	case query.CodeFilterOp:
		c.fail("invalid_input.filter_op_unknown", map[string]any{"detail": err.Error()})
	case query.CodeScopeUnsupported:
		c.fail("invalid_input.param_invalid", map[string]any{"param": "scope_id", "detail": err.Error()})
	case query.CodeCursorInvalid:
		c.fail("invalid_input.cursor_invalid", nil)
	case query.CodeLimitInvalid:
		c.fail("invalid_input.limit_invalid", map[string]any{"min": 1, "max": query.MaxLimit})
	case query.CodeIncludeUnknown:
		c.fail("invalid_input.include_unknown", map[string]any{"detail": err.Error()})
	case query.CodeObjectNotFound:
		c.notFound("object")
	case graph.CodeKGNotFound:
		c.notFound("kg")
	case graph.CodeGenerationChanged:
		c.fail("conflict.generation_changed", nil)
	case graph.CodeWaitTimeout, graph.CodeNotReadable:
		c.fail("stale.min_seq_not_reached", map[string]any{"detail": err.Error()})
	case kernel.CodeInvalidValue:
		c.fail("invalid_input.field", map[string]any{"detail": err.Error()})
	case kernel.CodeUnsupported:
		c.fail("server.unavailable", map[string]any{"detail": err.Error()})
	default:
		c.s.log.Error("a read of the KG failed", "error", err, "trace_id", c.trace)
		c.fail("server.internal", nil)
	}
}

// graphReady says whether the server has a store; without one the KG routes answer 503.
func (c *call) graphReady() bool {
	if c.s.queries == nil || c.s.kgs == nil {
		c.fail("server.unavailable", map[string]any{"reason": "no_store"})
		return false
	}
	return true
}

// metaOf turns an envelope of the queries into the meta of the HTTP answer
// (rest-api.md 3-2): the KG id is `kg`, volatile_as_of lies in as_of, and
// hidden says whether anything was hidden.
func metaOf(env query.Envelope) map[string]any {
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

// kgOK writes an answer read from a KG. For a GET the ETag is the digest of
// data and meta, so a conditional read gets 304 exactly when nothing it shows
// changed; the answer to a question sent by POST carries none.
func (c *call) kgOK(data any, meta map[string]any) {
	if c.r.Method != http.MethodGet {
		c.ok(data, meta)
		return
	}
	raw, _ := json.Marshal(map[string]any{"data": data, "meta": meta})
	if c.notModified(digestETag(raw)) {
		return
	}
	c.ok(data, meta)
}

var kgParams = []string{"kg_version_id", "change_seq", "view", "min_seq", "wait_timeout", "max_staleness", "shape"}

// commonOf reads the inputs every KG read shares from a parsed query.
func commonOf(c *call, lq *listQuery) (query.Common, bool) {
	in := query.Common{KG: c.r.PathValue("kg"), Limit: lq.limit, Cursor: lq.cursor, Shape: query.ShapeItems,
		KGVersionID: lq.others["kg_version_id"], View: lq.others["view"]}
	if v := lq.others["shape"]; v != "" {
		in.Shape = v
	}
	var err error
	if in.ChangeSeq, err = optInt(lq.others["change_seq"]); err != nil {
		c.fail("invalid_input.param_invalid", map[string]any{"param": "change_seq"})
		return in, false
	}
	if seq, err := optInt(lq.others["min_seq"]); err != nil {
		c.fail("invalid_input.param_invalid", map[string]any{"param": "min_seq"})
		return in, false
	} else if seq != nil {
		in.MinSeq = *seq
	}
	for param, dst := range map[string]*time.Duration{"wait_timeout": &in.WaitTimeout, "max_staleness": &in.MaxStaleness} {
		if text := lq.others[param]; text != "" {
			d, err := parseDuration(text)
			if err != nil {
				c.fail("invalid_input.param_invalid", map[string]any{"param": param, "expected": "iso8601_duration"})
				return in, false
			}
			*dst = d
		}
	}
	return in, true
}

func optInt(text string) (*int64, error) {
	if text == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "not a change number")
	}
	return &n, nil
}

// parseDuration reads the ISO 8601 durations a client needs: P[nD][T[nH][nM][nS]].
func parseDuration(text string) (time.Duration, error) {
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
