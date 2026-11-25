package kgread_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// [REQ-314] [REQ-862]
// The refused tokens of one IPv6 network are one caller for the limiter (its /64 is the key), and the audit line keeps the
// whole address of each call, so the log says which address it was.
func TestXffGateCountsAnIPv6NetworkAsOneCallerAndLogsEachAddress(t *testing.T) {
	limiter := &blockList{blocked: map[string]bool{}, fails: map[string]int{}}
	authn := answerAuthn{err: kernel.Errorf(access.CodeTokenInvalid, "no such token")}
	g, logs := newGate(authn, limiter)
	ctx := context.Background()
	first := kgread.Caller{Key: "2001:db8:0:1::5", Path: "/v1/kgs", Trace: "trace-a"}
	second := kgread.Caller{Key: "2001:db8:0:1::9", Path: "/v1/kgs", Trace: "trace-b"}
	if _, p := g.Authenticate(ctx, first, "Bearer nope"); p == nil || p.Code != "auth.token_invalid" {
		t.Fatalf("the first refusal: %+v", p)
	}
	if _, p := g.Authenticate(ctx, second, "Bearer nope"); p == nil || p.Code != "auth.token_invalid" {
		t.Fatalf("the second refusal: %+v", p)
	}
	if limiter.fails["2001:db8:0:1::/64"] != 2 || len(limiter.fails) != 1 {
		t.Fatalf("the failures of one /64 are counted together: %v", limiter.fails)
	}
	if _, p := g.Authenticate(ctx, kgread.Caller{Key: "2001:db8:0:1::77"}, "Bearer nope"); p == nil || p.Code != "rate_limited.requests" {
		t.Fatalf("a third address of the same /64 is blocked: %+v", p)
	}
	for _, want := range []string{`"remote":"2001:db8:0:1::5"`, `"remote":"2001:db8:0:1::9"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("the audit line lacks %s: %s", want, logs.String())
		}
	}
}
