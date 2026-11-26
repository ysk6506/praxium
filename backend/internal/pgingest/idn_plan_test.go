package pgingest

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The cost of the identification (ADR-0108): each statement of a unit reads the rows of its range through an index and nothing of the
// registry that is not asked for, so that what a unit reads does not grow with the size of the source. The tests run a source of
// places (the five levels of a hierarchy, each object identified in its parent) twice, the second time with one record in ten changed,
// and ask the server how every statement of the second run went.

// idnRows and idnSize are the size of the source and of a unit of the measurement (go test -args -idn.rows=300000 -idn.size=500).
var (
	idnRows = flag.Int("idn.rows", 30_000, "records of the source the measurement identifies")
	idnSize = flag.Int("idn.size", 0, "rows of a unit of the measurement (0 is the default)")
	idnNow  = kernel.SystemClock{}
)

// The sizes of the sources of the two plan tests: each statement's reads must not grow with the source, so the smaller source only has to
// be big enough for the fixed allowances (the +50 rows and the eight times the pairs of a unit) to hold on both.
const (
	idnPlanSmallRecords = 7_000
	idnPlanLargeRecords = 21_000
	idnHotSmallRecords  = 3_000
	idnHotLargeRecords  = 9_000
)

// idnRowsGiven reports whether the command line set -idn.rows, which is what turns the measurement on.
func idnRowsGiven() bool {
	given := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "idn.rows" {
			given = true
		}
	})
	return given
}

// idnPlace is what a statement read: the rows each relation gave (with the loops counted), the scans in words.
type idnRead struct {
	rows  map[string]float64
	scans []string
}

// idnPlans keeps, for each statement of the run (by what it says it does), the most that one unit of it read.
type idnPlans struct {
	most map[string]*idnRead
	seen int
}

func (p *idnPlans) idnHook(tx *sql.Tx, what, query string, args []any) {
	if strings.HasPrefix(strings.TrimSpace(query), "SET ") {
		return
	}
	if _, err := tx.Exec("SAVEPOINT idn_plan"); err != nil {
		panic(err)
	}
	defer func() {
		if _, err := tx.Exec("ROLLBACK TO SAVEPOINT idn_plan"); err != nil {
			panic(err)
		}
	}()
	// the tables of the test are small enough for a hash or a merge join over a whole table to cost less than an index; the tables of a run
	// of millions of records are read by their indexes, so the plan is asked for the kind that wins there (ADR-0121; the plan of the
	// questions that the statistics give is held by TestIdnTheQuestionsOfARoundAreReadByTheirIndexWithTheStatisticsTheRunGivesThePlanner)
	for _, set := range []string{"SET LOCAL enable_hashjoin = off", "SET LOCAL enable_mergejoin = off", "SET LOCAL enable_seqscan = off"} {
		if _, err := tx.Exec(set); err != nil {
			panic(err)
		}
	}
	var doc []byte
	if err := tx.QueryRow("EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, FORMAT JSON) "+query, args...).Scan(&doc); err != nil {
		panic(fmt.Sprintf("%s: %v", what, err))
	}
	var top []struct{ Plan map[string]any }
	if err := json.Unmarshal(doc, &top); err != nil {
		panic(err)
	}
	read := &idnRead{rows: map[string]float64{}}
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
	for _, t := range top {
		walk(t.Plan)
	}
	p.seen++
	old := p.most[what]
	if old == nil {
		p.most[what] = read
		return
	}
	for table, n := range read.rows {
		if n > old.rows[table] {
			old.rows[table] = n
		}
	}
	old.scans = append(old.scans, read.scans...)
}

// idnPlaceRows is the rows of the places of n records: a site for every two thousand records and a building for every thousand, floors and rooms, a rack and a tag for each.
func idnPlaceRows(n int, withNew bool, tag func(i int) string) string {
	var b strings.Builder
	b.WriteString(idnSite)
	rows := n
	if withNew {
		rows = n + n/100
	}
	// the records that are new in the second run are one in a hundred and one of the places they are in, so that no place has more of
	// them the larger the source is
	for i, made := 0, 0; made < rows; i++ {
		if !withNew && i%101 == 100 {
			continue
		}
		made++
		fmt.Fprintf(&b, "%d,S%d,B%d,F%d,R%d,K%d,%s\n", i, i/2000, i/1000, i/500, i/100, i, tag(i))
	}
	return b.String()
}

// idnScaled runs the identification of a source of n records, then of the same source with one record in ten changed (its tag, the
// alias of its rack) and one in a hundred new, and gives what the second run read and how long it took.
func idnScaled(t *testing.T, n, size int, explain bool) (map[string]*idnRead, time.Duration, ingest.IdentifyCounts) {
	t.Helper()
	return idnScaledWith(t, n, size, explain, idnPlaceRows)
}

// idnScaledWith is idnScaled with the rows of the source made by rows (the records, whether the second run has the new ones, the tag).
func idnScaledWith(t *testing.T, n, size int, explain bool, rows func(int, bool, func(int) string) string) (map[string]*idnRead, time.Duration, ingest.IdentifyCounts) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	texts := func(rows string) pgiTexts {
		return pgiTexts{"ds_places": rows, "ds_servers": pgiServers[:strings.Index(pgiServers, "\n")+1], "ds_vms": ""}
	}
	run := func(rows string, at time.Time) (ingest.RunLease, ingest.IdentifyCounts) {
		began := idnNow.Now()
		l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
		if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts(rows), Plan: s.plan, Options: ingest.PageOptions{MaxRows: 1000}}); err != nil {
			t.Fatal(err)
		}
		if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(at, size)); err != nil {
			t.Fatal(err)
		}
		t.Logf("received and compared: %v", idnNow.Now().Sub(began))
		keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
		counts, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, idnStart(s, size, 1), keys)
		if err != nil {
			t.Fatal(err)
		}
		return l, counts
	}
	if !explain {
		clock := &idnClock{spent: map[string]time.Duration{}, plans: map[string]string{}}
		s.e.st.identSeen = clock.idnHook
		defer func() {
			s.e.st.identSeen = nil
			t.Log(clock.idnReport())
		}()
	}
	l1, _ := run(rows(n, false, func(i int) string { return "T" + strconv.Itoa(i) }), rdfT0)
	mark := idnNow.Now()
	idnApply(s, t)
	t.Logf("apply: %v", idnNow.Now().Sub(mark))
	mark = idnNow.Now()
	s.rdfSettle(t, l1)
	t.Logf("settle: %v", idnNow.Now().Sub(mark))
	mark = idnNow.Now()
	s.e.exec(t, `VACUUM ANALYZE`)
	t.Logf("vacuum: %v", idnNow.Now().Sub(mark))
	plans := &idnPlans{most: map[string]*idnRead{}}
	if explain {
		s.e.st.identSeen = plans.idnHook
		defer func() { s.e.st.identSeen = nil }()
	}
	tag := func(i int) string {
		if i%10 == 0 {
			return "U" + strconv.Itoa(i)
		}
		return "T" + strconv.Itoa(i)
	}
	start := idnNow.Now()
	_, counts := run(rows(n, true, tag), rdfT0.Add(time.Hour))
	return plans.most, idnNow.Now().Sub(start), counts
}

// idnCheckReads holds what a unit of the second run read, in the larger source, to what the same unit read in the smaller one.
func idnCheckReads(t *testing.T, small, large map[string]*idnRead, most, growth float64) {
	t.Helper()
	var whats []string
	for what := range large {
		whats = append(whats, what)
	}
	sort.Strings(whats)
	for _, what := range whats {
		read := large[what]
		for table, n := range read.rows {
			// a unit reads, for each of its records, the keys of five feeds and the parents of each (up to four): 40 rows a record
			if n > most {
				t.Errorf("%s reads %v rows of %s, more than the %v a unit may read", what, n, table, most)
			}
			if before, ok := small[what]; ok && n > growth*before.rows[table]+50 {
				t.Errorf("%s reads %v rows of %s in the larger source and %v in the smaller one", what, n, table, before.rows[table])
			}
		}
		whole := map[string]bool{}
		for _, scan := range read.scans {
			if table, ok := strings.CutPrefix(scan, "Seq Scan on "); ok && read.rows[table] > most && !whole[table] {
				whole[table] = true
				t.Errorf("%s reads the whole of %s", what, table)
			}
		}
		if testing.Verbose() {
			var before map[string]float64
			if p, ok := small[what]; ok {
				before = p.rows
			}
			t.Logf("%s: rows read by a unit, the larger source %v, the smaller one %v", what, read.rows, before)
		}
	}
}

// [REQ-883]
func TestIdnEveryStatementOfAUnitReadsTheRegistryByItsIndexesAndWhatItReadsOfItDoesNotGrowWithTheSource(t *testing.T) {
	const size = 100
	small, _, _ := idnScaled(t, idnPlanSmallRecords, size, true)
	large, _, counts := idnScaled(t, idnPlanLargeRecords, size, true)
	if counts.NewObjects == 0 || len(small) < 10 {
		t.Fatalf("%d statements, %+v", len(small), counts)
	}
	idnCheckReads(t, small, large, 40*size, 1.5)
	// the statements Go reads with (the questions, the records that have no key) are seen by the same hook, and read by the same bounds
	if _, ok := large["read the questions"]; !ok {
		t.Errorf("the hook did not see the read of the questions: %d statements", len(large))
	}
}

// idnHotRows is the rows of n records that are all in one room of one floor of one building (one place all of them share), a rack and a
// tag of its own for each.
func idnHotRows(n int, withNew bool, tag func(i int) string) string {
	var b strings.Builder
	b.WriteString(idnSite)
	rows := n
	if withNew {
		rows = n + n/100
	}
	for i, made := 0, 0; made < rows; i++ {
		if !withNew && i%101 == 100 {
			continue
		}
		made++
		fmt.Fprintf(&b, "%d,HQ,B0,F0,R0,K%d,%s\n", i, i, tag(i))
	}
	return b.String()
}

// [REQ-883]
func TestIdnWhenEveryRecordIsInTheSamePlaceAUnitReadsOfTheRegistryWhatItDidWhenTheyWereInManyAndNoMore(t *testing.T) {
	const size = 100
	small, _, _ := idnScaledWith(t, idnHotSmallRecords, size, true, idnHotRows)
	large, _, counts := idnScaledWith(t, idnHotLargeRecords, size, true, idnHotRows)
	if counts.NewObjects == 0 || len(small) < 10 {
		t.Fatalf("%d statements, %+v", len(small), counts)
	}
	// the records that are new or changed all have the place in common, so they are one group (a tenth of the source) and a unit reads
	// the group: what it reads grows with that group, three times for three times the source, and is not more than a few times its pairs
	// (the known limit of ADR-0108)
	idnCheckReads(t, small, large, 8*idnHotLargeRecords/10, 3.5)
}

// [REQ-883]
// The one measurement: -idn.rows (default 30000) records, one in ten changed and one in a hundred new, with the five levels of context keys.
// It asserts nothing, so it runs only when -idn.rows is given (go test -args -idn.rows=30000).
func TestIdnMeasureTheIdentificationOfARunWithAChangeOfOneInTen(t *testing.T) {
	if !idnRowsGiven() {
		t.Skip("the measurement asserts nothing and runs only when -idn.rows is given (ADR-0108 measures it by hand)")
	}
	n := *idnRows
	_, took, counts := idnScaled(t, n, *idnSize, false)
	t.Logf("%d records, %d changed or new: %v, %+v", n, n/10+n/100, took, counts)
}

// idnClock tells where the time of the units went: the time from one statement to the next is the first one's.
type idnClock struct {
	spent map[string]time.Duration
	plans map[string]string
	last  string
	at    time.Time
}

func (c *idnClock) idnHook(tx *sql.Tx, what, query string, args []any) {
	if _, seen := c.plans[what]; !seen && (what == "copy the keys" || what == "make the slots") {
		var lines []string
		if rows, err := tx.Query("EXPLAIN (COSTS OFF) "+query, args...); err == nil {
			for rows.Next() {
				var line string
				_ = rows.Scan(&line)
				lines = append(lines, line)
			}
			_ = rows.Close()
		}
		c.plans[what] = strings.Join(lines, "\n")
	}
	now := idnNow.Now()
	if c.last != "" {
		c.spent[c.last] += now.Sub(c.at)
		if now.Sub(c.at) > 2*time.Second {
			fmt.Printf("slow: %s %v\n", c.last, now.Sub(c.at))
		}
	}
	c.last, c.at = what, now
}

func (c *idnClock) idnReport() string {
	var names []string
	for k := range c.spent {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return c.spent[names[i]] > c.spent[names[j]] })
	var b strings.Builder
	for _, k := range names {
		fmt.Fprintf(&b, "%s: %v\n", k, c.spent[k].Round(time.Millisecond))
	}
	for k, p := range c.plans {
		fmt.Fprintf(&b, "plan of %s:\n%s\n", k, p)
	}
	return b.String()
}
