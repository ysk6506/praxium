package pgstore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// sapDamaged is a staged pair whose store has a connection of its own to the database, to take a table away.
type sapDamaged struct {
	*sapPair
	admin *sql.DB
	a     *kgAccess
}

func sapOpenDamaged(t *testing.T, items int) *sapDamaged {
	t.Helper()
	var admin *sql.DB
	p := sapStagedInto(t, items, func(tb TB) (*Store, func()) {
		dsn := NewTestDatabase(tb)
		st, err := Open(context.Background(), Config{DSN: dsn})
		if err != nil {
			tb.Fatalf("open: %v", err)
		}
		tb.Cleanup(func() { _ = st.Close() })
		admin, err = sql.Open("postgres", dsn)
		if err != nil {
			tb.Fatalf("admin: %v", err)
		}
		tb.Cleanup(func() { _ = admin.Close() })
		return st, func() {}
	})
	a, err := p.staged.st.access(context.Background(), p.staged.kg.ID)
	noErr(t, "access", err)
	return &sapDamaged{sapPair: p, admin: admin, a: a}
}

// without runs fn while the table is away.
func (d *sapDamaged) without(t *testing.T, table string, fn func()) {
	t.Helper()
	v2rExec(t, d.admin, `ALTER TABLE `+d.a.t(table)+` RENAME TO `+pgschemaQuote(table+"_away"))
	defer v2rExec(t, d.admin, `ALTER TABLE `+d.a.t(table+"_away")+` RENAME TO `+pgschemaQuote(table))
	fn()
}

// A table that a statement of the staged apply needs is not there: the chunk fails with an error, leaves nothing
// and the head stays where it was; the same goes for the question about the position and for the intervals of
// volatile values. A store whose pool is closed fails all three the same way.
//
// [REQ-721]
func TestSapDamagedTablesFailTheStagedApplyWithAnErrorAndLeaveTheHeadAlone(t *testing.T) {
	d := sapOpenDamaged(t, 120)
	ctx := context.Background()
	k := d.staged
	d.sapStageChunks(t, sapMerge(d.first, len(d.first)))
	start, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	typeTable := pgschema.TypeTable(d.first[0].Objects[0].New.TypeNo)
	for _, table := range []string{pgschema.TableIngestDiff, pgschema.TableStageObject, pgschema.TableStageLink, pgschema.TableStageIdent,
		pgschema.TableObject, pgschema.TableLink, pgschema.TableObjectKey, pgschema.TableChangeEntry,
		pgschema.TableChangeBatch, pgschema.TableObjectIdentifier, pgschema.TableIngestOrder, typeTable} {
		d.without(t, table, func() {
			if _, err := k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), start); err == nil {
				t.Errorf("a chunk with %s missing: no error", table)
			}
		})
		if h, err := k.st.Head(ctx, k.kg.ID); err != nil || h.AppliedSeq != 0 || h.Open() {
			t.Fatalf("the head after the chunk with %s missing: %+v %v", table, h, err)
		}
	}
	d.without(t, pgschema.TableIngestDiff, func() {
		if _, err := k.st.StagedProgress(ctx, k.sapApply("sap-run")); err == nil {
			t.Error("the position with the difference missing: no error")
		}
	})
	sapRun(t, k, "sap-run")
	for _, table := range []string{pgschema.TableStageVolat, pgschema.TableObject, pgschema.TableObjectState, pgschema.TableLightChange, pgschema.TableChangeBatch} {
		d.without(t, table, func() {
			if _, err := sapInterval(t, k, graph.StagedVolatileCursor{}, 100); err == nil {
				t.Errorf("an interval with %s missing: no error", table)
			}
		})
	}
	noErr(t, "close", k.st.Close())
	if _, err := k.st.StagedProgress(ctx, k.sapApply("sap-run")); err == nil {
		t.Error("the position on a closed pool: no error")
	}
	if _, err := k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), start); err == nil {
		t.Error("a chunk on a closed pool: no error")
	}
	if _, err := sapInterval(t, k, graph.StagedVolatileCursor{}, 100); err == nil {
		t.Error("an interval on a closed pool: no error")
	}
}

// The chain of the chunks that are done is made from the stored difference when the apply is half done; a difference
// that is not there, or has a hole in its chunks, is an error and not a wrong chain.
//
// [REQ-721]
func TestSapDamagedDifferenceFailsTheChainOfAnOpenApply(t *testing.T) {
	d := sapOpenDamaged(t, 120)
	ctx := context.Background()
	k := d.staged
	prog, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	for i := 0; i < 2; i++ {
		prog, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), prog)
		noErr(t, "chunk", err)
	}
	d.without(t, pgschema.TableIngestDiff, func() {
		if _, err := k.st.StagedProgress(ctx, k.sapApply("sap-run")); err == nil {
			t.Error("the chain with the difference missing: no error")
		}
	})
	sapExec(t, k, `DELETE FROM {ingest_diff} WHERE {chunk_no} = 0`)
	if _, err := k.st.StagedProgress(ctx, k.sapApply("sap-run")); err == nil {
		t.Error("the chain of a difference that has lost its first chunk: no error")
	}
}
