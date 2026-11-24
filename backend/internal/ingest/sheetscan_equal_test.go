package ingest

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"testing"
)

// The scanner reads the sheets that the code before it read (commit d1df3c0) as that code did. What was written down then is in
// testdata: the whole table of each hand-written case, and the digest of the table of each of the sheets that a seeded generator
// makes. The inputs are in xs_corpus_test.go. The cases whose answer the scanner changes on purpose are apart (xs_changed_scan.json).

//go:embed testdata/xs_scan_hand.json
var xsHandFile []byte

//go:embed testdata/xs_scan_random.json
var xsRandomFile []byte

// [REQ-800]
func TestXsScanReadsTheHandWrittenSheetsAsTheCodeBeforeIt(t *testing.T) {
	var want struct {
		Commit string
		Cases  []struct {
			Name  string
			Table json.RawMessage
		}
	}
	if err := json.Unmarshal(xsHandFile, &want); err != nil {
		t.Fatal(err)
	}
	cases := xsHandCases()
	if len(want.Cases) != len(cases) {
		t.Fatalf("%d cases written down, %d cases", len(want.Cases), len(cases))
	}
	for i, c := range cases {
		if want.Cases[i].Name != c.name {
			t.Fatalf("case %d is %q, written down as %q", i, c.name, want.Cases[i].Name)
		}
		got, err := json.Marshal(ParseSheet(c.ds, c.wb))
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		if err := json.Unmarshal(got, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want.Cases[i].Table, &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, want.Cases[i].Table)
		}
	}
}

// [REQ-800] [REQ-803]
func TestXsScanReadsTheGeneratedSheetsAsTheCodeBeforeIt(t *testing.T) {
	var want struct {
		Seed    uint64
		Count   int
		Digests []string
	}
	if err := json.Unmarshal(xsRandomFile, &want); err != nil {
		t.Fatal(err)
	}
	cases := xsRandomCases(want.Seed, want.Count)
	if len(want.Digests) != len(cases) {
		t.Fatalf("%d digests written down, %d cases", len(want.Digests), len(cases))
	}
	bad, left := 0, 0
	for i, c := range cases {
		if got := xsDigest(ParseSheet(c.ds, c.wb)); got != want.Digests[i] {
			bad++
			if bad <= 5 {
				t.Errorf("%s: the table is not the one the code before the scanner made", c.name)
			}
		}
		// The same sheet with the cells that the dataset does not read left without text, as a reader that builds only what it is
		// asked for gives them, is the same table: what a row is and what is blank do not depend on that text.
		if v := xsSkipVariant(c); v != nil {
			left++
			if got := xsDigest(ParseSheet(c.ds, v)); got != want.Digests[i] {
				bad++
				if bad <= 5 {
					t.Errorf("%s: with the cells that are not read left out, the table is not the same", c.name)
				}
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d tables differ", bad, len(cases))
	}
	if left < len(cases)/2 {
		t.Fatalf("only %d of %d sheets have cells left out: the variant tests too little", left, len(cases))
	}
}
