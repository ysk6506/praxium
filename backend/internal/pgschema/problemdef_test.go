package pgschema

import (
	"strings"
	"testing"
)

// The number of the definition of a problem (ADR-0128): a feed keeps its number, a link definition one below zero, and the two are told
// apart by the sign alone. The statements that keep the mark of the objects held to retire name the tables of the schema they are given.

// [REQ-984]
func TestPdfAFeedAndALinkDefinitionOfOneNumberKeepTwoNumbersThatReadBackAsWhatTheyWere(t *testing.T) {
	seen := map[int]bool{}
	for n := 0; n < 50; n++ {
		feed, link := ProblemDefOfFeed(n), ProblemDefOfLink(n)
		if seen[feed] || seen[link] || feed < 0 || link >= 0 {
			t.Fatalf("the feed %d keeps %d and the link definition %d keeps %d", n, feed, n, link)
		}
		seen[feed], seen[link] = true, true
		if k, got := ProblemDefRead(feed); k != ProblemDefFeed || got != n {
			t.Errorf("%d reads back as %d, %d", feed, k, got)
		}
		if k, got := ProblemDefRead(link); k != ProblemDefLink || got != n {
			t.Errorf("%d reads back as %d, %d", link, k, got)
		}
	}
	for _, c := range []struct{ got, want string }{
		{ProblemDefFeedSQL("f.no"), "(f.no)"},
		{ProblemDefLinkSQL("w.def_no"), "(-(w.def_no) - 1)"},
		{ProblemDefIsLinkSQL("p.def_no"), "(p.def_no < 0)"},
		{ProblemDefIsFeedSQL("p.def_no"), "(p.def_no >= 0)"},
	} {
		if c.got != c.want {
			t.Errorf("%q, want %q", c.got, c.want)
		}
	}
}

// [REQ-987]
func TestPdfTheStatementsOfTheMarkNameTheTablesOfTheSchemaTheyAreGiven(t *testing.T) {
	schema := SchemaName(7)
	for _, q := range []string{MarkRetirableSQL(schema), MarkRetirableChunkSQL(schema), UnmarkRetiredChunkSQL(schema)} {
		if strings.Contains(q, "{") || !strings.Contains(q, Qualified(schema, TableObjectRetire)) {
			t.Errorf("the statement does not name its tables: %s", q)
		}
	}
	if q := MarkRetirableChunkSQL(schema); !strings.Contains(q, Qualified(schema, TableIngestDiff)) || !strings.Contains(q, "$1") {
		t.Errorf("the mark of a chunk does not read the chunk: %s", q)
	}
}
