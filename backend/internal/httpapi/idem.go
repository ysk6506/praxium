package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The Idempotency-Key of the creating and acting calls of the management API (rest-api.md 3-6, ADR-0044). The
// server keeps the answers of one process for a short time: the same key with the same body replays the first
// answer, the same key with another body is a conflict. An answer that holds a secret (a new token) is never kept:
// the key of such a call cannot be used again, so the secret is shown once and in one response only.
const (
	idemTTL     = 10 * time.Minute
	idemMaxKeys = 10000
	idemMinKey  = 8
	idemMaxKey  = 128
)

type idemEntry struct {
	bodyHash string
	done     bool
	secret   bool
	status   int
	body     []byte
	expires  time.Time
}

type idemCache struct {
	mu      sync.Mutex
	clock   kernel.Clock
	entries map[string]*idemEntry
}

func newIdemCache(clock kernel.Clock) *idemCache {
	return &idemCache{clock: clock, entries: map[string]*idemEntry{}}
}

// idemSlot is the claim a call holds on its key until it answers.
type idemSlot struct {
	cache  *idemCache
	key    string
	secret bool
}

func idemKeyOf(subject, method, path, key string) string {
	sum := sha256.Sum256([]byte(subject + "\x00" + method + " " + path + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

// begin claims the key. When it returns an entry, the key was used before.
func (ic *idemCache) begin(key, bodyHash string) *idemEntry {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	now := ic.clock.Now()
	if e, ok := ic.entries[key]; ok && now.Before(e.expires) {
		return e
	}
	if len(ic.entries) >= idemMaxKeys {
		for k, e := range ic.entries {
			if !now.Before(e.expires) || e.done {
				delete(ic.entries, k)
				if len(ic.entries) < idemMaxKeys {
					break
				}
			}
		}
	}
	ic.entries[key] = &idemEntry{bodyHash: bodyHash, expires: now.Add(idemTTL)}
	return nil
}

func (ic *idemCache) finish(key string, status int, body []byte, secret bool) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	e, ok := ic.entries[key]
	if !ok {
		return
	}
	if status/100 != 2 {
		delete(ic.entries, key)
		return
	}
	e.done, e.status, e.secret = true, status, secret
	if !secret {
		e.body = body
	}
}

// readIdempotent reads the body of a creating or acting call and claims its Idempotency-Key. It returns false when
// it has answered itself: a missing key, a key that is in use, or a replay of the first answer.
func (c *call) readIdempotent(dst any) bool {
	raw, err := io.ReadAll(c.r.Body)
	if err != nil {
		c.fail("invalid_input.body_too_large", map[string]any{"max_bytes": c.s.maxBody})
		return false
	}
	c.r.Body = io.NopCloser(bytes.NewReader(raw))
	key := c.r.Header.Get("Idempotency-Key")
	if len(key) < idemMinKey || len(key) > idemMaxKey {
		c.fail("invalid_input.idempotency_key_required", map[string]any{"min_length": idemMinKey, "max_length": idemMaxKey})
		return false
	}
	sum := sha256.Sum256(raw)
	id := idemKeyOf(c.subject.ID, c.r.Method, c.r.URL.Path, key)
	if e := c.s.idem.begin(id, hex.EncodeToString(sum[:])); e != nil {
		c.replay(e, hex.EncodeToString(sum[:]))
		return false
	}
	c.idem = &idemSlot{cache: c.s.idem, key: id}
	return c.readBody(dst)
}

func (c *call) replay(e *idemEntry, bodyHash string) {
	switch {
	case e.bodyHash != bodyHash:
		c.fail("conflict.idempotency_key_reused", map[string]any{"reason": "another_body"})
	case !e.done:
		c.fail("conflict.idempotency_key_reused", map[string]any{"reason": "in_progress"})
	case e.secret:
		c.fail("conflict.idempotency_key_reused", map[string]any{"reason": "secret_shown_once"})
	default:
		c.w.Header().Set("Idempotent-Replayed", "true")
		c.w.Header().Set("Content-Type", jsonMediaType)
		c.w.WriteHeader(e.status)
		_, _ = c.w.Write(e.body)
	}
}

// remember keeps the answer of a call that holds a key.
func (c *call) remember(status int, body []byte) {
	if c.idem != nil {
		c.idem.cache.finish(c.idem.key, status, body, c.idem.secret)
		c.idem = nil
	}
}
