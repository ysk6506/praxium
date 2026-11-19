package ingest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// [REQ-691]
// A header that makes a server read a POST as another method is one the definition may not write, as a header of the
// connection and as the header of an authentication, in any case.
func TestHsfHeadersThatChangeTheMethodAreNotAllowed(t *testing.T) {
	for _, name := range []string{"X-HTTP-Method-Override", "x-http-method", "X-Method-Override", "X-Http-Method-Override"} {
		headers := adpMin(`,"headers":{"`+name+`":"DELETE"}`, `"request":{"path":"/a"},`)
		auth := adpMin(`,"auth":{"kind":"header","credential":{"secret_id":"k"},"header":"`+name+`"}`, `"request":{"path":"/a"},`)
		for doc, pointer := range map[string]string{headers: "/connection/headers/" + name, auth: "/connection/auth/header"} {
			_, err := ParseAdapterSource(strings.NewReader(doc))
			de, ok := DefinitionErrorsOf(err)
			if !ok || !slices.ContainsFunc(de.List, func(e DefinitionError) bool {
				return e.Code == "source.connection.header_not_allowed" && e.Pointer == pointer
			}) {
				t.Errorf("%s at %s: %v", name, pointer, err)
			}
		}
	}
	if _, err := ParseAdapterSource(strings.NewReader(adpMin(`,"headers":{"X-Request-Id":"a"}`, `"request":{"path":"/a"},`))); err != nil {
		t.Errorf("a header that moves nothing: %v", err)
	}
}

// [REQ-691]
// max_response_bytes may be the limit and not more.
func TestHsfMaxResponseBytesHasALimit(t *testing.T) {
	read := func(n int) error {
		_, err := ParseAdapterSource(strings.NewReader(adpMin(fmt.Sprintf(`,"max_response_bytes":%d`, n), `"request":{"path":"/a"},`)))
		return err
	}
	if err := read(MaxResponseBytesLimit); err != nil {
		t.Errorf("the limit itself: %v", err)
	}
	for _, n := range []int{MaxResponseBytesLimit + 1, 1 << 30} {
		_, err := ParseAdapterSource(strings.NewReader(adpMin(fmt.Sprintf(`,"max_response_bytes":%d`, n), `"request":{"path":"/a"},`)))
		de, ok := DefinitionErrorsOf(err)
		if !ok || !slices.ContainsFunc(de.List, func(e DefinitionError) bool {
			return e.Code == "source.connection.value_out_of_range" && e.Pointer == "/connection/max_response_bytes"
		}) {
			t.Errorf("%d: %v", n, err)
		}
	}
	if MaxResponseBytesLimit != 256<<20 {
		t.Errorf("the limit is %d", MaxResponseBytesLimit)
	}
}

// [REQ-693]
// A token that is made by a function is made when a page is cut, once for a page, from the record the page ends with,
// and a page the function gives nothing for has no token.
func TestHsfPageWriterMakesTheTokenWhenAPageIsCutAndNotForEveryRecord(t *testing.T) {
	ds := Dataset{ID: "d", NaturalKey: []string{"k"}, Columns: []Column{{Name: "k"}}}
	b, err := NewRowBuilder(ds)
	if err != nil {
		t.Fatal(err)
	}
	const records = 1000
	for _, rows := range []int{7, 100, 5000} {
		w := NewPageWriter(ds, PageOptions{MaxRows: rows})
		made := 0
		var pages []Page
		yield := func(p Page, err error) bool {
			if err != nil {
				t.Fatal(err)
			}
			pages = append(pages, p)
			return true
		}
		for i := 1; i <= records; i++ {
			after := func() []byte {
				made++
				return []byte(fmt.Sprint(i))
			}
			if i%2 == 0 && rows == 7 {
				after = nil
			}
			if !w.AddLazy(context.Background(), b.Record(map[string]any{"k": fmt.Sprint(i)}, i, ""), after, yield) {
				t.Fatal("the writer stopped")
			}
		}
		w.Finish(yield, Completeness{Complete: true})
		wantMade := 0
		for ends := rows; ends < records; ends += rows {
			if rows != 7 || ends%2 == 1 {
				wantMade++
			}
		}
		if cut := len(pages) - 1; made != wantMade || (rows != 7 && made != cut) {
			t.Errorf("%d rows: the function was called %d times for %d cut pages, want %d", rows, made, cut, wantMade)
		}
		read := 0
		for i, p := range pages {
			read += len(p.Rows)
			switch {
			case p.Last:
				if p.Token != nil || i != len(pages)-1 {
					t.Errorf("%d rows: the last page %+v", rows, p.Token)
				}
			case rows == 7 && read%2 == 0:
				if p.Token != nil {
					t.Errorf("%d rows: page %d has the token %q of a nil function", rows, i, p.Token)
				}
			case string(p.Token) != fmt.Sprint(read):
				t.Errorf("%d rows: page %d has the token %q, want the one after record %d", rows, i, p.Token, read)
			}
		}
	}
}
