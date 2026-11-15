package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kgread"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// KGSource is the part of the storage port the KG resource reads.
type KGSource = kgread.KGSource

// request is who asks and what they name, for the use case: the subject, the trace id, the KG and the object of the
// path.
func (c *call) request() kgread.Request {
	return kgread.Request{Subject: c.subject, Trace: c.trace, KG: c.r.PathValue("kg"), Object: c.r.PathValue("object_id")}
}

// failErr maps an error of the storage or of the queries to a code of the catalogue.
func (c *call) failErr(err error) { c.failProblem(c.s.read.Refuse(c.request(), err)) }

// graphReady says whether the server has a store; without one the KG routes answer 503.
func (c *call) graphReady() bool {
	if p := c.s.read.Ready(); p != nil {
		c.failProblem(p)
		return false
	}
	return true
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
	if in.ChangeSeq, err = kgread.OptInt(lq.others["change_seq"]); err != nil {
		c.fail("invalid_input.param_invalid", map[string]any{"param": "change_seq"})
		return in, false
	}
	if seq, err := kgread.OptInt(lq.others["min_seq"]); err != nil {
		c.fail("invalid_input.param_invalid", map[string]any{"param": "min_seq"})
		return in, false
	} else if seq != nil {
		in.MinSeq = *seq
	}
	for param, dst := range map[string]*time.Duration{"wait_timeout": &in.WaitTimeout, "max_staleness": &in.MaxStaleness} {
		if text := lq.others[param]; text != "" {
			d, err := kgread.ParseDuration(text)
			if err != nil {
				c.fail("invalid_input.param_invalid", map[string]any{"param": param, "expected": "iso8601_duration"})
				return in, false
			}
			*dst = d
		}
	}
	return in, true
}
