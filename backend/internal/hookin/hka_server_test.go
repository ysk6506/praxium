package hookin_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/hookin"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-862]
func TestHkaOnTheServerTheAccessLogHasNoQueryAndNoOtherAnswerComesOnThePath(t *testing.T) {
	rig := hkaNew(t)
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	srv, err := httpapi.New(httpapi.Config{
		Registry: reg, Clock: kernel.NewManualClock(hkaStart), IDs: kernel.NewSequentialIDs(), Version: "test",
		Logger: slog.New(slog.NewJSONHandler(logs, nil)),
		Mounts: []httpapi.Mount{{Match: hookin.Match, Handler: rig.h}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.MarkStarted()
	secret := rig.secrets["url"]
	do := func(method, target string, body string, headers map[string]string) (int, string, http.Header) {
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		r.RemoteAddr = hkaGoodIP
		r.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		rec := hkaRecord()
		srv.Handler().ServeHTTP(rec, r)
		return rec.Code, rec.Body.String(), rec.Header()
	}
	path := "/v1/sources/crm/webhooks/url:receive"
	if code, body, h := do(http.MethodPost, path+"?token="+secret, `{}`, nil); code != 202 || h.Get("X-Trace-Id") == "" || !strings.Contains(body, `"accepted":true`) {
		t.Fatalf("on the server: %d %s", code, body)
	}
	// A wrong call answers with the trace id of the server, and the path it was sent to is nowhere in the answer.
	code, body, h := do(http.MethodPost, "/v1/sources/crm/webhooks/not-there:receive?token=wrong-value", `{}`, nil)
	if code != 401 || !strings.Contains(body, h.Get("X-Trace-Id")) || strings.Contains(body, "not-there") {
		t.Errorf("a wrong call: %d %s", code, body)
	}
	// A path of the shape that is not a webhook is the answer of the entrance as well, not the 404 that echoes the path.
	for _, target := range []string{"/v1/sources/crm/webhooks/%00x:receive", "/v1/sources/zz/webhooks/yy:receive", "/v1/sources/crm/webhooks/a/b/secretword:receive"} {
		code, body, _ := do(http.MethodPost, target, `{}`, nil)
		if code != 401 || strings.Contains(body, "secretword") || strings.Contains(body, "not_found") {
			t.Errorf("%s: %d %s", target, code, body)
		}
	}
	if code, body, _ := do(http.MethodGet, path, ``, nil); code != 405 || strings.Contains(body, "crm") {
		t.Errorf("GET: %d %s", code, body)
	}
	log := logs.String()
	for _, leak := range []string{secret, "wrong-value", "token="} {
		if strings.Contains(log, leak) {
			t.Errorf("the log of the server holds %q:\n%s", leak, log)
		}
	}
	if !strings.Contains(log, "/v1/sources/crm/webhooks/url:receive") {
		t.Errorf("the access log has no line of the call (the path is what it keeps):\n%s", log)
	}
	if strings.Contains(rig.logs.String(), secret) {
		t.Error("the log of the entrance holds the secret")
	}
}

// [REQ-862]
func TestHkaABodyThatArrivesTooSlowlyIsGivenUpAtTheDeadline(t *testing.T) {
	rig := hkaNew(t, func(c *hookin.Config) {
		c.Clock = kernel.SystemClock{}
		c.ReadTimeout = 200 * time.Millisecond
	})
	ts := httptest.NewServer(rig.h)
	defer ts.Close()
	// The headers, a promise of a body of 1000 bytes, and one byte of it; the rest never comes.
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() { _, _ = pw.Write([]byte("{")) }()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/sources/crm/webhooks/header:receive", pr)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = 1000
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Token", rig.secrets["header"])
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("a body that stops: %d", resp.StatusCode)
	}
	if rig.h.Counts()["fail_read"] != 1 {
		t.Errorf("counts %v", rig.h.Counts())
	}
	if rig.spy.signals.Load() != 0 {
		t.Error("a signal was marked for a call whose body never came")
	}
}

// [REQ-862]
func TestHkaTheReadDeadlineIsAskedOfTheClockAndHeldToThirtySeconds(t *testing.T) {
	var asked time.Time
	rig := hkaNew(t, func(c *hookin.Config) { c.ReadTimeout = time.Hour })
	rec := &hkaDeadline{ResponseRecorder: httptest.NewRecorder(), set: &asked}
	rig.h.ServeHTTP(rec, hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]}))
	if want := hkaStart.Add(hookin.MaxReadTimeout); !asked.Equal(want) {
		t.Errorf("deadline %v, want %v", asked, want)
	}
	rig = hkaNew(t)
	rig.h.ServeHTTP(&hkaDeadline{ResponseRecorder: httptest.NewRecorder(), set: &asked}, hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]}))
	if want := hkaStart.Add(hookin.DefaultReadTimeout); !asked.Equal(want) {
		t.Errorf("default deadline %v, want %v", asked, want)
	}
}

type hkaDeadline struct {
	*httptest.ResponseRecorder
	set *time.Time
}

func (d *hkaDeadline) SetReadDeadline(t time.Time) error { *d.set = t; return nil }

// [REQ-862]
func TestHkaAVerifierThatCannotAnswerIsAFailureThatTellsTheCallerNothing(t *testing.T) {
	rig := hkaNew(t)
	h, err := hookin.New(hookin.Config{Directory: rig.spy, Sink: rig.spy, Verifier: hkaBrokenVerifier{rig.spy}, Clock: rig.clock, Logger: slog.New(slog.NewJSONHandler(rig.logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	rec := hkaRecord()
	h.ServeHTTP(rec, hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]}))
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), "auth.webhook_invalid") || strings.Contains(rec.Body.String(), "store") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rig.logs.String(), "could not be checked") {
		t.Error("a fault of the verifier is not logged")
	}
}

type hkaBrokenVerifier struct{ *hkaSpy }

func (hkaBrokenVerifier) VerifyToken(context.Context, string, string, []byte) (bool, error) {
	return false, errors.New("the store is gone")
}

// [REQ-862]
func TestHkaTheMarksOfAFloodOfWebhooksAndAddressesStayBounded(t *testing.T) {
	rig := hkaNew(t)
	for i := 0; i < 9000; i++ {
		r := hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": "wrong"})
		r.RemoteAddr = "10." + strings.Join([]string{itoa(i / 65536 % 256), itoa(i / 256 % 256), itoa(i % 256)}, ".") + ":1"
		if got := rig.serve(r); got.status != 401 {
			t.Fatalf("%d: %d", i, got.status)
		}
	}
	// The right call still passes after the tables of marks were emptied.
	if got := rig.serve(hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]})); got.status != 202 {
		t.Errorf("after a flood: %d", got.status)
	}
	for i := 0; i < 9000; i++ {
		rig.serve(hkaReq("header", `{}`, map[string]string{"X-Webhook-Token": rig.secrets["header"]}))
	}
	hkaDrain(strings.NewReader(base64.StdEncoding.EncodeToString(nil)))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
