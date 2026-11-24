package ingest

import (
	"strings"
	"testing"
)

// The tests of the token request that goes to another host than the API's (ADR-0106). Every helper here begins with hxc so
// that it never meets a name of the older tests of this folder.

// hxcCross is a token request to the authentication server of another host, as OAuth 2.0 client credentials are asked for
// with the client's id and secret in the form.
const hxcCross = `{"kind":"token_request","credential":{"secret_id":"k"},"request":{"url":"https://idp.example.test/oauth2/token","format":"form","body":{"grant_type":"client_credentials","client_id":"{{credential.username}}","client_secret":"{{credential.password}}"}},"token_path":"access_token","expires_in_path":"expires_in"}`

// hxcRequest swaps the request of hxcCross for another one.
func hxcRequest(t *testing.T, request string) string {
	t.Helper()
	const was = `"request":{"url":"https://idp.example.test/oauth2/token","format":"form","body":{"grant_type":"client_credentials","client_id":"{{credential.username}}","client_secret":"{{credential.password}}"}}`
	if !strings.Contains(hxcCross, was) {
		t.Fatal("the request of the base definition is not where it was")
	}
	return strings.Replace(hxcCross, was, `"request":`+request, 1)
}

func hxcSwap(t *testing.T, doc, from, to string) string {
	t.Helper()
	if strings.Count(doc, from) != 1 {
		t.Fatalf("%q is in the document %d times", from, strings.Count(doc, from))
	}
	return strings.Replace(doc, from, to, 1)
}

// [REQ-840]
// The address of the token request is one of two: a path under the base address (the API's host) or a whole address, which
// may be another host. A whole address has an origin and a path and nothing else.
func TestHxcTheTokenRequestHasOneAddressAPathOrAWholeOne(t *testing.T) {
	hxaWant(t, "a whole address of another host", hxaDoc(hxcCross, "", ""))
	hxaWant(t, "the forms of the three kinds of OAuth client", hxaDoc(hxcRequest(t, `{"url":"https://idp.example.test/oauth2/token","credentials_in":"basic","format":"form","body":{"grant_type":"client_credentials"}}`), "", ""))
	hxaWant(t, "a key in a JSON body", hxaDoc(`{"kind":"token_request","credential":{"secret_id":"k"},"request":{"url":"https://auth.example.test:8443/api/login","format":"json","body":{"key":"{{credential.token}}"}},"token_path":"data.token"}`, "", ""))
	hxaWant(t, "a whole address of the API's origin", hxaDoc(hxcRequest(t, `{"url":"https://x.example.test/t","format":"json","body":{"u":"{{credential.username}}"}}`), "", ""))
	hxaWant(t, "an address of a plain http host", hxaDoc(hxcRequest(t, `{"url":"http://idp.example.test:8080/t","format":"form","body":{"a":"b"}}`), "", ""))
	bad := "source.auth.token_request_invalid"
	cases := []struct {
		name, request, want string
	}{
		{"a path and an address", `{"path":"/t","url":"https://idp.example.test/t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"neither", `{"format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/path"},
		{"another scheme", `{"url":"ftp://idp.example.test/t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"no scheme", `{"url":"idp.example.test/t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"no host", `{"url":"https:///t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a user name", `{"url":"https://u:p@idp.example.test/t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a query", `{"url":"https://idp.example.test/t?tenant=a","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"an empty query", `{"url":"https://idp.example.test/t?","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a fragment", `{"url":"https://idp.example.test/t#x","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"no path", `{"url":"https://idp.example.test","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a slash and nothing else is a path", `{"url":"https://idp.example.test//t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a step that goes up", `{"url":"https://idp.example.test/a/../t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a backslash", `{"url":"https://idp.example.test/a\\t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a brace", `{"url":"https://idp.example.test/{{credential.token}}","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a space", `{"url":"https://idp.example.test/a b","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a port that is none", `{"url":"https://idp.example.test:0/t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
		{"a port that is too big", `{"url":"https://idp.example.test:65536/t","format":"json","body":{"a":"b"}}`, bad + " /connection/auth/request/url"},
	}
	for _, c := range cases {
		hxaWant(t, c.name, hxaDoc(hxcRequest(t, c.request), "", ""), c.want)
	}
}

// [REQ-844]
// The certificate authority of the token host is its own: it is given for another origin than base_url, and never for the
// API's (whose is the connection's), and a path form has none to give.
func TestHxcTheCertificateAuthorityOfTheTokenHostIsItsOwn(t *testing.T) {
	pem := strings.ReplaceAll(adpCert, "\n", "\\n")
	other := `{"url":"https://idp.example.test/t","ca_pem":"` + pem + `","format":"json","body":{"a":"b"}}`
	hxaWant(t, "a certificate authority for the other host", hxaDoc(hxcRequest(t, other), "", ""))
	same := `{"url":"https://x.example.test/t","ca_pem":"` + pem + `","format":"json","body":{"a":"b"}}`
	hxaWant(t, "one for the API's own origin", hxaDoc(hxcRequest(t, same), "", ""), "source.auth.token_request_invalid /connection/auth/request/ca_pem")
	byPath := `{"path":"/t","ca_pem":"` + pem + `","format":"json","body":{"a":"b"}}`
	hxaWant(t, "one for a path", hxaDoc(hxcRequest(t, byPath), "", ""), "source.auth.token_request_invalid /connection/auth/request/ca_pem")
	key := `{"url":"https://idp.example.test/t","ca_pem":"-----BEGIN PRIVATE KEY-----\\nAAAA\\n-----END PRIVATE KEY-----\\n","format":"json","body":{"a":"b"}}`
	hxaWant(t, "a private key", hxaDoc(hxcRequest(t, key), "", ""), "source.connection.ca_pem_invalid /connection/auth/request/ca_pem")
	hxaWant(t, "text that is no certificate", hxaDoc(hxcRequest(t, strings.Replace(other, pem, "not a certificate", 1)), "", ""), "source.connection.ca_pem_invalid /connection/auth/request/ca_pem")
}

// [REQ-840]
// An origin is a scheme, a host and a port, with the port of the scheme where none is written: the same host on another
// port, or with another scheme, is another origin, and a spelling of the same host in capitals is the same.
func TestHxcAnOriginIsTheSchemeTheHostAndThePort(t *testing.T) {
	asDoc := func(url string) string {
		return hxaDoc(hxcRequest(t, `{"url":"`+url+`","format":"json","body":{"u":"{{credential.username}}"}}`), `,"headers":{"X-Team":"a"}`, hxaPostDataset(`{"k":"{{credential.password}}"}`))
	}
	elsewhere := "source.placeholder.not_allowed_here /datasets/0/request/body/k"
	for _, c := range []struct {
		name, url string
		cross     bool
	}{
		{"the same origin", "https://x.example.test/t", false},
		{"the default port written out", "https://x.example.test:443/t", false},
		{"the host in capitals", "https://X.Example.TEST/t", false},
		{"another port", "https://x.example.test:8443/t", true},
		{"another scheme", "http://x.example.test/t", true},
		{"another host", "https://idp.example.test/t", true},
		{"a host that is a part of the name", "https://x.example.test.evil.test/t", true},
		{"a sub domain", "https://auth.x.example.test/t", true},
	} {
		got := hxaCodes(t, asDoc(c.url))
		if cross := hxaHas(got, elsewhere); cross != c.cross {
			t.Errorf("%s: a credential in a body of the API is refused %v, want %v (%v)", c.name, cross, c.cross, got)
		}
	}
}

// [REQ-841]
// When the token is asked for at another host, a request of the API carries the token and no value of the secret: a
// credential in a body of the API and an entry of auth.headers that sends one are refused. The token itself may go to the
// API in a body and in a header, and when the token host is the API's own nothing is refused.
func TestHxcAnotherTokenHostKeepsTheSecretOutOfTheRequestsOfTheAPI(t *testing.T) {
	const bodyOfAPI = `{"id":"ds_a","post_reads":true,"request":{"method":"POST","path":"/a","body":%s},"natural_key":["a"],"columns":[{"name":"a"}]}`
	api := func(body string) string { return strings.Replace(bodyOfAPI, "%s", body, 1) }
	elsewhere := "source.placeholder.not_allowed_here /datasets/0/request/body/k"
	hxaWant(t, "a credential in a body of the API", hxaDoc(hxcCross, "", api(`{"k":"{{credential.password}}"}`)), elsewhere)
	hxaWant(t, "a credential deep in a body of the API", hxaDoc(hxcCross, "", api(`{"j":{"l":["{{credential.username}}"]}}`)), "source.placeholder.not_allowed_here /datasets/0/request/body/j/l/0")
	hxaWant(t, "the token in a body of the API", hxaDoc(hxcCross, "", api(`{"k":"{{auth.token}}"}`)))
	hxaWant(t, "a body with no placeholder", hxaDoc(hxcCross, "", api(`{"k":"v"}`)))
	withHeaders := func(h string) string {
		return hxcSwap(t, hxcCross, `"token_path":"access_token"`, `"token_path":"access_token","headers":[`+h+`]`)
	}
	hxaWant(t, "the token in a header of the API", hxaDoc(withHeaders(`{"name":"X-Session","from":"token","prefix":"S "}`), "", ""))
	hxaWant(t, "a field of the secret in a header of the API", hxaDoc(withHeaders(`{"name":"X-Client","from":"credential.username"}`), "", ""), "source.auth.header_source_invalid /connection/auth/headers/0/from")
	hxaWant(t, "one of two entries", hxaDoc(withHeaders(`{"name":"X-Session","from":"token"},{"name":"X-Client","from":"credential.password"}`), "", ""), "source.auth.header_source_invalid /connection/auth/headers/1/from")
	own := hxcRequest(t, `{"url":"https://x.example.test/t","format":"json","body":{"u":"{{credential.username}}"}}`)
	hxaWant(t, "the token host is the API's own: a credential in a body", hxaDoc(own, "", api(`{"k":"{{credential.password}}"}`)))
	hxaWant(t, "the token host is the API's own: a field in a header", hxaDoc(hxcSwap(t, own, `"token_path":"access_token"`, `"token_path":"access_token","headers":[{"name":"X-Client","from":"credential.username"}]`), "", ""))
	path := `{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json","body":{"u":"{{credential.username}}"}},"token_path":"token"}`
	hxaWant(t, "a path: a credential in a body", hxaDoc(path, "", api(`{"k":"{{credential.password}}"}`)))
}

// [REQ-841]
// The headers of the token request are its own, as texts: they are checked as the headers of the connection are, and the
// header of the authentication is not one of them.
func TestHxcTheTokenRequestHasHeadersOfItsOwnThatAreTexts(t *testing.T) {
	with := func(headers string) string {
		return hxaDoc(hxcRequest(t, `{"url":"https://idp.example.test/t","headers":`+headers+`,"format":"json","body":{"a":"b"}}`), "", "")
	}
	hxaWant(t, "a header", with(`{"X-Realm":"main"}`))
	hxaWant(t, "a header of a path request", hxaDoc(hxcRequest(t, `{"path":"/t","headers":{"X-Realm":"main"},"format":"json","body":{"a":"b"}}`), "", ""))
	hdr := "source.connection.header_not_allowed /connection/auth/request/headers/"
	hxaWant(t, "the header of the authentication", with(`{"Authorization":"x"}`), hdr+"Authorization")
	hxaWant(t, "a header the product sets", with(`{"Host":"x"}`), hdr+"Host")
	hxaWant(t, "a name that is none", with(`{"X Realm":"main"}`), hdr+"X Realm")
	hxaWant(t, "a secret in a header", with(`{"X-Key":"{{credential.password}}"}`), hdr+"X-Key")
	hxaWant(t, "the token in a header", with(`{"X-Key":"{{auth.token}}"}`), hdr+"X-Key")
	hxaWant(t, "a control character", with(`{"X-Key":"a\u0001b"}`), hdr+"X-Key")
	hxaWant(t, "a name in two spellings", with(`{"X-Realm":"a","x-realm":"b"}`), hdr+"x-realm")
}

// [REQ-842]
// The lifetime of the token is read from a path of the answer, which is a path of the answer and no more: not with the whole
// body as the token, and only with the kind that asks for a token.
func TestHxcTheLifetimeOfTheTokenIsReadFromAPathOfTheAnswer(t *testing.T) {
	hxaWant(t, "a path", hxaDoc(hxcCross, "", ""))
	hxaWant(t, "a path of a path request", hxaDoc(strings.Replace(hxaTokenAuth, `"token_path":"token"`, `"token_path":"token","expires_in_path":"data.life"`, 1), "", ""))
	hxaWant(t, "with the whole body as the token", hxaDoc(`{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/api/session","credentials_in":"basic"},"token_from":"body","expires_in_path":"life"}`, "", ""),
		"source.auth.token_use_invalid /connection/auth/expires_in_path")
	hxaWant(t, "a path that is none", hxaDoc(hxcSwap(t, hxcCross, `"expires_in"`, `"a["`), "", ""), "source.path.invalid /connection/auth/expires_in_path")
	hxaWant(t, "a kind that asks for no token", hxaDoc(`{"kind":"bearer","credential":{"secret_id":"k"},"expires_in_path":"x"}`, "", ""), "source.auth.field_not_for_kind /connection/auth/expires_in_path")
}

// [REQ-843]
// A change of where the token is asked for, of what is trusted there, of the headers that go there and of when the token is
// asked for again is a change of the connection: each of the fields is in the table of ClassifyChange (the reflection test
// of the older tests keeps it so), and the changes are told as such here.
func TestHxcAChangeOfTheTokenAddressIsAChangeOfTheConnection(t *testing.T) {
	parse := func(doc string) Source {
		src, err := ParseAdapterSource(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("%v", err)
		}
		return src
	}
	pem := strings.ReplaceAll(adpCert, "\n", "\\n")
	base := hxaDoc(hxcCross, "", "")
	byPath := hxaDoc(hxaTokenAuth, "", "")
	for _, c := range []struct {
		name     string
		from, to string
	}{
		{"the host of the token", base, hxcSwap(t, base, "https://idp.example.test/", "https://idp2.example.test/")},
		{"the port of the token", base, hxcSwap(t, base, "https://idp.example.test/", "https://idp.example.test:8443/")},
		{"the path of the token", base, hxcSwap(t, base, "/oauth2/token", "/oauth2/other")},
		{"the scheme of the token", base, hxcSwap(t, base, "https://idp.example.test/", "http://idp.example.test/")},
		{"from a path to a whole address", byPath, hxaDoc(strings.Replace(hxaTokenAuth, `"path":"/t"`, `"url":"https://x.example.test/t"`, 1), "", "")},
		{"a certificate authority", base, hxcSwap(t, base, `"url":"https://idp.example.test/oauth2/token"`, `"url":"https://idp.example.test/oauth2/token","ca_pem":"`+pem+`"`)},
		{"a header of the token request", base, hxcSwap(t, base, `"url":"https://idp.example.test/oauth2/token"`, `"url":"https://idp.example.test/oauth2/token","headers":{"X-Realm":"a"}`)},
		{"the path of the lifetime", base, hxcSwap(t, base, `"expires_in_path":"expires_in"`, `"expires_in_path":"lifetime"`)},
		{"the lifetime goes", base, hxcSwap(t, base, `,"expires_in_path":"expires_in"`, ``)},
	} {
		got := ClassifyChange(parse(c.from), parse(c.to))
		if got.Kind != ChangeConnection || !hxaHas(got.Reasons, "connection.auth") {
			t.Errorf("%s: %s %v", c.name, got.Kind, got.Reasons)
		}
	}
	if got := ClassifyChange(parse(base), parse(hxcSwap(t, base, `"columns":[{"name":"a"}]`, `"columns":[{"name":"a"},{"name":"b"}]`))); got.Kind != ChangeMapping {
		t.Errorf("a column of a source whose token is at another host: %s %v", got.Kind, got.Reasons)
	}
}

// [REQ-841]
// Whether a definition asks for its token at another origin is told in one place, safe the way round: a definition that is
// not a token request, or has a path, is not; one whose addresses cannot be read is.
func TestHxcAnotherOriginIsToldInOnePlace(t *testing.T) {
	parse := func(doc string) Source {
		src, err := ParseAdapterSource(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("%v", err)
		}
		return src
	}
	if !TokenGoesToAnotherOrigin(parse(hxaDoc(hxcCross, "", ""))) {
		t.Error("a token at another host was not told")
	}
	for name, doc := range map[string]string{
		"a path":         hxaDoc(hxaTokenAuth, "", ""),
		"no auth":        hxaDoc("", "", ""),
		"another kind":   hxaDoc(hxaCustom, "", ""),
		"the same place": hxaDoc(hxcRequest(t, `{"url":"https://x.example.test/t","format":"json","body":{"a":"b"}}`), "", ""),
	} {
		if TokenGoesToAnotherOrigin(parse(doc)) {
			t.Errorf("%s was told to go to another origin", name)
		}
	}
	if TokenGoesToAnotherOrigin(Source{Adapter: AdapterFile}) || TokenGoesToAnotherOrigin(Source{Adapter: AdapterHTTP}) {
		t.Error("a source with no connection was told to go to another origin")
	}
	broken := Source{Adapter: AdapterHTTP, Connection: &Connection{BaseURL: "::", Auth: &Auth{Kind: AuthTokenRequest, Request: &TokenRequest{URL: "https://idp.example.test/t"}}}}
	if !TokenGoesToAnotherOrigin(broken) {
		t.Error("a base address that cannot be read was taken for the token's origin")
	}
	broken.Connection.BaseURL = "https://x.example.test"
	broken.Connection.Auth.Request.URL = "%zz"
	if !TokenGoesToAnotherOrigin(broken) {
		t.Error("a token address that cannot be read was taken for the API's origin")
	}
}
