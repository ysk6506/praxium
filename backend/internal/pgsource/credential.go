package pgsource

import (
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// account is the user name and the password of the opened secret, as the two texts the connection is made from.
type account struct {
	user     string
	password string
}

// accountOf reads the account out of an opened password secret. It is the one place of this package that reveals a
// value of a secret (a test of the sources keeps it so), and the values live only as long as the connection
// configuration that is built from them.
func accountOf(opened *secrets.Opened) (account, *ingest.SourceError) {
	if opened.Kind() != secrets.KindPassword {
		return account{}, ingest.NewSourceError(ingest.SrcSecretUnusable, "the secret is not a password secret (a postgres source needs a user name and a password)", nil)
	}
	user, okUser := opened.Value("username")
	pass, okPass := opened.Value("password")
	if !okUser || !okPass {
		return account{}, ingest.NewSourceError(ingest.SrcSecretUnusable, "the secret has no user name or no password", nil)
	}
	a := account{user: user.Reveal(), password: pass.Reveal()}
	if a.user == "" || a.password == "" {
		return account{}, ingest.NewSourceError(ingest.SrcSecretUnusable, "the secret has an empty user name or an empty password (a connection with no password is not made)", nil)
	}
	return a, nil
}
