package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// More of the comparison held to the old pipeline (rdf_parity_test.go): random captures, a key that repeats through the runs, and a
// row whose filtering turns over.

// rdfRandSteps are the captures of a run of random ones: a handful of keys that come and go, are filtered and freed, repeat, and
// datasets that are not given or are cut off, at times that follow one another by an hour to a day.
func rdfRandSteps(rng *kernel.SeededRng, n int) []rdfStep {
	// rdfDraw is a number from 0 to below n, from the seeded generator of the kernel (results stay reproducible, REQ-001).
	rdfDraw := func(n int) int { return int(rng.Uint64() % uint64(n)) }
	keys := []string{"a", "b", "c", "d", "e", "f", "g"}
	gen := func(cut *bool) []string {
		*cut = rdfDraw(10) == 0
		if rdfDraw(10) == 0 {
			return nil
		}
		out := []string{}
		for _, k := range keys {
			if rdfDraw(10) < 3 {
				continue
			}
			reps := 1
			if rdfDraw(6) == 0 {
				reps = 2 + rdfDraw(2)
			}
			for i := 0; i < reps; i++ {
				flag := "on"
				if rdfDraw(4) == 0 {
					flag = "off"
				}
				out = append(out, fmt.Sprintf("%s/%s/%d", k, flag, 1+rdfDraw(2)))
			}
		}
		for i := len(out) - 1; i > 0; i-- {
			j := rdfDraw(i + 1)
			out[i], out[j] = out[j], out[i]
		}
		return out
	}
	hour := 0
	var steps []rdfStep
	for i := 0; i < n; i++ {
		st := rdfStep{hour: hour}
		st.racks = gen(&st.racksCut)
		st.pods = gen(&st.podsCut)
		steps = append(steps, st)
		hour += []int{1, 1, 2, 9, 13, 25}[rdfDraw(6)]
	}
	return steps
}

// [REQ-784]
func TestRdfTheComparisonGivesWhatTheOldPipelineGivesForRandomCapturesAndAnyCutOfTheBuckets(t *testing.T) {
	for _, c := range []struct {
		seed uint64
		size int
	}{{1, 1}, {2, 2}, {3, 0}} {
		p := rdfNewParity(t, c.size)
		p.onlyRaw = true
		steps := rdfRandSteps(kernel.NewSeededRng(c.seed), 30)
		for n, st := range steps {
			p.rdfStepOn(n, st)
			if t.Failed() {
				t.Fatalf("seed %d, %d rows to a bucket: it differs at step %d: %+v", c.seed, c.size, n, st)
			}
		}
	}
}

// [REQ-784]
func TestRdfARowWhoseHashIsTheSameAndWhoseFilteringTurnsOverMakesTheObjectsTheOldPipelineMakes(t *testing.T) {
	for _, size := range []int{0, 1} {
		p := rdfNewParity(t, size)
		p.flips = true
		rows := []string{"a/on/1", "f/off/1", "g/off/2"}
		// filtered, shown whole, filtered again, and whole once more: no row changes but in how the mapping treats it. The comparison
		// opens a record again for each row that is filtered by one mapping and not by the other (f, g and pf), and for no other.
		opens := []int{5, 3, 3, 0, 3, 0}
		for n, loose := range []bool{false, true, false, false, true, true} {
			p.rdfStepOn(n, rdfStep{hour: 10 * n, loose: loose, racks: rows, pods: []string{"p1/on/1", "pf/off/1"}})
			opened := p.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM %s WHERE opened_seq = $1`, pgiTable(p.s.ref, pgschema.TableRawRecord)), p.runNos[n])
			if opened != opens[n] {
				t.Errorf("step %d: the comparison opens %d records, not %d", n, opened, opens[n])
			}
		}
	}
}

// [REQ-784]
func TestRdfAKeyThatRepeatsKeepsItsProblemFromItsStartThroughTheRunsThatDoNotChangeItToItsEnd(t *testing.T) {
	p := rdfNewParity(t, 0)
	problems := func() int {
		return p.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".record_problem WHERE kind = 'duplicate_key' AND key = 'b' AND def_no IS NULL`, p.s.ref.KGNo))
	}
	records := func() int {
		return p.s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record WHERE key = 'b'`, p.s.ref.KGNo))
	}
	// a record that a later step of a run (not the comparison) puts to the set, for the key that repeats, with no problem of its own
	later := func() {
		p.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why)
			SELECT src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, %d FROM %s WHERE key = 'b'`, p.s.ref.KGNo, pgschema.CalcWhyUnresolved, pgiTable(p.s.ref, pgschema.TableRawRecord)))
	}
	for n, c := range []struct {
		rows        []string
		later       bool
		wantRecords int
		wantProblem int
	}{
		{[]string{"a/on/1", "b/on/1", "b/on/2"}, false, 1, 1}, // the key repeats from now on: the comparison makes a record, with the problem
		{[]string{"a/on/1", "b/on/2", "b/on/2"}, false, 0, 1}, // it repeats the same: no record of the comparison's, and the problem stays
		{[]string{"a/on/1", "b/on/2", "b/on/2"}, true, 1, 1},  // a later step puts a record of its own for it: the problem stays
		{[]string{"a/on/1", "b/on/2"}, false, 1, 0},           // it repeats no more: the comparison says so and the problem is taken away
	} {
		p.beforeSettle = nil
		if c.later {
			p.beforeSettle = later
		}
		var seen int
		inner := p.beforeSettle
		p.beforeSettle = func() {
			seen = records()
			if inner != nil {
				inner()
			}
		}
		p.rdfStepOn(n, rdfStep{hour: n, racks: c.rows, pods: []string{"p1/on/1"}})
		wantSeen := c.wantRecords
		if c.later {
			wantSeen = 0
		}
		if seen != wantSeen {
			t.Errorf("run %d: the comparison put %d records of the key to the set, not %d", n, seen, wantSeen)
		}
		if got := problems(); got != c.wantProblem {
			t.Errorf("run %d: the key has %d problems of a repeated key, not %d", n, got, c.wantProblem)
		}
	}
}
