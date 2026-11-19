package httpsource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// This file is the one place where the values of a secret are taken out of their wrappers (REQ-442): every call of
// Reveal of the package is here, and a test that reads the sources of the package keeps it so. The values go into the
// headers and the body of a request as it is made and nowhere else. A token the source gave for a token_request is held
// as a secret too: it is wrapped, it prints as nothing, and it is wiped when the read ends.

// secretText holds a text that is a secret, in a function value as secrets.Value does, so that printing it in any way
// (or printing a struct that holds it) gives no part of it.
type secretText struct{ get func() string }

func holdText(v string) secretText { return secretText{get: func() string { return v }} }

func (t secretText) text() string {
	if t.get == nil {
		return ""
	}
	return t.get()
}

func (secretText) String() string               { return secrets.Redacted }
func (secretText) GoString() string             { return secrets.Redacted }
func (secretText) Format(f fmt.State, _ rune)   { _, _ = io.WriteString(f, secrets.Redacted) }
func (secretText) MarshalText() ([]byte, error) { return []byte(secrets.Redacted), nil }
func (secretText) MarshalJSON() ([]byte, error) { return []byte(`"` + secrets.Redacted + `"`), nil }

// maskSet holds the texts that must not appear in any words (the values of the secrets and what is made of them) in the
// same way: in a function value, printing as nothing.
type maskSet struct{ get func() []string }

func holdTexts(v []string) maskSet { return maskSet{get: func() []string { return v }} }

func (m maskSet) list() []string {
	if m.get == nil {
		return nil
	}
	return m.get()
}

func (maskSet) String() string               { return secrets.Redacted }
func (maskSet) GoString() string             { return secrets.Redacted }
func (maskSet) Format(f fmt.State, _ rune)   { _, _ = io.WriteString(f, secrets.Redacted) }
func (maskSet) MarshalText() ([]byte, error) { return []byte(secrets.Redacted), nil }

// signer signs the requests of a read in. It holds the opened secret for as long as the read lasts.
type signer struct {
	auth   ingest.Auth
	opened *secrets.Opened
	token  secretText
	mask   maskSet
}

var placeholderRe = regexp.MustCompile(`^\{\{credential\.([A-Za-z0-9_]+)\}\}$`)

func kindFor(authKind string) (secrets.Kind, []string) {
	switch authKind {
	case ingest.AuthBearer, ingest.AuthHeader:
		return secrets.KindToken, []string{"token"}
	}
	return secrets.KindPassword, []string{"username", "password"}
}

// openSigner opens the secret of the authentication for this source, at the host and the port the requests go to. A kind
// of authentication that has no secret gives a signer that signs nothing.
func openSigner(ctx context.Context, opener secrets.Opener, set settings, purpose secrets.Purpose) (*signer, error) {
	s := &signer{auth: set.auth}
	if !set.needsSecret() {
		return s, nil
	}
	opened, err := opener.Open(ctx, set.auth.Credential.SecretID, secrets.Use{SourceID: set.id, Host: set.host, Port: set.port, Purpose: purpose})
	if err != nil {
		return nil, openError(ctx, err)
	}
	s.opened = opened
	kind, fields := kindFor(set.auth.Kind)
	if opened.Kind() != kind {
		s.close()
		return nil, ingest.NewSourceError(ingest.SrcSecretUnusable, "the secret is not of the kind this authentication needs ("+string(kind)+")", nil)
	}
	for _, f := range fields {
		v, ok := opened.Value(f)
		if !ok || v.Reveal() == "" {
			s.close()
			return nil, ingest.NewSourceError(ingest.SrcSecretUnusable, "the secret has no value in a field this authentication needs", nil)
		}
	}
	s.mask = holdTexts(s.maskTexts())
	return s, nil
}

// maskTexts are the values that must not appear in any text: the secret's own and what is made of them.
func (s *signer) maskTexts() []string {
	switch s.auth.Kind {
	case ingest.AuthBearer, ingest.AuthHeader:
		return []string{s.field("token")}
	case ingest.AuthBasic:
		return []string{s.field("password"), s.basicValue()}
	case ingest.AuthTokenRequest:
		return []string{s.field("password")}
	}
	return nil
}

func (s *signer) field(name string) string {
	if s.opened == nil {
		return ""
	}
	v, _ := s.opened.Value(name)
	return v.Reveal()
}

func (s *signer) basicValue() string {
	return base64.StdEncoding.EncodeToString([]byte(s.field("username") + ":" + s.field("password")))
}

// address is the host and the port the secret was opened for; the requests go there.
func (s *signer) address() (secrets.Address, bool) {
	if s.opened == nil {
		return secrets.Address{}, false
	}
	return s.opened.Address(), true
}

// apply puts the credentials into the headers of a request.
func (s *signer) apply(h map[string][]string) {
	switch s.auth.Kind {
	case ingest.AuthBearer:
		setHeader(h, "Authorization", "Bearer "+s.field("token"))
	case ingest.AuthBasic:
		setHeader(h, "Authorization", "Basic "+s.basicValue())
	case ingest.AuthHeader:
		setHeader(h, s.auth.Header, ingest.UnescapeBraces(s.auth.Prefix)+s.field("token"))
	case ingest.AuthTokenRequest:
		if tok := s.token.text(); tok != "" {
			setHeader(h, s.sendHeader(), s.sendPrefix()+tok)
		}
	}
}

func (s *signer) sendHeader() string {
	if s.auth.SendHeader != "" {
		return s.auth.SendHeader
	}
	return "Authorization"
}

func (s *signer) sendPrefix() string {
	if s.auth.SendPrefix != nil {
		return ingest.UnescapeBraces(*s.auth.SendPrefix)
	}
	return "Bearer "
}

func (s *signer) refreshable() bool { return s.auth.Kind == ingest.AuthTokenRequest }

func (s *signer) needsToken() bool {
	return s.auth.Kind == ingest.AuthTokenRequest && s.token.text() == ""
}

// drop forgets the token, so that the next request asks for another.
func (s *signer) drop() { s.token = secretText{} }

func (s *signer) close() {
	s.drop()
	s.opened.Close()
}

// tokenBody makes the body of the token request from the template of the definition: a value that is exactly
// {{credential.<field>}} becomes that field of the secret, as a value; every other value is the text it is.
func (s *signer) tokenBody(tr *ingest.TokenRequest) ([]byte, string, error) {
	values := make(map[string]string, len(tr.Body))
	for name, text := range tr.Body {
		if m := placeholderRe.FindStringSubmatch(text); m != nil {
			v, ok := s.opened.Value(m[1])
			if !ok {
				return nil, "", ingest.NewSourceError(ingest.SrcSecretUnusable, "the body of the token request names a field the secret does not have", nil)
			}
			values[name] = v.Reveal()
			continue
		}
		values[name] = ingest.UnescapeBraces(text)
	}
	if tr.Format == "form" {
		form := url.Values{}
		for k, v := range values {
			form.Set(k, v)
		}
		return []byte(form.Encode()), "application/x-www-form-urlencoded", nil
	}
	body, err := json.Marshal(values)
	if err != nil {
		return nil, "", ingest.NewSourceError(ingest.SrcInternal, "the token request could not be made", nil)
	}
	return body, "application/json", nil
}

// ensure gets a token for a token_request authentication that has none.
func (s *signer) ensure(ctx context.Context, rn *run) error {
	if !s.needsToken() {
		return nil
	}
	body, ctype, err := s.tokenBody(s.auth.Request)
	if err != nil {
		return err
	}
	u := *rn.origin
	u.Path += s.auth.Request.Path
	rep, err := rn.exchange(ctx, plan{method: ingest.MethodPost, url: &u, body: body, ctype: ctype, token: true})
	if err != nil {
		return err
	}
	doc, err := decodeJSON(rep.body)
	if err != nil {
		return rn.fail(ingest.SrcResponseFormat, "the answer to the token request is not JSON", nil)
	}
	path, perr := ingest.ParsePath(s.auth.TokenPath)
	tok, ok := "", false
	if perr == nil {
		tok, ok = path.Pick(doc)
	}
	if !ok {
		return rn.fail(ingest.SrcResponseFormat, "the answer to the token request has no token at token_path", nil)
	}
	s.token = holdText(tok)
	s.mask = holdTexts(append(s.mask.list(), tok))
	rn.prog.authenticated = true
	return nil
}
