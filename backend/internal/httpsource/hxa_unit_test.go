package httpsource

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The parts of the signer that the definition check keeps from being asked a wrong thing are tested one by one, so that
// what each refuses is the same when a caller gets round the check (ADR-0100).

func TestHxaASignerWithoutASecretRefusesWhatNeedsOne(t *testing.T) {
	s := &signer{}
	if got := s.field("token"); got != "" {
		t.Errorf("a signer without a secret gave a value: %q", got)
	}
	if _, err := s.valueOf(ingest.Placeholder{Field: "token"}); hxaKind(t, err) != string(ingest.SrcSecretUnusable) {
		t.Errorf("a field of no secret was not refused as unusable: %v", err)
	}
	if _, err := s.valueOf(ingest.Placeholder{Token: true}); err == nil {
		t.Error("a token that the source never gave was asked for and no error came")
	}
	tmpl := map[string]any{"a": []any{map[string]any{"b": "{{credential.token}}"}}}
	if _, err := s.fill(tmpl, true); err == nil {
		t.Error("a placeholder inside a list inside a map was filled with no secret")
	}
	if _, err := s.fill(map[string]any{"a": "{{credential.token}}"}, true); err == nil {
		t.Error("a placeholder inside a map was filled with no secret")
	}
	if _, _, err := s.bodyOf(plan{tmpl: tmpl}); err == nil {
		t.Error("a body of a placeholder with no secret was made")
	}
	if _, _, err := s.bodyOf(plan{tmpl: map[string]any{"n": 1.0}, form: true}); hxaKind(t, err) != string(ingest.SrcInternal) {
		t.Errorf("a form with a value that is no text was not refused: %v", err)
	}
	if body, kind, err := s.bodyOf(plan{}); body != nil || kind != "" || err != nil {
		t.Errorf("no template made a body: %q %q %v", body, kind, err)
	}
}

func TestHxaWhatASecretIsAskedForIsTheUnionOfTheKindAndTheBody(t *testing.T) {
	if got := unionOf([]string{"token"}, []string{"token", "other"}); len(got) != 2 || got[1] != "other" {
		t.Errorf("the union of the fields repeated a field: %v", got)
	}
	kind, known, fields := needs(settings{auth: ingest.Auth{Kind: ingest.AuthTokenRequest}}, ingest.Dataset{})
	if kind != secrets.KindPassword || !known || len(fields) != 2 {
		t.Errorf("a token request that names no field asks for the user name and the password: %v %v %v", kind, known, fields)
	}
	body := map[string]any{"k": "{{credential.token}}"}
	kind, known, fields = needs(settings{auth: ingest.Auth{Kind: ingest.AuthBearer}}, ingest.Dataset{Request: &ingest.Request{Body: body}})
	if kind != secrets.KindToken || !known || len(fields) != 1 {
		t.Errorf("a bearer token and a body that asks for it again asks for the token once: %v %v %v", kind, known, fields)
	}
}

func TestHxaAnEntryThatNamesNoSourceAddsNothing(t *testing.T) {
	h := map[string][]string{}
	if err := (&signer{}).applyEntry(h, ingest.SecretHeader{Name: "X-Key", From: "nowhere"}); err != nil || len(h) != 0 {
		t.Errorf("an entry with an unknown source put a header: %v %v", h, err)
	}
}
