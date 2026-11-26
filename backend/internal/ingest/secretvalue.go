package ingest

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// What is certainly the value of a secret written into a definition (REQ-440, ADR-0132). A revision is never deleted, so
// a value stored in a definition stays for good; only what is certain is refused, and what looks like nothing is the
// person's to keep out (a secret reference, {"secret_id": ...}, is the place of a value). The headers a definition may
// not write (Authorization and the rest of the refused list) are refused when the source is read (headerProblem).

var (
	ecSecretNameValue = regSet("definition.secret.name_value", kernel.CodeInvalidValue, "both", "A text of the definition holds name=value where the name says the value is a secret.", "name")
	ecSecretField     = regSet("definition.secret.field_value", kernel.CodeInvalidValue, "source", "A field of the source definition whose name says it is a secret has a value written in the definition.", "name")
	ecSecretAccount   = regSet("definition.secret.address_account", kernel.CodeInvalidValue, "both", "An address of the definition holds an account (a user, and maybe a password) before its host.", "")
	ecSecretKey       = regSet("definition.secret.private_key", kernel.CodeInvalidValue, "both", "A text of the definition holds the first line of a private key.", "")
)

// secretWords are the names (in lower case, with - read as _) whose value is a secret. They are whole names: a name that
// only holds one of them (token_path, is_secret) is no secret's.
var secretWords = []string{"password", "passwd", "token", "access_token", "refresh_token", "api_key", "apikey", "x_api_key",
	"secret", "client_secret", "authorization"}

var (
	nameValueText = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_.-]*)=([^&\s;,)"]*)`)
	accountText   = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^/?#\s@]+@`)
	privateKey    = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
)

func secretWord(name string) bool {
	return containsString(secretWords, strings.ReplaceAll(strings.ToLower(name), "-", "_"))
}

// placeholderValue says whether a value stands for one that is given later (a placeholder of the product, a parameter
// of SQL) or is no value at all.
func placeholderValue(v string) bool {
	v = strings.Trim(v, `'"`)
	return v == "" || strings.HasPrefix(v, "{{") || strings.ContainsAny(v[:1], "?$:%")
}

// SecretValues looks at the text of a definition (document is source or mapping) for a value of a secret written in it:
// name=value with a name of a secret, an account in an address, the first line of a private key, and in a source
// definition a field named for a secret whose value is a text. A text that is not JSON is the business of the parse and
// gives nothing here. The errors say where and which name; never the value.
func SecretValues(document string, text []byte) error {
	dec := json.NewDecoder(bytes.NewReader(text))
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	c := newCollector(document)
	walkSecretValues(c, v, nil, document == DocumentSource)
	return c.err()
}

func walkSecretValues(c *collector, v any, at []any, fields bool) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			here := append(append([]any{}, at...), k)
			if s, ok := x[k].(string); ok && fields && secretWord(k) && !placeholderValue(s) {
				c.add(ecSecretField, ptr(here...), Params{"name": k},
					"the field %s has a value written in the definition; a secret is never written here: refer to it ({\"secret_id\": ...}) and put the value in with praxium secret put", k)
				continue
			}
			walkSecretValues(c, x[k], here, fields)
		}
	case []any:
		for i, e := range x {
			walkSecretValues(c, e, append(append([]any{}, at...), i), fields)
		}
	case string:
		secretInText(c, x, ptr(at...))
	}
}

// secretInText records what of a secret one text holds.
func secretInText(c *collector, s, pointer string) {
	for _, m := range nameValueText.FindAllStringSubmatch(s, -1) {
		if secretWord(m[1]) && !placeholderValue(m[2]) {
			c.add(ecSecretNameValue, pointer, Params{"name": m[1]},
				"the text has %s=... with a value written in it; a secret is never written in a definition: refer to it ({\"secret_id\": ...}) and put the value in with praxium secret put", m[1])
			break
		}
	}
	if accountText.MatchString(s) {
		c.add(ecSecretAccount, pointer, nil, "the address has an account before its host (user@ or user:password@); give the account as a secret and refer to it")
	}
	if privateKey.MatchString(s) {
		c.add(ecSecretKey, pointer, nil, "the text holds a private key; a key is never written in a definition")
	}
}
