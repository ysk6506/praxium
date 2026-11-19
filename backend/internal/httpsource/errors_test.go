package httpsource

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-665]
func TestEveryFailureOfTheConnectionIsOneOfTheClosedKindsWithItsClass(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		kind  string
		class ingest.CheckKind
		seen  int
	}{
		{"a refused address", &egress.Error{Kind: egress.KindPolicy, Reason: "the address is not allowed by the policy", Host: "api.example.test"}, ingest.SrcPolicyDenied, ingest.CheckConfig, 1},
		{"a name with no address", &egress.Error{Kind: egress.KindResolve, Reason: "the name did not resolve"}, ingest.SrcNameNotFound, ingest.CheckConfig, 1},
		{"a certificate", egress.Sanitize(hsUnknownAuthority()), ingest.SrcTLSRejected, ingest.CheckConfig, 1},
		{"a redirect", &egress.Error{Kind: egress.KindRedirect, Reason: "the source answered with a redirect, which is not followed"}, ingest.SrcRequestRejected, ingest.CheckConfig, 1},
		{"a broken connection", &egress.Error{Kind: egress.KindConnect, Reason: "the connection failed or broke"}, ingest.SrcConnectionLost, ingest.CheckTransient, 4},
		{"a time limit", &egress.Error{Kind: egress.KindTimeout, Reason: "the time limit ran out"}, ingest.SrcTimeout, ingest.CheckTransient, 4},
		{"something that is not an egress error", errors.New(`Get "https://api.example.test/x?token=abc": EOF`), ingest.SrcConnectionLost, ingest.CheckTransient, 4},
		{"a kind that is not known", &egress.Error{Kind: "other", Reason: "x"}, ingest.SrcInternal, ingest.CheckProduct, 1},
	}
	for _, c := range cases {
		g := hsNew(t, hsDoc("", "", "", ""), func(_ int, _ hsRequest) hsReply { return hsReply{hsErr: c.err} })
		_, err := g.hsRead(ingest.PageOptions{})
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != c.kind || se.Class != c.class || len(g.hsSrv.hsSeen) != c.seen {
			t.Errorf("%s: %v after %d requests", c.name, err, len(g.hsSrv.hsSeen))
			continue
		}
		for _, bad := range []string{"token=abc", "api.example.test/x", "EOF", "Get "} {
			if strings.Contains(se.Message, bad) {
				t.Errorf("%s: the message holds %q: %s", c.name, bad, se.Message)
			}
		}
		if ingest.ClassifyCheckError(err) != c.class {
			t.Errorf("%s: the classifier says another class", c.name)
		}
	}
}

// [REQ-665]
func TestWhatIsUnderAnErrorStaysForTheOperatorAndNeverInTheWords(t *testing.T) {
	refused := &egress.Error{Kind: egress.KindPolicy, Reason: "the address is not allowed by the policy", Host: "api.example.test"}
	g := hsNew(t, hsDoc("", "", "", ""), func(_ int, _ hsRequest) hsReply { return hsReply{hsErr: refused} })
	_, err := g.hsRead(ingest.PageOptions{})
	var under *egress.Error
	if !errors.As(err, &under) || under != refused {
		t.Fatal("the error of the egress unit is not reachable for the operator")
	}
	se, _ := ingest.SourceErrorOf(err)
	if se.Detail != "" || strings.Contains(se.Message, "api.example.test") {
		t.Fatalf("detail %q, message %q", se.Detail, se.Message)
	}
}

// [REQ-665]
func TestAnErrorOfTheCallersContextIsNotAFailureOfTheSource(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), func(_ int, _ hsRequest) hsReply {
		return hsReply{hsErr: &egress.Error{Kind: egress.KindCanceled, Reason: "the request was cancelled"}}
	})
	_, err := g.hsRead(ingest.PageOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancel the context did not make: %v", err)
	}
	if _, ok := ingest.SourceErrorOf(err); ok {
		t.Fatal("a cancellation is no source error")
	}
}

// [REQ-665]
func TestAReaderIsMadeForOneSourceAndOneHttpDatasetAndSourcesThatAreOffAreNotRead(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), hsOne)
	for _, err := range g.hsReader.ReadPages(hsCtx, "src_other", g.hsDef.Datasets[0], ingest.PageOptions{}) {
		if se, ok := ingest.SourceErrorOf(err); !ok || se.Kind != ingest.SrcInternal {
			t.Fatalf("another source: %v", err)
		}
	}
	for _, err := range g.hsReader.ReadPages(hsCtx, hsSource, ingest.Dataset{ID: "ds_file"}, ingest.PageOptions{}) {
		if kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Fatalf("a dataset with no request: %v", err)
		}
	}
	off := hsNew(t, hsDoc("", "", "", ""), hsOne)
	off.hsReader.cfg.Source.Disabled = true
	for _, err := range off.hsReader.ReadPages(hsCtx, hsSource, off.hsDef.Datasets[0], ingest.PageOptions{}) {
		if kernel.CodeOf(err) != kernel.CodeNotFound {
			t.Fatalf("a source that is turned off: %v", err)
		}
	}
	if len(off.hsSrv.hsSeen) != 0 || off.hsSrv.hsClients != 0 {
		t.Fatal("a source that is turned off was reached")
	}
}

// hsResolver answers every name with the same addresses, so that no name is ever looked up.
type hsResolver struct{ hsAddrs []netip.Addr }

func (r hsResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.hsAddrs, nil
}

// [REQ-665]
func TestThroughTheRealEgressClientAnAddressThePolicyRefusesIsAConfigurationErrorAndNothingIsSent(t *testing.T) {
	for _, addr := range []string{"169.254.169.254", "0.0.0.0", "224.0.0.1"} {
		d, err := egress.New(egress.Config{Resolver: hsResolver{hsAddrs: []netip.Addr{netip.MustParseAddr(addr)}}})
		if err != nil {
			t.Fatal(err)
		}
		g := hsNew(t, hsDoc(hsBearer, "", "", ""), hsOne)
		cfg := g.hsReader.cfg
		cfg.Connect = ViaDialer(d)
		r, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, err := range r.ReadPages(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PageOptions{}) {
			se, ok := ingest.SourceErrorOf(err)
			if !ok || se.Kind != ingest.SrcPolicyDenied || se.Class != ingest.CheckConfig || strings.Contains(se.Message, addr) || strings.Contains(se.Message, hsTokenValue) {
				t.Fatalf("%s: %v", addr, err)
			}
		}
	}
}

// [REQ-665]
func TestACertificateAuthorityThatCannotBeUsedIsAConfigurationError(t *testing.T) {
	d, err := egress.New(egress.Config{Resolver: hsResolver{hsAddrs: []netip.Addr{netip.MustParseAddr("10.1.2.3")}}})
	if err != nil {
		t.Fatal(err)
	}
	g := hsNew(t, hsDoc("", "", "", ""), hsOne)
	cfg := g.hsReader.cfg
	cfg.Connect = ViaDialer(d)
	cfg.Source.Connection.CAPEM = "this is not a certificate"
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range r.ReadPages(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PageOptions{}) {
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != ingest.SrcTLSRejected || strings.Contains(se.Message, "this is not") {
			t.Fatalf("%v", err)
		}
	}
}
