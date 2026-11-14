package httpapi

import (
	"net/http"

	"github.com/ysk6506/praxium/backend/internal/contract"
)

// healthLive says the process runs.
func (c *call) healthLive() {
	c.ok(map[string]any{"status": "live"}, nil)
}

// healthReady says the server takes work; it turns 503 while a shutdown drains.
func (c *call) healthReady() {
	if c.s.draining.Load() {
		c.fail("server.unavailable", map[string]any{"reason": "draining"})
		return
	}
	c.ok(map[string]any{"status": "ready", "model_hash": c.s.lists.generation}, nil)
}

// healthStarted says start-up is finished.
func (c *call) healthStarted() {
	if !c.s.started.Load() {
		c.fail("server.unavailable", map[string]any{"reason": "starting"})
		return
	}
	c.ok(map[string]any{"status": "started", "version": c.s.version}, nil)
}

// contractDocument serves the OpenAPI document as it is, not in an envelope:
// tools read it as OpenAPI. The ETag is the hash of the bytes.
func (c *call) contractDocument() {
	data := contract.OpenAPI()
	if c.notModified(digestETag(data)) {
		return
	}
	c.w.Header().Set("Content-Type", "application/json")
	c.w.WriteHeader(http.StatusOK)
	_, _ = c.w.Write(data)
}

// problemDetail explains one error code: the address a problem's type points at.
func (c *call) problemDetail() {
	entry, ok := contract.LookupProblem(c.r.PathValue("code"))
	if !ok {
		c.notFound("problem", c.r.PathValue("code"))
		return
	}
	c.ok(entry, nil)
}
