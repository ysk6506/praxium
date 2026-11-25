package secretsfile

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

func hkaOpenWebhooks(t *testing.T, disk Disk) *Store {
	t.Helper()
	st, err := OpenWebhooks(disk)
	if err != nil {
		t.Fatalf("open the webhooks: %v", err)
	}
	return st
}

// [REQ-860]
// The secrets of the receiving webhooks live in a file of their own, are kept as ciphertext and a hash, survive a
// restart, and are checked by a program that has the webhook file and the webhook key and neither the file of the
// sources nor the master key.
func TestHkaTheSecretsOfTheWebhooksSurviveARestartAndAreCheckedWithTheWebhookFileAndKeyAlone(t *testing.T) {
	disk := NewMemDisk()
	wk := secrets.GenerateWebhookKey(kernel.NewSeededBytes(3))
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := func(src, hooks secrets.Store, master secrets.KeyProvider) secrets.Config {
		return secrets.Config{Store: src, WebhookStore: hooks, Keys: master, WebhookKeys: secrets.StaticKey(wk), Clock: clock, Bytes: kernel.NewSeededBytes(9)}
	}
	writer, err := secrets.New(cfg(open(t, disk), hkaOpenWebhooks(t, disk), secrets.StaticKey(key())))
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
	for _, name := range []string{FileName, WebhookFileName} {
		raw, ok := disk.Bytes(name)
		if !ok {
			t.Fatalf("there is no %s", name)
		}
		for _, leak := range []string{signed.Value, strings.TrimPrefix(signed.Value, "whsec_"), poke.Value} {
			if strings.Contains(string(raw), leak) {
				t.Fatalf("%s holds a value in the clear", name)
			}
		}
	}
	// The process of the API: the webhook file and the webhook key, and no file of the sources and no master key.
	api, err := secrets.New(secrets.Config{Store: secrets.NewMemoryStore(), WebhookStore: hkaOpenWebhooks(t, disk), Keys: secrets.NoKey(), WebhookKeys: secrets.StaticKey(wk), Clock: clock, Bytes: kernel.NewSeededBytes(9)})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := api.Verifier().VerifyToken(ctx, "crm", "poke", []byte(poke.Value)); !ok || err != nil {
		t.Errorf("the token after a restart: %v %v", ok, err)
	}
	if _, err := api.Open(ctx, "db_main", okUse); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Errorf("the process of the webhooks reached the secret of a connection: %v", err)
	}
	if _, err := api.Open(ctx, secrets.SecretIDFor("crm", "alerts"), okUse); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Errorf("the key of a webhook was reached by the open of a use: %v", err)
	}
}

// [REQ-860]
// The file of the secrets of the sources is what every version of the program reads. A webhook secret that is made,
// rotated, retired and deleted by the new program changes not one byte of it, and it has never a record of a webhook.
func TestHkaTheFileOfTheSourcesIsNotTouchedByTheWebhooksAndHasOnlyTheFormOfAlwaysAndNoKindOfAWebhook(t *testing.T) {
	disk := NewMemDisk()
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	wk := secrets.GenerateWebhookKey(kernel.NewSeededBytes(3))
	svc, err := secrets.New(secrets.Config{Store: open(t, disk), WebhookStore: hkaOpenWebhooks(t, disk), Keys: secrets.StaticKey(key()), WebhookKeys: secrets.StaticKey(wk), Clock: clock, Bytes: kernel.NewSeededBytes(9)})
	if err != nil {
		t.Fatal(err)
	}
	create(t, svc, "db_main")
	before, _ := disk.Bytes(FileName)
	for _, step := range []func() error{
		func() error {
			_, err := svc.IssueWebhookSecret(ctx, "crm", "alerts", secrets.WebhookAuthHMAC)
			return err
		},
		func() error {
			_, err := svc.IssueWebhookSecret(ctx, "crm", "poke", secrets.WebhookAuthBasic)
			return err
		},
		func() error { _, err := svc.RotateWebhookSecret(ctx, "crm", "alerts"); return err },
		func() error { _, err := svc.RetireWebhookVersion(ctx, "crm", "alerts", 1); return err },
		func() error { return svc.DeleteWebhookSecret(ctx, "crm", "poke") },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
		if after, _ := disk.Bytes(FileName); !bytes.Equal(before, after) {
			t.Fatal("a webhook command changed secrets.json")
		}
	}
	if !slices.Equal(disk.Names(), []string{FileName, WebhookFileName}) {
		t.Errorf("the data folder holds %v", disk.Names())
	}
	var head struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
		Secrets []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(before, &head); err != nil {
		t.Fatal(err)
	}
	if head.Format != "praxium-secrets" || head.Version != 1 || len(head.Secrets) != 1 {
		t.Fatalf("secrets.json is %+v", head)
	}
	for _, s := range head.Secrets {
		if s.Kind != "password" && s.Kind != "token" || strings.HasPrefix(s.ID, secrets.WebhookIDPrefix) {
			t.Errorf("secrets.json holds %+v, which a program of before does not know", s)
		}
	}
	// The way back: a program that reads only the file of the sources opens it and finds the secret of the source, and
	// the file of the webhooks is a file it does not look at.
	back := open(t, disk)
	if list, _ := back.List(ctx); len(list) != 1 || list[0].ID != "db_main" {
		t.Errorf("the file of the sources read alone holds %d records", len(list))
	}
	other, _ := disk.Bytes(WebhookFileName)
	if !strings.Contains(string(other), `"format": "praxium-webhook-secrets"`) || bytes.Contains(other, []byte("password")) {
		t.Errorf("the file of the webhooks is not of its own form: %.80s", other)
	}
}

// [REQ-860]
// The two files do not stand in for each other: each is refused where the other belongs, and nothing is overwritten.
func TestHkaEachFileIsReadOnlyAsItsOwnAndAWebhookRecordInTheFileOfTheSourcesIsRefused(t *testing.T) {
	disk := NewMemDisk()
	wk := secrets.GenerateWebhookKey(kernel.NewSeededBytes(3))
	svc, err := secrets.New(secrets.Config{Store: open(t, disk), WebhookStore: hkaOpenWebhooks(t, disk), Keys: secrets.StaticKey(key()), WebhookKeys: secrets.StaticKey(wk),
		Clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), Bytes: kernel.NewSeededBytes(9)})
	if err != nil {
		t.Fatal(err)
	}
	create(t, svc, "db_main")
	if _, err := svc.IssueWebhookSecret(ctx, "crm", "alerts", secrets.WebhookAuthHMAC); err != nil {
		t.Fatal(err)
	}
	sources, _ := disk.Bytes(FileName)
	hooks, _ := disk.Bytes(WebhookFileName)
	// The file of the webhooks put where the file of the sources is, and the other way round.
	swapped := NewMemDisk()
	swapped.Put(FileName, hooks)
	if _, err := Open(swapped); err == nil {
		t.Error("the file of the webhooks was read as the file of the sources")
	}
	swapped = NewMemDisk()
	swapped.Put(WebhookFileName, sources)
	if _, err := OpenWebhooks(swapped); err == nil {
		t.Error("the file of the sources was read as the file of the webhooks")
	}
	// A record of a webhook inside a file that says it is the file of the sources: the file cannot be used.
	var doc map[string]any
	var recs map[string]any
	_ = json.Unmarshal(hooks, &recs)
	_ = json.Unmarshal(sources, &doc)
	doc["secrets"] = append(doc["secrets"].([]any), recs["secrets"].([]any)...)
	mixed, _ := json.Marshal(doc)
	m := NewMemDisk()
	m.Put(FileName, mixed)
	if _, err := Open(m); err == nil || !strings.Contains(err.Error(), "secrets.json cannot be used") {
		t.Errorf("a webhook record in secrets.json: %v", err)
	}
	if got, _ := m.Bytes(FileName); !bytes.Equal(got, mixed) {
		t.Error("a file that cannot be used was overwritten")
	}
}
