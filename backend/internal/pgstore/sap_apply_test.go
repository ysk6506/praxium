package pgstore

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// The tests of the way a staged apply goes: from chunk to chunk, after a crash, when it is empty, when an object
// retires, when it is abandoned. Every name starts with sap.

// sapCount runs a query that gives one number in the schema of a KG.
func sapCount(t *testing.T, k *sapKG, query string) int {
	t.Helper()
	ctx := context.Background()
	a, err := k.st.access(ctx, k.kg.ID)
	noErr(t, "access", err)
	var n int
	noErr(t, query, a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error { return tx.QueryRowContext(ctx, a.stagedSQL(query)).Scan(&n) }))
	return n
}

// sapExec runs a statement in the schema of a KG.
func sapExec(t *testing.T, k *sapKG, statement string) {
	t.Helper()
	ctx := context.Background()
	a, err := k.st.access(ctx, k.kg.ID)
	noErr(t, "access", err)
	noErr(t, statement, a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, a.stagedSQL(statement))
		return err
	}))
}

// sapStaged is a pair with the world staged on the one side and not applied.
func sapStaged(t *testing.T) *sapPair {
	t.Helper()
	return sapStagedInto(t, 120, func(tb TB) (*Store, func()) { return NewTestStore(tb), func() {} })
}

// A server that dies between two chunks leaves the head open at the chunk it was at; the one that takes the work over
// asks where the apply stands, makes the chain of the chunks before again from the difference, goes on with the next
// chunk and ends in the state the rows path has.
//
// [REQ-721]
func TestSapStagedApplyGoesOnAfterACrashBetweenChunks(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	start, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	if start.State != graph.StagedPending || start.ChunkCount < 4 {
		t.Fatalf("before the first chunk: %+v", start)
	}
	mid := start
	for i := 0; i < 2; i++ {
		mid, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), mid)
		noErr(t, "chunk", err)
	}
	if mid.State != graph.StagedOpen || mid.NextChunk != 2 || mid.Chain == (kernel.Hash{}) {
		t.Fatalf("after two chunks: %+v", mid)
	}
	again, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress after the crash", err)
	if again != mid {
		t.Fatalf("the progress after the crash is not the one the chunk gave:\n%+v\n%+v", again, mid)
	}
	_, err = sapInterval(t, k, graph.StagedVolatileCursor{}, 100)
	wantCode(t, "volatile values while the apply is half done", err, graph.CodeApplyInProgress)
	end, err := k.st.StagedProgress(ctx, k.sapApply("sap-other"))
	if kernel.CodeOf(err) != graph.CodeApplyInProgress {
		t.Fatalf("another apply on an open head: %+v %v", end, err)
	}
	for again.State == graph.StagedOpen {
		again, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), again)
		noErr(t, "chunk", err)
	}
	if again.State != graph.StagedDone {
		t.Fatalf("the end: %+v", again)
	}
	sapVolatile(t, k, "sap-run", 1000)
	sapSameState(t, p, "after the crash")
	done, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	if err != nil || done.State != graph.StagedDone {
		t.Fatalf("an apply that is done is told so: %+v %v", done, err)
	}
	_, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), mid)
	wantCode(t, "a chunk of an apply that is done", err, graph.CodeHeadMoved)
}

// A chunk is applied against the position the caller saw: a caller that is behind is refused and loses nothing.
//
// [REQ-721]
func TestSapStagedChunkOfAStalePositionIsRefused(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	start, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	next, err := k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), start)
	noErr(t, "chunk", err)
	_, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), start)
	wantCode(t, "the chunk applied twice", err, graph.CodeHeadMoved)
	h, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	if h.AppliedSeq != next.AppliedSeq || h.NextChunkNo != 1 {
		t.Fatalf("the refused chunk moved the head: %+v", h)
	}
	_, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), graph.StagedProgress{State: graph.StagedDone})
	wantCode(t, "a chunk from a done position", err, kernel.CodeInvalidValue)
}

// A difference that has no row is not applied: the head and the chain of the KG are left as they are, and so is a KG
// that has an apply behind it.
//
// [REQ-722]
func TestSapEmptyDifferenceLeavesTheHeadAlone(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	noErr(t, "reset", p.stg.sapReset(ctx))
	empty, err := k.st.StagedProgress(ctx, k.sapApply("sap-empty"))
	noErr(t, "progress", err)
	if empty.State != graph.StagedEmpty || empty.ChunkCount != 0 {
		t.Fatalf("an empty difference: %+v", empty)
	}
	before, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	_, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-empty"), graph.StagedProgress{State: graph.StagedPending})
	wantCode(t, "a chunk of an empty difference", err, kernel.CodeInvalidValue)
	after, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	if before.AppliedSeq != 0 || after.AppliedSeq != 0 || after.ApplyID != "" || after.LastApplyID != "" || !after.Hash.Equal(before.Hash) {
		t.Fatalf("the head moved: %+v then %+v", before, after)
	}
	if n := sapCount(t, k, `SELECT count(*) FROM {change_batch}`) + sapCount(t, k, `SELECT count(*) FROM {ingest_order}`); n != 0 {
		t.Fatalf("an empty apply wrote %d records", n)
	}
	p.sapStageChunks(t, p.first)
	sapRun(t, k, "sap-first")
	noErr(t, "reset", p.stg.sapReset(ctx))
	done, err := k.st.StagedProgress(ctx, k.sapApply("sap-second"))
	noErr(t, "progress", err)
	sealed, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	if done.State != graph.StagedEmpty || sealed.LastApplyID != "sap-first" || sealed.Open() {
		t.Fatalf("an empty difference after an apply: %+v, head %+v", done, sealed)
	}
}

// An unknown outcome of a COMMIT is decided by the head: the chunk the head shows is a chunk that was written, and any
// other head is an error that the caller meets with a new question about the position.
//
// [REQ-723]
func TestSapAChunkWhoseCommitGotNoAnswerIsJudgedByTheHead(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	a, err := k.st.access(ctx, k.kg.ID)
	noErr(t, "access", err)
	start, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	var want graph.Head
	var out graph.StagedProgress
	noErr(t, "chunk", a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		var err error
		out, want, err = k.st.stagedChunkIn(ctx, tx, a, k.sapApply("sap-run"), start)
		return err
	}))
	lost := kernel.Errorf(pgconn.CodeOutcomeUnknown, "no answer to the COMMIT")
	if err := k.st.resolve(ctx, a, want, lost); err != nil {
		t.Fatalf("a commit that happened: %v", err)
	}
	if out.NextChunk != 1 || want.NextChunkNo != 1 || want.ApplyID != "sap-run" {
		t.Fatalf("the head the chunk leaves: %+v, progress %+v", want, out)
	}
	moved := want
	moved.AppliedSeq++
	err = k.st.resolve(ctx, a, moved, lost)
	if kernel.CodeOf(err) != pgconn.CodeConnectionLost {
		t.Fatalf("a head that is not the one the chunk leaves: %v", err)
	}
	now, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	if now.NextChunk != 1 || now.State != graph.StagedOpen {
		t.Fatalf("the question about the position after the doubt: %+v", now)
	}
}

// The chunk whose COMMIT never happened (the backend is ended before it) leaves the head where it was, and the
// same chunk is applied again.
//
// [REQ-723]
func TestSapAChunkThatWasNotCommittedIsApplyAgain(t *testing.T) {
	p := sapStagedInto(t, 120, v2oStore)
	ctx := context.Background()
	k := p.staged
	start, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	p.arm()
	_, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), start)
	wantCode(t, "a commit that never happened", err, pgconn.CodeConnectionLost)
	h, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	if h.AppliedSeq != 0 || h.Open() {
		t.Fatalf("the head after the lost commit: %+v", h)
	}
	if n := sapCount(t, k, `SELECT count(*) FROM {object}`); n != 0 {
		t.Fatalf("%d objects were left by the chunk that was not committed", n)
	}
	again, err := k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), start)
	noErr(t, "the same chunk again", err)
	if again.NextChunk != 1 || again.State != graph.StagedOpen {
		t.Fatalf("after the repeat: %+v", again)
	}
}

// The links of an object the chunk retires are closed with it, whoever made them and whether the difference names them
// or not; a line of the difference that retires a link that went with its object is not a mismatch. The state hash
// of the rows that are left is the one the head keeps.
//
// [REQ-724]
func TestSapRetiredObjectTakesItsLinksWithIt(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	sapRun(t, p.staged, "sap-1")
	facts := sapFactsOf(p.staged.reg, p.first)
	gone, links := facts.sapRetired()
	if len(links) < 2 {
		t.Fatalf("the object has %d links", len(links))
	}
	var row *graph.ObjectRow
	for _, o := range facts.objects {
		if o.New.ID == gone {
			row = o.New
		}
	}
	retire := graph.ChunkWrite{Objects: []graph.ObjectChange{{Old: row}}}
	later := graph.ChunkWrite{}
	for _, l := range facts.links {
		if l.ID == links[0] {
			later.Links = append(later.Links, graph.LinkChange{Old: &l})
		}
	}
	noErr(t, "reset", p.stg.sapReset(ctx))
	noErr(t, "stage the retirement", p.stg.sapStage(ctx, retire))
	noErr(t, "stage the link line that comes after it", p.stg.sapStage(ctx, later))
	before := sapCount(t, p.staged, `SELECT count(*) FROM {link}`)
	sapRun(t, p.staged, "sap-2")
	no := p.stg.objNo[gone.String()]
	open := sapCount(t, p.staged, `SELECT count(*) FROM {link} WHERE src = `+strconv.FormatInt(no, 10)+` OR dst = `+strconv.FormatInt(no, 10))
	closed := sapCount(t, p.staged, `SELECT count(*) FROM {link_hist} WHERE src = `+strconv.FormatInt(no, 10)+` OR dst = `+strconv.FormatInt(no, 10))
	if open != 0 || closed != len(links) || sapCount(t, p.staged, `SELECT count(*) FROM {link}`) != before-len(links) {
		t.Fatalf("open links of the object %d, closed %d of %d", open, closed, len(links))
	}
	if n := sapCount(t, p.staged, `SELECT count(*) FROM {change_entry} WHERE entity_kind = {EL} AND change_kind = {CR}`); n != len(links) {
		t.Fatalf("%d entries retire a link, wanted %d", n, len(links))
	}
	noErr(t, "verify", p.staged.app.Verify(ctx, p.staged.kg.ID))
}

// An apply that is abandoned between two chunks goes back by the change numbers, the identification index with it
// (the key lines of the chunks that were applied), and the same apply can be made again from the first chunk.
//
// [REQ-725]
func TestSapAbandonBetweenChunksUndoesKeysAndTheApplyCanStartAgain(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	prog, err := k.st.StagedProgress(ctx, k.sapApply("sap-run"))
	noErr(t, "progress", err)
	for i := 0; i < 3; i++ {
		prog, err = k.st.ApplyStagedChunk(ctx, k.sapApply("sap-run"), prog)
		noErr(t, "chunk", err)
	}
	if sapCount(t, k, `SELECT count(*) FROM {object_key}`) == 0 || sapCount(t, k, `SELECT count(*) FROM {object}`) == 0 {
		t.Fatal("the chunks wrote no object or key")
	}
	noErr(t, "abandon", k.app.Abandon(ctx, k.kg.ID, k.lease))
	for _, table := range []string{"object_key", "link", "object_identifier", "change_entry", "change_batch", "ingest_order"} {
		if n := sapCount(t, k, `SELECT count(*) FROM {`+table+`}`); n != 0 {
			t.Fatalf("%d rows are left in %s", n, table)
		}
	}
	if n := sapCount(t, k, `SELECT count(*) FROM {object} WHERE retired_seq IS NULL`); n != 0 {
		t.Fatalf("%d objects of the registry are still live after the abandon (the registry keeps its rows, retired)", n)
	}
	h, err := k.st.Head(ctx, k.kg.ID)
	noErr(t, "head", err)
	if h.Open() || h.AppliedSeq != 0 || h.ApplyID != "" {
		t.Fatalf("the head after the abandon: %+v", h)
	}
	if sapCount(t, k, `SELECT count(*) FROM {ingest_diff}`) == 0 {
		t.Fatal("the abandon took the stored difference away")
	}
	end := sapRun(t, k, "sap-run")
	if end.State != graph.StagedDone {
		t.Fatalf("the apply made again: %+v", end)
	}
	sapVolatile(t, k, "sap-run", 1000)
	sapSameState(t, p, "after the abandon")
}
