package secrets

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var hkbCtx = context.Background()

func hkbService(t *testing.T, hooks Store) (*Service, *kernel.ManualClock) {
	t.Helper()
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	svc, err := New(Config{Store: NewMemoryStore(), WebhookStore: hooks, Keys: NoKey(), WebhookKeys: StaticKey(GenerateWebhookKey(kernel.NewSeededBytes(4))), Clock: clock, Bytes: kernel.NewSeededBytes(5)})
	if err != nil {
		t.Fatal(err)
	}
	return svc, clock
}

func hkbAllZero(parts [][]byte) bool {
	for _, p := range parts {
		for _, b := range p {
			if b != 0 {
				return false
			}
		}
	}
	return len(parts) > 0
}

// [REQ-860]
func TestHkaTheKeyThatLeavesTheCacheIsOverwrittenAndACheckThatIsRunningIsNotHurtByIt(t *testing.T) {
	svc, clock := hkbService(t, NewWebhookMemoryStore())
	issued, err := svc.IssueWebhookSecret(hkbCtx, "src_a", "poke", WebhookAuthHeaderToken)
	if err != nil {
		t.Fatal(err)
	}
	id := SecretIDFor("src_a", "poke")
	check := func() bool {
		ok, err := svc.Verifier().VerifyToken(hkbCtx, "src_a", "poke", []byte(issued.Value))
		if err != nil {
			t.Error(err)
		}
		return ok
	}
	if !check() {
		t.Fatal("the token does not check")
	}
	old := svc.vf.entries[id]
	held := append([][]byte(nil), old.material...)
	if hkbAllZero(held) {
		t.Fatal("the entry is empty from the start")
	}
	// A minute later the entry is not used: it is replaced, and what it held is overwritten.
	clock.Set(clock.Now().Add(2 * DefaultVerifyCacheTTL))
	if !check() {
		t.Fatal("the token does not check after the entry was replaced")
	}
	if !hkbAllZero(held) {
		t.Error("the key material of the entry that left the cache was not overwritten")
	}
	// The whole cache is cleared: the same.
	now := svc.vf.entries[id]
	held = append([][]byte(nil), now.material...)
	svc.vf.mu.Lock()
	svc.vf.clear()
	svc.vf.mu.Unlock()
	if !hkbAllZero(held) || len(svc.vf.entries) != 0 {
		t.Error("clearing the cache left key material")
	}
	// Checks that run while entries are being replaced are all right (the race detector looks at this too).
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				if !check() {
					t.Error("a check failed while the cache was being cleared")
					return
				}
			}
		}()
	}
	for j := 0; j < 40; j++ {
		svc.vf.mu.Lock()
		svc.vf.clear()
		svc.vf.mu.Unlock()
	}
	wg.Wait()
}

// [REQ-860]
func TestHkaTheWebhookDeleteRefusesARecordThatIsNotTheSecretOfAWebhook(t *testing.T) {
	hooks := NewMemoryStore() // a store of the sources where a store of the webhooks belongs: a mistake of the assembly
	svc, _ := hkbService(t, hooks)
	source, err := New(Config{Store: NewMemoryStore(), Keys: StaticKey(GenerateMasterKey(kernel.NewSeededBytes(6))), Clock: kernel.NewManualClock(time.Unix(1e9, 0)), Bytes: kernel.NewSeededBytes(7)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Create(hkbCtx, CreateInput{ID: "conn_a", Kind: KindToken, Fields: NewFields(map[string]string{"token": "tt"})}); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := source.cfg.Store.Get(hkbCtx, "conn_a")
	rec.ID = SecretIDFor("src_a", "poke")
	if err := hooks.Put(hkbCtx, rec); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteWebhookSecret(hkbCtx, "src_a", "poke"); kernel.CodeOf(err) != CodeIntegrity {
		t.Errorf("the delete of a record that is not a webhook's: %v", err)
	}
	if _, found, _ := hooks.Get(hkbCtx, rec.ID); !found {
		t.Error("the record was deleted")
	}
}
