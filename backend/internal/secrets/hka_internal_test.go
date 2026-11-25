package secrets

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-860]
func TestHkaAWebhookSecretWhoseScopeLacksTheSourceProvesNothingEvenWhenItIsSealed(t *testing.T) {
	ctx := context.Background()
	store := NewWebhookMemoryStore()
	wk := GenerateWebhookKey(kernel.NewSeededBytes(4))
	svc, err := New(Config{Store: NewMemoryStore(), WebhookStore: store, Keys: NoKey(), WebhookKeys: StaticKey(wk), Clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), Bytes: kernel.NewSeededBytes(5)})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.IssueWebhookSecret(ctx, "src_a", "poke", WebhookAuthHeaderToken)
	if err != nil {
		t.Fatal(err)
	}
	rec, _, _ := store.Get(ctx, SecretIDFor("src_a", "poke"))
	for _, scope := range [][]string{{"src_b"}, {"src_a", "src_b"}, {}} {
		rec.Scope = scope
		if err := svc.bind(&rec, wk); err != nil {
			t.Fatal(err)
		}
		if err := store.Put(ctx, rec); err != nil {
			t.Fatal(err)
		}
		if ok, err := svc.Verifier().VerifyToken(ctx, "src_a", "poke", []byte(issued.Value)); ok || err != nil {
			t.Errorf("the scope %v: %v %v, want a refusal", scope, ok, err)
		}
	}
}

// [REQ-860]
func TestHkaARecordThatWasChangedByHandIsRefusedAndTheKeyOfAnotherStoreIsToo(t *testing.T) {
	ctx := context.Background()
	store := NewWebhookMemoryStore()
	wk := GenerateWebhookKey(kernel.NewSeededBytes(4))
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	svc, _ := New(Config{Store: NewMemoryStore(), WebhookStore: store, Keys: NoKey(), WebhookKeys: StaticKey(wk), Clock: clock, Bytes: kernel.NewSeededBytes(5)})
	issued, err := svc.IssueWebhookSecret(ctx, "src_a", "poke", WebhookAuthBasic)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := New(Config{Store: NewMemoryStore(), WebhookStore: store, Keys: NoKey(), WebhookKeys: StaticKey(GenerateWebhookKey(kernel.NewSeededBytes(8))), Clock: clock, Bytes: kernel.NewSeededBytes(5)})
	if ok, err := other.Verifier().VerifyToken(ctx, "src_a", "poke", []byte(issued.Value)); ok || err != nil {
		t.Errorf("another webhook key: %v %v", ok, err)
	}
	rec, _, _ := store.Get(ctx, SecretIDFor("src_a", "poke"))
	rec.Scope = []string{"src_a", "src_z"}
	_ = store.Put(ctx, rec)
	if ok, _ := svc.Verifier().VerifyToken(ctx, "src_a", "poke", []byte(issued.Value)); ok {
		t.Error("a record whose binding does not authenticate proved a call")
	}
}
