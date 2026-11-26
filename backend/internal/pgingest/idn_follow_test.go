package pgingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The follow-up of the identification (ADR-0116): the groups a round leaves are left whole with what shares a key with them in the
// rounds after it, Go reads a page of the groups that are not whole by its range and keeps the lease of the run, and the tests of the
// end of a search cut every call and hold the conditions of the statement of the groups of several objects. Every helper starts with
// idnf, so that none meets a name of another test file of this folder.

// idnfRackRun is the second run of the tests of the groups that are left: a chain of racks in a room there is (each shares a tag with
// the next, and the tag tK is the tag of a rack there is), and the racks of a room that is new, which are ready in a later round.
func idnfRackRun(later ...string) []string {
	rows := []string{"a0,S,B,F,R1,rkA,tA", "k0,S,B,F,R9,rkK,tK"}
	for i := 0; i < 12; i++ {
		tag := fmt.Sprintf("t%d", i/2)
		if i/2 == 2 {
			tag = "tK"
		}
		rows = append(rows, fmt.Sprintf("c%02d,S,B,F,R1,n%d,%s", i, (i+1)/2, tag))
	}
	return append(rows, later...)
}

// idnfFirstRun makes the room R1 with the rack a0 and the room R9 with the rack k0, and gives the setup the next runs go on from.
func idnfFirstRun(t *testing.T) *rdfSetup {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	l1, _ := idnPipelineWith(s, t, idnPlacesTexts("a0,S,B,F,R1,rkA,tA", "k0,S,B,F,R9,rkK,tK"), rdfT0, 2, 0, 0, 0, 0)
	idnApply(s, t)
	s.rdfSettle(t, l1)
	return s
}

// idnfConflicts is how many problems of identity the run has.
func idnfConflicts(s *rdfSetup, t *testing.T) int {
	t.Helper()
	return s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem WHERE kind = 'identity_conflict'`, s.ref.KGNo))
}

// [REQ-958]
func TestIdnARecordThatSharesAKeyOnlyWithARecordThatIsLeftWithAChainIsLeftWithThemAndTheNextRunMakesThemAllOneObject(t *testing.T) {
	s := idnfFirstRun(t)
	// p0 shares the last tag of the chain, which is left (a turn, and Go holds one pair); q0 shares nothing with the chain, only its room
	// and its name with p0, so it is in the group of p0 and is left with it
	second := idnfRackRun("p0,S,B,F,RN,np,t5", "q0,S,B,F,RN,np,tQ")
	l2, counts2 := idnPipelineWith(s, t, idnPlacesTexts(second...), rdfT0.Add(time.Hour), 2, 0, 1, 0, 1)
	if counts2.Unsettled == 0 || idnfConflicts(s, t) != 0 {
		t.Fatalf("the limited run: %+v, %d conflicts", counts2, idnfConflicts(s, t))
	}
	for _, b := range idnBounds(s, t) {
		if b.Feed == 4 && (strings.HasPrefix(b.Key, "c") || b.Key == "p0" || b.Key == "q0") {
			t.Errorf("%+v: bound in the run that left its group", b)
		}
	}
	idnApply(s, t)
	s.rdfSettle(t, l2)
	_, counts3 := idnPipelineWith(s, t, idnPlacesTexts(second...), rdfT0.Add(2*time.Hour), 2, 0, 0, 0, 0)
	refs := map[string]bool{}
	for _, b := range idnBounds(s, t) {
		if b.Feed == 4 && (strings.HasPrefix(b.Key, "c") || b.Key == "p0" || b.Key == "q0" || b.Key == "k0") {
			refs[b.Ref] = true
		}
	}
	probs := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem`, s.ref.KGNo))
	if len(refs) != 1 || probs != 0 || counts3.Unsettled != 0 || counts3.Unresolved != 0 {
		t.Errorf("the chain, p0, q0 and k0 are %d objects, %d problems: %+v", len(refs), probs, counts3)
	}
}

// idnfStatementRead is what one call of a statement read: the rows of each relation (with the loops counted) and the scans.
type idnfStatementRead struct {
	what  string
	rows  map[string]float64
	scans []string
}

// idnfWatch gives a hook that asks the server, for each call of the statements named, what it read, in the plan the server chooses
// (no setting of the planner is changed): the reads are appended to into.
func idnfWatch(whats map[string]bool, into *[]idnfStatementRead) identObserver {
	return func(tx *sql.Tx, what, query string, args []any) {
		if !whats[what] {
			return
		}
		if _, err := tx.Exec("SAVEPOINT idnf_watch"); err != nil {
			panic(err)
		}
		defer func() {
			if _, err := tx.Exec("ROLLBACK TO SAVEPOINT idnf_watch"); err != nil {
				panic(err)
			}
		}()
		var doc []byte
		if err := tx.QueryRow("EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, FORMAT JSON) "+query, args...).Scan(&doc); err != nil {
			panic(fmt.Sprintf("%s: %v", what, err))
		}
		var top []struct{ Plan map[string]any }
		if err := json.Unmarshal(doc, &top); err != nil {
			panic(err)
		}
		read := idnfStatementRead{what: what, rows: map[string]float64{}}
		var walk func(map[string]any)
		walk = func(node map[string]any) {
			if name, ok := node["Relation Name"].(string); ok {
				actual, _ := node["Actual Rows"].(float64)
				loops, _ := node["Actual Loops"].(float64)
				removed, _ := node["Rows Removed by Filter"].(float64)
				read.rows[name] += (actual + removed) * loops
				read.scans = append(read.scans, fmt.Sprintf("%v on %s", node["Node Type"], name))
			}
			if kids, ok := node["Plans"].([]any); ok {
				for _, k := range kids {
					if m, ok := k.(map[string]any); ok {
						walk(m)
					}
				}
			}
		}
		for _, p := range top {
			walk(p.Plan)
		}
		*into = append(*into, read)
	}
}

// idnfMost is the most each statement read in one call, in the form idnCheckReads holds.
func idnfMost(reads []idnfStatementRead) map[string]*idnRead {
	out := map[string]*idnRead{}
	for _, r := range reads {
		m := out[r.what]
		if m == nil {
			m = &idnRead{rows: map[string]float64{}}
			out[r.what] = m
		}
		for table, n := range r.rows {
			m.rows[table] = max(m.rows[table], n)
		}
		m.scans = append(m.scans, r.scans...)
	}
	return out
}

// idnfSettleWhats are the statements of Go's read of a page and of the copy of the labels Go did not write.
var idnfSettleWhats = map[string]bool{"settle end": true, "read the pairs of the groups that are not whole": true, "copy end": true,
	"copy the labels of the groups that were whole": true, "take away the labels that are not read again": true}

// idnfSettled runs the identification of a chain of n records in a random order of its keys with one turn, so that Go ends the
// search, in pages of the size, and gives what each call of Go's read and of the copy after it read.
func idnfSettled(t *testing.T, n, size int) ([]idnfStatementRead, ingest.IdentifyCounts) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	var reads []idnfStatementRead
	s.e.st.identSeen = idnfWatch(idnfSettleWhats, &reads)
	defer func() { s.e.st.identSeen = nil }()
	_, counts := idnPipelineWith(s, t, idnServers(idnShuffled(n, int64(n))...), rdfT0, 5000, size, 1, 0, 0)
	if objs := idnObjectsOf(s, t); len(objs) != 1 || counts.Unsettled != 0 {
		t.Fatalf("a chain of %d records is %d objects: %+v", n, len(objs), counts)
	}
	return reads, counts
}

// [REQ-959]
func TestIdnGoReadsAPageOfTheGroupsThatAreNotWholeByItsRangeAndWhatAPageReadsDoesNotGrowWithThePairsThatAreLeftToRead(t *testing.T) {
	// three times the pairs (the sizes keep the test under ten seconds: 5,000 and 15,000 pairs take fourteen, and a page that reads the
	// pairs left after it reads hundreds of thousands of rows at these sizes already)
	const size, smaller, larger = 500, 2_000, 6_000
	small, _ := idnfSettled(t, smaller, size)
	large, _ := idnfSettled(t, larger, size)
	pages := 0
	for _, r := range large {
		if r.what == "read the pairs of the groups that are not whole" {
			pages++
		}
	}
	if pages < larger/size {
		t.Fatalf("Go read %d pages of %d pairs", pages, size)
	}
	// a page reads its pairs, their keys and the pairs that have the keys (two for each key of a chain): some rows for each pair of the
	// page, and the same for the first page as for the last, whatever the pairs that are left after it; the copy of the labels Go did not
	// write is held the same way
	idnCheckReads(t, idnfMost(small), idnfMost(large), 40*size, 1.5)
}
