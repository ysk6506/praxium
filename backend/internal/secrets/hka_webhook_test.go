package secrets_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The secrets of the receiving webhooks (ADR-0107): made by the server, checked by the Verifier, never opened for a use.

type hkaEvents struct{ names []string }

func (e *hkaEvents) Event(name, _ string) { e.names = append(e.names, name) }

func hkaCount(e *hkaEvents, name string) int {
	n := 0
	for _, x := range e.names {
		if x == name {
			n++
		}
	}
	return n
}

type hkaRig struct {
	svc    *secrets.Service
	store  *secrets.MemoryStore
	clock  *kernel.ManualClock
	hookKy *secrets.MasterKey
	events *hkaEvents
}

func hkaNew(t *testing.T) *hkaRig {
	t.Helper()
	store := secrets.NewMemoryStore()
	clock := kernel.NewManualClock(epoch)
	events := &hkaEvents{}
	wk := secrets.GenerateWebhookKey(kernel.NewSeededBytes(31))
	svc, err := secrets.New(secrets.Config{
		Store: store, Keys: secrets.StaticKey(newKey(99)), WebhookKeys: secrets.StaticKey(wk),
		Clock: clock, Bytes: kernel.NewSeededBytes(7), Observer: events,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &hkaRig{svc: svc, store: store, clock: clock, hookKy: wk, events: events}
}

func hkaSign(secret, id, ts, body string) []byte {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		panic(err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + body))
	return mac.Sum(nil)
}

// [REQ-860]
func TestHkaAnIssuedSignedSecretChecksASignatureAndNothingElse(t *testing.T) {
	r := hkaNew(t)
	issued, err := r.svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issued.Value, "whsec_") || issued.Meta.Kind != secrets.KindHMACKey || issued.Meta.Version != 1 {
		t.Fatalf("the issue is %+v", issued.Meta)
	}
	v := r.svc.Verifier()
	good := hkaSign(issued.Value, "msg_1", "1", "{}")
	bad := hkaSign(issued.Value, "msg_1", "1", "{ }")
	for _, c := range []struct {
		name string
		sigs [][]byte
		want bool
	}{{"the signature", [][]byte{good}, true}, {"among others", [][]byte{bad, good}, true}, {"another body", [][]byte{bad}, false}, {"none", nil, false}} {
		got, err := v.VerifyHMAC(ctx, "src_a", "alerts", []byte("msg_1.1.{}"), c.sigs)
		if err != nil || got != c.want {
			t.Errorf("%s: %v %v, want %v", c.name, got, err, c.want)
		}
	}
	if ok, _ := v.VerifyHMAC(ctx, "src_b", "alerts", []byte("msg_1.1.{}"), [][]byte{good}); ok {
		t.Error("the key of a webhook of one source proved a call of another")
	}
	if ok, _ := v.VerifyToken(ctx, "src_a", "alerts", []byte(issued.Value)); ok {
		t.Error("a signed webhook took a token")
	}
}

// [REQ-860]
func TestHkaATokenIsKeptAsItsHashAndIsCheckedWithoutBeingKept(t *testing.T) {
	r := hkaNew(t)
	issued, err := r.svc.IssueWebhookSecret(ctx, "src_a", "poke", secrets.WebhookAuthHeaderToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.Value) < 40 || issued.Meta.Kind != secrets.KindWebhookToken {
		t.Fatalf("the token is %d characters of kind %s", len(issued.Value), issued.Meta.Kind)
	}
	recs, _ := r.store.List(ctx)
	if len(recs) != 1 || strings.Contains(string(recs[0].Versions[0].Value.Data), issued.Value) {
		t.Fatal("the store holds the token")
	}
	v := r.svc.Verifier()
	for _, c := range []struct {
		name, token string
		want        bool
	}{{"the token", issued.Value, true}, {"a longer one", issued.Value + "x", false}, {"a shorter one", issued.Value[1:], false}, {"empty", "", false}, {"a long one", strings.Repeat("a", 100000), false}} {
		got, err := v.VerifyToken(ctx, "src_a", "poke", []byte(c.token))
		if err != nil || got != c.want {
			t.Errorf("%s: %v %v, want %v", c.name, got, err, c.want)
		}
	}
	if ok, _ := v.VerifyHMAC(ctx, "src_a", "poke", []byte("x"), [][]byte{[]byte("y")}); ok {
		t.Error("a token webhook took a signature")
	}
}

// [REQ-860]
func TestHkaAKindOfTheWebhooksIsNeverOpenedForAUseAndAKindOfTheSourcesNeverChecked(t *testing.T) {
	r := hkaNew(t)
	if _, err := r.svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC); err != nil {
		t.Fatal(err)
	}
	id := secrets.SecretIDFor("src_a", "alerts")
	use := secrets.Use{SourceID: "src_a", Host: "db.example.test", Port: 5432, Purpose: secrets.PurposeRun}
	if _, err := r.svc.Open(ctx, id, use); kernel.CodeOf(err) != secrets.CodeReceiveOnly {
		t.Errorf("the open of the key of a webhook: %v", err)
	}
	if _, err := r.svc.AddVersion(ctx, id, secrets.NewFields(map[string]string{"token": "x"})); kernel.CodeOf(err) != secrets.CodeReceiveOnly {
		t.Errorf("a version by the master path: %v", err)
	}
	if _, err := r.svc.SetScope(ctx, id, []string{"src_b"}); kernel.CodeOf(err) != secrets.CodeReceiveOnly {
		t.Errorf("a scope by the master path: %v", err)
	}
	if _, err := r.svc.RetireVersion(ctx, id, 1); kernel.CodeOf(err) != secrets.CodeReceiveOnly {
		t.Errorf("a retirement by the master path: %v", err)
	}
	if _, err := r.svc.Create(ctx, secrets.CreateInput{ID: "x1", Kind: secrets.KindHMACKey, Fields: secrets.NewFields(map[string]string{"key": "abc"})}); err == nil {
		t.Error("a secret of a webhook was made as the secret of a source")
	}
	// A secret of a connection that is put under the id of a webhook, with the source in its scope, is not a proof.
	other := secrets.SecretIDFor("src_a", "other")
	if _, err := r.svc.Create(ctx, secrets.CreateInput{ID: other, Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken}),
		Scope: []string{"src_a"}, AllowedHosts: []secrets.Address{dbAddress}}); err != nil {
		t.Fatal(err)
	}
	ok, err := r.svc.Verifier().VerifyToken(ctx, "src_a", "other", []byte(testToken))
	if ok || err != nil {
		t.Errorf("the token of a connection proved a webhook: %v %v", ok, err)
	}
	sum := sha256.Sum256([]byte(testToken))
	ok, _ = r.svc.Verifier().VerifyHMAC(ctx, "src_a", "other", []byte("x"), [][]byte{sum[:]})
	if ok {
		t.Error("a secret of a connection proved a signature")
	}
}

// [REQ-860]
func TestHkaTheKeysAreTwoAndOneDoesNotStandInForTheOther(t *testing.T) {
	r := hkaNew(t)
	master := newKey(99)
	if _, err := secrets.ParseWebhookKey(master.Encode()); kernel.CodeOf(err) != secrets.CodeMasterKeyInvalid {
		t.Errorf("a master key file read as a webhook key: %v", err)
	}
	if _, err := secrets.ParseMasterKey(r.hookKy.Encode()); kernel.CodeOf(err) != secrets.CodeMasterKeyInvalid {
		t.Errorf("a webhook key file read as a master key: %v", err)
	}
	back, err := secrets.ParseWebhookKey(r.hookKy.Encode())
	if err != nil || back.ID() != r.hookKy.ID() || !strings.HasPrefix(back.ID(), "wk-") || strings.HasPrefix(master.ID(), "wk-") {
		t.Errorf("the webhook key is read back as %v (%v)", back, err)
	}
	asMaster, err := secrets.New(secrets.Config{Store: r.store, Keys: secrets.StaticKey(r.hookKy), Clock: r.clock, Bytes: kernel.NewSeededBytes(1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := asMaster.Create(ctx, secrets.CreateInput{ID: "x1", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": "t"})}); kernel.CodeOf(err) != secrets.CodeMasterKeyInvalid {
		t.Errorf("the webhook key used as the master key: %v", err)
	}
	if _, err := r.svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC); err != nil {
		t.Fatal(err)
	}
	onlyMaster, err := secrets.New(secrets.Config{Store: r.store, Keys: secrets.StaticKey(master), Clock: r.clock, Bytes: kernel.NewSeededBytes(2)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := onlyMaster.IssueWebhookSecret(ctx, "src_a", "poke", secrets.WebhookAuthBasic); kernel.CodeOf(err) != secrets.CodeWebhookKeyMissing {
		t.Errorf("an issue without the webhook key: %v", err)
	}
	if ok, err := onlyMaster.Verifier().VerifyToken(ctx, "src_a", "alerts", []byte("x")); ok || err == nil {
		t.Errorf("a check without the webhook key: %v %v", ok, err)
	}
	onlyHook, err := secrets.New(secrets.Config{Store: r.store, Keys: secrets.NoKey(), WebhookKeys: secrets.StaticKey(r.hookKy), Clock: r.clock, Bytes: kernel.NewSeededBytes(3)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := onlyHook.Create(ctx, secrets.CreateInput{ID: "x1", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": "t"})}); kernel.CodeOf(err) != secrets.CodeMasterKeyMissing {
		t.Errorf("the process of the webhooks put in a secret of a source: %v", err)
	}
}

// [REQ-860]
func TestHkaTwoVersionsCheckWhileTheOldOneIsNotRetiredAndARetiredOneAtOnceNoLonger(t *testing.T) {
	r := hkaNew(t)
	first, err := r.svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC)
	if err != nil {
		t.Fatal(err)
	}
	v := r.svc.Verifier()
	check := func(secret string) bool {
		ok, err := v.VerifyHMAC(ctx, "src_a", "alerts", []byte("m.1.{}"), [][]byte{hkaSign(secret, "m", "1", "{}")})
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !check(first.Value) {
		t.Fatal("the first version does not check")
	}
	second, err := r.svc.RotateWebhookSecret(ctx, "src_a", "alerts")
	if err != nil || second.Meta.Version != 2 || second.Value == first.Value {
		t.Fatalf("the rotation: %+v %v", second.Meta, err)
	}
	if !check(first.Value) || !check(second.Value) {
		t.Error("the two versions do not both check in the time they overlap")
	}
	if _, err := r.svc.RetireWebhookVersion(ctx, "src_a", "alerts", 1); err != nil {
		t.Fatal(err)
	}
	// The cache of the minute is not what is trusted: the record has changed, and it is read again at once.
	if check(first.Value) || !check(second.Value) {
		t.Error("a retired version still checks, or the new one does not")
	}
	if _, err := r.svc.RetireWebhookVersion(ctx, "src_a", "alerts", 2); err != nil {
		t.Fatal(err)
	}
	if check(second.Value) {
		t.Error("a secret with every version retired still checks")
	}
}

// [REQ-860]
func TestHkaAnOpenedKeyStaysInMemoryAMinuteAtMostAndIsAuditedWhenItIsOpened(t *testing.T) {
	r := hkaNew(t)
	issued, err := r.svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC)
	if err != nil {
		t.Fatal(err)
	}
	v := r.svc.Verifier()
	sigs := [][]byte{hkaSign(issued.Value, "m", "1", "{}")}
	for i := 0; i < 5; i++ {
		if ok, _ := v.VerifyHMAC(ctx, "src_a", "alerts", []byte("m.1.{}"), sigs); !ok {
			t.Fatal("a good signature was refused")
		}
	}
	if got := hkaCount(r.events, secrets.EventSecretOpened); got != 1 {
		t.Errorf("%d audit lines for five checks in one minute, want one", got)
	}
	if got := hkaCount(r.events, secrets.EventVerifyAccepted); got != 5 {
		t.Errorf("%d accepted checks counted, want five", got)
	}
	_, _ = v.VerifyHMAC(ctx, "src_a", "alerts", []byte("m.1.{ }"), sigs)
	if hkaCount(r.events, secrets.EventVerifyRefused) != 1 {
		t.Error("a refusal was not counted")
	}
	if _, err := r.clock.Advance(59 * time.Second); err != nil {
		t.Fatal(err)
	}
	_, _ = v.VerifyHMAC(ctx, "src_a", "alerts", []byte("m.1.{}"), sigs)
	if got := hkaCount(r.events, secrets.EventSecretOpened); got != 1 {
		t.Errorf("the key was opened again after 59 seconds (%d lines)", got)
	}
	if _, err := r.clock.Advance(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	_, _ = v.VerifyHMAC(ctx, "src_a", "alerts", []byte("m.1.{}"), sigs)
	if got := hkaCount(r.events, secrets.EventSecretOpened); got != 2 {
		t.Errorf("the key was not opened again after a minute (%d lines)", got)
	}
}

// [REQ-860]
func TestHkaADeletedSecretStopsCheckingAtOnceAndAnIssueTwiceIsAConflict(t *testing.T) {
	r := hkaNew(t)
	issued, err := r.svc.IssueWebhookSecret(ctx, "src_a", "poke", secrets.WebhookAuthURLToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.IssueWebhookSecret(ctx, "src_a", "poke", secrets.WebhookAuthURLToken); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Errorf("a second issue: %v", err)
	}
	if ok, _ := r.svc.Verifier().VerifyToken(ctx, "src_a", "poke", []byte(issued.Value)); !ok {
		t.Fatal("the token does not check")
	}
	if err := r.svc.DeleteWebhookSecret(ctx, "src_a", "poke"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.svc.Verifier().VerifyToken(ctx, "src_a", "poke", []byte(issued.Value)); ok {
		t.Error("a deleted secret still checks")
	}
	if _, err := r.svc.RotateWebhookSecret(ctx, "src_a", "poke"); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Errorf("a rotation of a secret that is not there: %v", err)
	}
	if _, err := r.svc.IssueWebhookSecret(ctx, "src_a", "poke", "digest"); err == nil {
		t.Error("a type of proof that is none of the four was issued")
	}
	if _, err := r.svc.IssueWebhookSecret(ctx, "Src A", "poke", secrets.WebhookAuthBasic); err == nil {
		t.Error("an id that is not an id was issued")
	}
}

// [REQ-860]
func TestHkaTwoWebhooksOfOneSourceAndTwoSourcesHaveTwoSecretIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range [][2]string{{"src_a", "alerts"}, {"src_a", "poke"}, {"src_b", "alerts"}, {"src_a_alerts", ""}, {"src_a", "x_alerts"}, {"src", "a_alerts"}} {
		id := secrets.SecretIDFor(p[0], p[1])
		if seen[id] || secrets.ValidateID(id) != nil || len(id) > 64 {
			t.Errorf("the id %q of %v is repeated or invalid", id, p)
		}
		seen[id] = true
	}
}
