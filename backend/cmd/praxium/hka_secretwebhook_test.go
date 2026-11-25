package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
	"github.com/ysk6506/praxium/backend/internal/secretsfile"
)

func hkaValueLine(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "the value follows") && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	t.Fatalf("no value in %q", out)
	return ""
}

// [REQ-860]
func TestHkaTheWebhookCommandsMakeTheSecretShowItOnceAndKeepTheWebhookKeyApartFromTheMasterKey(t *testing.T) {
	e := newSecEnv(t)
	e.secPut("db_main") // the master key and the folder are made
	out := e.secOk("", "webhook-issue", "crm/alerts", "-kind", "hmac", "-data-dir", e.dataDir)
	value := hkaValueLine(t, out)
	if !strings.HasPrefix(value, "whsec_") || !strings.Contains(out, "made the webhook key file") {
		t.Fatalf("the issue printed %q", out)
	}
	keys, _ := filepath.Glob(filepath.Join(e.cfgDir, "praxium", "keys", "*"+webhookKeyFileSuffix))
	if len(keys) != 1 || len(e.secKeyFiles()) != 2 {
		t.Fatalf("webhook key files %v of all key files %v", keys, e.secKeyFiles())
	}
	if _, err := secrets.ParseWebhookKey(mustRead(t, keys[0])); err != nil {
		t.Errorf("the webhook key file: %v", err)
	}
	// Not again: the value is shown once, and the list never shows it.
	secAssertAbsent(t, []string{e.secOk("", "list", "-data-dir", e.dataDir), e.secOk("", "show", secrets.SecretIDFor("crm", "alerts"), "-data-dir", e.dataDir)}, []string{value, strings.TrimPrefix(value, "whsec_")})
	e.secFails(exitFailure, "", "webhook-issue", "crm/alerts", "-kind", "hmac", "-data-dir", e.dataDir)

	var rotated struct {
		Version int    `json:"version"`
		Value   string `json:"value"`
	}
	if err := json.Unmarshal([]byte(e.secOk("", "webhook-rotate", "crm/alerts", "-data-dir", e.dataDir, "--json")), &rotated); err != nil || rotated.Version != 2 || rotated.Value == value {
		t.Fatalf("the rotation: %+v %v", rotated, err)
	}
	if got := e.secOk("", "webhook-retire", "crm/alerts", "-version", "1", "-data-dir", e.dataDir); !strings.Contains(got, "retired version 1") {
		t.Errorf("the retirement: %s", got)
	}
	e.secOk("", "webhook-issue", "crm/poke", "-kind", "url_token", "-data-dir", e.dataDir)
	if got := e.secOk("", "webhook-delete", "crm/poke", "-data-dir", e.dataDir, "--json"); !strings.Contains(got, `"deleted"`) {
		t.Errorf("the deletion: %s", got)
	}
	// The master secret of the folder is as it was, and the webhook secret is not one a source can open.
	if got := e.secOpen("db_main", "password"); got["password"] != secPass {
		t.Error("the secret of the source changed")
	}
	st, err := openSecretStore(e.w, secretLocation{dataDir: e.dataDir}, secretChange)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, err := st.svc.Opener().Open(ctx0, secrets.SecretIDFor("crm", "alerts"), secrets.Use{SourceID: "crm", Host: "a.example.test", Port: 1, Purpose: secrets.PurposeRun}); kernel.CodeOf(err) != secrets.CodeReceiveOnly {
		t.Errorf("the key of a webhook was opened as a connection secret: %v", err)
	}
}

// [REQ-860]
func TestHkaTheWebhookCommandsRefuseWhatIsWrongAndKeepTheKeyOutsideTheDataFolder(t *testing.T) {
	e := newSecEnv(t)
	for _, args := range [][]string{
		{"webhook-issue", "crm", "-kind", "hmac"}, {"webhook-issue", "crm/Alerts", "-kind", "hmac"}, {"webhook-issue", "crm/alerts", "-kind", "digest"},
		{"webhook-issue", "crm/alerts"}, {"webhook-retire", "crm/alerts"}, {"webhook-rotate", "crm/alerts"}, {"webhook-delete", "crm/alerts"},
	} {
		code, _, _ := e.secRun("", append(args, "-data-dir", e.dataDir)...)
		if code == exitOK {
			t.Errorf("%v was taken", args)
		}
	}
	if _, err := os.Stat(e.dataDir); err == nil {
		// only a successful issue may make the folder
		t.Error("a refused command made the data folder")
	}
	inside := filepath.Join(e.dataDir, "hook.key")
	e.secOk("", "webhook-issue", "crm/alerts", "-kind", "basic", "-data-dir", e.dataDir, "-secrets-key-file", filepath.Join(e.cfgDir, "elsewhere.key"))
	e.secFails(exitFailure, "", "webhook-rotate", "crm/alerts", "-data-dir", e.dataDir, "-secrets-key-file", inside)
	// A key file that is not the one the secret was made with is refused, and a missing one is not made again.
	other := filepath.Join(e.cfgDir, "other.key")
	if err := os.WriteFile(other, secrets.GenerateWebhookKey(kernel.NewSeededBytes(3)).Encode(), 0o600); err != nil {
		t.Fatal(err)
	}
	e.secFails(exitFailure, "", "webhook-rotate", "crm/alerts", "-data-dir", e.dataDir, "-secrets-key-file", other)
	e.secFails(exitFailure, "", "webhook-rotate", "crm/alerts", "-data-dir", e.dataDir, "-secrets-key-file", filepath.Join(e.cfgDir, "gone.key"))
	disk, _ := openSecretDisk(e.dataDir, true)
	if _, err := secretsfile.Open(disk); err != nil {
		t.Error(err)
	}
}

var ctx0 = context.Background()

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// [REQ-860]
func TestHkaTheWebhookCommandsAnswerForASecretThatIsNotThereAndForAKeyFileThatIsNotAWebhookKey(t *testing.T) {
	e := newSecEnv(t)
	e.secPut("db_main")
	master := e.secKeyFiles()[0]
	// The master key file is not a webhook key, and it is refused as one without being repeated.
	e.secFails(exitFailure, "", "webhook-issue", "crm/alerts", "-kind", "hmac", "-data-dir", e.dataDir, "-secrets-key-file", master)
	hook := filepath.Join(e.cfgDir, "hook.key")
	made := e.secOk("", "webhook-issue", "crm/alerts", "-kind", "header_token", "-data-dir", e.dataDir, "-secrets-key-file", hook, "--json")
	if !strings.Contains(made, `"webhook_key_file_made"`) || !strings.Contains(made, `"value"`) {
		t.Errorf("the issue as JSON: %s", made)
	}
	// What is not there: another webhook, a version that does not exist, and the deletion of a secret that is gone.
	e.secFails(exitFailure, "", "webhook-rotate", "crm/other", "-data-dir", e.dataDir, "-secrets-key-file", hook)
	e.secFails(exitFailure, "", "webhook-retire", "crm/alerts", "-version", "9", "-data-dir", e.dataDir, "-secrets-key-file", hook)
	e.secFails(exitFailure, "", "webhook-delete", "crm/other", "-data-dir", e.dataDir)
	if err := os.WriteFile(hook, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.secFails(exitFailure, "", "webhook-rotate", "crm/alerts", "-data-dir", e.dataDir, "-secrets-key-file", hook)
	// A data folder that is not there, a key file that others can read, a key file under a plain file, and a system with
	// no configuration folder to keep the key in.
	e.secFails(exitFailure, "", "webhook-rotate", "crm/alerts", "-data-dir", filepath.Join(e.cfgDir, "no-such-folder"))
	if err := os.WriteFile(hook, secrets.GenerateWebhookKey(kernel.NewSeededBytes(4)).Encode(), 0o644); err != nil {
		t.Fatal(err)
	}
	e.secFails(exitFailure, "", "webhook-rotate", "crm/alerts", "-data-dir", e.dataDir, "-secrets-key-file", hook)
	plain := filepath.Join(e.cfgDir, "plain-file")
	if err := os.WriteFile(plain, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.secFails(exitFailure, "", "webhook-issue", "crm/second", "-kind", "basic", "-data-dir", e.dataDir, "-secrets-key-file", filepath.Join(plain, "sub", "k.key"))
	loop := filepath.Join(e.cfgDir, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	e.secFails(exitFailure, "", "webhook-issue", "crm/fourth", "-kind", "basic", "-data-dir", e.dataDir, "-secrets-key-file", filepath.Join(loop, "k.key"))
	if err := os.WriteFile(filepath.Join(e.dataDir, "FOLDER_ID"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.secFails(exitFailure, "", "webhook-issue", "crm/fifth", "-kind", "basic", "-data-dir", e.dataDir)
	e.w.configDir = func() (string, error) { return "", nil }
	e.secFails(exitFailure, "", "webhook-issue", "crm/third", "-kind", "basic", "-data-dir", e.dataDir)
	// A data folder that is locked by someone else is not changed.
	disk, _ := openSecretDisk(e.dataDir, false)
	unlock, err := disk.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()
	e.secFails(exitFailure, "", "webhook-delete", "crm/alerts", "-data-dir", e.dataDir)
}
