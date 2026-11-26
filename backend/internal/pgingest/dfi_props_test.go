package pgingest

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The properties of the new flow up to the staged apply (ADR-0127, ingestion-at-scale.md 3): I1 (the KG and the ids of the objects hang
// neither on the order of the rows, nor on the pages, nor on the order of the datasets) and I2 (after any changes, the computation of
// everything finds nothing to change, and the problems it finds are the ones the runs that computed what they touched found).

// dfiSourceReversed is the first source with its datasets in the other order.
const dfiSourceReversed = `{"id":"src_rcl_a","name":"rcl a","adapter":"file","datasets":[
 {"id":"ds_vm","parser":"csv","natural_key":["uuid"],"columns":[{"name":"uuid"}]},
 {"id":"ds_sv","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"mfr"},{"name":"serial"},{"name":"rack_tag"},{"name":"pos"},{"name":"flag"}]},
 {"id":"ds_rk","parser":"csv","natural_key":["rack_id"],"columns":[{"name":"rack_id"},{"name":"tag"}]}]}`

// dfiReversed is the texts with the rows of each dataset in the other order (the header first).
func dfiReversed(texts pgiTexts) pgiTexts {
	out := pgiTexts{}
	for name, text := range texts {
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		rows := slices.Clone(lines[1:])
		slices.Reverse(rows)
		out[name] = lines[0] + "\n" + idnJoin(rows...)
	}
	return out
}

// dfiI1Steps are the captures the cases of I1 give one after another: a first run, and a run that moves a server, changes a position, adds a
// server whose serial another record has (the two are one object, which two racks are said for) and a virtual machine.
func dfiI1Steps() []pgiTexts {
	return []pgiTexts{
		rclTextsA(dfiRacks, dfiServers, []string{"u1", "u2"}),
		rclTextsA(dfiRacks, []string{"SV1,Zorvane,sn1,RK1,5,on", "SV2,Zorvane,sn2,RK2,2,on", "SV3,Zorvane,sn3,RK3,3,on", "SV3b,Zorvane,sn3,RK1,4,on", "SV4,Zorvane,sn4,RK1,6,on"},
			[]string{"u1", "u2", "u3"}),
	}
}

// dfiI1Run gives the steps to a KG of the source given, with the texts made by the change given, the rows of a page and of a unit of the
// difference, and gives for each step the difference, its counts, and the state hash and the text of the KG after the apply.
func dfiI1Run(t *testing.T, source string, change func(pgiTexts) pgiTexts, pageRows, rows int) []string {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, source, rclMappingA))
	k := dfiApplierOf(s, t)
	var out []string
	for n, texts := range dfiI1Steps() {
		counts := dfiStepWith(s, t, k, change(texts), rdfT0.Add(time.Duration(n)*time.Hour), false, pageRows, rows, 3)
		h := dfiHead(s, t, k)
		out = append(out, fmt.Sprintf("step %d: %d rows %d chunks %v\n%s\n%x %d\n%s", n, counts.Rows, counts.Chunks, counts.Tally,
			strings.Join(dfiRows(s, t), "\n"), h.Sum, h.Rows, dfiKGText(s, t)))
	}
	return out
}

// [REQ-965] [REQ-970]
func TestDfiTheDifferenceAndTheKGHangNeitherOnTheOrderOfTheRowsNorOnThePagesNorOnTheUnits(t *testing.T) {
	same := func(x pgiTexts) pgiTexts { return x }
	want := dfiI1Run(t, rclSourceA, same, 2, 0)
	got := dfiI1Run(t, rclSourceA, dfiReversed, 1, 1)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("the rows in the other order, pages of one row and units of one:\n%s\nwant\n%s", got[i], want[i])
		}
	}
}

// [REQ-965] [REQ-970]
func TestDfiTheDifferenceAndTheKGHangNotOnTheOrderOfTheDatasets(t *testing.T) {
	same := func(x pgiTexts) pgiTexts { return x }
	want := dfiI1Run(t, rclSourceA, same, 2, 2)
	got := dfiI1Run(t, dfiSourceReversed, same, 2, 2)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("the datasets in the other order:\n%s\nwant\n%s", got[i], want[i])
		}
	}
}

// dfiOwnedKinds are the kinds of problem the fourth and the fifth step give every record they read (the ones the computation of everything
// gives again for every record).
func dfiOwnedKinds() []string {
	return append(pgschema.StepFourProblemKinds(), pgschema.StepFiveProblemKinds()...)
}

// dfiProblemsOf is the problems of the kinds given of the records of the run, by the record (dataset/key), and the records of the run.
func dfiProblemsOf(s *rdfSetup, t *testing.T, kinds []string) (map[string][]string, []string) {
	t.Helper()
	k := s.ref.KGNo
	records := expList(s.e, t, fmt.Sprintf(`SELECT d.dataset_id || '/' || c.key FROM "s_%d".calc_record c JOIN source_dataset d ON d.dataset_no = c.dataset_no`, k))
	out := map[string][]string{}
	for _, l := range expList(s.e, t, fmt.Sprintf(`SELECT d.dataset_id || '/' || c.key || ' ' || p.kind || ' ' || COALESCE(p.def_no, -1) || ' ' || p.field || ' ' || p.reason
		FROM "s_%[1]d".calc_problem p JOIN "s_%[1]d".calc_record c ON c.rid = p.rid JOIN source_dataset d ON d.dataset_no = c.dataset_no
		WHERE p.kind IN ('%[2]s') ORDER BY 1`, k, strings.Join(kinds, "', '"))) {
		record, problem, _ := strings.Cut(l, " ")
		out[record] = append(out[record], problem)
	}
	return out, records
}

// [REQ-970]
func TestDfiAfterRandomChangesTheComputationOfEverythingFindsNothingToChangeAndTheSameProblems(t *testing.T) {
	rng := kernel.NewSeededRng(27)
	pick := func(n int) int { return int(rng.Uint64() % uint64(n)) }
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	k := dfiApplierOf(s, t)
	// the servers (asset, serial, rack, position): a record may say the serial of another, so that two records stand for one server and
	// say two racks
	type server struct{ asset, serial, rack string }
	servers := []server{{"SV1", "sn1", "RK1"}, {"SV2", "sn2", "RK2"}}
	vms := []string{"u1"}
	held := map[string][]string{}
	changed := 0
	var at time.Time
	for n := 0; n < 4; n++ {
		at = rdfT0.Add(time.Duration(n) * time.Hour)
		for c := 0; c < 2; c++ {
			switch pick(4) {
			case 0:
				servers = append(servers, server{fmt.Sprintf("SV%d", len(servers)+1), fmt.Sprintf("sn%d", len(servers)+1), fmt.Sprintf("RK%d", 1+pick(3))})
			case 1:
				servers = append(servers, server{fmt.Sprintf("SV%db", len(servers)+1), servers[pick(len(servers))].serial, fmt.Sprintf("RK%d", 1+pick(3))})
			case 2:
				servers[pick(len(servers))].rack = fmt.Sprintf("RK%d", 1+pick(4))
			default:
				vms = append(vms, fmt.Sprintf("u%d", len(vms)+1))
			}
		}
		var rows []string
		for i, sv := range servers {
			rows = append(rows, fmt.Sprintf("%s,Zorvane,%s,%s,%d,on", sv.asset, sv.serial, sv.rack, i))
		}
		counts := dfiStep(s, t, k, rclTextsA(dfiRacks, rows, vms), at, false)
		changed += counts.Rows
		// the problems the run found for the records it read take the place of the ones they had
		problems, records := dfiProblemsOf(s, t, dfiOwnedKinds())
		for _, r := range records {
			held[r] = problems[r]
		}
	}
	// the computation of everything, on the same captures
	var rows []string
	for i, sv := range servers {
		rows = append(rows, fmt.Sprintf("%s,Zorvane,%s,%s,%d,on", sv.asset, sv.serial, sv.rack, i))
	}
	l, counts := dfiRunTo(s, t, rclTextsA(dfiRacks, rows, vms), at.Add(time.Hour), true, 0, 0)
	if counts.Rows != 0 {
		t.Errorf("the computation of everything finds %d rows to change: %v\n%s", counts.Rows, counts.Tally, strings.Join(dfiRows(s, t), "\n"))
	}
	all, records := dfiProblemsOf(s, t, dfiOwnedKinds())
	var differ []string
	for _, r := range records {
		a, b := all[r], held[r]
		sort.Strings(a)
		sort.Strings(b)
		if !slices.Equal(a, b) {
			differ = append(differ, fmt.Sprintf("%s: the computation of everything %v, the runs %v", r, a, b))
		}
	}
	if len(differ) > 0 {
		t.Errorf("the problems differ:\n%s", strings.Join(differ, "\n"))
	}
	nProblems := 0
	for _, p := range all {
		nProblems += len(p)
	}
	if changed < 20 || nProblems == 0 || len(records) < len(servers)+len(vms) {
		t.Errorf("the case changed %d rows, found %d problems in %d records", changed, nProblems, len(records))
	}
	dfiApply(s, t, k, l)
}
