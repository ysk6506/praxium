package pgstore

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The mark of the objects held to retire for good, as the apply of a chunk keeps it (ADR-0128): the objects of the chunk that a retirable
// reference of the run points at are marked, and an object the chunk retires loses its mark in the same transaction, after it. Every name
// starts with stm.

// stmTable is the name of a table of the storage schema of the KG, quoted and qualified.
func stmTable(t *testing.T, k *sapKG, table string) string {
	t.Helper()
	a, err := k.st.access(context.Background(), k.kg.ID)
	noErr(t, "access", err)
	return a.t(table)
}

// stmMarks is the objects that are marked, as their numbers.
func stmMarks(t *testing.T, k *sapKG) string {
	t.Helper()
	ctx := context.Background()
	a, err := k.st.access(ctx, k.kg.ID)
	noErr(t, "access", err)
	var out string
	noErr(t, "read the marks", a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT COALESCE(string_agg(obj_no::text, ' ' ORDER BY obj_no), '') FROM "+a.t(pgschema.TableObjectRetire)).Scan(&out)
	}))
	return out
}

// [REQ-987]
func TestStmTheApplyMarksTheObjectsOfAChunkARetirableReferencePointsAtAndTakesTheMarkAwayWhenItRetiresThem(t *testing.T) {
	// the world the other tests of the staged apply share (the rows path is run once for all of them)
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	facts := sapFactsOf(k.reg, p.first)
	gone, links := facts.sapRetired()
	var row *graph.ObjectRow
	var other graph.ObjectID
	for _, o := range facts.objects {
		if o.New.ID == gone {
			row = o.New
		} else if other.IsZero() {
			other = o.New.ID
		}
	}
	no, otherNo := p.stg.objNo[gone.String()], p.stg.objNo[other.String()]
	ref := stmTable(t, k, pgschema.TableCalcRef)
	// the run says that a feed that retires made the object; another object is made by a feed that does not retire, and a third reference
	// that retires is not solved
	sapExec(t, k, "INSERT INTO "+ref+" (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) VALUES (1, 1, 0, 0, NULL, "+strconv.FormatInt(no, 10)+
		", 0, true), (2, 1, 0, 0, NULL, "+strconv.FormatInt(otherNo, 10)+", 0, false), (3, 1, 0, 0, NULL, "+strconv.FormatInt(otherNo, 10)+", 1, true)")
	sapRun(t, k, "sap-1")
	if got := stmMarks(t, k); got != strconv.FormatInt(no, 10) {
		t.Fatalf("the marks after the first apply: %q", got)
	}
	// the chunk that retires the object takes its mark away, though a reference of the run still says it is retirable
	retire := graph.ChunkWrite{Objects: []graph.ObjectChange{{Old: row}}}
	for _, l := range facts.links {
		if l.ID == links[0] {
			retire.Links = append(retire.Links, graph.LinkChange{Old: &l})
		}
	}
	noErr(t, "reset", p.stg.sapReset(ctx))
	noErr(t, "stage the retirement", p.stg.sapStage(ctx, retire))
	sapRun(t, k, "sap-2")
	if got := stmMarks(t, k); got != "" {
		t.Fatalf("the marks after the object was retired: %q", got)
	}
	// the object comes back by a feed that does not retire: no mark comes back with it, so no mark retires it again
	sapExec(t, k, "UPDATE "+ref+" SET retirable = false")
	back := *row
	noErr(t, "reset", p.stg.sapReset(ctx))
	noErr(t, "stage the object that comes back", p.stg.sapStage(ctx, graph.ChunkWrite{Objects: []graph.ObjectChange{{New: &back}}}))
	// the stager numbers an object that is added anew; the one that comes back keeps the number the registry has
	newNo := p.stg.objNo[gone.String()]
	sapExec(t, k, `UPDATE {stage_object} SET {obj_no} = `+strconv.FormatInt(no, 10)+` WHERE {obj_no} = `+strconv.FormatInt(newNo, 10))
	sapExec(t, k, `UPDATE {ingest_diff} SET {entity_no} = `+strconv.FormatInt(no, 10)+` WHERE {entity_no} = `+strconv.FormatInt(newNo, 10)+` AND {kind} = {KO}`)
	sapExec(t, k, `DELETE FROM {ingest_diff} WHERE {kind} = {KK}`)
	p.stg.objNo[gone.String()] = no
	sapRun(t, k, "sap-3")
	if got := stmMarks(t, k); got != "" {
		t.Errorf("the marks after the object came back: %q", got)
	}
	if n := sapCount(t, k, `SELECT count(*) FROM {object} WHERE retired_seq IS NULL AND obj_no = `+strconv.FormatInt(no, 10)); n != 1 {
		t.Errorf("the object did not come back: %d", n)
	}
	// a chunk that both marks an object and retires it leaves no mark (the mark goes first, the retirement after)
	sapExec(t, k, "UPDATE "+ref+" SET retirable = true WHERE rid = 1")
	noErr(t, "reset", p.stg.sapReset(ctx))
	noErr(t, "stage the retirement again", p.stg.sapStage(ctx, graph.ChunkWrite{Objects: []graph.ObjectChange{{Old: &back}}}))
	sapRun(t, k, "sap-4")
	if got := stmMarks(t, k); strings.Contains(" "+got+" ", " "+strconv.FormatInt(no, 10)+" ") {
		t.Errorf("the object retired in a chunk that marks it keeps the mark: %q", got)
	}
}
