package secretsfile

import (
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-860]
// The secrets of the receiving webhooks live in the same file as the secrets of the sources, are kept as ciphertext and
// a hash, survive a restart, and are checked by a program that has the webhook key and no master key.
func TestHkaTheSecretsOfTheWebhooksSurviveARestartAndAreCheckedWithTheWebhookKeyAlone(t *testing.T) {
	disk := NewMemDisk()
	wk := secrets.GenerateWebhookKey(kernel.NewSeededBytes(3))
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := func(st secrets.Store, master secrets.KeyProvider) secrets.Config {
		return secrets.Config{Store: st, Keys: master, WebhookKeys: secrets.StaticKey(wk), Clock: clock, Bytes: kernel.NewSeededBytes(9)}
	}
	writer, err := secrets.New(cfg(open(t, disk), secrets.StaticKey(key())))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Create(ctx, secrets.CreateInput{ID: "db_main", Kind: secrets.KindPassword, Fields: fields(testPassword), Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}}); err != nil {
		t.Fatal(err)
	}
	signed, err := writer.IssueWebhookSecret(ctx, "crm", "alerts", secrets.WebhookAuthHMAC)
	if err != nil {
		t.Fatal(err)
	}
	poke, err := writer.IssueWebhookSecret(ctx, "crm", "poke", secrets.WebhookAuthURLToken)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := disk.Bytes(FileName)
	for _, leak := range []string{signed.Value, strings.TrimPrefix(signed.Value, "whsec_"), poke.Value} {
		if strings.Contains(string(raw), leak) {
			t.Fatal("the file holds a value in the clear")
		}
	}
	// The process of the API: the webhook key and no master key.
	api, err := secrets.New(cfg(open(t, disk), secrets.NoKey()))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := api.Verifier().VerifyToken(ctx, "crm", "poke", []byte(poke.Value)); !ok || err != nil {
		t.Errorf("the token after a restart: %v %v", ok, err)
	}
	if _, err := api.Open(ctx, "db_main", okUse); kernel.CodeOf(err) != secrets.CodeMasterKeyMissing {
		t.Errorf("the process of the webhooks opened the secret of a connection: %v", err)
	}
	if _, err := api.Open(ctx, secrets.SecretIDFor("crm", "alerts"), okUse); kernel.CodeOf(err) != secrets.CodeReceiveOnly {
		t.Errorf("the key of a webhook was opened for a use: %v", err)
	}
}
