package pgsource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-826] A key that is not pinned is worded as that, and a source that pins is never told that its chain, its name or its
// dates were wrong: they are not looked at.
func TestPgsHxbThePinnedFailureIsWordedAsOne(t *testing.T) {
	pins := []string{"sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	mismatch := fmt.Errorf("driver: %w", &egress.PinMismatchError{Presented: "sha256/x"})
	generic := &egress.Error{Kind: egress.KindTLS, Reason: "the TLS handshake failed"}
	for name, c := range map[string]struct {
		err        error
		pins       []string
		wantPinned bool
	}{
		"a mismatch, pinned":            {mismatch, pins, true},
		"a mismatch, no pins in sight":  {mismatch, nil, true},
		"another failure, pinned":       {generic, pins, true},
		"another failure, not pinned":   {generic, nil, false},
		"a failure of the chain":        {&egress.Error{Kind: egress.KindTLS, Reason: "the certificate is not signed by a trusted authority"}, nil, false},
		"a policy refusal, pinned":      {&egress.Error{Kind: egress.KindPolicy, Reason: "no"}, pins, false},
		"a connection failure, pinned ": {errors.New("boom"), pins, false},
	} {
		out := classifyPinned(context.Background(), c.err, c.pins)
		se, ok := ingest.SourceErrorOf(out)
		if !ok {
			t.Errorf("%s: %v", name, out)
			continue
		}
		mentionsPins := strings.Contains(se.Message, "pinned in the definition") || strings.Contains(se.Message, "pins the key")
		if mentionsPins != c.wantPinned {
			t.Errorf("%s: %q", name, se.Message)
		}
		if c.wantPinned && (se.Kind != ingest.SrcTLSRejected || strings.Contains(se.Message, "expired") || strings.Contains(se.Message, "host name")) {
			t.Errorf("%s: %q", name, se.Message)
		}
	}
}
