package secrets_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-860]
func TestHkaTheValueThatWasIssuedIsNotPrintedByAnyOfTheWaysAValueIsPrinted(t *testing.T) {
	r := hkaNew(t)
	for _, auth := range []string{secrets.WebhookAuthHMAC, secrets.WebhookAuthHeaderToken} {
		issued, err := r.svc.IssueWebhookSecret(ctx, "src_a", "w_"+auth, auth)
		if err != nil {
			t.Fatal(err)
		}
		raw := strings.TrimPrefix(issued.Value, "whsec_")
		logs := &bytes.Buffer{}
		slog.New(slog.NewJSONHandler(logs, nil)).Info("issued", "result", issued)
		text := slog.New(slog.NewTextHandler(logs, nil))
		text.Info("issued", slog.Any("result", issued))
		encoded, err := json.Marshal(issued)
		if err != nil {
			t.Fatal(err)
		}
		pointer := &issued
		shown := []string{
			fmt.Sprintf("%v", issued), fmt.Sprintf("%+v", issued), fmt.Sprintf("%#v", issued), fmt.Sprintf("%s", issued),
			fmt.Sprintf("%v", pointer), fmt.Sprint(issued), logs.String(), string(encoded),
		}
		for i, out := range shown {
			if strings.Contains(out, raw) {
				t.Errorf("%s, way %d: the value is in %q", auth, i, out)
			}
		}
		if !strings.Contains(shown[0], issued.Meta.ID) {
			t.Errorf("%s: the print does not name the secret: %q", auth, shown[0])
		}
	}
}

// [REQ-860]
func TestHkaTheStoreOfTheSourcesKeepsNoWebhookKindAndTheStoreOfTheWebhooksNoOtherKind(t *testing.T) {
	r := hkaNew(t)
	if _, err := r.svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Create(ctx, secrets.CreateInput{ID: "conn_a", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken})}); err != nil {
		t.Fatal(err)
	}
	hookRecs, _ := r.hooks.List(ctx)
	srcRecs, _ := r.store.List(ctx)
	if len(hookRecs) != 1 || len(srcRecs) != 1 {
		t.Fatalf("the records are %d of the webhooks and %d of the sources", len(hookRecs), len(srcRecs))
	}
	if err := r.store.Put(ctx, hookRecs[0]); err == nil {
		t.Error("the store of the sources took the record of a webhook")
	}
	if err := r.hooks.Put(ctx, srcRecs[0]); err == nil {
		t.Error("the store of the webhooks took the record of a source")
	}
	if _, err := secrets.OpenMemoryStore(hookRecs, nil); err == nil {
		t.Error("a store of the sources was opened with the record of a webhook")
	}
	if _, err := secrets.OpenWebhookMemoryStore(srcRecs, nil); err == nil {
		t.Error("a store of the webhooks was opened with the record of a source")
	}
	if err := hookRecs[0].Validate(); err == nil {
		t.Error("Validate, the rule that every version of the program reads, accepts the kind of a webhook")
	}
	// A program that has no store for the webhooks cannot issue or check one, and says why.
	bare, err := secrets.New(secrets.Config{Store: secrets.NewMemoryStore(), Keys: secrets.NoKey(), WebhookKeys: secrets.StaticKey(r.hookKy), Clock: r.clock, Bytes: kernel.NewSeededBytes(2)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.IssueWebhookSecret(ctx, "src_a", "x", secrets.WebhookAuthBasic); kernel.CodeOf(err) != secrets.CodeWebhookStoreMissing {
		t.Errorf("an issue with no store: %v", err)
	}
	if ok, err := bare.Verifier().VerifyToken(ctx, "src_a", "x", []byte("t")); ok || kernel.CodeOf(err) != secrets.CodeWebhookStoreMissing {
		t.Errorf("a check with no store: %v %v", ok, err)
	}
	if _, err := bare.ListWebhookSecrets(ctx); kernel.CodeOf(err) != secrets.CodeWebhookStoreMissing {
		t.Errorf("a list with no store: %v", err)
	}
}

// [REQ-860]
func TestHkaASecretOfASourceCannotTakeTheIDOfAWebhookAndTheWebhookDeleteTouchesOnlyWebhooks(t *testing.T) {
	r := hkaNew(t)
	for _, id := range []string{"wh_", "wh_0123", secrets.SecretIDFor("src_a", "alerts")} {
		_, err := r.svc.Create(ctx, secrets.CreateInput{ID: id, Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken})})
		if kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("a secret of a source under %q: %v", id, err)
		}
	}
	if _, err := r.svc.Create(ctx, secrets.CreateInput{ID: "who_is", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken})}); err != nil {
		t.Errorf("an id that only looks like the prefix: %v", err)
	}
}

type hkaOpenedCounter struct{ opened int }

func (c *hkaOpenedCounter) Event(name, _ string) {
	if name == secrets.EventSecretOpened {
		c.opened++
	}
}

// [REQ-860]
// The key of a webhook is kept in memory for a minute at most, whatever time is asked for, and for the time that is asked
// when that is shorter. The audit line of a fill of the cache is what shows when the key was opened again.
func TestHkaTheKeyIsKeptInMemoryForAMinuteAtMostWhateverTimeIsAskedFor(t *testing.T) {
	for name, c := range map[string]struct {
		ttl            time.Duration
		at             time.Duration // a check at this time after the first one is the second fill
		before, opened int
	}{
		"an hour is asked for":         {time.Hour, 61 * time.Second, 1, 2},
		"nothing is asked for":         {0, 61 * time.Second, 1, 2},
		"ten seconds are asked for":    {10 * time.Second, 11 * time.Second, 1, 2},
		"a minute, and a second under": {time.Minute, 59 * time.Second, 1, 1},
	} {
		seen := &hkaOpenedCounter{}
		clock := kernel.NewManualClock(epoch)
		svc, err := secrets.New(secrets.Config{Store: secrets.NewMemoryStore(), WebhookStore: secrets.NewWebhookMemoryStore(), Keys: secrets.NoKey(),
			WebhookKeys: secrets.StaticKey(secrets.GenerateWebhookKey(kernel.NewSeededBytes(31))), Clock: clock, Bytes: kernel.NewSeededBytes(7), Observer: seen, VerifyCacheTTL: c.ttl})
		if err != nil {
			t.Fatal(err)
		}
		issued, err := svc.IssueWebhookSecret(ctx, "src_a", "poke", secrets.WebhookAuthHeaderToken)
		if err != nil {
			t.Fatal(err)
		}
		check := func() {
			if ok, err := svc.Verifier().VerifyToken(ctx, "src_a", "poke", []byte(issued.Value)); !ok || err != nil {
				t.Fatalf("%s: %v %v", name, ok, err)
			}
		}
		check()
		check()
		if seen.opened != c.before {
			t.Errorf("%s: the key was opened %d times for two checks at once", name, seen.opened)
		}
		clock.Advance(c.at)
		check()
		if seen.opened != c.opened {
			t.Errorf("%s: the key was opened %d times, want %d", name, seen.opened, c.opened)
		}
	}
}

// [REQ-860]
// A secret of one kind proves nothing of the other kind, even for a caller who knows how the stored form is made: the hash
// that a token webhook keeps is, as bytes, a key of the right size, and a signature made with it is not accepted.
func TestHkaTheStoredHashOfATokenIsNotASigningKeyAndTheKindOfASecretIsKept(t *testing.T) {
	r := hkaNew(t)
	token, err := r.svc.IssueWebhookSecret(ctx, "src_a", "poke", secrets.WebhookAuthHeaderToken)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(token.Value))
	mac := hmac.New(sha256.New, sum[:])
	mac.Write([]byte("m.1.{}"))
	if ok, err := r.svc.Verifier().VerifyHMAC(ctx, "src_a", "poke", []byte("m.1.{}"), [][]byte{mac.Sum(nil)}); ok || err != nil {
		t.Errorf("a signature made with the hash of a token: %v %v", ok, err)
	}
	signed, err := r.svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.svc.Verifier().VerifyToken(ctx, "src_a", "alerts", []byte(signed.Value)); ok {
		t.Error("the signing key presented as a token")
	}
	if ok, _ := r.svc.Verifier().VerifyToken(ctx, "src_a", "alerts", []byte(strings.TrimPrefix(signed.Value, "whsec_"))); ok {
		t.Error("the signing key, without its prefix, presented as a token")
	}
}
