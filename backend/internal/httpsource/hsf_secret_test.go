package httpsource

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// hsfForms lists the shapes the secrets of a source may come back in, for a test to look for.
func hsfForms() []string {
	basic := base64.StdEncoding.EncodeToString([]byte(hsUserValue + ":" + hsPassValue))
	return maskForms([]string{hsTokenValue, "Bearer " + hsTokenValue, hsPassValue, hsUserValue, basic, "Basic " + basic, hsMintedValue, "Bearer " + hsMintedValue})
}

// hsfLeaks says which of the shapes are in a text.
func hsfLeaks(text string) []string {
	var found []string
	for _, f := range hsfForms() {
		if strings.Contains(text, f) {
			found = append(found, f[:min(len(f), 6)]+"...")
		}
	}
	return found
}

func hsfNoSecret(t *testing.T, where string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if found := hsfLeaks(string(data)); len(found) > 0 {
		t.Errorf("%s holds a value of a secret (%v)", where, found)
	}
}

// hsfColumns gives the first dataset of a document the columns that the tests below put the echoes in.
func hsfColumns(doc string) string {
	return strings.Replace(doc, `{"name":"rack","paths":["location.rack"]}`,
		`{"name":"rack"},{"name":"seen"},{"name":"b64"},{"name":"enc"},{"name":"deep","paths":["deep.0.x"]},{"name":"n"}`, 1)
}

// [REQ-690]
// The values a secret may come back in are the values of the secret, the escapes of them in a query and in a path and
// their base64 in both alphabets, with the padding and without, and nothing under four bytes, whatever the secret was.
func TestHsfTheShapesOfASecretAreAllListedAndAShortValueIsNone(t *testing.T) {
	value := "p a/th+q?=&é~token"
	got := map[string]bool{}
	for _, f := range maskForms([]string{value}) {
		got[f] = true
	}
	b := []byte(value)
	for name, want := range map[string]string{
		"as it is": value, "as a query": url.QueryEscape(value), "as a path": url.PathEscape(value),
		"standard base64": base64.StdEncoding.EncodeToString(b), "standard base64 without padding": base64.RawStdEncoding.EncodeToString(b),
		"URL base64": base64.URLEncoding.EncodeToString(b), "URL base64 without padding": base64.RawURLEncoding.EncodeToString(b),
	} {
		if !got[want] {
			t.Errorf("%s is not among the shapes", name)
		}
	}
	forms := maskForms([]string{value, value, "abc", "abcd", ""})
	for i, f := range forms {
		if len(f) < minMaskBytes || (i > 0 && len(forms[i-1]) < len(f)) {
			t.Fatalf("the shape %q is short or out of order", f)
		}
	}
	if m := holdTexts(maskForms([]string{"abc"})); m.repl != nil {
		t.Error("a value of three bytes is taken out of texts")
	} else if text, hit := m.clean("abc abc"); hit || text != "abc abc" {
		t.Errorf("%q", text)
	}
	m := holdTexts(maskForms([]string{"abcd"}))
	if text, hit := m.clean("x abcd y YWJjZA== z"); !hit || strings.Contains(text, "abcd") || strings.Contains(text, "YWJjZA") || text != "x *** y *** z" {
		t.Errorf("%q", text)
	}
}

// [REQ-690]
// A source that puts what it was sent in a path it gives for the next page does not get the secret into the events or into the
// record of the exchanges of a preview, and the request that follows the address is the one the source asked for.
func TestHsfASecretEchoedIntoTheNextPathIsInNoEventAndNoExchange(t *testing.T) {
	for name, auth := range map[string]string{"bearer": hsBearer, "basic": hsBasic, "header": hsHeader} {
		handle := func(n int, r hsRequest) hsReply {
			if n%2 == 1 {
				echo := r.hsFirst("Authorization") + r.hsFirst("X-Api-Key")
				return hsJSON(`{"items":` + hsRecords(0, 1) + `,"next":"` + echo + `"}`)
			}
			return hsJSON(`{"items":` + hsRecords(1, 2) + `,"next":""}`)
		}
		g := hsNew(t, hsDoc(auth, "", `{"kind":"next_url","next_path":"next"}`, ""), handle)
		pages, err := g.hsRead(ingest.PageOptions{})
		if err != nil || len(hsSerials(pages)) != 2 {
			t.Fatalf("%s: %v", name, err)
		}
		if got, want := len(g.hsSrv.hsSeen), 2; got != want || hsfLeaks(g.hsSrv.hsSeen[1].hsURL) == nil {
			t.Fatalf("%s: the request for the next page is not the address the source gave: %v", name, g.hsSrv.hsSeen[len(g.hsSrv.hsSeen)-1].hsURL)
		}
		hsfNoSecret(t, name+": the events", g.hsEvents)
		pv, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{MaxPages: 2, MaxRows: 10})
		if err != nil || len(pv.Exchanges) != 2 {
			t.Fatalf("%s: %v, %d exchanges", name, err, len(pv.Exchanges))
		}
		hsfNoSecret(t, name+": the exchanges", pv.Exchanges)
		if !strings.Contains(pv.Exchanges[1].Path, ingest.MaskedValue) || !pv.Masked {
			t.Errorf("%s: the path of the second exchange is %q, masked %v", name, pv.Exchanges[1].Path, pv.Masked)
		}
	}
}

// [REQ-690]
// Every text of an exchange has the values taken out: the path, the names and values of the query of an address the source
// gave, the names of the headers and the values of the three that are shown.
func TestHsfEveryTextOfAnExchangeHasTheValuesTakenOut(t *testing.T) {
	handle := func(n int, r hsRequest) hsReply {
		echo := strings.TrimPrefix(r.hsFirst("Authorization"), "Bearer ")
		rep := hsJSON(`{"items":` + hsRecords(n-1, n) + `,"next":"/api/v2/servers/` + url.PathEscape(echo) + `?` + url.QueryEscape(echo) + `=1&k=v"}`)
		rep.hsHeader["Content-Type"] = []string{"application/json; note=" + echo}
		rep.hsHeader["X-"+echo] = []string{"x"}
		rep.hsHeader["Retry-After"] = []string{echo}
		if n == 2 {
			rep = hsJSON(`{"items":[],"next":""}`)
		}
		return rep
	}
	g := hsNew(t, hsDoc(hsBearer, "", `{"kind":"next_url","next_path":"next"}`, ""), handle)
	pv, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{MaxPages: 3, MaxRows: 10})
	if err != nil || len(pv.Exchanges) != 2 {
		t.Fatalf("%v, %d exchanges", err, len(pv.Exchanges))
	}
	hsfNoSecret(t, "the exchanges", pv.Exchanges)
	hsfNoSecret(t, "the events", g.hsEvents)
	hasPath := false
	for _, e := range pv.Exchanges {
		hasPath = hasPath || strings.Contains(e.Path, ingest.MaskedValue)
	}
	if !hasPath {
		t.Errorf("the path of the address was not masked: %+v", pv.Exchanges)
	}
}

// [REQ-690]
// The rows of a read have the values taken out of every text of every record, at any depth, in the shapes a source may
// give them back in, and a preview says that it left something out.
func TestHsfEchoedSecretsAreTakenOutOfTheRowsOfARunAndOfAPreview(t *testing.T) {
	for name, auth := range map[string]string{"bearer": hsBearer, "basic": hsBasic, "header": hsHeader, "token request": hsTokReq} {
		handle := func(n int, r hsRequest) hsReply {
			if strings.HasSuffix(r.hsPath(), "/auth/token") {
				return hsJSON(`{"token":"` + hsMintedValue + `"}`)
			}
			got := r.hsFirst("Authorization") + r.hsFirst("X-Api-Key")
			rec := map[string]any{"serial": "s-1", "seen": got, "b64": base64.StdEncoding.EncodeToString([]byte(got)),
				"enc": url.QueryEscape(strings.TrimPrefix(got, "Bearer ")), "deep": []any{map[string]any{"x": "<" + got + ">"}}, "n": json.Number("1234")}
			data, _ := json.Marshal(map[string]any{"items": []any{rec}})
			return hsJSON(string(data))
		}
		g := hsNew(t, hsfColumns(hsDoc(auth, "", "", "")), handle)
		pages, err := g.hsRead(ingest.PageOptions{})
		if err != nil || len(hsSerials(pages)) != 1 {
			t.Fatalf("%s: %v", name, err)
		}
		for _, p := range pages {
			hsfNoSecret(t, name+": a row", p.Rows)
		}
		if got := pages[0].Rows[0].Fields; got["serial"] != "s-1" || got["n"] != "1234" || !strings.Contains(got["seen"], ingest.MaskedValue) {
			t.Errorf("%s: the row %v", name, got)
		}
		pv, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{})
		if err != nil || len(pv.Pages) != 1 || !pv.Masked {
			t.Fatalf("%s: %v, masked %v", name, err, pv.Masked)
		}
		hsfNoSecret(t, name+": the preview", pv)
	}
}

// [REQ-690]
// The key of a row is made of the record after the values are taken out, so that the secret is not in the identity of a row.
func TestHsfARecordWhoseKeyIsASecretHasNoSecretInItsRow(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", "", ""), func(_ int, r hsRequest) hsReply {
		return hsJSON(`{"items":[{"serial":"` + strings.TrimPrefix(r.hsFirst("Authorization"), "Bearer ") + `","location":{"rack":"r1"}}]}`)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(pages[0].Rows) != 1 {
		t.Fatalf("%v", err)
	}
	hsfNoSecret(t, "the row", pages[0].Rows)
	if pages[0].Rows[0].Fields["serial"] != ingest.MaskedValue {
		t.Errorf("the row %v", pages[0].Rows[0].Fields)
	}
}

// [REQ-690]
// A cursor that holds a value of the secret is not put in a token: the page goes without one and one event says so, and the
// other pages have the tokens they had.
func TestHsfACursorThatHoldsASecretIsNotPutInAToken(t *testing.T) {
	for _, form := range []string{"raw", "base64"} {
		handle := func(n int, r hsRequest) hsReply {
			echo := r.hsFirst("Authorization")
			if form == "base64" {
				echo = base64.StdEncoding.EncodeToString([]byte(echo))
			}
			if n == 1 {
				return hsJSON(`{"items":` + hsRecords(0, 2) + `,"meta":{"next":"` + echo + `"}}`)
			}
			return hsJSON(`{"items":` + hsRecords(2, 4) + `,"meta":{"next":""}}`)
		}
		doc := hsDoc(hsBearer, "", `{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next"}`, "")
		g := hsNew(t, doc, handle)
		pages, err := g.hsRead(ingest.PageOptions{MaxRows: 1})
		if err != nil || len(hsSerials(pages)) != 4 || len(pages) != 4 {
			t.Fatalf("%s: %d pages, %v", form, len(pages), err)
		}
		var has []bool
		for _, p := range pages {
			has = append(has, p.Token != nil)
			hsfNoSecret(t, form+": a token", string(p.Token))
		}
		// pages: [s-0000] has a token, [s-0001] ends the first answer and its token holds the cursor, [s-0002] was asked for
		// with the cursor, [s-0003] is the last page
		if fmt.Sprint(has) != "[true false false false]" {
			t.Errorf("%s: the pages that have a token: %v", form, has)
		}
		left := 0
		for _, e := range g.hsEvents {
			if e.Kind == tokenLeft {
				left++
			}
		}
		if left != 2 {
			t.Errorf("%s: %d events say a token was left out", form, left)
		}
		hsfNoSecret(t, form+": the events", g.hsEvents)
	}
}

// [REQ-690]
// A cursor with no value of a secret is a token as it always was.
func TestHsfACursorWithNoSecretStaysInTheToken(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", `{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next","size_param":"limit","size":2}`, ""), hsAPI(ingest.PageCursor, 4, 2, 1))
	pages, err := g.hsRead(ingest.PageOptions{MaxRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range pages[:len(pages)-1] {
		if p.Token == nil {
			t.Errorf("page %d has no token", i)
		}
	}
	for _, e := range g.hsEvents {
		if e.Kind == tokenLeft {
			t.Errorf("an event says a token was left out: %+v", e)
		}
	}
}

// [REQ-690]
// The cursor is looked at as it is and not only as the token holds it, which may write it with escapes.
func TestHsfTheCursorIsLookedAtAsItIsAndNotOnlyInsideTheToken(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", "", ""), hsOne)
	rn, err := g.hsReader.begin(hsCtx, hsSource, g.hsDef.Datasets[0], "run")
	if err != nil {
		t.Fatal(err)
	}
	defer rn.close()
	if got := rn.safeToken([]byte(`{"v":1}`), "plain-cursor"); string(got) != `{"v":1}` {
		t.Errorf("a cursor with no secret: %q", got)
	}
	if got := rn.safeToken([]byte(`{"v":1}`), "a"+hsTokenValue); got != nil {
		t.Errorf("a cursor with a secret: %q", got)
	}
	if got := rn.safeToken([]byte(`{"c":"`+hsTokenValue+`"}`), ""); got != nil {
		t.Errorf("a token with a secret: %q", got)
	}
	hsfNoSecret(t, "the events", g.hsEvents)
}
