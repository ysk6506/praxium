package hookin_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/hookin"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

var hkaStart = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

const (
	hkaSource  = "crm"
	hkaGoodIP  = "192.0.2.10:4000"
	hkaOtherIP = "198.51.100.9:4000"
)

// hkaSpy stands for what lies behind the proof: it fails the test when it is asked before a caller was proven.
type hkaSpy struct {
	t         *testing.T
	inner     secrets.Verifier
	accepted  atomic.Int64
	receiving atomic.Bool
	reads     atomic.Int64
	signals   atomic.Int64
	sinkErr   error
	stateErr  error
	hooks     map[string]hookin.Hook
	mu        sync.Mutex
}

func (s *hkaSpy) Hook(source, webhook string) (hookin.Hook, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.hooks[source+"/"+webhook]
	return h, ok
}

func (s *hkaSpy) Receiving(_ context.Context, _ string) (bool, error) {
	s.reads.Add(1)
	if s.accepted.Load() == 0 {
		s.t.Error("the state of the source was read before any caller was proven")
	}
	return s.receiving.Load(), s.stateErr
}

func (s *hkaSpy) Signal(_ context.Context, source string) error {
	s.signals.Add(1)
	if s.accepted.Load() == 0 {
		s.t.Error("a signal was marked before any caller was proven")
	}
	if source != hkaSource {
		s.t.Errorf("the signal is for the source %q", source)
	}
	return s.sinkErr
}

func (s *hkaSpy) VerifyHMAC(ctx context.Context, src, wh string, signed []byte, sigs [][]byte) (bool, error) {
	ok, err := s.inner.VerifyHMAC(ctx, src, wh, signed, sigs)
	if ok {
		s.accepted.Add(1)
	}
	return ok, err
}

func (s *hkaSpy) VerifyToken(ctx context.Context, src, wh string, p []byte) (bool, error) {
	ok, err := s.inner.VerifyToken(ctx, src, wh, p)
	if ok {
		s.accepted.Add(1)
	}
	return ok, err
}

func (s *hkaSpy) Dummy(a []byte, b [][]byte, c []byte) { s.inner.Dummy(a, b, c) }

type hkaRig struct {
	t       *testing.T
	clock   *kernel.ManualClock
	svc     *secrets.Service
	spy     *hkaSpy
	h       *hookin.Handler
	logs    *bytes.Buffer
	secrets map[string]string // the value given at the issue, by webhook id
}

func hkaHook(auth, profile string, limits ingest.WebhookLimits, allow ...string) hookin.Hook {
	h, err := hookin.HookOf(ingest.Webhook{ID: "x", Kind: ingest.WebhookSignal, Auth: ingest.WebhookAuth{Type: auth, Profile: profile}, AllowIPs: allow, Limits: &limits})
	if err != nil {
		panic(err)
	}
	return h
}

func hkaNew(t *testing.T, edit ...func(*hookin.Config)) *hkaRig {
	t.Helper()
	clock := kernel.NewManualClock(hkaStart)
	key := secrets.GenerateWebhookKey(kernel.NewSeededBytes(7))
	svc, err := secrets.New(secrets.Config{Store: secrets.NewMemoryStore(), Keys: secrets.NoKey(), WebhookKeys: secrets.StaticKey(key), Clock: clock, Bytes: kernel.NewSeededBytes(8)})
	if err != nil {
		t.Fatal(err)
	}
	spy := &hkaSpy{t: t, inner: svc.Verifier(), hooks: map[string]hookin.Hook{}}
	spy.receiving.Store(true)
	logs := &bytes.Buffer{}
	rig := &hkaRig{t: t, clock: clock, svc: svc, spy: spy, logs: logs, secrets: map[string]string{}}
	for id, hk := range map[string]hookin.Hook{
		"signed":   hkaHook(ingest.WebhookAuthHMAC, ingest.ProfileStandardWebhooks, ingest.WebhookLimits{BodyBytes: 4096}),
		"bodyonly": hkaHook(ingest.WebhookAuthHMAC, ingest.ProfileBodyOnly, ingest.WebhookLimits{}),
		"header":   hkaHook(ingest.WebhookAuthHeaderToken, "", ingest.WebhookLimits{}),
		"basic":    hkaHook(ingest.WebhookAuthBasic, "", ingest.WebhookLimits{}),
		"url":      hkaHook(ingest.WebhookAuthURLToken, "", ingest.WebhookLimits{}),
		"allowed":  hkaHook(ingest.WebhookAuthHeaderToken, "", ingest.WebhookLimits{}, "192.0.2.0/24"),
		"slow":     hkaHook(ingest.WebhookAuthHeaderToken, "", ingest.WebhookLimits{RatePerSecond: 1, Burst: 2}),
	} {
		spy.hooks[hkaSource+"/"+id] = hk
		auth := map[string]string{"signed": "hmac", "bodyonly": "hmac", "header": "header_token", "basic": "basic", "url": "url_token", "allowed": "header_token", "slow": "header_token"}[id]
		issued, err := svc.IssueWebhookSecret(context.Background(), hkaSource, id, auth)
		if err != nil {
			t.Fatal(err)
		}
		rig.secrets[id] = issued.Value
	}
	cfg := hookin.Config{Directory: spy, Sink: spy, Verifier: spy, Clock: clock, Logger: slog.New(slog.NewJSONHandler(logs, nil))}
	for _, e := range edit {
		e(&cfg)
	}
	h, err := hookin.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rig.h = h
	return rig
}

type hkaReply struct {
	status int
	header http.Header
	body   string
}

func (r hkaReply) code() string {
	var p struct{ Code string }
	_ = json.Unmarshal([]byte(r.body), &p)
	return p.Code
}

// same is the reply without the trace id, which is the one thing that differs between two calls.
func (r hkaReply) same() string {
	var m map[string]any
	_ = json.Unmarshal([]byte(r.body), &m)
	delete(m, "trace_id")
	out, _ := json.Marshal(m)
	h := r.header.Clone()
	h.Del("X-Trace-Id")
	return strconv.Itoa(r.status) + "|" + string(out) + "|" + hkaFlat(h)
}

func hkaFlat(h http.Header) string {
	var b strings.Builder
	for _, k := range []string{"Content-Type", "Www-Authenticate", "Retry-After", "Cache-Control"} {
		b.WriteString(k + "=" + h.Get(k) + ";")
	}
	return b.String()
}

func (rig *hkaRig) serve(r *http.Request) hkaReply {
	rec := httptest.NewRecorder()
	rig.h.ServeHTTP(rec, r)
	return hkaReply{rec.Code, rec.Header(), rec.Body.String()}
}

func hkaReq(webhook, body string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/sources/"+hkaSource+"/webhooks/"+webhook+":receive", strings.NewReader(body))
	r.RemoteAddr = hkaGoodIP
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func hkaSign(secret string, msg string) string {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return "v1," + base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func (rig *hkaRig) signed(webhook, id string, at time.Time, body string) *http.Request {
	ts := strconv.FormatInt(at.Unix(), 10)
	return hkaReq(webhook, body, map[string]string{"Webhook-Id": id, "Webhook-Timestamp": ts, "Webhook-Signature": hkaSign(rig.secrets[webhook], id+"."+ts+"."+body)})
}

func hkaDrain(r io.Reader) { _, _ = io.Copy(io.Discard, r); _ = netip.Addr{} }
