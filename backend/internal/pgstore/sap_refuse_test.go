package pgstore

import (
	"context"
	"errors"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// What the staged apply refuses: a KG that is not there, a generation or a lease that is not the caller's, a limit of
// no values, an ordering event that is not in the list, an apply that is not the one the head has half done, a difference
// that does not start at its first chunk or has lost a chunk that is done, and an abandon that was cut in the middle.
//
// [REQ-721]
func TestSapStagedApplyRefusesWhatIsNotTheCallers(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	lost := k.sapApply("sap-run")
	lost.KG = sapUnknownKG(t)
	start, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	_, err = k.st.StagedProgress(ctx, lost)
	wantCode(t, "the position of a KG that is not there", err, graph.CodeKGNotFound)
	_, err = k.st.ApplyStagedChunk(ctx, lost, start)
	wantCode(t, "a chunk of a KG that is not there", err, graph.CodeKGNotFound)
	_, err = k.st.ApplyStagedVolatile(ctx, graph.StagedVolatile{KG: lost.KG, Limit: 10})
	wantCode(t, "an interval of a KG that is not there", err, graph.CodeKGNotFound)
	_, err = k.st.ApplyStagedVolatile(ctx, graph.StagedVolatile{KG: k.kg.ID, Generation: k.kg.Generation, Lease: k.lease})
	wantCode(t, "an interval of no values", err, kernel.CodeInvalidValue)
	other := k.sapApply("sap-run")
	other.Generation += 5
	_, err = k.st.ApplyStagedChunk(ctx, other, start)
	wantCode(t, "a chunk of another generation", err, graph.CodeGenerationChanged)
	_, err = k.st.ApplyStagedVolatile(ctx, graph.StagedVolatile{KG: k.kg.ID, Generation: k.kg.Generation + 5, Lease: k.lease, Limit: 10})
	wantCode(t, "an interval of another generation", err, graph.CodeGenerationChanged)
	bogus := k.sapApply("sap-run")
	bogus.Event = graph.OrderKind("bogus")
	sapRunTo(t, k, "sap-run", start.ChunkCount-1)
	last, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	_, err = k.st.ApplyStagedChunk(ctx, bogus, last)
	wantCode(t, "the last chunk with an event that is not in the list", err, kernel.CodeInvalidValue)
	other = k.sapApply("sap-other")
	_, err = k.st.ApplyStagedChunk(ctx, other, last)
	wantCode(t, "a chunk of an apply that is not the one the head has half done", err, graph.CodeApplyInProgress)
}

// sapUnknownKG is the id of a KG that no catalog has.
func sapUnknownKG(t *testing.T) graph.KGID {
	t.Helper()
	id, err := kernel.ParseID[graph.KGKind]("kg-00000000000000ff")
	noErr(t, "id", err)
	return id
}

// sapRunTo applies chunks until n of them are done.
func sapRunTo(t *testing.T, k *sapKG, id string, n int) {
	t.Helper()
	ctx := context.Background()
	p, err := k.st.StagedProgress(ctx, k.sapApply(id))
	noErr(t, "progress", err)
	for p.NextChunk < n {
		p, err = k.st.ApplyStagedChunk(ctx, k.sapApply(id), p)
		noErr(t, "chunk", err)
	}
}

// A difference whose chunks do not start at 0, or that has lost a chunk that was applied, is refused with an error:
// the numbers are what tell the position.
//
// [REQ-721]
func TestSapDifferenceThatDoesNotStartAtItsFirstChunkIsRefused(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	sapExec(t, k, `DELETE FROM {ingest_diff} WHERE {chunk_no} = 0`)
	_, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	wantCode(t, "a difference without its first chunk", err, kernel.CodeInvalidValue)
	p.sapStageChunks(t, p.first)
	sapRunTo(t, k, "sap-run", 3)
	sapExec(t, k, `DELETE FROM {ingest_diff} WHERE {chunk_no} = 2`)
	_, err = k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	wantCode(t, "a difference that lost a chunk that is done", err, kernel.CodeInternal)
}

// An abandon that was cut in the middle leaves the head with its work: the staged apply neither tells the position
// nor writes a chunk until the abandon is ended.
//
// [REQ-725]
func TestSapAbandonCutInTheMiddleStopsTheStagedApplyUntilItIsEnded(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	sapRunTo(t, k, "sap-run", 3)
	k.st.StopWorkAfter(1, errors.New("the server died"))
	if err := k.app.Abandon(ctx, k.kg.ID, k.lease); err == nil {
		t.Fatal("the abandon that was cut did not fail")
	}
	_, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	wantCode(t, "the position while the abandon is not ended", err, graph.CodeApplyInProgress)
	_, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), graph.StagedProgress{State: graph.StagedOpen, NextChunk: 3, ChunkCount: 9})
	if err == nil {
		t.Fatal("a chunk while the abandon is not ended: no error")
	}
}

// A staged volatile value that cannot be written is refused with an error and nothing of its interval is written: an
// entry that is not a document of prov, an attribute that has no table of values, an object the KG does not have.
//
// [REQ-727]
func TestSapStagedVolatileValueThatCannotBeWrittenIsRefused(t *testing.T) {
	ctx := context.Background()
	first := `(SELECT {obj_no} FROM {stage_volatile} ORDER BY {obj_no}, {attr_no} LIMIT 1)`
	for name, change := range map[string]struct {
		statement string
		code      kernel.Code
	}{
		"an entry that is not a document": {`UPDATE {stage_volatile} SET {entry} = '"text"'::jsonb WHERE {obj_no} = ` + first, ""},
		"an attribute with no table":      {`UPDATE {stage_volatile} SET {attr_no} = 99999 WHERE {obj_no} = ` + first, kernel.CodeInvalidValue},
		"an object the KG does not have":  {`UPDATE {stage_volatile} SET {obj_no} = 987654321 WHERE {obj_no} = ` + first, graph.CodeHeadMoved},
	} {
		k := sapStaged(t).staged
		sapRun(t, k, "sap-run")
		before, err := k.st.Head(ctx, k.kg.ID)
		noErr(t, "head", err)
		sapExec(t, k, change.statement)
		_, err = sapInterval(t, k, graph.StagedVolatileCursor{}, 5000)
		if err == nil || (change.code != "" && kernel.CodeOf(err) != change.code) {
			t.Fatalf("%s: %v", name, err)
		}
		if h, err := k.st.Head(ctx, k.kg.ID); err != nil || h.AppliedSeq != before.AppliedSeq {
			t.Fatalf("%s moved the head: %+v %v", name, h, err)
		}
	}
}
