package pgingest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The cases of the difference (ADR-0127), on worlds made in SQL: every source and every change, nothing that changes, a type of more rows
// than a chunk, an object whose type changes, what the isolation took out.

// dfiAllWant is the difference of dfiAll with chunks of two rows: the chunk, the kind, the type, the id and the change. The objects of the
// racks, then the servers, then the virtual machine; the keys; the links; the identifiers. The keys and the identifiers of one object are
// in the order of their hash, which the words of the fixture put as they are here.
func dfiAllWant(m isoModel) []string {
	r, s, v, l := m.rack, m.server, m.vm, m.mounted
	rows := []string{
		fmt.Sprintf("0 %d r-changed 1", r), fmt.Sprintf("0 %d r-new 0", r), fmt.Sprintf("0 %d r-retiring 2", r),
		fmt.Sprintf("0 %d s-changed 1", s), fmt.Sprintf("0 %d s-new 0", s), fmt.Sprintf("0 %d s-new2 0", s), fmt.Sprintf("0 %d s-retiring 2", s), fmt.Sprintf("0 %d s-revived 0", s),
		fmt.Sprintf("0 %d v-new 0", v),
		fmt.Sprintf("1 %d r-new 0", r), fmt.Sprintf("1 %d s-kg 0", s), fmt.Sprintf("1 %d s-new 0", s), fmt.Sprintf("1 %d s-new 0", s),
		fmt.Sprintf("2 %d l-changed 1", l), fmt.Sprintf("2 %d l-iso 2", l), fmt.Sprintf("2 %d l-new 0", l), fmt.Sprintf("2 %d l-retiring 2", l),
		fmt.Sprintf("2 %d l-retiring2 2", l), fmt.Sprintf("2 %d l-run 2", l),
		fmt.Sprintf("3 %d s-changed", s), fmt.Sprintf("3 %d s-changed", s), fmt.Sprintf("3 %d s-new 0", s), fmt.Sprintf("3 %d s-new 0", s),
	}
	// the chunks: two rows at most, and a chunk ends where the kind or the type does
	out := make([]string, len(rows))
	chunk, in, prev := -1, 0, ""
	for i, row := range rows {
		parts := strings.Fields(row)
		group := parts[0] + " " + parts[1]
		if group != prev || in == 2 {
			chunk, in, prev = chunk+1, 0, group
		}
		in++
		out[i] = fmt.Sprintf("%d %s", chunk, row)
	}
	return out
}

// [REQ-960] [REQ-961] [REQ-963] [REQ-964]
func TestDfiEveryChangeOfEverySourceIsARowAndWhatIsTheSameIsNone(t *testing.T) {
	f := dfiAll(t, false)
	counts := dfiDo(f.s, t, f.l, 2, 2)
	dfiShape(f.s, t, counts, 2)
	got := dfiAllRows(f, t)
	// the two identifiers of s-changed are one added and one retired, in the order of their hash: the change is looked at below
	for i, g := range got {
		if strings.Contains(g, " 3 ") && strings.HasSuffix(g[:len(g)-2], "s-changed") {
			got[i] = g[:len(g)-2]
		}
	}
	if want := dfiAllWant(f.m); !slices.Equal(got, want) {
		t.Errorf("the difference is\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if changes := f.s.e.text(t, f.dfiQ(`SELECT string_agg(change_kind::text, ',' ORDER BY change_kind) FROM {k}.ingest_diff WHERE kind = 3 AND ref = 's-changed'`)); changes != "0,2" {
		t.Errorf("the identifiers of s-changed are the changes %s", changes)
	}
	// a key row names the key: the context and the hash of the shape
	if n := f.s.e.count(t, f.dfiQ(`SELECT count(*) FROM {k}.ingest_diff d JOIN {k}.calc_newkey n ON n.type_no = d.type_no AND n.ctx_no = d.ctx_no AND n.sig_hash = d.sig_hash
		AND n.obj_no = d.entity_no WHERE d.kind = 1`)); n != 4 {
		t.Errorf("%d key rows name the key they bind", n)
	}
	// the staged apply takes the difference: the retired objects and the links at them closed, the links and the identifiers as the run
	// staged them, the keys bound
	k := dfiApplierOf(f.s, t)
	if p := dfiApply(f.s, t, k, f.l); p.State != graph.StagedDone || p.Chain != counts.Fingerprint || p.ChunkCount != counts.Chunks {
		t.Fatalf("the apply ended at %+v, the difference has %+v", p, counts)
	}
	for _, c := range []struct{ query, want string }{
		{`SELECT string_agg(ref, ' ' ORDER BY ref) FROM {k}.object WHERE retired_seq IS NOT NULL`, "r-retiring s-gone s-retiring"},
		{`SELECT string_agg(ref, ' ' ORDER BY ref) FROM {k}.link`, "l-bare l-changed l-kept l-new l-same"},
		{`SELECT string_agg(o.ref || '=' || i.value, ' ' ORDER BY o.ref || '=' || i.value COLLATE "C") FROM {k}.object_identifier i JOIN {k}.object o ON o.obj_no = i.obj_no WHERE i.seq_to IS NULL`,
			"s-changed=sn-changed-new s-kg=sn-kg s-new=sn-new s-new=sn-new-b s-retiring=sn-retiring s-same=sn-same"},
		{`SELECT count(*)::text FROM {k}.object_key`, "5"},
	} {
		if got := f.s.e.text(t, f.dfiQ(c.query)); got != c.want {
			t.Errorf("%s: %s, want %s", c.query, got, c.want)
		}
	}
}

// [REQ-964] [REQ-967]
func TestDfiARunThatChangesNothingHasNoRowNoChunkTheFingerprintOfNothingAndAnEmptyApply(t *testing.T) {
	f := dfiNewFix(t, false)
	f.dfiObj(t, "s-same", f.m.server, "same")
	f.dfiObj(t, "r-kg", f.m.rack, "kg")
	f.dfiObj(t, "s-left", f.m.server, "left")
	f.dfiObj(t, "s-gone", f.m.server, "gone")
	f.dfiLink(t, "l-same", f.m.mounted, "s-same", "r-kg", "same")
	f.dfiLink(t, "l-kept", f.m.mounted, "s-left", "r-kg", "kept")
	f.dfiIdent(t, "s-same", "sn", true, true)
	f.dfiKey(t, "s-same", "key", f.m.server, true)
	counts := dfiDo(f.s, t, f.l, 1, 1)
	if counts.Rows != 0 || counts.Chunks != 0 || counts.Fingerprint != (kernel.Hash{}) || len(counts.Tally) != 0 {
		t.Fatalf("a run that changes nothing: %+v", counts)
	}
	dfiShape(f.s, t, counts, 1)
	if n := f.s.e.count(t, f.dfiQ(`SELECT (SELECT count(*) FROM {k}.ingest_diff) + (SELECT count(*) FROM {k}.calc_diffplan) + (SELECT count(*) FROM {k}.calc_diffrow)`)); n != 0 {
		t.Errorf("%d rows", n)
	}
	if p := dfiApply(f.s, t, dfiApplierOf(f.s, t), f.l); p.State != graph.StagedEmpty {
		t.Errorf("the apply of nothing is %+v", p)
	}
	if print := f.s.e.text(t, `SELECT encode(change_fingerprint, 'hex') FROM sync_run WHERE run_no = $1`, f.l.No); print != strings.Repeat("0", 64) {
		t.Errorf("the record of the run keeps the fingerprint %q", print)
	}
	place, err := f.s.e.st.DiffPlace(f.s.e.ctx, f.l)
	if err != nil || !place.Done() || place.Phase != ingest.DiffPhaseDone {
		t.Errorf("%+v %v", place, err)
	}
}

// [REQ-964]
func TestDfiATypeOfMoreRowsThanAChunkIsCutInTheOrderOfTheIdsAndAChunkHasOneKindAndOneType(t *testing.T) {
	f := dfiNewFix(t, true)
	for i := 0; i < 7; i++ {
		f.dfiObj(t, fmt.Sprintf("s-%02d", (i*5)%7), f.m.server, "new")
	}
	f.dfiObj(t, "r-a", f.m.rack, "new")
	counts := dfiDo(f.s, t, f.l, 2, 3)
	dfiShape(f.s, t, counts, 3)
	got := strings.Join(dfiAllRows(f, t), ",")
	want := fmt.Sprintf("0 0 %[1]d r-a 0,1 0 %[2]d s-00 0,1 0 %[2]d s-01 0,1 0 %[2]d s-02 0,2 0 %[2]d s-03 0,2 0 %[2]d s-04 0,2 0 %[2]d s-05 0,3 0 %[2]d s-06 0", f.m.rack, f.m.server)
	if got != want {
		t.Errorf("the chunks are\n%s\nwant\n%s", got, want)
	}
	if counts.Chunks != 4 || counts.Of(ingest.DiffObject, ingest.DiffAdded) != 8 {
		t.Errorf("%+v", counts)
	}
}

// [REQ-960] [REQ-961] [REQ-964]
func TestDfiAnObjectWhoseTypeChangesIsRetiredInTheChunkOfItsOldTypeAndAddedInTheChunkOfItsNew(t *testing.T) {
	// the same natural key under another type is another id: the object of the old type has nothing under it and retires, with its link
	f := dfiNewFix(t, false)
	f.dfiObj(t, "x-as-host", f.m.host, "retiring")
	f.dfiObj(t, "x-as-server", f.m.server, "new")
	f.dfiObj(t, "r-kg", f.m.rack, "kg")
	f.dfiLink(t, "l-old", f.m.mounted, "x-as-host", "r-kg", "kept")
	f.dfiLink(t, "l-new", f.m.mounted, "x-as-server", "r-kg", "new")
	counts := dfiDo(f.s, t, f.l, 0, 0)
	dfiShape(f.s, t, counts, 0)
	// the objects are in the order of the numbers of their types, each type a chunk of its own
	objects := []string{fmt.Sprintf("0 %d x-as-host 2", f.m.host), fmt.Sprintf("0 %d x-as-server 0", f.m.server)}
	if f.m.server < f.m.host {
		objects[0], objects[1] = objects[1], objects[0]
	}
	want := []string{"0 " + objects[0], "1 " + objects[1], fmt.Sprintf("2 2 %d l-new 0", f.m.mounted), fmt.Sprintf("2 2 %d l-old 2", f.m.mounted)}
	if got := dfiAllRows(f, t); !slices.Equal(got, want) {
		t.Errorf("the difference is %v, want %v", got, want)
	}
	if p := dfiApply(f.s, t, dfiApplierOf(f.s, t), f.l); p.State != graph.StagedDone {
		t.Fatalf("%+v", p)
	}
	if got := f.s.e.text(t, f.dfiQ(`SELECT string_agg(o.ref || '>' || l.ref, ' ' ORDER BY l.ref) FROM {k}.link l JOIN {k}.object o ON o.obj_no = l.src WHERE o.retired_seq IS NULL`)); got != "x-as-server>l-new" {
		t.Errorf("the links of the KG are %q", got)
	}
}

// [REQ-960]
func TestDfiWhatTheIsolationTookOutHasNoRowAndANewObjectTakenBackHasNone(t *testing.T) {
	f := dfiNewFix(t, false)
	f.dfiObj(t, "s-new", f.m.server, "new")
	f.dfiObj(t, "r-kg", f.m.rack, "kg")
	// a new link the isolation took out is not staged and not in the KG; a link of the KG it took out closes
	f.dfiNext("l-taken", true)
	f.s.e.exec(t, f.dfiQ(`INSERT INTO {k}.calc_isolated (kind, entity_no, ref, phase, why, reason) VALUES (2, $1, 'l-taken', 3, 5, 'taken out')`), f.no["l-taken"])
	f.dfiLink(t, "l-iso", f.m.mounted, "s-new", "r-kg", "iso")
	// an identifier line the isolation took out is not staged: the object is made without it
	f.s.e.exec(t, f.dfiQ(`INSERT INTO {k}.calc_isolated (kind, entity_no, scheme, value, scope, phase, why, reason) VALUES (3, $1, $2, 'sn-lost', '', 1, 1, 'kept by another')`), f.no["s-new"], isoScheme)
	// an object the run would have made and took back has nothing staged and no key of the run
	f.dfiNext("s-back", false)
	f.s.e.exec(t, f.dfiQ(`INSERT INTO {k}.calc_left (obj_no, why, lines, is_new, reason) VALUES ($1, 2, 1, true, 'unreadable')`), f.no["s-back"])
	counts := dfiDo(f.s, t, f.l, 0, 0)
	dfiShape(f.s, t, counts, 0)
	want := []string{fmt.Sprintf("0 0 %d s-new 0", f.m.server), fmt.Sprintf("1 2 %d l-iso 2", f.m.mounted)}
	if got := dfiAllRows(f, t); !slices.Equal(got, want) {
		t.Errorf("the difference is %v, want %v", got, want)
	}
}
