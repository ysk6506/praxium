package secrets_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

func hkaFlakyRig(t *testing.T) (*secrets.Service, *faultyStore) {
	t.Helper()
	flaky := &faultyStore{MemoryStore: secrets.NewWebhookMemoryStore()}
	svc, err := secrets.New(secrets.Config{
		Store: secrets.NewMemoryStore(), WebhookStore: flaky, Keys: secrets.StaticKey(newKey(99)),
		WebhookKeys: secrets.StaticKey(secrets.GenerateWebhookKey(kernel.NewSeededBytes(31))),
		Clock:       kernel.NewManualClock(epoch), Bytes: kernel.NewSeededBytes(7),
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, flaky
}

// [REQ-860]
// A store that fails is said so with the internal code by every webhook command, and a failed write keeps nothing.
func TestHkaAFailingStoreIsAnInternalErrorOfEveryWebhookCommand(t *testing.T) {
	svc, f := hkaFlakyRig(t)
	if _, err := svc.IssueWebhookSecret(ctx, "src_a", "alerts", secrets.WebhookAuthHMAC); err != nil {
		t.Fatal(err)
	}
	cmds := map[string]func() error{
		"issue": func() error {
			_, err := svc.IssueWebhookSecret(ctx, "src_a", "other", secrets.WebhookAuthHMAC)
			return err
		},
		"rotate": func() error { _, err := svc.RotateWebhookSecret(ctx, "src_a", "alerts"); return err },
		"retire": func() error { _, err := svc.RetireWebhookVersion(ctx, "src_a", "alerts", 1); return err },
		"delete": func() error { return svc.DeleteWebhookSecret(ctx, "src_a", "alerts") },
		"list":   func() error { _, err := svc.ListWebhookSecrets(ctx); return err },
	}
	for _, mode := range []string{"get", "put", "list", "del"} {
		f.get, f.put, f.list, f.del = mode == "get", mode == "put", mode == "list", mode == "del"
		for name, run := range cmds {
			touched := (mode == "get" && name != "list") || (mode == "put" && (name == "issue" || name == "rotate" || name == "retire")) ||
				(mode == "list" && name == "list") || (mode == "del" && name == "delete")
			if !touched {
				continue
			}
			if err := run(); kernel.CodeOf(err) != kernel.CodeInternal {
				t.Errorf("%s with a failing %s: %v", name, mode, err)
			}
		}
	}
	f.get, f.put, f.list, f.del = false, false, false, false
	metas, err := svc.ListWebhookSecrets(ctx)
	if err != nil || len(metas) != 1 || metas[0].Version != 1 {
		t.Errorf("a failed write left %v %v", metas, err)
	}
}

// [REQ-860]
// A name that is not a name, a webhook without a secret, a version that is not there and a version retired twice are
// told apart by each command.
func TestHkaTheWebhookCommandsRefuseWhatTheyCannotDo(t *testing.T) {
	r := hkaNew(t)
	bad := "Bad Name"
	checks := []struct {
		what string
		err  error
		want kernel.Code
	}{
		{"issue, bad name", func() error { _, e := r.svc.IssueWebhookSecret(ctx, bad, "a", secrets.WebhookAuthHMAC); return e }(), kernel.CodeInvalidValue},
		{"rotate, bad name", func() error { _, e := r.svc.RotateWebhookSecret(ctx, bad, "a"); return e }(), kernel.CodeInvalidValue},
		{"retire, bad name", func() error { _, e := r.svc.RetireWebhookVersion(ctx, bad, "a", 1); return e }(), kernel.CodeInvalidValue},
		{"delete, bad name", r.svc.DeleteWebhookSecret(ctx, bad, "a"), kernel.CodeInvalidValue},
		{"rotate, no secret", func() error { _, e := r.svc.RotateWebhookSecret(ctx, "src_a", "none"); return e }(), kernel.CodeNotFound},
		{"retire, no secret", func() error { _, e := r.svc.RetireWebhookVersion(ctx, "src_a", "none", 1); return e }(), kernel.CodeNotFound},
		{"delete, no secret", r.svc.DeleteWebhookSecret(ctx, "src_a", "none"), kernel.CodeNotFound},
	}
	for _, c := range checks {
		if kernel.CodeOf(c.err) != c.want {
			t.Errorf("%s: %v, want %s", c.what, c.err, c.want)
		}
	}
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
		t.Errorf("a version retired twice: %v", err)
	}
}
