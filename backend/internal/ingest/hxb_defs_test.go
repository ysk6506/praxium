package ingest

import (
	"crypto/sha256"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

// The tests of the pins of the connection (ADR-0105). Every helper here begins with hxb so that it never meets a name of
// the older tests of this folder.

func hxbPinText(b byte) string {
	sum := sha256.Sum256([]byte{b})
	return "sha256/" + base64.StdEncoding.EncodeToString(sum[:])
}

const hxbDataset = `{"id":"ds_a","request":{"path":"/a"},"natural_key":["a"],"columns":[{"name":"a"}]}`

// hxbHTTP is an http source with the given text added to its connection.
func hxbHTTP(base, extra string) string {
	return `{"version":1,"id":"src_x","adapter":"http","connection":{"base_url":"` + base + `"` + extra + `},"datasets":[` + hxbDataset + `]}`
}

func hxbPG(extra string) string {
	return `{"version":1,"id":"src_p","adapter":"postgres","connection":{"host":"db.example.test","database":"d","credential":{"secret_id":"k"}` + extra + `},"datasets":[{"id":"ds_a","table":"t","natural_key":["a"],"columns":[{"name":"a"}]}]}`
}

func hxbCodes(t *testing.T, doc string) []string {
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

func hxbTLS(pins ...string) string {
	quoted := make([]string, len(pins))
	for i, p := range pins {
		quoted[i] = `"` + p + `"`
	}
	return `,"tls":{"pins":[` + strings.Join(quoted, ",") + `]}`
}

// [REQ-822] A connection of either adapter may name from one to eight pins; a pin written twice counts once and the list that
// the definition is given back with has them in the form they are compared in.
func TestHxbPinsOfAConnectionAreCheckedAndNormalized(t *testing.T) {
	for name, doc := range map[string]string{
		"http one":      hxbHTTP("https://x.example.test", hxbTLS(hxbPinText(1))),
		"http eight":    hxbHTTP("https://x.example.test", hxbTLS(hxbPinText(1), hxbPinText(2), hxbPinText(3), hxbPinText(4), hxbPinText(5), hxbPinText(6), hxbPinText(7), hxbPinText(8))),
		"http repeated": hxbHTTP("HTTPS://x.example.test", hxbTLS(hxbPinText(1), hxbPinText(1))),
		"postgres":      hxbPG(hxbTLS(hxbPinText(1), hxbPinText(2))),
	} {
		if got := hxbCodes(t, doc); len(got) != 0 {
			t.Errorf("%s: refused: %v", name, got)
		}
	}
	src, err := ParseAdapterSource(strings.NewReader(hxbHTTP("https://x.example.test", hxbTLS(hxbPinText(2), hxbPinText(1), hxbPinText(2)))))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{hxbPinText(2), hxbPinText(1)}; !reflect.DeepEqual(src.Connection.TLS.Pins, want) {
		t.Errorf("pins %v", src.Connection.TLS.Pins)
	}
}

// [REQ-822] A pin that is not sha256/ and the base64 of 32 bytes, no pin, nine pins, pins with ca_pem and pins for a connection
// that has no TLS are definition errors with the code of the pins, at the place of the fault.
func TestHxbPinsThatAreWrongAreDefinitionErrors(t *testing.T) {
	const code = "source.connection.tls_pins_invalid"
	nine := make([]string, 9)
	for i := range nine {
		nine[i] = hxbPinText(byte(i))
	}
	good := hxbPinText(1)
	for name, tc := range map[string]struct{ doc, want string }{
		"no prefix":         {hxbHTTP("https://x.example.test", hxbTLS(strings.TrimPrefix(good, "sha256/"))), code + " /connection/tls/pins/0"},
		"upper case":        {hxbHTTP("https://x.example.test", hxbTLS("SHA256/"+strings.TrimPrefix(good, "sha256/"))), code + " /connection/tls/pins/0"},
		"sha1":              {hxbHTTP("https://x.example.test", hxbTLS("sha1/"+strings.TrimPrefix(good, "sha256/"))), code + " /connection/tls/pins/0"},
		"short":             {hxbHTTP("https://x.example.test", hxbTLS("sha256/"+base64.StdEncoding.EncodeToString([]byte("short")))), code + " /connection/tls/pins/0"},
		"url encoding":      {hxbHTTP("https://x.example.test", hxbTLS("sha256/"+base64.URLEncoding.EncodeToString(make([]byte, 33)))), code + " /connection/tls/pins/0"},
		"second is wrong":   {hxbHTTP("https://x.example.test", hxbTLS(good, "sha256/zz")), code + " /connection/tls/pins/1"},
		"empty list":        {hxbHTTP("https://x.example.test", `,"tls":{"pins":[]}`), code + " /connection/tls/pins"},
		"no list":           {hxbHTTP("https://x.example.test", `,"tls":{}`), code + " /connection/tls/pins"},
		"nine":              {hxbHTTP("https://x.example.test", hxbTLS(nine...)), code + " /connection/tls/pins"},
		"with ca_pem":       {hxbHTTP("https://x.example.test", `,"ca_pem":"`+strings.ReplaceAll(adpCert, "\n", `\n`)+`"`+hxbTLS(good)), code + " /connection/tls/pins"},
		"on an http:// url": {hxbHTTP("http://x.example.test", hxbTLS(good)), code + " /connection/tls/pins"},
		"postgres without":  {hxbPG(`,"sslmode":"disable"` + hxbTLS(good)), code + " /connection/tls/pins"},
		"postgres with ca":  {hxbPG(`,"ca_pem":"` + strings.ReplaceAll(adpCert, "\n", `\n`) + `"` + hxbTLS(good)), code + " /connection/tls/pins"},
	} {
		got := hxbCodes(t, tc.doc)
		found := false
		for _, g := range got {
			found = found || g == tc.want
		}
		if !found {
			t.Errorf("%s: want %s, got %v", name, tc.want, got)
		}
	}
	// no other field of tls is read: a switch that turns the verification off is an unknown field
	for _, extra := range []string{`,"tls":{"pins":["` + good + `"],"verify":false}`, `,"tls":{"insecure_skip_verify":true}`, `,"tls_verify":false`} {
		if got := hxbCodes(t, hxbHTTP("https://x.example.test", extra)); len(got) == 0 {
			t.Errorf("%s was accepted", extra)
		}
	}
}

// [REQ-825] A change of the pins, adding them or taking them away, is a change of the connection (both abilities are needed),
// as is a change of ca_pem; a change of the mapping alone is not.
func TestHxbAChangeOfThePinsIsAChangeOfTheConnection(t *testing.T) {
	parse := func(doc string) Source {
		src, err := ParseAdapterSource(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("%v\n%s", err, doc)
		}
		return src
	}
	base := parse(hxbHTTP("https://x.example.test", ""))
	pinned := parse(hxbHTTP("https://x.example.test", hxbTLS(hxbPinText(1))))
	more := parse(hxbHTTP("https://x.example.test", hxbTLS(hxbPinText(1), hxbPinText(2))))
	swapped := parse(hxbHTTP("https://x.example.test", hxbTLS(hxbPinText(2))))
	withCA := parse(hxbHTTP("https://x.example.test", `,"ca_pem":"`+strings.ReplaceAll(adpCert, "\n", `\n`)+`"`))
	for name, c := range map[string][2]Source{
		"added": {base, pinned}, "removed": {pinned, base}, "one more": {pinned, more}, "replaced": {pinned, swapped}, "ca_pem": {base, withCA},
	} {
		got := ClassifyChange(c[0], c[1])
		if got.Kind != ChangeConnection {
			t.Errorf("%s: %s %v", name, got.Kind, got.Reasons)
		}
	}
	if got := ClassifyChange(pinned, pinned); got.Kind != ChangeMapping {
		t.Errorf("no change: %s %v", got.Kind, got.Reasons)
	}
	// the same pins in another order are another list; the same pins written twice are the same list
	twice := parse(hxbHTTP("https://x.example.test", hxbTLS(hxbPinText(1), hxbPinText(1))))
	if got := ClassifyChange(pinned, twice); got.Kind != ChangeMapping {
		t.Errorf("a pin written twice: %s %v", got.Kind, got.Reasons)
	}
	renamed := pinned
	renamed.Name = "another name"
	if got := ClassifyChange(pinned, renamed); got.Kind != ChangeMapping {
		t.Errorf("a name: %s %v", got.Kind, got.Reasons)
	}
	pg := parse(hxbPG(""))
	pgPinned := parse(hxbPG(hxbTLS(hxbPinText(1))))
	if got := ClassifyChange(pg, pgPinned); got.Kind != ChangeConnection {
		t.Errorf("postgres: %s %v", got.Kind, got.Reasons)
	}
	if got := ClassifyChange(pinned, more); !hxbHas(got.Reasons, "connection.tls") {
		t.Errorf("the reason of a change of the pins: %v", got.Reasons)
	}
}

func hxbHas(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
