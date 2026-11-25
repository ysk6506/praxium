// Package hookin is the entrance of the receiving webhooks (ADR-0107): the handler of
// POST /v1/sources/{source_id}/webhooks/{webhook_id}:receive, which products other than people call. It proves the caller,
// keeps the entrance small enough to survive a flood, and for a webhook of the kind signal marks that the source changed.
//
// The order of what it does is the order of the design (webhook-ingest 1.1), and nothing before the proof reaches the
// database: the Directory it asks first answers from memory, and the Sink and the state of the source are asked after the
// caller is proven.
//
//  1. the path is read, and the webhook is looked up in memory (Handler.ServeHTTP, lookup)
//  2. the address is checked against the allowed ones (admit)
//  3. the room of the entrance: calls at once of the address, calls at once, the memory of the bodies, calls at once of the
//     webhook (reserve)
//  4. a proof that is in the headers or the query is checked before the body is read (proveByHeader)
//  5. the body is read, to the limit of the webhook and no further, with a deadline for its first byte and one for all
//     of it (readBody)
//  6. a signature is checked, which needs the body (proveBySignature), then the body is checked to be JSON (checkJSON)
//  7. the state of the source, then the rate of the webhook (afterProof)
//  8. the mark of the signal (afterProof)
//  9. the answer 202 (accepted)
package hookin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ysk6506/praxium/backend/internal/contract"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// Hook is what the entrance needs to know of one webhook, kept in memory from the definition that is applied.
type Hook struct {
	Kind     string
	Auth     ingest.WebhookAuth
	AllowIPs []netip.Prefix
	Limits   ingest.WebhookLimits
}

// HookOf makes the Hook of a webhook of a definition that was read (its limits and its addresses are checked there).
func HookOf(w ingest.Webhook) (Hook, error) {
	h := Hook{Kind: w.Kind, Auth: w.Auth, Limits: ingest.WebhookLimits{
		RatePerSecond: ingest.DefaultWebhookRate, Burst: ingest.DefaultWebhookBurst, BodyBytes: ingest.DefaultWebhookBodyBytes}}
	if l := w.Limits; l != nil {
		if l.RatePerSecond > 0 {
			h.Limits.RatePerSecond = l.RatePerSecond
		}
		if l.Burst > 0 {
			h.Limits.Burst = l.Burst
		}
		if l.BodyBytes > 0 {
			h.Limits.BodyBytes = l.BodyBytes
		}
	}
	for _, e := range w.AllowIPs {
		p, err := ingest.ParseAllowEntry(e)
		if err != nil {
			return Hook{}, kernel.Wrap(kernel.CodeInvalidValue, err, "an address of the webhook is not an address")
		}
		h.AllowIPs = append(h.AllowIPs, p)
	}
	return h, nil
}

// Directory is what the entrance knows of the webhooks. Hook answers from memory and never from a database: it is asked
// before the caller is proven. Receiving is asked after the proof and may read the state of the source.
type Directory interface {
	// Hook returns the webhook of an applied definition, and false when there is none (also for a webhook that is taken
	// away, and for a kind the entrance does not handle yet).
	Hook(sourceID, webhookID string) (Hook, bool)
	// Receiving says whether the source takes calls now (it is not stopped).
	Receiving(ctx context.Context, sourceID string) (bool, error)
}

// SignalSink takes the mark that a source changed. It must do nothing of cost when a mark is already there.
type SignalSink interface {
	Signal(ctx context.Context, sourceID string) error
}

// Config of a Handler.
type Config struct {
	Directory Directory
	Sink      SignalSink
	Verifier  secrets.Verifier
	Clock     kernel.Clock
	Logger    *slog.Logger
	// TrustProxy reads the address of the caller from the last X-Forwarded-For a proxy of the operator appended.
	TrustProxy bool
	// MaxInFlight is how many calls the whole entrance takes at once (default 128); MaxBodyMemory is the sum of the memory
	// the bodies being read hold (default 32 MiB); PerWebhookInFlight is the calls at once of one webhook (16);
	// PerAddressInFlight is the calls at once of one address, an IPv6 address counted by its /64 (4). The last is checked
	// before anything is held, so an address that opens many calls is refused with the 429 before it takes room from the rest.
	MaxInFlight        int
	MaxBodyMemory      int64
	PerWebhookInFlight int
	PerAddressInFlight int
	// ReadTimeout is the time a whole body may take to arrive (default 10 s, at most 30 s); FirstByteTimeout is the time the
	// first byte of it may take (default 2 s, and no more than ReadTimeout).
	ReadTimeout      time.Duration
	FirstByteTimeout time.Duration
	// FailureRate and FailureBurst are the bucket of the failures of one (webhook, address): default 5 a second, 50.
	FailureRate  float64
	FailureBurst int
}

// Entrance limits that are not a setting.
const (
	DefaultMaxInFlight        = 128
	DefaultMaxBodyMemory      = 32 << 20
	DefaultPerWebhookInFlight = 16
	DefaultPerAddressInFlight = 4
	DefaultReadTimeout        = 10 * time.Second
	DefaultFirstByteTimeout   = 2 * time.Second
	MaxReadTimeout            = 30 * time.Second
	// unknownLengthBytes is the memory a body of no declared length is given to start with; it grows as the body is read.
	unknownLengthBytes = 64 << 10
	// unknownBodyBytes is how much of a body is read for a webhook that is not known: the smallest limit.
	unknownBodyBytes = 1024
	// MaxJSONDepth and MaxJSONTokens bound the check of the body of a signal, which is thrown away.
	MaxJSONDepth  = 32
	MaxJSONTokens = 100000
	// SignatureTolerance is how far the time of a signed message may be from now.
	SignatureTolerance = 300 * time.Second
	maxHeaderValue     = 2048
	maxSignatures      = 8
	maxTrackedKeys     = 8192
	logEvery           = time.Minute
)

// Handler is the http.Handler of the entrance. Build it with New.
type Handler struct {
	cfg      Config
	inflight atomic.Int64
	memory   atomic.Int64

	mu      sync.Mutex
	perHook map[string]int
	perAddr map[string]int     // calls at once by address group
	buckets map[string]*bucket // the rate of a webhook, by (source, webhook)
	fails   map[string]*bucket // the failures, by (source, webhook, address)
	logged  map[string]*failLog

	counts counters

	noDeadlineLogged atomic.Bool // the entrance says once that the server cannot set a read deadline
}

// withDefaults fills what the caller left out and holds the read timeout to its highest value.
func (c Config) withDefaults() Config {
	if c.MaxInFlight <= 0 {
		c.MaxInFlight = DefaultMaxInFlight
	}
	if c.MaxBodyMemory <= 0 {
		c.MaxBodyMemory = DefaultMaxBodyMemory
	}
	if c.PerWebhookInFlight <= 0 {
		c.PerWebhookInFlight = DefaultPerWebhookInFlight
	}
	if c.PerAddressInFlight <= 0 {
		c.PerAddressInFlight = DefaultPerAddressInFlight
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = DefaultReadTimeout
	}
	if c.ReadTimeout > MaxReadTimeout {
		c.ReadTimeout = MaxReadTimeout
	}
	if c.FirstByteTimeout <= 0 {
		c.FirstByteTimeout = DefaultFirstByteTimeout
	}
	if c.FirstByteTimeout > c.ReadTimeout {
		c.FirstByteTimeout = c.ReadTimeout
	}
	if c.FailureRate <= 0 {
		c.FailureRate = 5
	}
	if c.FailureBurst <= 0 {
		c.FailureBurst = 50
	}
	return c
}

// InFlight is how many calls the entrance holds now.
func (h *Handler) InFlight() int64 { return h.inflight.Load() }

// New builds the handler. A Directory, a Sink, a Verifier, a Clock and a Logger are needed.
func New(cfg Config) (*Handler, error) {
	if cfg.Directory == nil || cfg.Sink == nil || cfg.Verifier == nil || cfg.Clock == nil || cfg.Logger == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the entrance of the webhooks needs a directory, a sink, a verifier, a clock and a logger")
	}
	cfg = cfg.withDefaults()
	return &Handler{cfg: cfg, perHook: map[string]int{}, perAddr: map[string]int{}, buckets: map[string]*bucket{}, fails: map[string]*bucket{}, logged: map[string]*failLog{}}, nil
}

const (
	pathPrefix = "/v1/sources/"
	pathSuffix = ":receive"
	pathMiddle = "/webhooks/"
)

// Match says whether a path is one of the entrance: every path of this shape is answered here, with a valid id or not,
// so that the answer for a path that is not a webhook is the answer for a webhook that is wrong.
func Match(path string) bool {
	return strings.HasPrefix(path, pathPrefix) && strings.HasSuffix(path, pathSuffix) && strings.Contains(path, pathMiddle)
}

// ids reads the source and the webhook from a path; known is false when they are not valid ids.
func ids(path string) (source, webhook string, valid bool) {
	rest := strings.TrimSuffix(strings.TrimPrefix(path, pathPrefix), pathSuffix)
	source, webhook, ok := strings.Cut(rest, pathMiddle)
	if !ok || !ingest.ValidID(source) || !ingest.ValidID(webhook) {
		return "", "", false
	}
	return source, webhook, true
}

// request is what one call carries through the steps.
type request struct {
	h       *Handler
	w       http.ResponseWriter
	r       *http.Request
	source  string
	webhook string
	hook    Hook
	known   bool
	addr    netip.Addr
	vouched bool   // the address is the one a proxy of the operator wrote, or the connection's without a proxy (callerAddr)
	group   string // the address as it is counted: an IPv4 address, or the /64 of an IPv6 one (addrGroup)
	key     string // (source, webhook) when known, else "unknown"
	limit   int64
	body    []byte
	start   time.Time // when the entrance took the call in; the deadlines are from it
	held    int64     // the bytes of the memory of bodies that this call holds (reserve, readBody)
	copies  int64     // how many times the body is in memory at once: 2 when a signed message is made of it
}

// ServeHTTP is steps 1 to 9.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.problem(w, "invalid_input.method_not_allowed", map[string]any{"allowed": []string{http.MethodPost}}, 0)
		return
	}
	q := h.lookup(w, r)
	q.addr, q.vouched = h.callerAddr(r)
	q.group = addrGroup(q.addr)
	if !q.admit() {
		return
	}
	release, ok := q.reserve()
	if !ok {
		return
	}
	defer release()
	// The first byte of the body has a short time of its own, and the whole body a longer one: a sender that sends nothing,
	// or a byte a minute, holds its place for seconds and not for the whole of the read time.
	q.start = h.cfg.Clock.Now()
	if !q.setDeadline(h.cfg.FirstByteTimeout) {
		return
	}
	if q.known && q.hook.Auth.Type != ingest.WebhookAuthHMAC && !q.proveByHeader() {
		return
	}
	if !q.readBody() {
		return
	}
	if !q.known || q.hook.Auth.Type == ingest.WebhookAuthHMAC {
		if !q.proveBySignature() {
			return
		}
	}
	q.afterProof()
}

// lookup is step 1: the path is read and the webhook is looked up in memory.
func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) *request {
	q := &request{h: h, w: w, r: r, key: "unknown", limit: unknownBodyBytes}
	if source, webhook, ok := ids(r.URL.Path); ok {
		if hook, found := h.cfg.Directory.Hook(source, webhook); found && hook.Kind == ingest.WebhookSignal {
			q.source, q.webhook, q.hook, q.known = source, webhook, hook, true
			q.key = source + "/" + webhook
			q.limit = int64(hook.Limits.BodyBytes)
		}
	}
	return q
}

// admit is step 2: the address of the caller against the allowed ones of the webhook.
func (q *request) admit() bool {
	if !q.known || len(q.hook.AllowIPs) == 0 {
		return true
	}
	if q.vouched {
		for _, p := range q.hook.AllowIPs {
			if p.Contains(q.addr) {
				return true
			}
		}
	}
	q.fail("address")
	return false
}

// afterProof is steps 6 (the body is JSON) to 9: only a caller that is proven comes here.
func (q *request) afterProof() {
	h := q.h
	if mt, _, err := mime.ParseMediaType(q.r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		h.counts.add("bad_media_type")
		h.problem(q.w, "invalid_input.unsupported_media_type", map[string]any{"accepted": []string{"application/json"}}, 0)
		return
	}
	if err := checkJSON(q.body); err != nil {
		h.counts.add("bad_body")
		h.problem(q.w, "invalid_input.webhook_body", map[string]any{"reason": err.Error()}, 0)
		return
	}
	ctx := q.r.Context()
	receiving, err := h.cfg.Directory.Receiving(ctx, q.source)
	if err != nil {
		h.cfg.Logger.Error("the state of a source could not be read for a webhook", "webhook", q.key, "error", kernel.CodeOf(err))
		q.w.Header().Set("Retry-After", "5")
		h.problem(q.w, "server.unavailable", nil, 0)
		return
	}
	if !receiving {
		h.counts.add("not_receiving")
		q.w.Header().Set("Retry-After", "60")
		h.problem(q.w, "stale.source_not_receiving", nil, 0)
		return
	}
	if wait, ok := h.takeRate(q.key, q.hook.Limits); !ok {
		h.counts.add("rate_limited")
		h.problem(q.w, "rate_limited.webhook_rate", nil, wait)
		return
	}
	if err := h.cfg.Sink.Signal(ctx, q.source); err != nil {
		h.cfg.Logger.Error("a signal could not be marked", "webhook", q.key, "error", kernel.CodeOf(err))
		q.w.Header().Set("Retry-After", "5")
		h.problem(q.w, "server.unavailable", nil, 0)
		return
	}
	h.counts.add("accepted")
	q.w.Header().Set("Content-Type", "application/json")
	q.w.WriteHeader(http.StatusAccepted)
	_, _ = io.WriteString(q.w, `{"data":{"accepted":true,"duplicate":false},"meta":{}}`)
}

// checkJSON reads the body as a stream and keeps nothing of it: one JSON value, not deeper and not longer than the caps.
func checkJSON(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // a number is read as its text: 1e999 is a number of JSON, though it is no float64
	depth, tokens := 0, 0
	done := false
	for {
		t, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil || done {
			return errNotJSON
		}
		if tokens++; tokens > MaxJSONTokens {
			return errTooManyTokens
		}
		if d, ok := t.(json.Delim); ok && (d == '{' || d == '[') {
			if depth++; depth > MaxJSONDepth {
				return errTooDeep
			}
			continue
		} else if ok {
			depth--
		}
		done = depth == 0
	}
	if !done {
		return errNotJSON
	}
	return nil
}

type jsonErr string

func (e jsonErr) Error() string { return string(e) }

const (
	errNotJSON       = jsonErr("not_json")
	errTooDeep       = jsonErr("too_deep")
	errTooManyTokens = jsonErr("too_many_tokens")
)

// problem answers with the problem of a catalogue code, and with Retry-After when wait is positive.
func (h *Handler) problem(w http.ResponseWriter, code string, params map[string]any, wait time.Duration) {
	entry, ok := contract.LookupProblem(code)
	if !ok {
		entry, _ = contract.LookupProblem("server.internal")
		params = nil
	}
	if wait > 0 {
		secs := int((wait + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	body := struct {
		Type    string         `json:"type"`
		Title   string         `json:"title"`
		Status  int            `json:"status"`
		Code    string         `json:"code"`
		Params  map[string]any `json:"params,omitempty"`
		TraceID string         `json:"trace_id"`
	}{"/v1/problems/" + entry.Code, titles[entry.Group], entry.Status, entry.Code, params, w.Header().Get("X-Trace-Id")}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(entry.Status)
	_ = json.NewEncoder(w).Encode(body)
}

var titles = map[string]string{
	"auth": "Unauthorized", "invalid_input": "Invalid input", "rate_limited": "Too many requests", "stale": "Stale", "server": "Server error",
}
