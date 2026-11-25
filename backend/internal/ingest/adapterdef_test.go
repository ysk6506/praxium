package ingest

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// A throwaway certificate made for these tests (no one holds its key); it stands for the CA a definition may carry.
const adpCert = "-----BEGIN CERTIFICATE-----\nMIIBhTCCASugAwIBAgIURtGaGZSb7VAVHQQp7STkhcncoR8wCgYIKoZIzj0EAwIw\nFzEVMBMGA1UEAwwMZXhhbXBsZS50ZXN0MCAXDTI2MTExOTEwMjEyN1oYDzIxMjYx\nMDI2MTAyMTI3WjAXMRUwEwYDVQQDDAxleGFtcGxlLnRlc3QwWTATBgcqhkjOPQIB\nBggqhkjOPQMBBwNCAASCdORDNqElggutE2cVqCCOTZ44AGGGM7ACcNJtJstRWgLZ\njCOmhbFMo01h2e0j2AL6MazLrO2T6jji4af51UPho1MwUTAdBgNVHQ4EFgQUVXuy\nfSxGvyMsczWPNhux+3ufAVAwHwYDVR0jBBgwFoAUVXuyfSxGvyMsczWPNhux+3uf\nAVAwDwYDVR0TAQH/BAUwAwEB/zAKBggqhkjOPQQDAgNIADBFAiEAo/VQIH+m9EXV\nQiZCDGBFQCvCy5hLs3tw573C7HWM7aQCIH6ZkOZqpPiLzlSDmQtrce0tjz0n/oWp\nLxeolirs3Vs/\n-----END CERTIFICATE-----"

const adpHTTP = `{"version":1,"id":"src_inventory_api","name":"Inventory API","adapter":"http",
"connection":{"base_url":"https://inventory.example.test/api/v2","timeout_seconds":30,"requests_per_second":5,"headers":{"Accept":"application/json"},
 "auth":{"kind":"token_request","credential":{"secret_id":"sec_inventory"},
  "request":{"path":"/auth/token","format":"json","body":{"username":"{{credential.username}}","password":"{{credential.password}}"}},
  "token_path":"token","send_header":"Authorization","send_prefix":"Bearer "}},
"datasets":[{"id":"ds_servers","request":{"method":"GET","path":"/servers","query":{"status":"active"}},"records_path":"items",
 "pagination":{"kind":"page","page_param":"page","size_param":"pageSize","size":500,"first_page":0,"total_path":"pageInfo.totalCount"},
 "natural_key":["serial"],"columns":[{"name":"serial"},{"name":"rack","paths":["location.rack"]},{"name":"app","paths":["labels[\"app.name\"]"]}]}]}`

const adpPG = `{"version":1,"id":"src_cmdb","adapter":"postgres",
"connection":{"host":"cmdb.example.test","port":5432,"database":"cmdb","credential":{"secret_id":"sec_cmdb"},"sslmode":"verify-full","statement_timeout_seconds":60},
"datasets":[{"id":"ds_servers","table":"inventory.servers","natural_key":["serial"],"columns":[{"name":"serial"},{"name":"rack_name"}]},
 {"id":"ds_vm_hosts","sql":"SELECT vm_id, host_id FROM placement WHERE retired_at IS NULL","natural_key":["vm_id"],"columns":[{"name":"vm_id"},{"name":"host_id"}]}]}`

const adpFile = `{"id":"s","adapter":"file","datasets":[{"id":"d","parser":"csv","natural_key":["a"],"columns":[{"name":"a"}]}]}`

// adpSwap changes one text of a document; the text has to be in it, so that a case that stops meaning anything is found.
func adpSwap(t *testing.T, doc, from, to string) string {
	t.Helper()
	if strings.Count(doc, from) != 1 {
		t.Fatalf("the text %q is in the document %d times, not once", from, strings.Count(doc, from))
	}
	return strings.Replace(doc, from, to, 1)
}

type adpCase struct {
	name    string
	doc     string
	code    string
	pointer string
}

func adpCases(t *testing.T) []adpCase {
	h := func(from, to string) string { return adpSwap(t, adpHTTP, from, to) }
	p := func(from, to string) string { return adpSwap(t, adpPG, from, to) }
	conn := func(doc string) string { return adpSwap(t, doc, `"datasets"`, `"x":0,"datasets"`) }
	_ = conn
	return []adpCase{
		{"no connection", `{"id":"s","adapter":"http","datasets":[{"id":"d","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]}]}`, "source.connection.missing", "/connection"},
		{"a file with a connection", adpSwap(t, adpFile, `"datasets"`, `"connection":{},"datasets"`), "source.connection.not_for_adapter", "/connection"},
		{"postgres field in http", h(`"timeout_seconds":30`, `"timeout_seconds":30,"host":"h.example.test"`), "source.connection.field_not_for_adapter", "/connection/host"},
		{"http field in postgres", p(`"database":"cmdb"`, `"database":"cmdb","base_url":"https://h.example.test"`), "source.connection.field_not_for_adapter", "/connection/base_url"},
		{"no base_url", h(`"base_url":"https://inventory.example.test/api/v2",`, ""), "source.connection.field_missing", "/connection/base_url"},
		{"no host", p(`"host":"cmdb.example.test",`, ""), "source.connection.field_missing", "/connection/host"},
		{"no database", p(`"database":"cmdb",`, ""), "source.connection.field_missing", "/connection/database"},
		{"no credential, so no password", p(`"credential":{"secret_id":"sec_cmdb"},`, ""), "source.connection.field_missing", "/connection/credential"},
		{"base_url scheme", h("https://inventory", "ftp://inventory"), "source.connection.base_url_invalid", "/connection/base_url"},
		{"base_url user", h("https://inventory", "https://u:p@inventory"), "source.connection.base_url_invalid", "/connection/base_url"},
		{"base_url query", h("/api/v2\"", "/api/v2?k=v\""), "source.connection.base_url_invalid", "/connection/base_url"},
		{"base_url double slash", h("/api/v2\"", "//api\""), "source.connection.base_url_invalid", "/connection/base_url"},
		{"base_url port", h("example.test/api", "example.test:99999/api"), "source.connection.base_url_invalid", "/connection/base_url"},
		{"base_url brace", h("/api/v2", "/api/{v}"), "source.connection.base_url_invalid", "/connection/base_url"},
		{"host with a comma", p("cmdb.example.test", "a.example.test,b.example.test"), "source.connection.host_invalid", "/connection/host"},
		{"host with a slash", p("cmdb.example.test", "/var/run"), "source.connection.host_invalid", "/connection/host"},
		{"host with @", p("cmdb.example.test", "u@cmdb.example.test"), "source.connection.host_invalid", "/connection/host"},
		{"host with a port", p("cmdb.example.test", "cmdb.example.test:5432"), "source.connection.host_invalid", "/connection/host"},
		{"port", p(`"port":5432`, `"port":70000`), "source.connection.port_invalid", "/connection/port"},
		{"sslmode", p("verify-full", "prefer"), "source.connection.sslmode_invalid", "/connection/sslmode"},
		{"sslmode require lowers the check", p("verify-full", "require"), "source.connection.sslmode_invalid", "/connection/sslmode"},
		{"sslmode verify-ca lowers the check", p("verify-full", "verify-ca"), "source.connection.sslmode_invalid", "/connection/sslmode"},
		{"ca_pem is no certificate", h(`"timeout_seconds":30`, `"timeout_seconds":30,"ca_pem":"not a certificate"`), "source.connection.ca_pem_invalid", "/connection/ca_pem"},
		{"ca_pem is a key", h(`"timeout_seconds":30`, `"timeout_seconds":30,"ca_pem":"-----BEGIN PRIVATE KEY-----\\nAAAA\\n-----END PRIVATE KEY-----\\n"`), "source.connection.ca_pem_invalid", "/connection/ca_pem"},
		{"ca_pem without TLS", p("verify-full", "disable\",\"ca_pem\":\""+strings.ReplaceAll(adpCert, "\n", "\\n")), "source.connection.ca_pem_invalid", "/connection/ca_pem"},
		{"timeout", h(`"timeout_seconds":30`, `"timeout_seconds":99999`), "source.connection.value_out_of_range", "/connection/timeout_seconds"},
		{"rate", h(`"requests_per_second":5`, `"requests_per_second":-1`), "source.connection.value_out_of_range", "/connection/requests_per_second"},
		{"statement timeout", p(`"statement_timeout_seconds":60`, `"statement_timeout_seconds":86400`), "source.connection.value_out_of_range", "/connection/statement_timeout_seconds"},
		{"header Host", h(`"Accept":"application/json"`, `"Host":"evil.example.test"`), "source.connection.header_not_allowed", "/connection/headers/Host"},
		{"header Authorization", h(`"Accept":"application/json"`, `"authorization":"x"`), "source.connection.header_not_allowed", "/connection/headers/authorization"},
		{"header Proxy-", h(`"Accept":"application/json"`, `"Proxy-Authorization":"x"`), "source.connection.header_not_allowed", "/connection/headers/Proxy-Authorization"},
		{"header name", h(`"Accept":"application/json"`, `"A B":"x"`), "source.connection.header_not_allowed", "/connection/headers/A B"},
		{"header twice", h(`"Accept":"application/json"`, `"Accept":"a","accept":"b"`), "source.connection.header_not_allowed", "/connection/headers/accept"},
		{"auth kind", h(`"kind":"token_request"`, `"kind":"oauth2"`), "source.auth.kind_unsupported", "/connection/auth/kind"},
		{"bearer without a secret", `{"id":"s","adapter":"http","connection":{"base_url":"https://x.example.test","auth":{"kind":"bearer"}},"datasets":[{"id":"d","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]}]}`, "source.auth.field_missing", "/connection/auth/credential"},
		{"header without a name", `{"id":"s","adapter":"http","connection":{"base_url":"https://x.example.test","auth":{"kind":"header","credential":{"secret_id":"k"}}},"datasets":[{"id":"d","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]}]}`, "source.auth.field_missing", "/connection/auth/header"},
		{"token request without a token_path", h(`"token_path":"token",`, ""), "source.auth.field_missing", "/connection/auth/token_path"},
		{"token request without a request", `{"id":"s","adapter":"http","connection":{"base_url":"https://x.example.test","auth":{"kind":"token_request","credential":{"secret_id":"k"},"token_path":"t"}},"datasets":[{"id":"d","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]}]}`, "source.auth.field_missing", "/connection/auth/request"},
		{"bearer with a header", `{"id":"s","adapter":"http","connection":{"base_url":"https://x.example.test","auth":{"kind":"bearer","credential":{"secret_id":"k"},"header":"X"}},"datasets":[{"id":"d","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]}]}`, "source.auth.field_not_for_kind", "/connection/auth/header"},
		{"secret id", h("sec_inventory", "Sec Inventory"), "source.credential.secret_id_invalid", "/connection/auth/credential/secret_id"},
		{"token request format", h(`"format":"json"`, `"format":"xml"`), "source.auth.token_request_invalid", "/connection/auth/request/format"},
		{"token request path", h(`"path":"/auth/token"`, `"path":"auth/token"`), "source.auth.token_request_invalid", "/connection/auth/request/path"},
		{"token_from that is none", h(`"token_path":"token",`, `"token_path":"token","token_from":"headers",`), "source.auth.token_use_invalid", "/connection/auth/token_from"},
		{"send that is none", h(`"token_path":"token",`, `"token_path":"token","send":"later",`), "source.auth.token_use_invalid", "/connection/auth/send"},
		{"headers of the authentication from nowhere", h(`"token_path":"token",`, `"token_path":"token","headers":[{"name":"X-K","from":"nowhere"}],`), "source.auth.header_source_invalid", "/connection/auth/headers/0/from"},
		{"custom that sends nothing", `{"id":"s","adapter":"http","connection":{"base_url":"https://x.example.test","auth":{"kind":"custom","credential":{"secret_id":"k"}}},"datasets":[{"id":"d","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]}]}`, "source.auth.sends_nothing", "/connection/auth/headers"},
		{"custom with two kinds of secret", `{"id":"s","adapter":"http","connection":{"base_url":"https://x.example.test","auth":{"kind":"custom","credential":{"secret_id":"k"},"headers":[{"name":"X-A","from":"credential.token"},{"name":"X-B","from":"credential.username"}]}},"datasets":[{"id":"d","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]}]}`, "source.auth.secret_fields_invalid", "/connection/auth"},
		{"placeholder in a text", h("{{credential.password}}", "x-{{credential.password}}"), "source.auth.placeholder_invalid", "/connection/auth/request/body/password"},
		{"placeholder elsewhere", h(`"status":"active"`, `"status":"{{credential.token}}"`), "source.placeholder.not_allowed_here", "/datasets/0/request/query/status"},
		{"brace in a path", h(`"path":"/servers"`, `"path":"/servers/{id}"`), "source.text.braces_not_allowed", "/datasets/0/request/path"},
		{"brace in a statement", p("WHERE retired_at", "WHERE tags = '{a}' AND retired_at"), "source.text.braces_not_allowed", "/datasets/1/sql"},
		{"path with an empty step", h(`"paths":["location.rack"]`, `"paths":["location..rack"]`), "source.path.invalid", "/datasets/0/columns/1/paths/0"},
		{"records_path with a dot first", h(`"records_path":"items"`, `"records_path":".items"`), "source.path.invalid", "/datasets/0/records_path"},
		{"path with an unclosed name", h(`labels[\"app.name\"]`, `labels[\"app.name`), "source.path.invalid", "/datasets/0/columns/2/paths/0"},
		{"path with [*]", h(`"location.rack"`, `"racks[*].name"`), "source.path.unsupported", "/datasets/0/columns/1/paths/0"},
		{"path with [?]", h(`"records_path":"items"`, `"records_path":"items[?kind=a]"`), "source.path.unsupported", "/datasets/0/records_path"},
		{"parser on an http dataset", h(`"id":"ds_servers",`, `"id":"ds_servers","parser":"json_lines",`), "source.dataset.parser_not_for_adapter", "/datasets/0/parser"},
		{"parser on a postgres dataset", p(`"id":"ds_servers",`, `"id":"ds_servers","parser":"csv",`), "source.dataset.parser_not_for_adapter", "/datasets/0/parser"},
		{"table on an http dataset", h(`"id":"ds_servers",`, `"id":"ds_servers","table":"t",`), "source.dataset.field_not_for_adapter", "/datasets/0/table"},
		{"request on a postgres dataset", p(`"id":"ds_servers",`, `"id":"ds_servers","request":{"path":"/a"},`), "source.dataset.field_not_for_adapter", "/datasets/0/request"},
		{"delimiter on an http dataset", h(`"id":"ds_servers",`, `"id":"ds_servers","delimiter":"tab",`), "source.dataset.field_not_for_adapter", "/datasets/0/delimiter"},
		{"request on a file dataset", adpSwap(t, adpFile, `"parser":"csv"`, `"parser":"csv","post_reads":true`), "source.dataset.field_not_for_adapter", "/datasets/0/post_reads"},
		{"no request", h(`"request":{"method":"GET","path":"/servers","query":{"status":"active"}},`, ""), "source.dataset.request_missing", "/datasets/0/request"},
		{"method", h(`"method":"GET"`, `"method":"DELETE"`), "source.request.method_invalid", "/datasets/0/request/method"},
		{"POST not declared", h(`"method":"GET"`, `"method":"POST"`), "source.request.post_not_declared", "/datasets/0/request/method"},
		{"body on a GET", h(`"method":"GET"`, `"method":"GET","body":{"a":1}`), "source.request.body_needs_post", "/datasets/0/request/body"},
		{"request path without a slash", h(`"path":"/servers"`, `"path":"servers"`), "source.request.path_invalid", "/datasets/0/request/path"},
		{"request path with a query", h(`"path":"/servers"`, `"path":"/servers?a=b"`), "source.request.path_invalid", "/datasets/0/request/path"},
		{"request path with another host", h(`"path":"/servers"`, `"path":"//evil.example.test/x"`), "source.request.path_invalid", "/datasets/0/request/path"},
		{"request path that goes up", h(`"path":"/servers"`, `"path":"/a/../b"`), "source.request.path_invalid", "/datasets/0/request/path"},
		{"pagination kind", h(`"kind":"page"`, `"kind":"keyset"`), "source.pagination.kind_unsupported", "/datasets/0/pagination/kind"},
		{"page without a size", h(`"size":500,`, ""), "source.pagination.field_missing", "/datasets/0/pagination/size"},
		{"page without first_page", h(`"first_page":0,`, ""), "source.pagination.field_missing", "/datasets/0/pagination/first_page"},
		{"none with a size", h(`"kind":"page"`, `"kind":"none"`), "source.pagination.field_not_for_kind", "/datasets/0/pagination/page_param"},
		{"size out of range", h(`"size":500`, `"size":1000000`), "source.pagination.value_out_of_range", "/datasets/0/pagination/size"},
		{"first_page", h(`"first_page":0`, `"first_page":2`), "source.pagination.value_out_of_range", "/datasets/0/pagination/first_page"},
		{"max_pages", h(`"first_page":0`, `"first_page":0,"max_pages":2000000`), "source.pagination.value_out_of_range", "/datasets/0/pagination/max_pages"},
		{"param that the query has", h(`"page_param":"page"`, `"page_param":"status"`), "source.pagination.param_invalid", "/datasets/0/pagination/page_param"},
		{"cursor in a body of a GET", h(`"kind":"page","page_param":"page","size_param":"pageSize","size":500,"first_page":0,"total_path":"pageInfo.totalCount"`, `"kind":"cursor","cursor_param":"c","cursor_in":"body","next_cursor_path":"next"`), "source.request.body_needs_post", "/datasets/0/pagination/cursor_in"},
		{"both table and sql", p(`"table":"inventory.servers"`, `"table":"inventory.servers","sql":"SELECT 1"`), "source.dataset.table_or_sql", "/datasets/0/sql"},
		{"neither table nor sql", p(`"table":"inventory.servers",`, ""), "source.dataset.table_or_sql", "/datasets/0"},
		{"table with three parts", p("inventory.servers", "a.b.c"), "source.dataset.table_invalid", "/datasets/0/table"},
		{"table with quotes", p("inventory.servers", `\"x\"`), "source.dataset.table_invalid", "/datasets/0/table"},
		{"empty statement", p(`"sql":"SELECT vm_id, host_id FROM placement WHERE retired_at IS NULL"`, `"sql":"  "`), "source.dataset.sql_invalid", "/datasets/1/sql"},
		{"collation", p(`"table":"inventory.servers"`, `"table":"inventory.servers","key_collation":"a b"`), "source.dataset.key_collation_invalid", "/datasets/0/key_collation"},
		{"response_format that is none of the two", h(`"id":"ds_servers",`, `"id":"ds_servers","response_format":"ndjson",`), "source.response_format_invalid", "/datasets/0/response_format"},
		{"json_lines with pages", h(`"id":"ds_servers",`, `"id":"ds_servers","response_format":"json_lines",`), "source.response_format_pagination", "/datasets/0/pagination/kind"},
		{"missing_array without a [*]", h(`"id":"ds_servers",`, `"id":"ds_servers","missing_array":"empty",`), "source.dataset.expand_invalid", "/datasets/0/missing_array"},
	}
}

// [REQ-401]
// A source definition of the http adapter and one of the postgres adapter are read, with the defaults of their fields
// filled, and a definition that has a field of its own adapter, written as the design says, is not refused.
func TestAdpHTTPAndPostgresDefinitionsAreRead(t *testing.T) {
	src, err := ParseAdapterSource(strings.NewReader(adpHTTP))
	if err != nil {
		t.Fatal(err)
	}
	cn := src.Connection
	if cn.Auth.Kind != AuthTokenRequest || *cn.Auth.SendPrefix != "Bearer " || cn.MaxResponseBytes != DefaultMaxResponseBytes || cn.MaxRetryWaitSeconds != DefaultMaxRetryWaitSeconds {
		t.Errorf("connection %+v, auth %+v", cn, cn.Auth)
	}
	d := src.Datasets[0]
	if d.Request.Method != MethodGet || d.Pagination.MaxPages != DefaultMaxPages || d.Pagination.Kind != PagePage || d.Parser != "" {
		t.Errorf("dataset %+v, request %+v, pagination %+v", d, d.Request, d.Pagination)
	}
	pg, err := ParseAdapterSource(strings.NewReader(adpPG))
	if err != nil {
		t.Fatal(err)
	}
	if c := pg.Connection; c.SSLMode != SSLVerifyFull || c.ConnectTimeoutSeconds != DefaultConnectSeconds || c.LockTimeoutSeconds != DefaultLockSeconds || c.Port != 5432 {
		t.Errorf("connection %+v", c)
	}
	plain := adpSwap(t, adpHTTP, "https://inventory.example.test/api/v2", "http://127.0.0.1:8080")
	if _, err := ParseAdapterSource(strings.NewReader(plain)); err != nil {
		t.Errorf("a loopback http address is for the server's flags to allow, not for the definition to refuse: %v", err)
	}
	withCA := adpSwap(t, adpHTTP, `"timeout_seconds":30`, `"timeout_seconds":30,"ca_pem":"`+strings.ReplaceAll(adpCert, "\n", "\\n")+`"`)
	if _, err := ParseAdapterSource(strings.NewReader(withCA)); err != nil {
		t.Errorf("a certificate in PEM form: %v", err)
	}
	for _, off := range []string{`"max_response_bytes":2048,`, `"max_retry_wait_seconds":5,`} {
		if _, err := ParseAdapterSource(strings.NewReader(adpSwap(t, adpHTTP, `"timeout_seconds":30,`, `"timeout_seconds":30,`+off))); err != nil {
			t.Errorf("%s: %v", off, err)
		}
	}
}

// [REQ-401]
// Every case gives the code at the place it names, and every code of the table of the adapters is given by some case, with
// the kernel code the table says and no param it does not list.
func TestAdpEveryCodeOfTheAdapterTableIsGivenAtItsPlace(t *testing.T) {
	table := map[string]ErrorCode{}
	name := regexp.MustCompile(`^source(\.[a-z]+(_[a-z]+)*)+$`)
	for _, ec := range AdapterErrorCodes() {
		if _, dup := table[ec.Code]; dup || !name.MatchString(ec.Code) || !strings.HasSuffix(ec.Meaning, ".") || ec.Document != "source" {
			t.Errorf("the row %+v is a duplicate or is not a stable name with a one-line meaning", ec)
		}
		table[ec.Code] = ec
	}
	for _, ec := range ErrorCodes() {
		if _, clash := table[ec.Code]; clash {
			t.Errorf("%s is in both tables", ec.Code)
		}
	}
	given := map[string]bool{}
	for _, c := range adpCases(t) {
		_, err := ParseAdapterSource(strings.NewReader(c.doc))
		de, ok := DefinitionErrorsOf(err)
		if !ok {
			t.Errorf("%s: no definition error: %v", c.name, err)
			continue
		}
		hit := false
		for _, e := range de.List {
			given[e.Code] = true
			hit = hit || (e.Code == c.code && e.Pointer == c.pointer)
			row, known := table[e.Code]
			if known && (e.KernelCode() != row.Kernel || e.Message == "") {
				t.Errorf("%s: %s has the kernel code %s, and the table says %s", c.name, e.Code, e.KernelCode(), row.Kernel)
			}
			for k := range e.Params {
				if known && !slices.Contains(row.Params, k) {
					t.Errorf("%s: %s has the param %q that the table does not list", c.name, e.Code, k)
				}
			}
		}
		if !hit {
			t.Errorf("%s: no error with the code %s at %q: %+v", c.name, c.code, c.pointer, de.List)
		}
	}
	for code := range table {
		if !given[code] {
			t.Errorf("no case gives the code %s", code)
		}
	}
}

// [REQ-401]
// The file flow reads the file adapter only: a source of another adapter is refused there as one this build does not have,
// with the sentence it has always had, and the definition reader of the adapters says which three it reads.
func TestAdpTheFileFlowStaysFileOnly(t *testing.T) {
	_, err := ParseSource(strings.NewReader(adpHTTP))
	de, ok := DefinitionErrorsOf(err)
	if !ok || de.List[0].Code != "source.adapter.unsupported" || strings.Contains(err.Error(), "connection") || strings.Contains(err.Error(), "not a field") || !strings.Contains(err.Error(), "(only file)") || kernel.CodeOf(err) != kernel.CodeUnsupported {
		t.Fatalf("an http source in the file flow: %v", err)
	}
	if _, err := ParseSource(strings.NewReader(adpFile)); err != nil {
		t.Errorf("the file source is still read: %v", err)
	}
	_, err = ParseAdapterSource(strings.NewReader(adpSwap(t, adpFile, `"adapter":"file"`, `"adapter":"ftp"`)))
	if err == nil || !strings.Contains(err.Error(), "(only file, http, postgres)") {
		t.Errorf("an adapter that is none of the three: %v", err)
	}
}

// [REQ-401]
// A field that this build does not read yet is refused as an unknown field wherever it is written, and the sources of the
// design that come later (capabilities, keyed_read) are among them.
func TestAdpFieldsOfLaterStepsAreUnknown(t *testing.T) {
	for _, c := range []struct{ doc, field string }{
		{adpSwap(t, adpHTTP, `"adapter":"http"`, `"adapter":"http","capabilities":["full_read"]`), "capabilities"},
		{adpSwap(t, adpHTTP, `"id":"ds_servers"`, `"id":"ds_servers","keyed_read":true`), "keyed_read"},
		{adpSwap(t, adpHTTP, `"timeout_seconds":30`, `"timeout_seconds":30,"proxy":"http://p.example.test"`), "proxy"},
		{adpSwap(t, adpPG, `"database":"cmdb"`, `"database":"cmdb","sslcert":"x"`), "sslcert"},
		{adpSwap(t, adpHTTP, `"timeout_seconds":30`, `"timeout_seconds":30,"tls_verify":false`), "tls_verify"},
	} {
		_, err := ParseAdapterSource(strings.NewReader(c.doc))
		de, ok := DefinitionErrorsOf(err)
		if !ok || de.List[0].Code != "definition.unknown_field" || de.List[0].Params["field"] != c.field {
			t.Errorf("%s: %v", c.field, err)
		}
	}
}

// [REQ-401]
// A source that is disabled is read, and the flag is kept.
func TestAdpADisabledSourceIsRead(t *testing.T) {
	src, err := ParseAdapterSource(strings.NewReader(adpSwap(t, adpPG, `"adapter":"postgres"`, `"adapter":"postgres","disabled":true`)))
	if err != nil || !src.Disabled {
		t.Fatalf("%v, %+v", err, src)
	}
}

// [REQ-401]
// A brace written doubled is the brace itself, and UnescapeBraces turns it back; a single one is refused.
func TestAdpDoubledBracesStandForTheBrace(t *testing.T) {
	if got := UnescapeBraces("a {{b}} c"); got != "a {b} c" {
		t.Errorf("unescaped: %q", got)
	}
	for text, bad := range map[string]bool{"plain": false, "{{x}}": false, "{x}": true, "x}": true, "{{{": true, "}}}": true, "{{}}": false} {
		if hasBraceProblem(text) != bad {
			t.Errorf("%q: problem %v, want %v", text, !bad, bad)
		}
	}
}

const adpMinHTTP = `{"id":"s","adapter":"http","connection":{"base_url":"https://x.example.test"%s},"datasets":[{"id":"d",%s"natural_key":["a"],"columns":[{"name":"a"}]}]}`

func adpMin(conn, dataset string) string { return fmt.Sprintf(adpMinHTTP, conn, dataset) }

// [REQ-401]
// The other kinds of authentication and pagination are read when they are written as the design says, with their defaults,
// and a POST that says it only reads is read, with a body and a cursor sent in it.
func TestAdpOtherKindsOfAuthenticationAndPaginationAreRead(t *testing.T) {
	for name, doc := range map[string]string{
		"basic":      adpMin(`,"auth":{"kind":"basic","credential":{"secret_id":"sec_b"}}`, `"request":{"path":"/a"},`),
		"header":     adpMin(`,"auth":{"kind":"header","credential":{"secret_id":"sec_h"},"header":"X-Api-Key","prefix":"Key "}`, `"request":{"path":"/a"},`),
		"offset":     adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"offset","offset_param":"offset","size_param":"limit","size":100,"total_path":"meta.total"},`),
		"next_url":   adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"next_url","next_path":"links.next","size_param":"n","size":50},`),
		"link":       adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"link_header"},`),
		"cursor":     adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"cursor","cursor_param":"after","cursor_in":"query","next_cursor_path":"next"},`),
		"post":       adpMin("", `"post_reads":true,"request":{"method":"POST","path":"/search","body":{"q":[{"a":"{{literal}}"},1,true,null]}},"pagination":{"kind":"cursor","cursor_param":"cursor","cursor_in":"body","next_cursor_path":"next","size_param":"size","size":20},`),
		"token only": adpMin(`,"auth":{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"form","body":{"u":"{{credential.username}}","fixed":"x"}},"token_path":"access.token","send_prefix":""}`, `"request":{"path":"/a"},`),
		"ipv6":       strings.Replace(adpPG, "cmdb.example.test", "::1", 1),
	} {
		if _, err := ParseAdapterSource(strings.NewReader(doc)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	src, _ := ParseAdapterSource(strings.NewReader(adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"link_header"},`)))
	if src.Datasets[0].Pagination.Rel != "next" || src.Datasets[0].Request.Method != MethodGet || src.Connection.Auth.Kind != AuthNone {
		t.Errorf("defaults: %+v %+v", src.Datasets[0].Pagination, src.Connection.Auth)
	}
}

// [REQ-401]
// Further cases of definitions that are refused, each at the place it names, among them a page size given without its
// parameter, a brace deep in a body, and a header with a control character in its value.
func TestAdpFurtherRefusals(t *testing.T) {
	for _, c := range []adpCase{
		{"size without a parameter", adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"next_url","next_path":"n","size":5},`), "source.pagination.field_missing", "/datasets/0/pagination/size_param"},
		{"parameter without a size", adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"link_header","size_param":"n"},`), "source.pagination.field_missing", "/datasets/0/pagination/size"},
		{"brace deep in a body", adpMin("", `"post_reads":true,"request":{"method":"POST","path":"/a","body":{"q":[{"a":"{x}"}]}},`), "source.text.braces_not_allowed", "/datasets/0/request/body/q/0/a"},
		{"brace in a body name", adpMin("", `"post_reads":true,"request":{"method":"POST","path":"/a","body":{"{q":1}},`), "source.text.braces_not_allowed", "/datasets/0/request/body/{q"},
		{"header value", adpMin(`,"headers":{"X-A":"a\nb"}`, `"request":{"path":"/a"},`), "source.connection.header_not_allowed", "/connection/headers/X-A"},
		{"header auth name", adpMin(`,"auth":{"kind":"header","credential":{"secret_id":"k"},"header":"Cookie"}`, `"request":{"path":"/a"},`), "source.connection.header_not_allowed", "/connection/auth/header"},
		{"prefix with a control character", adpMin(`,"auth":{"kind":"header","credential":{"secret_id":"k"},"header":"X-K","prefix":"a\nb"}`, `"request":{"path":"/a"},`), "source.connection.header_not_allowed", "/connection/auth/prefix"},
		{"send_header", adpMin(`,"auth":{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json"},"token_path":"t","send_header":"Host"}`, `"request":{"path":"/a"},`), "source.connection.header_not_allowed", "/connection/auth/send_header"},
		{"send_prefix", adpMin(`,"auth":{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json"},"token_path":"t","send_prefix":"a\nb"}`, `"request":{"path":"/a"},`), "source.connection.header_not_allowed", "/connection/auth/send_prefix"},
		{"token path", adpMin(`,"auth":{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json"},"token_path":"a..b"}`, `"request":{"path":"/a"},`), "source.path.invalid", "/connection/auth/token_path"},
		{"token body with no name", adpMin(`,"auth":{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json","body":{"":"x"}},"token_path":"t"}`, `"request":{"path":"/a"},`), "source.auth.token_request_invalid", "/connection/auth/request/body"},
		{"token body brace", adpMin(`,"auth":{"kind":"token_request","credential":{"secret_id":"k"},"request":{"path":"/t","format":"json","body":{"a":"{b}"}},"token_path":"t"}`, `"request":{"path":"/a"},`), "source.text.braces_not_allowed", "/connection/auth/request/body/a"},
		{"base_url with a space", strings.Replace(adpMin("", `"request":{"path":"/a"},`), "https://x.example.test", "https://x .example.test", 1), "source.connection.base_url_invalid", "/connection/base_url"},
		{"base_url with a dot step", strings.Replace(adpMin("", `"request":{"path":"/a"},`), "https://x.example.test", "https://x.example.test/a/../b", 1), "source.connection.base_url_invalid", "/connection/base_url"},
		{"base_url with no host", strings.Replace(adpMin("", `"request":{"path":"/a"},`), "https://x.example.test", "https:///p", 1), "source.connection.base_url_invalid", "/connection/base_url"},
		{"request path with a control character", adpMin("", `"request":{"path":"/a\tb"},`), "source.request.path_invalid", "/datasets/0/request/path"},
		{"request path with a dot step", adpMin("", `"request":{"path":"/a/./b"},`), "source.request.path_invalid", "/datasets/0/request/path"},
		{"cursor_in", adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"cursor","cursor_param":"c","cursor_in":"cookie","next_cursor_path":"n"},`), "source.pagination.param_invalid", "/datasets/0/pagination/cursor_in"},
		{"parameter name", adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"offset","offset_param":"a b","size_param":"limit","size":1},`), "source.pagination.param_invalid", "/datasets/0/pagination/offset_param"},
		{"rel", adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"link_header","rel":"a b"},`), "source.pagination.param_invalid", "/datasets/0/pagination/rel"},
		{"pagination path", adpMin("", `"request":{"path":"/a"},"pagination":{"kind":"next_url","next_path":"a[*]"},`), "source.path.unsupported", "/datasets/0/pagination/next_path"},
		{"ca_pem trailing text", strings.Replace(adpMin("", `"request":{"path":"/a"},`), `"https://x.example.test"`, `"https://x.example.test","ca_pem":"`+strings.ReplaceAll(adpCert, "\n", "\\n")+`trailing"`, 1), "source.connection.ca_pem_invalid", "/connection/ca_pem"},
		{"database", strings.Replace(adpPG, `"database":"cmdb"`, `"database":"`+strings.Repeat("d", 64)+`"`, 1), "source.connection.field_missing", "/connection/database"},
		{"IPv6 host that is none", strings.Replace(adpPG, "cmdb.example.test", "a:b", 1), "source.connection.host_invalid", "/connection/host"},
		{"empty secret id", strings.Replace(adpPG, `"secret_id":"sec_cmdb"`, `"secret_id":""`, 1), "source.connection.field_missing", "/connection/credential"},
	} {
		_, err := ParseAdapterSource(strings.NewReader(c.doc))
		de, ok := DefinitionErrorsOf(err)
		hit := ok && slices.ContainsFunc(de.List, func(e DefinitionError) bool { return e.Code == c.code && e.Pointer == c.pointer })
		if !hit {
			t.Errorf("%s: no error with the code %s at %q: %v", c.name, c.code, c.pointer, err)
		}
	}
	var pe *PathError
	if _, err := ParsePath("a..b"); !errors.As(err, &pe) || !strings.Contains(err.Error(), "empty step") {
		t.Errorf("%v", err)
	}
}
