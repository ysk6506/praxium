package ingest

import (
	"strings"
	"testing"
)

// The tests of the host and the port of the address of a token request (ADR-0106): they are written in the one form the client
// reads them in, so that the definition check and the adapter tell the origin the same way. Every name here begins with hxc.

// [REQ-840]
// The host and the port of the address of a token request are written the canonical way: an address that the client would read
// as another host than it is written, or refuse, is a definition error, and one that is written the canonical way is not.
func TestHxcTheHostAndThePortOfTheTokenAddressAreWrittenTheCanonicalWay(t *testing.T) {
	pem := strings.ReplaceAll(adpCert, "\n", "\\n")
	const url = "source.auth.token_request_invalid /connection/auth/request/url"
	asDoc := func(address string) string {
		return hxaDoc(hxcRequest(t, `{"url":"`+address+`","ca_pem":"`+pem+`","format":"json","body":{"a":"b"}}`), "", "")
	}
	for _, address := range []string{
		"https://idp.example.test/t", "https://IDP.Example.Test/t", "https://idp.example.test:8443/t", "https://idp.example.test:443/t",
		"https://idp-1.example_test.test/t", "https://10.1.2.3/t", "https://[::1]:8443/t", "https://[2001:db8::1]/t", "https://[2001:DB8::1]/t",
		"https://xn--pi-xia.example.test/t", "http://idp.example.test:8080/t", "https://1a.example.test/t",
	} {
		hxaWant(t, address, asDoc(address))
	}
	for name, address := range map[string]string{
		"a dot that ends the name":      "https://idp.example.test./t",
		"a port with a zero before it":  "https://idp.example.test:0443/t",
		"an IPv6 address not canonical": "https://[0:0:0:0:0:0:0:1]/t",
		"an IPv6 address with a zone":   "https://[fe80::1%25eth0]/t",
		"a short IPv4 address":          "https://127.1/t",
		"an IPv4 address as a number":   "https://2130706433/t",
		"an IPv4 address in hex":        "https://0x7f.0.0.1/t",
		"an IPv4 address in octal":      "https://0177.0.0.1/t",
		"a name that is no ASCII":       "https://exämple.test/t",
		"a name with an empty label":    "https://a..b.test/t",
		"a name that is escaped":        "https://%61pi.example.test/t",
		"a label that is too long":      "https://" + strings.Repeat("a", 64) + ".test/t",
		"a name that ends in a number":  "https://idp.example.0x/t",
	} {
		hxaWant(t, name, asDoc(address), url)
	}
}

// [REQ-840]
// The origin of an address is told as the client tells it for every form that the client accepts: a dot that ends the name, a
// port with a zero before it and an IPv6 address that is not canonical are the same origin as the canonical forms, so that a
// ca_pem is never given for an origin that the client takes to be the base address's.
func TestHxcAnOriginIsToldAsTheClientTellsIt(t *testing.T) {
	for _, c := range []struct {
		base, url string
		cross     bool
	}{
		{"https://api.example.test.", "https://api.example.test/t", false},
		{"https://API.example.test:0443", "https://api.example.test/t", false},
		{"https://[0:0:0:0:0:0:0:1]:8443", "https://[::1]:8443/t", false},
		{"https://api.example.test", "https://api.example.test:8443/t", true},
		{"https://api.example.test", "http://api.example.test/t", true},
		{"https://api.example.test", "https://api.example.test.evil.test/t", true},
		{"https://127.0.0.1", "https://[::ffff:127.0.0.1]/t", true},
		{"https://api.example.test:99999999999999999999", "https://api.example.test/t", true},
	} {
		s := Source{Adapter: AdapterHTTP, Connection: &Connection{BaseURL: c.base,
			Auth: &Auth{Kind: AuthTokenRequest, Request: &TokenRequest{URL: c.url}}}}
		if got := TokenGoesToAnotherOrigin(s); got != c.cross {
			t.Errorf("%s and %s: another origin %v, want %v", c.base, c.url, got, c.cross)
		}
	}
}
