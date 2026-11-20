package ingest

import (
	"strings"
	"testing"
)

// The tests of the places where a secret or the token of an HTTP source goes into a request (ADR-0100). Every helper here
// begins with hxa so that it never meets a name of the older tests of this folder.

// hxaDoc makes the definition of an http source with the given authentication, headers of the connection and one dataset.
func hxaDoc(auth, connExtra, dataset string) string {
	if auth == "" {
		auth = `{"kind":"none"}`
	}
	if dataset == "" {
		dataset = `{"id":"ds_a","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]}`
	}
	return `{"version":1,"id":"src_x","adapter":"http","connection":{"base_url":"https://x.example.test"` + connExtra + `,"auth":` + auth + `},"datasets":[` + dataset + `]}`
}

const hxaPost = `{"id":"ds_a","post_reads":true,"request":{"method":"POST","path":"/a","body":%s},"natural_key":["a"],"columns":[{"name":"a"}]}`

func hxaPostDataset(body string) string { return strings.Replace(hxaPost, "%s", body, 1) }

const (
	hxaCustom    = `{"kind":"custom","credential":{"secret_id":"k"},"headers":[{"name":"X-Api-Key","from":"credential.token","prefix":"Token "}]}`
	hxaTokenAuth = `{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json","body":{"u":"{{credential.username}}","p":"{{credential.password}}"}},"token_path":"token"}`
)

// hxaCodes lists the code and the pointer of every error a definition has.
func hxaCodes(t *testing.T, doc string) []string {
	t.Helper()
	_, err := ParseAdapterSource(strings.NewReader(doc))
	if err == nil {
		return nil
	}
	de, ok := DefinitionErrorsOf(err)
	if !ok {
		t.Fatalf("not a definition error: %v", err)
	}
	var out []string
	for _, e := range de.List {
		out = append(out, e.Code+" "+e.Pointer)
	}
	return out
}

func hxaWant(t *testing.T, name, doc string, want ...string) {
	t.Helper()
	got := hxaCodes(t, doc)
	if len(want) == 0 {
		if len(got) != 0 {
			t.Errorf("%s: refused: %v", name, got)
		}
		return
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Errorf("%s: no error %q in %v", name, w, got)
		}
	}
}

// [REQ-742]
// The grammar of the places of a placeholder: a whole value of a body may be {{credential.<field>}} or {{auth.token}}, and a
// text that has one inside it, a name that is one, an address that has one and a header of the connection that has one are
// refused; the doubled braces stay the brace itself.
func TestHxaPlaceholdersAreWholeValuesOfABodyAndNothingElse(t *testing.T) {
	post := func(body string) string {
		return hxaDoc(`{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json","body":{"u":"{{credential.username}}"}},"token_path":"token"}`, "", hxaPostDataset(body))
	}
	elsewhere := "source.placeholder.not_allowed_here"
	cases := []struct {
		name, doc string
		want      []string
	}{
		{"a whole credential value", post(`{"k":"{{credential.password}}"}`), nil},
		{"a whole token value", post(`{"k":"{{auth.token}}"}`), nil},
		{"nested in an object and a list", post(`{"a":{"b":["{{auth.token}}",{"c":"{{credential.password}}"}]}}`), nil},
		{"the doubled braces are the brace", post(`{"k":"{{kept}}"}`), nil},
		{"a credential inside a text", post(`{"k":"x {{credential.password}}"}`), []string{elsewhere + " /datasets/0/request/body/k"}},
		{"a token inside a text", post(`{"k":"Bearer {{auth.token}}"}`), []string{elsewhere + " /datasets/0/request/body/k"}},
		{"four braces do not write the text of a placeholder", post(`{"k":"{{{{credential.password}}}}"}`), []string{elsewhere + " /datasets/0/request/body/k"}},
		{"a placeholder as a name", post(`{"{{credential.password}}":"v"}`), []string{elsewhere + " /datasets/0/request/body/{{credential.password}}"}},
		{"a name of a field that is not a name", post(`{"k":"{{credential.Bad-Name}}"}`), []string{elsewhere + " /datasets/0/request/body/k"}},
		{"a single brace", post(`{"k":"{auth.token}"}`), []string{"source.text.braces_not_allowed /datasets/0/request/body/k"}},
		{"a credential in a query", hxaDoc(hxaTokenAuth, "", `{"id":"ds_a","request":{"path":"/a","query":{"k":"{{credential.password}}"}},"natural_key":["a"],"columns":[{"name":"a"}]}`), []string{elsewhere + " /datasets/0/request/query/k"}},
		{"the token in a query", hxaDoc(hxaTokenAuth, "", `{"id":"ds_a","request":{"path":"/a","query":{"k":"{{auth.token}}"}},"natural_key":["a"],"columns":[{"name":"a"}]}`), []string{elsewhere + " /datasets/0/request/query/k"}},
		{"a credential in a path", hxaDoc(hxaTokenAuth, "", `{"id":"ds_a","request":{"path":"/a/{{credential.password}}"},"natural_key":["a"],"columns":[{"name":"a"}]}`), []string{elsewhere + " /datasets/0/request/path"}},
		{"a credential in a header of the connection", hxaDoc(hxaTokenAuth, `,"headers":{"X-K":"{{credential.password}}"}`, ""), []string{"source.connection.header_not_allowed /connection/headers/X-K"}},
		{"the token in a header of the connection", hxaDoc(hxaTokenAuth, `,"headers":{"X-K":"Bearer {{auth.token}}"}`, ""), []string{"source.connection.header_not_allowed /connection/headers/X-K"}},
		{"the old text of a header stays what it was", hxaDoc(hxaTokenAuth, `,"headers":{"X-Team":"a {{b}}"}`, ""), nil},
	}
	for _, c := range cases {
		hxaWant(t, c.name, c.doc, c.want...)
	}
}

// [REQ-742]
// The token does not exist yet in the request that asks for it, so no placeholder of it is written in its body, and the
// body may be a tree (a JSON-RPC request) as the body of a request for data may.
func TestHxaTheBodyOfTheTokenRequestIsATreeAndHasNoToken(t *testing.T) {
	tree := `{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/rpc","format":"json","body":{"jsonrpc":"2.0","method":"user.login","params":{"username":"{{credential.username}}","password":"{{credential.password}}"},"id":1}},"token_path":"result"}`
	hxaWant(t, "a tree", hxaDoc(tree, "", ""))
	src, err := ParseAdapterSource(strings.NewReader(hxaDoc(tree, "", "")))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := src.Connection.Auth.Request.Body["params"].(map[string]any); !ok {
		t.Errorf("the body %v", src.Connection.Auth.Request.Body)
	}
	elsewhere := "source.placeholder.not_allowed_here /connection/auth/request/body/t"
	hxaWant(t, "the token in the token request", hxaDoc(strings.Replace(hxaTokenAuth, `"p":"{{credential.password}}"`, `"t":"{{auth.token}}"`, 1), "", ""), elsewhere)
	deep := strings.Replace(hxaTokenAuth, `"p":"{{credential.password}}"`, `"t":{"x":["{{auth.token}}"]}`, 1)
	hxaWant(t, "the token deep in the token request", hxaDoc(deep, "", ""), "source.placeholder.not_allowed_here /connection/auth/request/body/t/x/0")
	form := strings.Replace(hxaTokenAuth, `"format":"json"`, `"format":"form"`, 1)
	hxaWant(t, "a form has texts only", hxaDoc(strings.Replace(form, `"p":"{{credential.password}}"`, `"p":{"x":"y"}`, 1), "", ""), "source.auth.token_request_invalid /connection/auth/request/body/p")
	hxaWant(t, "a form with a number", hxaDoc(strings.Replace(form, `"p":"{{credential.password}}"`, `"p":5`, 1), "", ""), "source.auth.token_request_invalid /connection/auth/request/body/p")
	hxaWant(t, "a text with a placeholder in it", hxaDoc(strings.Replace(hxaTokenAuth, `"p":"{{credential.password}}"`, `"p":"x{{credential.password}}"`, 1), "", ""),
		"source.auth.placeholder_invalid /connection/auth/request/body/p")
	var deepest = `"x"`
	for i := 0; i < 9; i++ {
		deepest = `{"a":` + deepest + `}`
	}
	hxaWant(t, "a body nested too deep", hxaDoc(strings.Replace(hxaTokenAuth, `"p":"{{credential.password}}"`, `"p":`+deepest, 1), "", ""), "source.auth.token_request_invalid /connection/auth/request/body/p/a/a/a/a/a/a/a/a")
}

// [REQ-742]
// Where the authentication gives what a placeholder stands for: a credential needs a secret, the token needs the kind
// token_request, the kind custom has to send something, a secret has one kind, and a token that is sent only by a template
// has to be used by one.
func TestHxaAPlaceholderNeedsWhatItStandsFor(t *testing.T) {
	elsewhere := "source.placeholder.not_allowed_here"
	body := hxaPostDataset(`{"k":"{{auth.token}}"}`)
	credBody := hxaPostDataset(`{"k":"{{credential.token}}"}`)
	hxaWant(t, "the token with the kind bearer", hxaDoc(`{"kind":"bearer","credential":{"secret_id":"k"}}`, "", body), elsewhere+" /datasets/0/request/body/k")
	hxaWant(t, "the token with the kind custom", hxaDoc(`{"kind":"custom","credential":{"secret_id":"k"}}`, "", body), elsewhere+" /datasets/0/request/body/k")
	hxaWant(t, "a credential with no authentication", hxaDoc("", "", credBody), elsewhere+" /datasets/0/request/body/k")
	hxaWant(t, "a credential with the kind bearer", hxaDoc(`{"kind":"bearer","credential":{"secret_id":"k"}}`, "", credBody))
	hxaWant(t, "the kind custom with a body that uses the secret", hxaDoc(`{"kind":"custom","credential":{"secret_id":"k"}}`, "", credBody))
	hxaWant(t, "the kind custom that sends nothing", hxaDoc(`{"kind":"custom","credential":{"secret_id":"k"}}`, "", ""), "source.auth.field_missing /connection/auth/headers")
	hxaWant(t, "the kind custom with a header", hxaDoc(hxaCustom, "", ""))
	hxaWant(t, "the kind custom with no secret", hxaDoc(`{"kind":"custom","headers":[{"name":"X-K","from":"credential.token"}]}`, "", ""), "source.auth.field_missing /connection/auth/credential")
	hxaWant(t, "a token and a password in one secret", hxaDoc(`{"kind":"custom","credential":{"secret_id":"k"},"headers":[{"name":"X-K","from":"credential.token"},{"name":"X-U","from":"credential.username"}]}`, "", ""),
		elsewhere+" /connection/auth")
	sendTemplate := strings.Replace(hxaTokenAuth, `"token_path":"token"`, `"token_path":"token","send":"template"`, 1)
	hxaWant(t, "a token that goes nowhere", hxaDoc(sendTemplate, "", ""), "source.auth.token_request_invalid /connection/auth/send")
	hxaWant(t, "a token sent by a body", hxaDoc(sendTemplate, "", body))
	hxaWant(t, "a token sent by a header", hxaDoc(strings.Replace(sendTemplate, `"send":"template"`, `"send":"template","headers":[{"name":"X-T","from":"token"}]`, 1), "", ""))
	hxaWant(t, "a send that is none of the two", hxaDoc(strings.Replace(hxaTokenAuth, `"token_path":"token"`, `"token_path":"token","send":"later"`, 1), "", ""), "source.auth.token_request_invalid /connection/auth/send")
	hxaWant(t, "send_header with a template", hxaDoc(strings.Replace(sendTemplate, `"send":"template"`, `"send":"template","send_header":"X-T"`, 1), "", body), "source.auth.token_request_invalid /connection/auth/send_header")
	hxaWant(t, "a token request that asks for a token secret", hxaDoc(`{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json","body":{"key":"{{credential.token}}"}},"token_path":"token"}`, "", ""))
}

// [REQ-741]
// auth.headers: the names a definition may write, none that servers keep in their logs, no name twice, a place the value
// comes from, and only the kinds custom and token_request have it.
func TestHxaTheHeadersOfTheAuthenticationAreNamedAndKeptOutOfLogs(t *testing.T) {
	custom := func(entries string) string {
		return hxaDoc(`{"kind":"custom","credential":{"secret_id":"k"},"headers":[`+entries+`]}`, "", "")
	}
	entry := func(name, from, prefix string) string {
		return `{"name":"` + name + `","from":"` + from + `","prefix":"` + prefix + `"}`
	}
	hxaWant(t, "a header with a prefix", custom(entry("X-Api-Key", "credential.token", "Token ")))
	hxaWant(t, "Authorization may be named", custom(entry("Authorization", "credential.token", "Token ")))
	forbidden := "source.connection.header_not_allowed /connection/auth/headers/0/name"
	for _, name := range []string{"Host", "Content-Length", "Transfer-Encoding", "Cookie", "Proxy-Authorization", "X-HTTP-Method-Override", "A B"} {
		hxaWant(t, name, custom(entry(name, "credential.token", "")), forbidden)
	}
	for _, name := range []string{"User-Agent", "Referer", "Forwarded", "Via", "X-Request-Id", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-IP", "traceparent", "From", "Origin"} {
		hxaWant(t, "a header that servers log: "+name, custom(entry(name, "credential.token", "")), forbidden)
	}
	hxaWant(t, "a name twice", custom(entry("X-K", "credential.token", "")+","+entry("x-k", "credential.token", "")), "source.connection.header_not_allowed /connection/auth/headers/1/name")
	hxaWant(t, "a from that is none", custom(entry("X-K", "secret", "")), "source.connection.header_not_allowed /connection/auth/headers/0/from")
	hxaWant(t, "a from with a name that is not one", custom(entry("X-K", "credential.Token", "")), "source.connection.header_not_allowed /connection/auth/headers/0/from")
	hxaWant(t, "the token with the kind custom", custom(entry("X-K", "token", "")), "source.connection.header_not_allowed /connection/auth/headers/0/from")
	hxaWant(t, "a prefix with a line break", custom(`{"name":"X-K","from":"credential.token","prefix":"a\nb"}`), "source.connection.header_not_allowed /connection/auth/headers/0/prefix")
	hxaWant(t, "a header of the token", hxaDoc(strings.Replace(hxaTokenAuth, `"token_path":"token"`, `"token_path":"token","headers":[`+entry("X-T", "token", "T ")+`]`, 1), "", ""))
	var many []string
	for i := 0; i < 17; i++ {
		many = append(many, entry("X-K"+strings.Repeat("a", i), "credential.token", ""))
	}
	hxaWant(t, "more than sixteen", custom(strings.Join(many, ",")), "source.connection.header_not_allowed /connection/auth/headers")
	hxaWant(t, "headers with the kind bearer", hxaDoc(`{"kind":"bearer","credential":{"secret_id":"k"},"headers":[`+entry("X-K", "credential.token", "")+`]}`, "", ""), "source.auth.field_not_for_kind /connection/auth/headers")
	hxaWant(t, "the connection header Authorization stays refused", hxaDoc(hxaCustom, `,"headers":{"Authorization":"x"}`, ""), "source.connection.header_not_allowed /connection/headers/Authorization")
}

// [REQ-740]
// A token request may send the credentials in a Basic header and then has no body, and the token may be the whole answer;
// the request of a token that has neither a path nor that is still refused, as the locked case has always been.
func TestHxaBasicCredentialsAndATokenThatIsTheWholeBody(t *testing.T) {
	session := `{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/api/session","credentials_in":"basic"},"token_from":"body","send_header":"X-Session","send_prefix":""}`
	hxaWant(t, "a session of the vCenter kind", hxaDoc(session, "", ""))
	src, err := ParseAdapterSource(strings.NewReader(hxaDoc(session, "", "")))
	if err != nil {
		t.Fatal(err)
	}
	if a := src.Connection.Auth; a.TokenFrom != TokenFromBody || a.Request.CredentialsIn != CredentialsInBasic || len(a.Request.Body) != 0 || a.TokenPath != "" {
		t.Errorf("auth %+v request %+v", a, a.Request)
	}
	hxaWant(t, "basic with a form of OAuth", hxaDoc(`{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"form","credentials_in":"basic","body":{"grant_type":"client_credentials"}},"token_path":"access_token"}`, "", ""))
	hxaWant(t, "basic with a body and no format", hxaDoc(`{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","credentials_in":"basic","body":{"a":"b"}},"token_path":"t"}`, "", ""), "source.auth.token_request_invalid /connection/auth/request/format")
	hxaWant(t, "basic and a credential in the body", hxaDoc(`{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json","credentials_in":"basic","body":{"u":"{{credential.username}}"}},"token_path":"t"}`, "", ""),
		"source.auth.token_request_invalid /connection/auth/request/body/u")
	hxaWant(t, "credentials_in that is none", hxaDoc(strings.Replace(session, `"basic"`, `"header"`, 1), "", ""), "source.auth.token_request_invalid /connection/auth/request/credentials_in")
	hxaWant(t, "body and no format", hxaDoc(strings.Replace(session, `,"credentials_in":"basic"`, ``, 1), "", ""), "source.auth.token_request_invalid /connection/auth/request/format")
	hxaWant(t, "token_from body with a token_path", hxaDoc(strings.Replace(session, `"token_from":"body"`, `"token_from":"body","token_path":"t"`, 1), "", ""), "source.auth.token_request_invalid /connection/auth/token_path")
	hxaWant(t, "token_from that is none", hxaDoc(strings.Replace(session, `"body"`, `"headers"`, 1), "", ""), "source.auth.token_request_invalid /connection/auth/token_from")
	hxaWant(t, "no token_path and no token_from", hxaDoc(strings.Replace(session, `"token_from":"body",`, ``, 1), "", ""), "source.auth.field_missing /connection/auth/token_path")
	hxaWant(t, "token_from path with a token_path", hxaDoc(strings.Replace(hxaTokenAuth, `"token_path":"token"`, `"token_path":"token","token_from":"path"`, 1), "", ""))
	hxaWant(t, "token_from with the kind bearer", hxaDoc(`{"kind":"bearer","credential":{"secret_id":"k"},"token_from":"body"}`, "", ""), "source.auth.field_not_for_kind /connection/auth/token_from")
}

// [REQ-745]
// What a change of a definition touches: the connection (where, with what secret, how much and what is sent) or the mapping.
func TestHxaAChangeOfTheConnectionIsToldFromAChangeOfTheMapping(t *testing.T) {
	parse := func(doc string) Source {
		src, err := ParseAdapterSource(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("%v", err)
		}
		return src
	}
	base := hxaDoc(hxaTokenAuth, `,"headers":{"X-Team":"a"}`, `{"id":"ds_a","request":{"path":"/a","query":{"k":"v"}},"records_path":"items","pagination":{"kind":"page","page_param":"p","size_param":"s","size":100,"first_page":1},"natural_key":["a"],"columns":[{"name":"a"}]}`)
	post := hxaDoc(hxaTokenAuth, "", `{"id":"ds_a","post_reads":true,"request":{"method":"POST","path":"/a","body":{"q":"x"}},"natural_key":["a"],"columns":[{"name":"a"}]}`)
	swap := func(doc, from, to string) string {
		if strings.Count(doc, from) != 1 {
			t.Fatalf("%q is in the document %d times", from, strings.Count(doc, from))
		}
		return strings.Replace(doc, from, to, 1)
	}
	cases := []struct {
		name     string
		from, to string
		want     ChangeKind
		reason   string
	}{
		{"the same definition", base, base, ChangeMapping, ""},
		{"a column", base, swap(base, `"columns":[{"name":"a"}]`, `"columns":[{"name":"a"},{"name":"b"}]`), ChangeMapping, ""},
		{"the path of a GET dataset", base, swap(base, `"path":"/a"`, `"path":"/b"`), ChangeMapping, ""},
		{"the records_path", base, swap(base, `"items"`, `"rows"`), ChangeMapping, ""},
		{"a pagination that reads less", base, swap(base, `"size":100`, `"size":50`), ChangeMapping, ""},
		{"a timeout that is lowered", swap(base, `"base_url"`, `"timeout_seconds":300,"base_url"`), swap(base, `"base_url"`, `"timeout_seconds":10,"base_url"`), ChangeMapping, ""},
		{"the base address", base, swap(base, "https://x.example.test", "https://y.example.test"), ChangeConnection, "connection.base_url"},
		{"the secret", base, swap(base, `"secret_id":"k"`, `"secret_id":"j"`), ChangeConnection, "connection.auth"},
		{"the path of the token request", base, swap(base, `"path":"/t"`, `"path":"/u"`), ChangeConnection, "connection.auth"},
		{"a header of the connection", base, swap(base, `"X-Team":"a"`, `"X-Team":"b"`), ChangeConnection, "connection.headers"},
		{"the authentication goes", base, swap(base, hxaTokenAuth, `{"kind":"none"}`), ChangeConnection, "connection.auth"},
		{"an entry of auth.headers", hxaDoc(hxaCustom, "", ""), hxaDoc(strings.Replace(hxaCustom, `"X-Api-Key"`, `"X-Other"`, 1), "", ""), ChangeConnection, "connection.auth"},
		{"a certificate", base, swap(base, `"base_url"`, `"ca_pem":"`+strings.ReplaceAll(adpCert, "\n", "\\n")+`","base_url"`), ChangeConnection, "connection.ca_pem"},
		{"a timeout that is raised", base, swap(base, `"base_url"`, `"timeout_seconds":300,"base_url"`), ChangeConnection, "connection.timeout_seconds"},
		{"a rate that is raised", base, swap(base, `"base_url"`, `"requests_per_second":50,"base_url"`), ChangeConnection, "connection.requests_per_second"},
		{"a response size that is raised", base, swap(base, `"base_url"`, `"max_response_bytes":100000000,"base_url"`), ChangeConnection, "connection.max_response_bytes"},
		{"max_pages that is raised", base, swap(base, `"first_page":1`, `"first_page":1,"max_pages":5000`), ChangeConnection, "datasets.ds_a.pagination"},
		{"a page that is bigger", base, swap(base, `"size":100`, `"size":500`), ChangeConnection, "datasets.ds_a.pagination"},
		{"post_reads is turned on", base, post, ChangeConnection, "datasets.ds_a.post_reads"},
		{"the request of a POST dataset", post, swap(post, `"path":"/a"`, `"path":"/b"`), ChangeConnection, "datasets.ds_a.post_reads"},
		{"the body of a POST dataset", post, swap(post, `"q":"x"`, `"q":"y"`), ChangeConnection, "datasets.ds_a.post_reads"},
		{"a column of a POST dataset", post, swap(post, `"columns":[{"name":"a"}]`, `"columns":[{"name":"a"},{"name":"b"}]`), ChangeMapping, ""},
		{"a placeholder is put in a body", post, swap(post, `"q":"x"`, `"q":"{{credential.password}}"`), ChangeConnection, "datasets.ds_a.placeholder"},
		{"a placeholder is taken out of a body", swap(post, `"q":"x"`, `"q":"{{credential.password}}"`), post, ChangeConnection, "datasets.ds_a.post_reads"},
		{"a dataset is new", base, swap(base, `"datasets":[`, `"datasets":[{"id":"ds_b","request":{"path":"/b"},"natural_key":["a"],"columns":[{"name":"a"}]},`), ChangeMapping, ""},
		{"a dataset that POSTs is new", base, swap(base, `"datasets":[`, `"datasets":[{"id":"ds_b","post_reads":true,"request":{"method":"POST","path":"/b"},"natural_key":["a"],"columns":[{"name":"a"}]},`), ChangeConnection, "datasets.ds_b.post_reads"},
		{"a dataset goes", swap(base, `"datasets":[`, `"datasets":[{"id":"ds_b","post_reads":true,"request":{"method":"POST","path":"/b"},"natural_key":["a"],"columns":[{"name":"a"}]},`), base, ChangeMapping, ""},
	}
	for _, c := range cases {
		got := ClassifyChange(parse(c.from), parse(c.to))
		if got.Kind != c.want || (c.reason != "" && !hxaHas(got.Reasons, c.reason)) || (c.want == ChangeMapping && len(got.Reasons) != 0) {
			t.Errorf("%s: %s %v, want %s %s", c.name, got.Kind, got.Reasons, c.want, c.reason)
		}
	}
	if got := ClassifyChange(Source{}, parse(base)); got.Kind != ChangeConnection || !hxaHas(got.Reasons, "new_source") {
		t.Errorf("a new source: %+v", got)
	}
	pg := parse(adpPG)
	moved := pg
	cn := *pg.Connection
	cn.Host = "other.example.test"
	moved.Connection = &cn
	if got := ClassifyChange(pg, moved); got.Kind != ChangeConnection || !hxaHas(got.Reasons, "connection.host") {
		t.Errorf("a host: %+v", got)
	}
	sql := pg
	sql.Datasets = append([]Dataset{}, pg.Datasets...)
	sql.Datasets[1].SQL = "SELECT vm_id, host_id FROM placement"
	if got := ClassifyChange(pg, sql); got.Kind != ChangeMapping {
		t.Errorf("a statement: %+v", got)
	}
	if got := ClassifyChange(pg, parse(base)); got.Kind != ChangeConnection || !hxaHas(got.Reasons, "adapter") {
		t.Errorf("an adapter: %+v", got)
	}
}

func hxaHas(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// [REQ-742]
// The one reading of a placeholder, and the fields an authentication and a body ask of the secret.
func TestHxaPlaceholdersAreReadInOnePlace(t *testing.T) {
	for text, want := range map[string]Placeholder{"{{credential.password}}": {Field: "password"}, "{{auth.token}}": {Token: true}} {
		if got, ok := ParsePlaceholder(text); !ok || got != want {
			t.Errorf("%s: %+v %v", text, got, ok)
		}
	}
	for _, text := range []string{"", "{{credential.}}", "{{credential.A}}", " {{auth.token}}", "{{auth.token}} ", "{auth.token}", "{{auth.other}}", "x{{credential.a}}"} {
		if _, ok := ParsePlaceholder(text); ok {
			t.Errorf("%q is read as a placeholder", text)
		}
	}
	for text, want := range map[string]HeaderSource{"token": {Token: true}, "credential.token": {Field: "token"}} {
		if got, ok := ParseHeaderFrom(text); !ok || got != want {
			t.Errorf("%s: %+v %v", text, got, ok)
		}
	}
	for _, text := range []string{"", "credential", "credential.", "Token", "credential.A"} {
		if _, ok := ParseHeaderFrom(text); ok {
			t.Errorf("%q is read as a source of a header", text)
		}
	}
	body := map[string]any{"a": "{{credential.token}}", "b": []any{"x", map[string]any{"c": "{{auth.token}}"}}, "d": 5.0}
	if got := PlaceholdersOf(body); len(got) != 2 || got[0] != (Placeholder{Field: "token"}) || got[1] != (Placeholder{Token: true}) {
		t.Errorf("%+v", got)
	}
	auth := Auth{Kind: AuthCustom, Headers: []SecretHeader{{Name: "X-U", From: "credential.username"}, {Name: "X-T", From: "token"}}}
	if got := CredentialFields(auth, map[string]any{"k": "{{credential.token}}"}); strings.Join(got, ",") != "username,token" {
		t.Errorf("%v", got)
	}
	basic := Auth{Kind: AuthTokenRequest, Request: &TokenRequest{CredentialsIn: CredentialsInBasic}}
	if got := CredentialFields(basic, nil); strings.Join(got, ",") != "username,password" {
		t.Errorf("%v", got)
	}
}
