package secrets_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-672] A value with a NUL is refused in every field of every kind, and the message names the field and not the value.
func TestNulSecretValuesAreRefused(t *testing.T) {
	r := newRig(t)
	for name, in := range map[string]secrets.CreateInput{
		"user name": {ID: "nul_user", Kind: secrets.KindPassword, Fields: secrets.NewFields(map[string]string{"username": "u\x00options", "password": "p"}), Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}},
		"password":  {ID: "nul_pass", Kind: secrets.KindPassword, Fields: secrets.NewFields(map[string]string{"username": "u", "password": "p\x00"}), Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}},
		"token":     {ID: "nul_tok", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": "t\x00t"}), Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}},
	} {
		_, err := r.svc.Create(ctx, in)
		if kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%s: %v", name, err)
		}
	}
}
