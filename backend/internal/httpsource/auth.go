package httpsource

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode"

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
type maskSet struct {
	get  func() []string
	repl *strings.Replacer
}

func holdTexts(v []string) maskSet {
	return maskSet{get: func() []string { return v }, repl: newReplacer(v)}
}

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

// signer signs the requests of a read in. It holds the opened secret for as long as the read lasts, and two sets of texts
// to take out of what is given out: mask is the one for the rows (the values of the secret's own fields, which are
// never data) and wide is the other one for the records of what happened (the events, the exchanges and the errors),
// which also holds the user name and the client id, because a person reads those and they are no secret.
type signer struct {
	auth   ingest.Auth
	opened *secrets.Opened
	token  secretText
	mask   maskSet
	wide   maskSet
	// fields are the fields of the secret this read asked for, and body the body of the request for data, whose
	// placeholders they come from.
	fields []string
}

// minRowMaskBytes is the length under which a value is not taken out of a row (ADR-0100). A row is data, and a short
// value (a user name, a client id, a word) is in the data of other rows too; the records of what happened have no such
// harm in them, so they take out values of minMaskBytes and more.
const minRowMaskBytes = 8

// legacyKind is the kind of secret, and the fields of it, that a kind of authentication has always read.
func legacyKind(authKind string) (secrets.Kind, []string) {
	switch authKind {
	case ingest.AuthBearer, ingest.AuthHeader:
		return secrets.KindToken, []string{"token"}
	}
	return secrets.KindPassword, []string{"username", "password"}
}

// needs says what a read of the dataset asks of the secret: the fields it must have and, when the placeholders and the
// kind of authentication make it known, the kind of secret that has them.
func needs(set settings, ds ingest.Dataset) (secrets.Kind, bool, []string) {
	var body map[string]any
	if ds.Request != nil {
		body = ds.Request.Body
	}
	used := ingest.CredentialFields(set.auth, body)
	switch set.auth.Kind {
	case ingest.AuthBearer, ingest.AuthHeader, ingest.AuthBasic:
		kind, fields := legacyKind(set.auth.Kind)
		return kind, true, unionOf(fields, used)
	case ingest.AuthTokenRequest:
		if len(used) == 0 {
			kind, fields := legacyKind(set.auth.Kind)
			return kind, true, fields
		}
	}
	kind := secrets.KindPassword
	if containsText(used, "token") {
		kind = secrets.KindToken
	}
	return kind, len(used) > 0, used
}

func containsText(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func unionOf(a, b []string) []string {
	out := append([]string{}, a...)
	for _, s := range b {
		if !containsText(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// openSigner opens the secret of the authentication for this source, at the host and the port the requests go to. A kind
// of authentication that has no secret gives a signer that signs nothing.
func openSigner(ctx context.Context, opener secrets.Opener, set settings, ds ingest.Dataset, purpose secrets.Purpose) (*signer, error) {
	s := &signer{auth: set.auth}
	if !set.needsSecret() {
		return s, nil
	}
	opened, err := opener.Open(ctx, set.auth.Credential.SecretID, secrets.Use{SourceID: set.id, Host: set.host, Port: set.port, Purpose: purpose})
	if err != nil {
		return nil, openError(ctx, err)
	}
	s.opened = opened
	kind, known, fields := needs(set, ds)
	if known && opened.Kind() != kind {
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
	s.fields = fields
	s.setMasks()
	return s, nil
}

// setMasks makes the two sets of texts to take out, from the values of the secret.
func (s *signer) setMasks() {
	secret, names := s.maskTexts()
	s.mask = holdTexts(maskForms(longEnough(secret, minRowMaskBytes)))
	s.wide = holdTexts(maskForms(append(append([]string{}, secret...), names...)))
}

// longEnough keeps the texts that have at least n bytes.
func longEnough(texts []string, n int) []string {
	var out []string
	for _, t := range texts {
		if len(t) >= n {
			out = append(out, t)
		}
	}
	return out
}

// maskTexts are the values that must not appear in any text: the ones the adapter took out of the secret and the ones it
// made of them for the requests (maskForms makes the other shapes they may come back in). The secret ones are those of the
// fields that are not a name: a user name or a client id is a name, kept apart because it is no secret.
func (s *signer) maskTexts() (secret, names []string) {
	for _, f := range s.fields {
		v := s.field(f)
		if f == "username" {
			names = append(names, v)
		} else {
			secret = append(secret, v)
		}
	}
	switch s.auth.Kind {
	case ingest.AuthBearer:
		secret = append(secret, "Bearer "+s.field("token"))
	case ingest.AuthHeader:
		secret = append(secret, ingest.UnescapeBraces(s.auth.Prefix)+s.field("token"))
	case ingest.AuthBasic:
		secret = append(secret, s.basicValue(), "Basic "+s.basicValue(), s.basicFormValue(), "Basic "+s.basicFormValue())
	case ingest.AuthTokenRequest:
		if s.basicRequest() {
			secret = append(secret, s.basicValue(), "Basic "+s.basicValue(), s.basicFormValue(), "Basic "+s.basicFormValue())
		}
	}
	for _, h := range s.auth.Headers {
		if src, ok := ingest.ParseHeaderFrom(h.From); ok && !src.Token {
			secret = append(secret, ingest.UnescapeBraces(h.Prefix)+s.field(src.Field))
		}
	}
	return secret, names
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

// basicFormValue is the value of a Basic header as the OAuth 2.0 client authentication writes it (RFC 6749 2.3.1): the
// two texts escaped as a form before they are joined. A server may hand it back in this shape, so it is taken out too.
func (s *signer) basicFormValue() string {
	return base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(s.field("username")) + ":" + url.QueryEscape(s.field("password"))))
}

func (s *signer) basicRequest() bool {
	return s.auth.Request != nil && s.auth.Request.CredentialsIn == ingest.CredentialsInBasic
}

// address is the host and the port the secret was opened for; the requests go there.
func (s *signer) address() (secrets.Address, bool) {
	if s.opened == nil {
		return secrets.Address{}, false
	}
	return s.opened.Address(), true
}

// unusable is the error of a value of the secret that cannot go where it is asked to go. The request is not sent.
func unusable(why string) error {
	return ingest.NewSourceError(ingest.SrcSecretUnusable, why, nil)
}

// headerSafe tells whether a text may be the value of a header: no line break and no other control character (a tab is
// allowed). Such a value would make the client refuse the request, which a retry does not change.
func headerSafe(v string) bool {
	return !strings.ContainsFunc(v, func(r rune) bool { return r != '\t' && (unicode.IsControl(r) && r < 0x80) })
}

// setSafe puts a header whose value comes from the secret, and refuses a value that has a line break or a control character,
// so that nothing is sent (and no retry is made) for a value that cannot be sent.
func setSafe(h map[string][]string, name, value string) error {
	if !headerSafe(value) {
		return unusable("a value of the secret that goes into a header has a line break or a control character, which a header cannot hold; nothing was sent")
	}
	setHeader(h, name, value)
	return nil
}

// apply puts the credentials into the headers of a request: the header the kind of authentication sends, and then the
// entries of auth.headers (which win a name that is the same).
func (s *signer) apply(h map[string][]string) error {
	if err := s.applyKind(h); err != nil {
		return err
	}
	for _, e := range s.auth.Headers {
		if err := s.applyEntry(h, e); err != nil {
			return err
		}
	}
	return nil
}

func (s *signer) applyKind(h map[string][]string) error {
	switch s.auth.Kind {
	case ingest.AuthBearer:
		return setSafe(h, "Authorization", "Bearer "+s.field("token"))
	case ingest.AuthBasic:
		return s.setBasic(h)
	case ingest.AuthHeader:
		return setSafe(h, s.auth.Header, ingest.UnescapeBraces(s.auth.Prefix)+s.field("token"))
	case ingest.AuthTokenRequest:
		if tok := s.token.text(); tok != "" && s.auth.Send != ingest.SendTemplate {
			setHeader(h, s.sendHeader(), s.sendPrefix()+tok)
		}
	}
	return nil
}

// applyEntry puts one entry of auth.headers: the token it names (when there is one), or a field of the secret.
func (s *signer) applyEntry(h map[string][]string, e ingest.SecretHeader) error {
	src, ok := ingest.ParseHeaderFrom(e.From)
	if !ok {
		return nil
	}
	prefix := ingest.UnescapeBraces(e.Prefix)
	if src.Token {
		if tok := s.token.text(); tok != "" {
			setHeader(h, e.Name, prefix+tok)
		}
		return nil
	}
	return setSafe(h, e.Name, prefix+s.field(src.Field))
}

// setBasic puts the Authorization header of the user name and the password. A user name with a colon cannot be told from
// the colon that ends it (RFC 7617), so it is refused and nothing is sent.
func (s *signer) setBasic(h map[string][]string) error {
	if strings.Contains(s.field("username"), ":") {
		return unusable("the user name of the secret has a colon, which a Basic header cannot carry; nothing was sent")
	}
	setHeader(h, "Authorization", "Basic "+s.basicValue())
	return nil
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

// valueOf gives what a placeholder stands for: a field of the secret or the token the source gave.
func (s *signer) valueOf(p ingest.Placeholder) (string, error) {
	if p.Token {
		tok := s.token.text()
		if tok == "" {
			return "", ingest.NewSourceError(ingest.SrcInternal, "a body asks for the token before the source gave one", nil)
		}
		return tok, nil
	}
	if s.opened == nil {
		return "", unusable("a body has a placeholder of the secret, and the authentication has no secret")
	}
	v, ok := s.opened.Value(p.Field)
	if !ok {
		return "", unusable("a body names a field the secret does not have")
	}
	return v.Reveal(), nil
}

// ensure gets a token for a token_request authentication that has none.
func (s *signer) ensure(ctx context.Context, rn *run) error {
	if !s.needsToken() {
		return nil
	}
	tr := s.auth.Request
	u := *rn.origin
	u.Path += tr.Path
	p := plan{method: ingest.MethodPost, url: &u, token: true, basic: s.basicRequest(), form: tr.Format == "form", tmpl: tr.Body}
	if p.tmpl == nil && !(p.basic && tr.Format == "") {
		p.tmpl = map[string]any{}
	}
	rep, err := rn.exchange(ctx, p)
	if err != nil {
		return err
	}
	data, err := rn.smallBody(ctx, p, rep, tokenAnswerMax)
	if err != nil {
		return err
	}
	tok, err := s.tokenOf(rn, data)
	if err != nil {
		return err
	}
	s.token = holdText(tok)
	s.addMasked(tok, s.sendPrefix()+tok)
	for _, h := range s.auth.Headers {
		if src, ok := ingest.ParseHeaderFrom(h.From); ok && src.Token {
			s.addMasked(ingest.UnescapeBraces(h.Prefix) + tok)
		}
	}
	rn.prog.authenticated = true
	return nil
}

// tokenOf reads the token from the answer: at token_path, or the whole body as a JSON text (token_from body, which is
// not an object, a number or an empty text). A token with a line break or a control character is refused: a header could
// not hold it, and nothing is sent with it.
func (s *signer) tokenOf(rn *run, data []byte) (string, error) {
	doc, err := decodeJSON(data)
	if err != nil {
		return "", rn.fail(ingest.SrcResponseFormat, "the answer to the token request is not JSON", nil)
	}
	tok, ok := "", false
	if s.auth.TokenFrom == ingest.TokenFromBody {
		tok, ok = doc.(string)
		if !ok || tok == "" {
			return "", rn.fail(ingest.SrcResponseFormat, "the answer to the token request is not a JSON text with a token in it (token_from is body)", nil)
		}
	} else {
		path, perr := ingest.ParsePath(s.auth.TokenPath)
		if perr == nil {
			tok, ok = path.Pick(doc)
		}
		if !ok {
			return "", rn.fail(ingest.SrcResponseFormat, "the answer to the token request has no token at token_path", nil)
		}
	}
	if !headerSafe(tok) {
		return "", rn.fail(ingest.SrcResponseFormat, "the token the source gave has a line break or a control character, which a header cannot hold", nil)
	}
	return tok, nil
}

// addMasked adds texts to take out of what is given out: to the rows when they are long enough, and always to the records
// of what happened.
func (s *signer) addMasked(texts ...string) {
	s.mask = holdTexts(append(s.mask.list(), maskForms(longEnough(texts, minRowMaskBytes))...))
	s.wide = holdTexts(append(s.wide.list(), maskForms(texts)...))
}
