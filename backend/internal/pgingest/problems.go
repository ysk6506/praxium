package pgingest

import (
	"encoding/json"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// problemJSON is a problem as the jsonb of a staged row keeps it. The record a problem is about is the row it is on, so the key
// is not in it; the line and the place are of a problem of the page, which has no record to say where it stood.
type problemJSON struct {
	Kind   string `json:"kind"`
	Feed   string `json:"feed,omitempty"`
	Field  string `json:"field,omitempty"`
	Reason string `json:"reason"`
	Line   int    `json:"line,omitempty"`
	Where  string `json:"where,omitempty"`
}

var validKinds = func() map[string]bool {
	out := map[string]bool{}
	for _, k := range pgschema.ProblemKinds() {
		out[k] = true
	}
	return out
}()

// groupProblems is the problems of a page as the text of a jsonb array for each record that has any, by the number of the
// record, and for the problems that are about no record of the page under pageRow. A kind that is not in the closed list is
// refused here, so that what a run staged can be put in record_problem later.
func groupProblems(list []ingest.StageProblem) (map[int]any, error) {
	if len(list) == 0 {
		return nil, nil
	}
	grouped := map[int][]problemJSON{}
	for _, p := range list {
		if !validKinds[p.Kind] {
			return nil, kernel.Errorf(kernel.CodeInternal, "the problem kind %q is not in the closed list of kinds", p.Kind)
		}
		at, one := p.No, problemJSON{Kind: p.Kind, Feed: p.Feed, Field: p.Field, Reason: p.Reason}
		if p.No < 0 {
			at, one.Line, one.Where = pageRow, p.Line, p.Where
		}
		grouped[at] = append(grouped[at], one)
	}
	out := make(map[int]any, len(grouped))
	for no, ps := range grouped {
		text, err := json.Marshal(ps)
		if err != nil {
			return nil, kernel.Wrap(kernel.CodeInternal, err, "write the problems of a page")
		}
		out[no] = string(text)
	}
	return out, nil
}
