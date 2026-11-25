package xlsx

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"testing"
)

// The reader reads the workbooks that the code before the row parser read as that code did (commit 8aecc37). What was written down
// then is in testdata: the whole answer for each hand-written file, and the digest of the answer for each file a seeded generator
// makes. The files are made in xs_corpus_test.go and xs_random_test.go.

//go:embed testdata/xs_read_hand.json
var xsHandFile []byte

//go:embed testdata/xs_read_random.json
var xsRandomFile []byte

// [REQ-805]
func TestXsReadWorkbookAnswersAsTheCodeBeforeTheRowParserDid(t *testing.T) {
	var want struct {
		Commit string
		Cases  []struct {
			Name   string
			Answer json.RawMessage
		}
	}
	if err := json.Unmarshal(xsHandFile, &want); err != nil {
		t.Fatal(err)
	}
	cases := xsHandCases()
	if len(want.Cases) != len(cases) {
		t.Fatalf("%d answers written down, %d cases", len(want.Cases), len(cases))
	}
	for i, c := range cases {
		if want.Cases[i].Name != c.name {
			t.Fatalf("case %d is %q, written down as %q", i, c.name, want.Cases[i].Name)
		}
		got, err := json.Marshal(xsRead(c))
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		if err := json.Unmarshal(got, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want.Cases[i].Answer, &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, want.Cases[i].Answer)
		}
	}
}

// [REQ-805]
func TestXsReadWorkbookAnswersAsTheCodeBeforeTheRowParserDidForGeneratedFiles(t *testing.T) {
	var want struct {
		Seed    uint64
		Digests []string
	}
	if err := json.Unmarshal(xsRandomFile, &want); err != nil {
		t.Fatal(err)
	}
	for i, c := range xsRandomCases(want.Seed, len(want.Digests)) {
		if got := xsDigest(xsRead(c)); got != want.Digests[i] {
			t.Errorf("%s: the answer is %s, written down as %s", c.name, got, want.Digests[i])
			if i > 5 {
				return
			}
		}
	}
}
