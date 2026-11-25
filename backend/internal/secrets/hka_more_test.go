package secrets_test

import (
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-860]
func TestHkaWhatGoesWrongInTheStoreOrTheKeyIsAnErrorAndNeverAProof(t *testing.T) {
	flaky := &faultyStore{MemoryStore: secrets.NewMemoryStore()}
	wk := secrets.GenerateWebhookKey(kernel.NewSeededBytes(31))
	clock := kernel.NewManualClock(epoch)
	svc, err := secrets.New(secrets.Config{Store: flaky, Keys: secrets.NoKey(), WebhookKeys: secrets.StaticKey(wk), Clock: clock, Bytes: kernel.NewSeededBytes(7), VerifyCacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC)
	if err != nil {
		t.Fatal(err)
	}
	sig := [][]byte{hkaSign(issued.Value, "m", "1", "{}")}
	flaky.get = true
	if ok, err := svc.Verifier().VerifyHMAC(ctx, "src_a", "alerts", []byte("m.1.{}"), sig); ok || err == nil {
		t.Errorf("a store that is down: %v %v", ok, err)
	}
	if _, err := svc.IssueWebhookSecret(ctx, "src_a", "poke", secrets.WebhookAuthBasic); err == nil {
		t.Error("an issue went through a store that is down")
	}
	if _, err := svc.RotateWebhookSecret(ctx, "src_a", "alerts"); err == nil {
		t.Error("a rotation went through a store that is down")
	}
	flaky.get, flaky.put = false, true
	if _, err := svc.IssueWebhookSecret(ctx, "src_a", "poke", secrets.WebhookAuthBasic); err == nil {
		t.Error("an issue that could not be written was taken for done")
	}
	if _, err := svc.RotateWebhookSecret(ctx, "src_a", "alerts"); err == nil {
		t.Error("a rotation that could not be written was taken for done")
	}
	if _, err := svc.RetireWebhookVersion(ctx, "src_a", "alerts", 1); err == nil {
		t.Error("a retirement that could not be written was taken for done")
	}
	flaky.put = false
	if ok, err := svc.Verifier().VerifyHMAC(ctx, "src_a", "alerts", []byte("m.1.{}"), sig); !ok || err != nil {
		t.Errorf("the check after the store came back: %v %v", ok, err)
	}
	// A cache time that is longer than a minute is a minute.
	clock.Set(epoch.Add(61 * time.Second))
	flaky.get = true
	if ok, err := svc.Verifier().VerifyHMAC(ctx, "src_a", "alerts", []byte("m.1.{}"), sig); ok || err == nil {
		t.Errorf("after a minute the record is read again, and the store is down: %v %v", ok, err)
	}
}

// [REQ-860]
func TestHkaAProgramWithoutAUsableWebhookKeyIssuesAndChecksNothing(t *testing.T) {
	for name, keys := range map[string]secrets.KeyProvider{"none": nil, "a provider with no key": nilKeys{}, "a master key": secrets.StaticKey(newKey(5)), "no key": secrets.NoKey()} {
		svc, err := secrets.New(secrets.Config{Store: secrets.NewMemoryStore(), Keys: secrets.NoKey(), WebhookKeys: keys, Clock: kernel.NewManualClock(epoch), Bytes: kernel.NewSeededBytes(7)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC); err == nil {
			t.Errorf("%s: a secret was issued", name)
		}
		if _, err := svc.RotateWebhookSecret(ctx, "src_a", "alerts"); err == nil {
			t.Errorf("%s: a secret was rotated", name)
		}
		if _, err := svc.RetireWebhookVersion(ctx, "src_a", "alerts", 1); err == nil {
			t.Errorf("%s: a version was retired", name)
		}
	}
}

// [REQ-860]
func TestHkaARetirementNamesAVersionThatIsThereAndNotRetiredAndTheNamesAreIDs(t *testing.T) {
	r := hkaNew(t)
	if _, err := r.svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.RetireWebhookVersion(ctx, "src_a", "alerts", 9); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Errorf("a version that is not there: %v", err)
	}
	if _, err := r.svc.RetireWebhookVersion(ctx, "src_a", "alerts", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.RetireWebhookVersion(ctx, "src_a", "alerts", 1); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Errorf("a version that is retired: %v", err)
	}
	for _, f := range []func() error{
		func() error { _, err := r.svc.RotateWebhookSecret(ctx, "Src", "alerts"); return err },
		func() error { _, err := r.svc.RetireWebhookVersion(ctx, "src_a", "A b", 1); return err },
		func() error { return r.svc.DeleteWebhookSecret(ctx, "", "alerts") },
	} {
		if kernel.CodeOf(f()) != kernel.CodeInvalidValue {
			t.Error("a name that is not an id was taken")
		}
	}
	if err := r.svc.DeleteWebhookSecret(ctx, "src_a", "nothing"); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Errorf("a delete of a secret that is not there: %v", err)
	}
}

// [REQ-860]
func TestHkaTheDummyCheckAnswersNothingAndTheListShowsTheSecretsOfTheWebhooksWithoutValues(t *testing.T) {
	r := hkaNew(t)
	r.svc.Verifier().Dummy([]byte("a.b.c"), [][]byte{[]byte("x")}, []byte("t"))
	issued, err := r.svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC)
	if err != nil {
		t.Fatal(err)
	}
	list, err := r.svc.Describer().List(ctx)
	if err != nil || len(list) != 1 || list[0].Kind != secrets.KindHMACKey || len(list[0].FieldNames) != 1 || list[0].Scope[0] != "src_a" {
		t.Fatalf("the list is %+v %v", list, err)
	}
	if got := issued.Meta.ID; got != secrets.SecretIDFor("src_a", "alerts") {
		t.Errorf("the id is %s", got)
	}
	if _, ok := secrets.KindOfWebhookAuth("digest"); ok {
		t.Error("a type of proof that is none of the four has a kind")
	}
	if !secrets.IsReceiveOnly(secrets.KindWebhookToken) || secrets.IsReceiveOnly(secrets.KindToken) {
		t.Error("the kinds of the webhooks are not told from the kinds of the sources")
	}
}
