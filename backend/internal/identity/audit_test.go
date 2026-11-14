package identity

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/access"
)

// idtEvents reads the audit lines of the log.
func idtEvents(t *testing.T, r *idtRig) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(r.logs.String()))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("a log line is not JSON: %v", err)
		}
		if line["audit"] == true {
			out = append(out, line)
		}
	}
	return out
}

// [REQ-314]
func TestTheAuditLogRecordsWhoDidWhatToWhomAndNeverASecret(t *testing.T) {
	r := idtNewRig(t)
	_, first, err := r.svc.Bootstrap(r.ctx, "root", "Root", 0)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	r.person("ann", access.RoleViewer)
	tok := r.token("ann")
	if _, err := r.svc.RevokeToken(r.ctx, idtAdmin, tok.Info.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := r.svc.DisablePrincipal(r.ctx, idtAdmin, access.SubjectPerson, "ann"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, _, err := r.svc.Bootstrap(r.ctx, "again", "", 0); err == nil {
		t.Fatal("a second bootstrap worked")
	}
	var events []string
	for _, e := range idtEvents(t, r) {
		events = append(events, e["event"].(string))
	}
	want := []string{"principal_created", "token_issued", "bootstrap", "principal_created", "token_issued", "token_revoked", "principal_disabled", "bootstrap_refused"}
	if strings.Join(events, " ") != strings.Join(want, " ") {
		t.Fatalf("events %v, want %v", events, want)
	}
	boot := idtEvents(t, r)[2]
	if boot["actor"] != "(local operator)" || boot["target"] != "root" || boot["token_id"] != first.Info.ID {
		t.Fatalf("the bootstrap line is %v", boot)
	}
	revoke := idtEvents(t, r)[5]
	if revoke["actor"] != "root" || revoke["token_id"] != tok.Info.ID || revoke["owner"] != "ann" {
		t.Fatalf("the revoke line is %v", revoke)
	}
	logs := r.logs.String()
	for _, issued := range []string{first.Secret, tok.Secret} {
		secretPart := issued[len(TokenPrefix)+16+1:]
		if strings.Contains(logs, secretPart) || strings.Contains(logs, hashSecret(issued[:len(TokenPrefix)+16], secretPart)) {
			t.Fatal("a secret or its hash is in the log")
		}
	}
}

// [REQ-314]
func TestAPlainUseWritesNoAuditLine(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	tok := r.token("ann")
	before := len(idtEvents(t, r))
	if _, err := r.svc.Authenticate(tok.Secret); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got := len(idtEvents(t, r)); got != before {
		t.Fatalf("a plain use wrote %d audit lines", got-before)
	}
}
