package httpsource

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
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
	// tokenOpened is the same secret opened for the host of the token request, when that is not the API's (cross): the
	// credentials of the token request are taken from it and from nothing else, and the requests of the API are signed with
	// the token and with no value of the secret. Without cross it is nil and opened serves both.
	tokenOpened *secrets.Opened
	cross       bool
	token       secretText
	// expires is when the token is asked for again, from the lifetime the answer gave; zero for a token that has none.
	expires time.Time
	mask    maskSet
	wide    maskSet
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
	// Before the secret is opened and before any request, the token request included: a definition that puts a value of the
	// secret in a request of the API, when the token is asked for at another host, is refused with nothing sent anywhere.
	if err := noSecretInTheRequestsOfTheAPI(set, ds); err != nil {
		return nil, err
	}
	opened, err := opener.Open(ctx, set.auth.Credential.SecretID, secrets.Use{SourceID: set.id, Host: set.host, Port: set.port, Purpose: purpose})
	if err != nil {
		return nil, openError(ctx, err)
	}
	s.opened = opened
	if err := s.openForTokenHost(ctx, opener, set, purpose); err != nil {
		return nil, err
	}
	kind, known, fields := needs(set, ds)
	for _, o := range s.openeds() {
		if known && o.Kind() != kind {
			s.close()
			return nil, ingest.NewSourceError(ingest.SrcSecretUnusable, "the secret is not of the kind this authentication needs ("+string(kind)+")", nil)
		}
		for _, f := range fields {
			v, ok := o.Value(f)
			if !ok || v.Reveal() == "" {
				s.close()
				return nil, ingest.NewSourceError(ingest.SrcSecretUnusable, "the secret has no value in a field this authentication needs", nil)
			}
		}
	}
	if !s.headerValuesSafe() {
		s.close()
		return nil, unusable("a value of the secret that goes into a header has a line break or a control character, which a header cannot hold; nothing was sent")
	}
	s.fields = fields
	s.setMasks()
	return s, nil
}

// openForTokenHost opens the secret a second time for the host of the token request, when that is another host than the API's.
// The store decides, at the one place where it decides, whether the secret may be used for that address too (ADR-0106); when it
// may not, no request has been made. What was opened is closed again when it fails.
func (s *signer) openForTokenHost(ctx context.Context, opener secrets.Opener, set settings, purpose secrets.Purpose) error {
	if !set.token.cross {
		return nil
	}
	s.cross = true
	tokenOpened, err := opener.Open(ctx, set.auth.Credential.SecretID, secrets.Use{SourceID: set.id, Host: set.token.host, Port: set.token.port, Purpose: purpose})
	if err != nil {
		s.close()
		return openError(ctx, err)
	}
	s.tokenOpened = tokenOpened
	return nil
}

// headerValuesSafe tells whether the values of the secret that go into a header can be a value of a header. It is asked
// when the secret is opened, so that no request goes out (not even the one for the token) with a value that cannot be sent.
func (s *signer) headerValuesSafe() bool {
	switch s.auth.Kind {
	case ingest.AuthBearer, ingest.AuthHeader:
		if !headerSafe(s.field("token")) {
			return false
		}
	}
	for _, e := range s.auth.Headers {
		if src, ok := ingest.ParseHeaderFrom(e.From); ok && !src.Token && !headerSafe(s.field(src.Field)) {
			return false
		}
	}
	return true
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
	// Every field of the secret is taken out, not only those this read uses: a source may hand back what another dataset
	// sent (ADR-0100). The fields this read uses are the ones it needs to have a value in. The secret opened for the host of
	// the token request is the same secret, taken out as well (it may be another version).
	for _, o := range s.openeds() {
		all, _ := secrets.FieldNames(o.Kind())
		for _, f := range all {
			v := fieldOf(o, f)
			if f == "username" {
				names = append(names, v)
			} else {
				secret = append(secret, v)
			}
		}
		secret = append(secret, s.kindTexts(o)...)
	}
	for _, h := range s.auth.Headers {
		if src, ok := ingest.ParseHeaderFrom(h.From); ok && !src.Token {
			secret = append(secret, ingest.UnescapeBraces(h.Prefix)+s.field(src.Field))
		}
	}
	return secret, names
}

// kindTexts are the values the adapter makes of an opened secret for the requests of its kind of authentication.
func (s *signer) kindTexts(o *secrets.Opened) []string {
	basic := []string{basicValueOf(o), "Basic " + basicValueOf(o), basicFormValueOf(o), "Basic " + basicFormValueOf(o)}
	switch s.auth.Kind {
	case ingest.AuthBearer:
		return []string{"Bearer " + fieldOf(o, "token")}
	case ingest.AuthHeader:
		return []string{ingest.UnescapeBraces(s.auth.Prefix) + fieldOf(o, "token")}
	case ingest.AuthBasic:
		return basic
	case ingest.AuthTokenRequest:
		if s.basicRequest() {
			return basic
		}
	}
	return nil
}

// openeds lists the opened secrets of the read: the one of the API's address, and the one of the token host's when that is
// another.
func (s *signer) openeds() []*secrets.Opened {
	out := []*secrets.Opened{s.opened}
	if s.tokenOpened != nil {
		out = append(out, s.tokenOpened)
	}
	return out
}

func (s *signer) field(name string) string { return fieldOf(s.opened, name) }

// fieldOf is the value of a field of an opened secret, or the empty text for a field it has not (and for no secret).
func fieldOf(o *secrets.Opened, name string) string {
	if o == nil {
		return ""
	}
	v, _ := o.Value(name)
	return v.Reveal()
}

func basicValueOf(o *secrets.Opened) string {
	return base64.StdEncoding.EncodeToString([]byte(fieldOf(o, "username") + ":" + fieldOf(o, "password")))
}

// basicFormValueOf is the value of a Basic header as the OAuth 2.0 client authentication writes it (RFC 6749 2.3.1): the
// two texts escaped as a form before they are joined. A server may hand it back in this shape, so it is taken out too.
func basicFormValueOf(o *secrets.Opened) string {
	return base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(fieldOf(o, "username")) + ":" + url.QueryEscape(fieldOf(o, "password"))))
}

func (s *signer) basicRequest() bool {
	return s.auth.Request != nil && s.auth.Request.CredentialsIn == ingest.CredentialsInBasic
}

// tokenSecret is the secret the credentials of the token request are taken from: the one opened for the token host when it is
// another host, and the one of the API's address otherwise.
func (s *signer) tokenSecret() *secrets.Opened {
	if s.tokenOpened != nil {
		return s.tokenOpened
	}
	return s.opened
}

// address is the host and the port the secret was opened for; the requests go there.
func (s *signer) address() (secrets.Address, bool) {
	if s.opened == nil {
		return secrets.Address{}, false
	}
	return s.opened.Address(), true
}

// tokenAddress is the host and the port the secret was opened for the token request at, when that is not the API's.
func (s *signer) tokenAddress() (secrets.Address, bool) {
	if s.tokenOpened == nil {
		return secrets.Address{}, false
	}
	return s.tokenOpened.Address(), true
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

// apply puts the credentials into the headers of a request: the header the kind of authentication sends, and then the
// entries of auth.headers (which win a name that is the same).
func (s *signer) apply(h map[string][]string) error {
	if err := s.applyKind(h); err != nil {
		return err
	}
	if err := s.noSecretForTheAPI(); err != nil {
		return err
	}
	for _, e := range s.auth.Headers {
		s.applyEntry(h, e)
	}
	return nil
}

func (s *signer) applyKind(h map[string][]string) error {
	switch s.auth.Kind {
	case ingest.AuthBearer:
		setHeader(h, "Authorization", "Bearer "+s.field("token"))
	case ingest.AuthBasic:
		return s.setBasic(h)
	case ingest.AuthHeader:
		setHeader(h, s.auth.Header, ingest.UnescapeBraces(s.auth.Prefix)+s.field("token"))
	case ingest.AuthTokenRequest:
		if tok := s.token.text(); tok != "" && s.auth.Send != ingest.SendTemplate {
			setHeader(h, s.sendHeader(), s.sendPrefix()+tok)
		}
	}
	return nil
}

// applyEntry puts one entry of auth.headers: the token it names (when there is one), or a field of the secret. The values
// were looked at when the secret was opened (headerValuesSafe) and the token when it was read.
func (s *signer) applyEntry(h map[string][]string, e ingest.SecretHeader) {
	src, ok := ingest.ParseHeaderFrom(e.From)
	if !ok {
		return
	}
	prefix := ingest.UnescapeBraces(e.Prefix)
	if !src.Token {
		setHeader(h, e.Name, prefix+s.field(src.Field))
	} else if tok := s.token.text(); tok != "" {
		setHeader(h, e.Name, prefix+tok)
	}
}

// setBasic puts the Authorization header of the user name and the password. A user name with a colon cannot be told from
// the colon that ends it (RFC 7617), so it is refused and nothing is sent.
func (s *signer) setBasic(h map[string][]string) error { return setBasicOf(h, s.opened) }

// setTokenBasic is setBasic for the token request: the user name and the password are those of the secret opened for the
// token host.
func (s *signer) setTokenBasic(h map[string][]string) error { return setBasicOf(h, s.tokenSecret()) }

func setBasicOf(h map[string][]string, o *secrets.Opened) error {
	if strings.Contains(fieldOf(o, "username"), ":") {
		return unusable("the user name of the secret has a colon, which a Basic header cannot carry; nothing was sent")
	}
	setHeader(h, "Authorization", "Basic "+basicValueOf(o))
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
func (s *signer) drop() {
	s.token = secretText{}
	s.expires = time.Time{}
}

// dropIfDue forgets a token that holds no longer (the lifetime the source gave is up).
func (s *signer) dropIfDue(now time.Time) {
	if !s.expires.IsZero() && !now.Before(s.expires) {
		s.drop()
	}
}

func (s *signer) close() {
	s.drop()
	s.opened.Close()
	s.tokenOpened.Close()
}

// valueOf gives what a placeholder stands for in a request of the API: a field of the secret or the token the source gave.
func (s *signer) valueOf(p ingest.Placeholder) (string, error) { return s.valueIn(p, false) }

// valueIn is valueOf for a request that is the token request or not. The token request has the credentials, from the secret
// opened for its host, and no token (the old one goes nowhere, and the new one does not exist yet); a request of the API has
// the token, and the credentials only when the token host is the API's host.
func (s *signer) valueIn(p ingest.Placeholder, forToken bool) (string, error) {
	if p.Token {
		tok := s.token.text()
		switch {
		case forToken:
			return "", ingest.NewSourceError(ingest.SrcInternal, "the token request asks for a token, and the token is what it asks for", nil)
		case tok == "":
			return "", ingest.NewSourceError(ingest.SrcInternal, "a body asks for the token before the source gave one", nil)
		}
		return tok, nil
	}
	if s.opened == nil {
		return "", unusable("a body has a placeholder of the secret, and the authentication has no secret")
	}
	if s.cross && !forToken {
		return "", unusable("a body of a request of the API has a placeholder of the secret, and the token is asked for at another host; the secret goes to that host only, and nothing was sent")
	}
	if forToken {
		return fieldOf(s.tokenSecret(), p.Field), nil
	}
	return s.field(p.Field), nil
}

// addMasked adds texts to take out of what is given out: to the rows when they are long enough, and always to the records
// of what happened.
func (s *signer) addMasked(texts ...string) {
	s.mask = holdTexts(append(s.mask.list(), maskForms(longEnough(texts, minRowMaskBytes))...))
	s.wide = holdTexts(append(s.wide.list(), maskForms(texts)...))
}
