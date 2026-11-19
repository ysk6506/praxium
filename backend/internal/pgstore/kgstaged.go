package pgstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The apply that works from the stored difference (ADR-0099): graph.StagedApplier. A chunk is the rows of ingest_diff
// that have one chunk number; it is one change number and one transaction, and the statements of it are written over
// the sets (the chunk's rows joined to the staged rows), so the chunk does not pass through the memory of the server
// but for the hashes of the rows it counts in and out of the hash state. Every name of a staged table and of its
// columns comes from adapter.pgschema: the statements below are templates with {name} for a table or a column and
// {KO}... for the codes of a kind, and stagedSQL gives the names their value.

var _ graph.StagedApplier = (*Store)(nil)

// stagedTokens are the names the templates use for the columns of the staged tables and the codes of the kinds of
// the difference and of its changes.
var stagedTokens = func() []string {
	var pairs []string
	for _, t := range []string{pgschema.TableIngestDiff, pgschema.TableStageObject, pgschema.TableStageLink, pgschema.TableStageIdent, pgschema.TableStageVolat} {
		for _, c := range pgschema.ColumnsOf(t) {
			pairs = append(pairs, "{"+c+"}", c)
		}
	}
	code := func(token string, v int16) { pairs = append(pairs, "{"+token+"}", strconv.Itoa(int(v))) }
	code("KO", pgschema.DiffKindObject)
	code("KK", pgschema.DiffKindKey)
	code("KL", pgschema.DiffKindLink)
	code("KI", pgschema.DiffKindIdentifier)
	code("CA", pgschema.ChangeKindAdded)
	code("CC", pgschema.ChangeKindChanged)
	code("CR", pgschema.ChangeKindRetired)
	code("EO", pgschema.EntityKindObject)
	code("EL", pgschema.EntityKindLink)
	return pairs
}()

// stagedTables are the tables the templates name, by the token they are named with.
var stagedTables = []string{
	pgschema.TableObject, pgschema.TableLink, pgschema.TableObjectHist, pgschema.TableLinkHist, pgschema.TableObjectKey,
	pgschema.TableChangeEntry, pgschema.TableChangeBatch, pgschema.TableLightChange, pgschema.TableObjectIdentifier,
	pgschema.TableIngestOrder, pgschema.TableHead, pgschema.TableIngestDiff, pgschema.TableStageObject, pgschema.TableStageLink,
	pgschema.TableStageIdent, pgschema.TableStageVolat,
}

// stagedSQL gives the names of a template their value: the tables of the schema of the KG, quoted and qualified, the
// columns of the staged tables and the codes. A name that is left in braces is one the plan does not have, and the
// statement fails when it runs.
func (a *kgAccess) stagedSQL(tpl string) string {
	pairs := append([]string(nil), stagedTokens...)
	for _, t := range stagedTables {
		pairs = append(pairs, "{"+t+"}", a.t(t))
	}
	return strings.NewReplacer(pairs...).Replace(tpl)
}

// stagedEnter is what a staged apply finds in the head of the KG: the apply is done, it is the one the head has half
// done (the position is the head's), it is not started, or the head belongs to another apply.
func stagedEnter(h graph.Head, applyID string) (graph.StagedState, error) {
	switch {
	case !h.Open() && h.LastApplyID == applyID:
		return graph.StagedDone, nil
	case h.Open() && h.ApplyID != applyID:
		return "", kernel.Errorf(graph.CodeApplyInProgress, "apply %s is half done: resume it or abandon it", h.ApplyID)
	case h.Open():
		return graph.StagedOpen, nil
	}
	return graph.StagedPending, nil
}

// StagedProgress implements graph.StagedApplier: where the apply of a.ApplyID stands. It reads the primary (the
// head it reads is the one a write would find).
func (s *Store) StagedProgress(ctx context.Context, ap graph.StagedApply) (graph.StagedProgress, error) {
	a, err := s.access(ctx, ap.KG)
	if err != nil {
		return graph.StagedProgress{}, err
	}
	if err := a.checked(); err != nil {
		return graph.StagedProgress{}, err
	}
	var p graph.StagedProgress
	err = a.sh.pool.TxOn(ctx, pgconn.WritePool, RoleApplier, true, func(tx *sql.Tx) error {
		var err error
		p, err = stagedProgressIn(ctx, tx, a, ap)
		return err
	})
	return p, err
}

func stagedProgressIn(ctx context.Context, tx *sql.Tx, a *kgAccess, ap graph.StagedApply) (graph.StagedProgress, error) {
	cur, err := loadKGHead(ctx, tx, a, false)
	if err != nil {
		return graph.StagedProgress{}, err
	}
	if cur.work == workAbandoning {
		return graph.StagedProgress{}, kernel.Errorf(graph.CodeApplyInProgress, "the apply of KG %s is being abandoned: end the abandon first", a.id)
	}
	h := cur.head
	state, err := stagedEnter(h, ap.ApplyID)
	if err != nil {
		return graph.StagedProgress{}, err
	}
	p := graph.StagedProgress{State: state, AppliedSeq: h.AppliedSeq}
	switch state {
	case graph.StagedOpen:
		p.NextChunk, p.ChunkCount = h.NextChunkNo, h.ChunkCount
		p.Chain, err = chainBefore(ctx, tx, a, h.NextChunkNo)
	case graph.StagedPending:
		p.ChunkCount, err = chunksOf(ctx, tx, a)
		if err == nil && p.ChunkCount == 0 {
			p.State = graph.StagedEmpty
		}
	}
	return p, err
}

// chunksOf is the number of chunks of the stored difference: 0 for none. The chunks are numbered from 0 without a
// gap; the first and the last number are read from the index, so the count is not a read of every row.
func chunksOf(ctx context.Context, tx *sql.Tx, a *kgAccess) (int, error) {
	var lo, hi sql.NullInt64
	err := tx.QueryRowContext(ctx, a.stagedSQL(`SELECT min({chunk_no}), max({chunk_no}) FROM {ingest_diff}`)).Scan(&lo, &hi)
	if err != nil {
		return 0, dbErr(err, "read the chunks of the difference")
	}
	if !hi.Valid {
		return 0, nil
	}
	if lo.Int64 != 0 {
		return 0, kernel.Errorf(kernel.CodeInvalidValue, "the chunks of the difference start at %d and not at 0", lo.Int64)
	}
	return int(hi.Int64) + 1, nil
}

// chainBefore makes the chain of the digests of the chunks before chunk n from the stored difference.
func chainBefore(ctx context.Context, tx *sql.Tx, a *kgAccess, n int) (kernel.Hash, error) {
	var chain [sha256.Size]byte
	if n == 0 {
		return kernel.Hash(chain), nil
	}
	rows, err := tx.QueryContext(ctx, pgschema.ChunkDigestsSQL(a.rec.KGNo), n)
	if err != nil {
		return kernel.Hash{}, dbErr(err, "read the digests of the chunks that are done")
	}
	defer func() { _ = rows.Close() }()
	next := 0
	for rows.Next() {
		var no int
		var d []byte
		if err := rows.Scan(&no, &d); err != nil {
			return kernel.Hash{}, dbErr(err, "read the digests of the chunks that are done")
		}
		if no != next || len(d) != sha256.Size {
			return kernel.Hash{}, kernel.Errorf(kernel.CodeInternal, "the difference has no chunk %d among the %d that are done", next, n)
		}
		var chunk [sha256.Size]byte
		copy(chunk[:], d)
		chain = pgschema.ChainDigest(chain, chunk)
		next++
	}
	if err := rows.Err(); err != nil {
		return kernel.Hash{}, dbErr(err, "read the digests of the chunks that are done")
	}
	if next != n {
		return kernel.Hash{}, kernel.Errorf(kernel.CodeInternal, "the difference has %d of the %d chunks that are done", next, n)
	}
	return kernel.Hash(chain), nil
}

// ApplyStagedChunk implements graph.StagedApplier: one transaction in the schema of the KG that locks the head,
// verifies it (the work in the middle, the generation, the lease, the engine, the head the chunk was planned against
// and the chunk it is at), writes the chunk and moves the head. When the COMMIT gets no answer the head is read
// again and decides (resolve).
func (s *Store) ApplyStagedChunk(ctx context.Context, ap graph.StagedApply, from graph.StagedProgress) (graph.StagedProgress, error) {
	a, err := s.access(ctx, ap.KG)
	if err != nil {
		return graph.StagedProgress{}, err
	}
	if err := a.checked(); err != nil {
		return graph.StagedProgress{}, err
	}
	if from.State != graph.StagedPending && from.State != graph.StagedOpen {
		return graph.StagedProgress{}, kernel.Errorf(kernel.CodeInvalidValue, "a chunk is applied from a pending or an open apply, not from %q", from.State)
	}
	var out graph.StagedProgress
	var want graph.Head
	err = a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		var err error
		out, want, err = s.stagedChunkIn(ctx, tx, a, ap, from)
		return err
	})
	if err = s.resolve(ctx, a, want, err); err != nil {
		return graph.StagedProgress{}, err
	}
	return out, nil
}

// stagedChunkIn is the transaction of one chunk. It returns the progress after the chunk and the head the chunk
// leaves, which resolve compares to the stored head when the COMMIT got no answer.
func (s *Store) stagedChunkIn(ctx context.Context, tx *sql.Tx, a *kgAccess, ap graph.StagedApply, from graph.StagedProgress) (graph.StagedProgress, graph.Head, error) {
	cur, err := s.kgGuard(ctx, tx, a, planned{ap.KG, ap.Generation, from.AppliedSeq, ap.Lease, ap.Now})
	if err != nil {
		return graph.StagedProgress{}, graph.Head{}, err
	}
	state, err := stagedEnter(cur.head, ap.ApplyID)
	if err != nil {
		return graph.StagedProgress{}, graph.Head{}, err
	}
	if state == graph.StagedDone || (state == graph.StagedOpen) != (from.State == graph.StagedOpen) ||
		cur.head.NextChunkNo != from.NextChunk || (state == graph.StagedOpen && cur.head.ChunkCount != from.ChunkCount) {
		return graph.StagedProgress{}, graph.Head{}, kernel.Errorf(graph.CodeHeadMoved, "the head of KG %s is not at chunk %d of apply %s", a.id, from.NextChunk, ap.ApplyID)
	}
	c := newStagedChunk(ctx, tx, a, ap, from, cur)
	c.run()
	if c.err != nil {
		return graph.StagedProgress{}, graph.Head{}, c.err
	}
	return c.finish()
}
