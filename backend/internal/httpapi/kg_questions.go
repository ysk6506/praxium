package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// bodyOf is the body of the request as the use case reads it: a read that fails because the body is over the limit
// of the server fails with the error the use case knows.
type bodyOf struct{ r io.Reader }

func (b bodyOf) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		err = &kgread.BodyTooLargeError{Limit: tooBig.Limit}
	}
	return n, err
}

func (c *call) body() io.Reader { return bodyOf{c.r.Body} }

// readBody decodes the JSON body into dst. An empty body is the empty object.
// It answers the problem itself and returns false when the body is not usable.
func (c *call) readBody(dst any) bool {
	if p := kgread.DecodeBody(c.body(), dst); p != nil {
		c.failProblem(p)
		return false
	}
	return true
}

// askFind serves POST /v1/kgs/{kg}/queries:find.
func (c *call) askFind() {
	ans, p := c.s.read.AskFind(c.r.Context(), c.request(), c.body())
	c.answer(ans.Data, ans.Meta, p)
}

// askGet serves POST /v1/kgs/{kg}/queries:get.
func (c *call) askGet() {
	ans, p := c.s.read.AskGet(c.r.Context(), c.request(), c.body())
	c.answer(ans.Data, ans.Meta, p)
}

// askNeighbors serves POST /v1/kgs/{kg}/queries:neighbors.
func (c *call) askNeighbors() {
	ans, p := c.s.read.AskNeighbors(c.r.Context(), c.request(), c.body())
	c.answer(ans.Data, ans.Meta, p)
}

// askImpact serves POST /v1/kgs/{kg}/queries:impact.
func (c *call) askImpact() {
	ans, p := c.s.read.AskImpact(c.r.Context(), c.request(), c.body())
	c.answer(ans.Data, ans.Meta, p)
}

// answer writes the result of a question read from a KG, or its problem.
func (c *call) answer(data any, meta map[string]any, p *kgread.Problem) {
	if p != nil {
		c.failProblem(p)
		return
	}
	c.kgOK(data, meta)
}
