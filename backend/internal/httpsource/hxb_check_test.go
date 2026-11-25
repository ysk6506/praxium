package httpsource

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The tests of what a check of an HTTP source tells about its certificate and its transport (ADR-0105). Every helper here
// begins with hxb so that it never meets a name of the older tests of this folder.

type hxbDoer struct {
	status int
	err    error
	n      int
}

func (d *hxbDoer) Do(_ context.Context, _ egress.Request) (*egress.Response, error) {
	d.n++
	if d.err != nil {
		return nil, d.err
	}
	return &egress.Response{Status: d.status, Header: map[string][]string{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"items":[{"serial":"a"}]}`))}, nil
}

func (d *hxbDoer) CloseIdleConnections() {}

// hxbProber is a client that can also show the certificate of its server.
type hxbProber struct {
	*hxbDoer
	sum   *egress.CertSummary
	err   error
	asked []string
}

func (p *hxbProber) ProbeCert(_ context.Context, rawURL string) (*egress.CertSummary, error) {
	p.asked = append(p.asked, rawURL)
	return p.sum, p.err
}

func hxbSource(t *testing.T, base, tlsExtra string) ingest.Source {
	t.Helper()
	doc := `{"version":1,"id":"src_x","adapter":"http","connection":{"base_url":"` + base + `"` + tlsExtra + `},"datasets":[{"id":"ds_a","request":{"path":"/a"},"records_path":"items","natural_key":["serial"],"columns":[{"name":"serial"}]}]}`
	src, err := ingest.ParseAdapterSource(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("the definition is not valid: %v", err)
	}
	return src
}

const hxbPinA = `,"tls":{"pins":["sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="]}`

func hxbReader(t *testing.T, src ingest.Source, doer Doer, seen *[]egress.ClientOptions) *Reader {
	t.Helper()
	r, err := New(Config{Source: src, Clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), Rng: kernel.NewSeededRng(1),
		Sleep: func(context.Context, time.Duration) error { return nil },
		Connect: func(o egress.ClientOptions) (Doer, error) {
			if seen != nil {
				*seen = append(*seen, o)
			}
			return doer, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func hxbNoteCodes(r ingest.CheckResult) []string {
	var out []string
	for _, n := range r.Notes {
		out = append(out, n.Code)
	}
	return out
}

// [REQ-821] A check says when the data goes without encryption and when a certificate is accepted by its key alone, with a
// result of any kind, and says nothing for a source that is verified as usual.
func TestHxbACheckTellsThePlaintextAndThePinsAsNotes(t *testing.T) {
	for name, tc := range map[string]struct {
		base, extra string
		want        []string
	}{
		"https":            {"https://api.example.test/api", "", nil},
		"http":             {"http://api.example.test/api", "", []string{ingest.NotePlaintext}},
		"https with a pin": {"https://api.example.test/api", hxbPinA, []string{ingest.NotePinExpiry}},
	} {
		src := hxbSource(t, tc.base, tc.extra)
		for _, doer := range []*hxbDoer{{status: 200}, {err: &egress.Error{Kind: egress.KindTimeout, Reason: "x"}}} {
			res := hxbReader(t, src, doer, nil).Check(context.Background(), "src_x", src.Datasets[0])
			if got := hxbNoteCodes(res); strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("%s (%s): notes %v, want %v", name, res.Kind, got, tc.want)
			}
			for _, n := range res.Notes {
				if n.Message == "" {
					t.Errorf("%s: a note without words", name)
				}
			}
		}
	}
	// a client that cannot be made is a result with the notes too
	src := hxbSource(t, "http://api.example.test/api", "")
	r, err := New(Config{Source: src, Clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), Rng: kernel.NewSeededRng(1),
		Sleep:   func(context.Context, time.Duration) error { return nil },
		Connect: func(egress.ClientOptions) (Doer, error) { return nil, errors.New("no") }})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Check(context.Background(), "src_x", src.Datasets[0])
	if res.Kind != ingest.CheckConfig || len(res.Notes) != 1 || !strings.Contains(res.Message, "tls.pins") {
		t.Errorf("%+v", res)
	}
}

// [REQ-822] The pins of the definition go to the client the read makes, as they are written in the definition, and ca_pem does
// not go with them.
func TestHxbThePinsOfTheDefinitionReachTheClient(t *testing.T) {
	src := hxbSource(t, "https://api.example.test/api", hxbPinA)
	var seen []egress.ClientOptions
	hxbReader(t, src, &hxbDoer{status: 200}, &seen).Check(context.Background(), "src_x", src.Datasets[0])
	if len(seen) != 1 || len(seen[0].Pins) != 1 || seen[0].Pins[0] != src.Connection.TLS.Pins[0] || seen[0].CAPEM != "" {
		t.Errorf("options %+v", seen)
	}
	plain := hxbSource(t, "https://api.example.test/api", "")
	seen = nil
	hxbReader(t, plain, &hxbDoer{status: 200}, &seen).Check(context.Background(), "src_x", plain.Datasets[0])
	if len(seen) != 1 || len(seen[0].Pins) != 0 {
		t.Errorf("a source without pins: %+v", seen)
	}
}

// [REQ-824] When the check failed because of the certificate, the result carries what the server presented (one probe, to the
// address of the source); for any other failure, and for a client that cannot show it, it carries none.
func TestHxbACheckShowsTheCertificateThatFailed(t *testing.T) {
	src := hxbSource(t, "https://api.example.test:8443/api", "")
	when := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	sum := &egress.CertSummary{Subject: "CN=x", Issuer: "CN=x", SANs: []string{"a.example.test"}, SANsMore: 3, NotBefore: when, NotAfter: when.Add(time.Hour), SPKISHA256: "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	tlsFail := &egress.Error{Kind: egress.KindTLS, Reason: "the certificate is not signed by a trusted authority"}
	p := &hxbProber{hxbDoer: &hxbDoer{err: tlsFail}, sum: sum}
	res := hxbReader(t, src, p, nil).Check(context.Background(), "src_x", src.Datasets[0])
	if res.Kind != ingest.CheckConfig || res.Cert == nil || res.Cert.SPKISHA256 != sum.SPKISHA256 || res.Cert.Subject != "CN=x" || res.Cert.SANsMore != 3 ||
		len(res.Cert.SANs) != 1 || !res.Cert.NotAfter.Equal(when.Add(time.Hour)) {
		t.Fatalf("result %+v cert %+v", res, res.Cert)
	}
	if len(p.asked) != 1 || p.asked[0] != "https://api.example.test:8443/api" || p.n != 1 {
		t.Errorf("asked %v after %d requests", p.asked, p.n)
	}
	// a failure that is not the certificate's is not probed
	other := &hxbProber{hxbDoer: &hxbDoer{status: 401}, sum: sum}
	if res := hxbReader(t, src, other, nil).Check(context.Background(), "src_x", src.Datasets[0]); res.Cert != nil || len(other.asked) != 0 {
		t.Errorf("a 401: %+v, asked %v", res.Cert, other.asked)
	}
	timeout := &hxbProber{hxbDoer: &hxbDoer{err: &egress.Error{Kind: egress.KindTimeout, Reason: "x"}}, sum: sum}
	if res := hxbReader(t, src, timeout, nil).Check(context.Background(), "src_x", src.Datasets[0]); res.Cert != nil || len(timeout.asked) != 0 {
		t.Errorf("a time out: %+v", res.Cert)
	}
	// a client that cannot show it, a probe that fails and a probe that gives nothing
	if res := hxbReader(t, src, &hxbDoer{err: tlsFail}, nil).Check(context.Background(), "src_x", src.Datasets[0]); res.Cert != nil || res.Kind != ingest.CheckConfig {
		t.Errorf("a client without a probe: %+v", res)
	}
	broken := &hxbProber{hxbDoer: &hxbDoer{err: tlsFail}, err: &egress.Error{Kind: egress.KindConnect, Reason: "x"}}
	if res := hxbReader(t, src, broken, nil).Check(context.Background(), "src_x", src.Datasets[0]); res.Cert != nil {
		t.Errorf("a probe that failed: %+v", res.Cert)
	}
	nothing := &hxbProber{hxbDoer: &hxbDoer{err: tlsFail}}
	if res := hxbReader(t, src, nothing, nil).Check(context.Background(), "src_x", src.Datasets[0]); res.Cert != nil {
		t.Errorf("a probe that gave nothing: %+v", res.Cert)
	}
	// the words of the failure hold no certificate text
	if strings.Contains(res.Message, "sha256/") {
		t.Errorf("message %q", res.Message)
	}
}

// [REQ-822] A pin that does not match is a failure of the setting that is not tried again: the first request is the only one.
func TestHxbAPinThatDoesNotMatchIsNotTriedAgain(t *testing.T) {
	src := hxbSource(t, "https://api.example.test/api", hxbPinA)
	d := &hxbDoer{err: &egress.Error{Kind: egress.KindTLS, Reason: "the certificate of the server is not one of the pinned ones"}}
	r := hxbReader(t, src, d, nil)
	var failed error
	for _, err := range r.ReadPages(context.Background(), "src_x", src.Datasets[0], ingest.PageOptions{}) {
		failed = err
		break
	}
	se, ok := ingest.SourceErrorOf(failed)
	if !ok || se.Kind != ingest.SrcTLSRejected || se.Class != ingest.CheckConfig || d.n != 1 {
		t.Errorf("error %v after %d requests", failed, d.n)
	}
}
