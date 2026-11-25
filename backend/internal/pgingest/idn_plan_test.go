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

// idnPlaceRows is the rows of the places of n records: five sites' worth of buildings, floors and rooms, a rack and a tag for each.
func idnPlaceRows(n, from int, tag func(i int) string) string {
	var b strings.Builder
	b.WriteString(idnSite)
	for i := from; i < n; i++ {
		fmt.Fprintf(&b, "%d,S%d,B%d,F%d,R%d,K%d,%s\n", i, i/20000, i/2000, i/500, i/100, i, tag(i))
	}
	return b.String()
}

// idnScaled runs the identification of a source of n records, then of the same source with one record in ten changed (its tag, the
// alias of its rack) and one in a hundred new, and gives what the second run read and how long it took.
func idnScaled(t *testing.T, n, size int, explain bool) (map[string]*idnRead, time.Duration, ingest.IdentifyCounts) {
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
		identSeen = clock.idnHook
		defer func() {
			identSeen = nil
			t.Log(clock.idnReport())
		}()
	}
	l1, _ := run(idnPlaceRows(n, 0, func(i int) string { return "T" + strconv.Itoa(i) }), rdfT0)
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
		identSeen = plans.idnHook
		defer func() { identSeen = nil }()
	}
	tag := func(i int) string {
		if i%10 == 0 {
			return "U" + strconv.Itoa(i)
		}
		return "T" + strconv.Itoa(i)
	}
	start := idnNow.Now()
	_, counts := run(idnPlaceRows(n+n/100, 0, tag), rdfT0.Add(time.Hour))
	return plans.most, idnNow.Now().Sub(start), counts
}

// idnRegistry are the relations that grow with what the KG and the source have kept and not with the run: what a unit reads of them is
// what its own range asks for. The working tables of the run (calc_*) are the size of the run, and a plan that reads the whole of a small
// one for a unit is the plan the server rightly takes: the measurement below shows what a source of a million records costs.
func idnIsRegistry(table string) bool {
	for _, p := range []string{"object", "object_key", "object_identifier", "record_ref", "raw_record"} {
		if table == p || strings.HasPrefix(table, p+"_p") {
			return true
		}
	}
	return false
}

// [REQ-883]
func TestIdnEveryStatementOfAUnitReadsTheRegistryByItsIndexesAndWhatItReadsOfItDoesNotGrowWithTheSource(t *testing.T) {
	const size = 500
	small, _, _ := idnScaled(t, 10_000, size, true)
	large, _, counts := idnScaled(t, 30_000, size, true)
	if counts.NewObjects == 0 || len(small) < 10 {
		t.Fatalf("%d statements, %+v", len(small), counts)
	}
	var whats []string
	for what := range large {
		whats = append(whats, what)
	}
	sort.Strings(whats)
	for _, what := range whats {
		read := large[what]
		for table, n := range read.rows {
			if !idnIsRegistry(table) {
				continue
			}
			// a unit of 500 records reads, for each of them, the keys of five feeds and the parents of each (up to four): 40 rows a record
			if n > 40*size {
				t.Errorf("%s reads %v rows of %s for a unit of %d", what, n, table, size)
			}
			if before, ok := small[what]; ok && n > 1.5*before.rows[table]+50 {
				t.Errorf("%s reads %v rows of %s in a source of 30000 records and %v in one of 10000", what, n, table, before.rows[table])
			}
		}
		for _, scan := range read.scans {
			if table, ok := strings.CutPrefix(scan, "Seq Scan on "); ok && idnIsRegistry(table) && read.rows[table] > 40*size {
				t.Errorf("%s reads the whole of %s", what, table)
			}
		}
	}
	if testing.Verbose() {
		for _, what := range whats {
			t.Logf("%s: %v", what, large[what].rows)
		}
	}
}

// [REQ-883]
// The one measurement: IDN_ROWS (default 30000) records, one in ten changed and one in a hundred new, with the five levels of context keys.
func TestIdnMeasureTheIdentificationOfARunWithAChangeOfOneInTen(t *testing.T) {
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
