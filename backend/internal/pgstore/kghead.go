package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The kinds of work that a KG's head can be in the middle of. Both are done one change number at a time, a
// transaction each, and the head says how far they got, so a server that finds the work half done ends it.
const (
	workNone       = ""
	workAbandoning = "abandoning"
	workRestoring  = "restoring"
)

// kgHeadRow is the head row of a KG with the state beside it that the writes check: the generation, whether it
// serves, the work in the middle, and the floor under which there is no history.
type kgHeadRow struct {
	headRow
	work      string
	workSeq   int64
	workFloor int64
	workHead  []byte
	floor     int64
}

const kgHeadColumns = `state, generation, applied_seq, readable_through, applied_through, hash_sum, hash_rows, hash_parts, input_digest,
	apply_id, next_chunk_no, chunk_count, rollback, last_apply_id, applied_semantic_version, work, work_seq, work_floor, work_head, history_floor`

// loadKGHead reads the head row of a KG; with lock it takes the row lock that every write starts with. A KG that
// is being restored is CodeKGRestoring (the read of the work in the middle that ends it uses loadKGHeadRaw).
func loadKGHead(ctx context.Context, tx *sql.Tx, a *kgAccess, lock bool) (kgHeadRow, error) {
	h, err := loadKGHeadRaw(ctx, tx, a, lock)
	if err != nil {
		return h, err
	}
	if h.work == workRestoring {
		return h, kernel.Errorf(CodeKGRestoring, "KG %s is being restored to change number %d; it is closed until the restore ends", a.id, h.workFloor)
	}
	return h, nil
}

func loadKGHeadRaw(ctx context.Context, tx *sql.Tx, a *kgAccess, lock bool) (kgHeadRow, error) {
	query := `SELECT ` + kgHeadColumns + ` FROM ` + a.t(pgschema.TableHead) + ` WHERE id = 1`
	if lock {
		query += ` FOR UPDATE`
	}
	out := kgHeadRow{headRow: headRow{head: graph.Head{KG: a.id}}}
	h := &out.head
	var (
		state                         string
		sum, parts, digest, rollbackJ []byte
		rows                          int64
	)
	err := tx.QueryRowContext(ctx, query).Scan(&state, &out.generation, &h.AppliedSeq, &h.ReadableThrough, &h.IngestSeq, &sum, &rows,
		&parts, &digest, &h.ApplyID, &h.NextChunkNo, &h.ChunkCount, &rollbackJ, &h.LastApplyID, &h.AppliedSemanticVersion,
		&out.work, &out.workSeq, &out.workFloor, &out.workHead, &out.floor)
	if err != nil {
		return kgHeadRow{}, notFound(err, "read the head", a.id)
	}
	out.state = graph.State(state)
	if h.Hash, err = hashStateOf(sum, rows, parts); err != nil {
		return kgHeadRow{}, err
	}
	if h.InputDigest, err = hashOf(digest); err != nil {
		return kgHeadRow{}, err
	}
	if h.Rollback, err = rollbackOf(rollbackJ); err != nil {
		return kgHeadRow{}, err
	}
	return out, nil
}

// saveKGHead replaces what the applier owns of the head row with the head it computed.
func saveKGHead(ctx context.Context, tx *sql.Tx, a *kgAccess, h graph.Head) error {
	parts, err := partsJSON(h.Hash.Parts)
	if err != nil {
		return err
	}
	rollback, err := rollbackJSON(h.Rollback)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, a.sub(`UPDATE @ SET applied_seq = $1, readable_through = $2, applied_through = $3,
		hash_sum = $4, hash_rows = $5, hash_parts = $6::jsonb, input_digest = $7, apply_id = $8, next_chunk_no = $9, chunk_count = $10,
		rollback = $11::jsonb, last_apply_id = $12, applied_semantic_version = $13 WHERE id = 1`, pgschema.TableHead),
		h.AppliedSeq, h.ReadableThrough, h.IngestSeq, sumBytes(h.Hash.Sum), h.Hash.Rows, string(parts), hashBytes(h.InputDigest),
		h.ApplyID, h.NextChunkNo, h.ChunkCount, rollback, h.LastApplyID, h.AppliedSemanticVersion)
	return dbErr(err, "write the head")
}

// headDoc is the stored form of the head an abandon or a restore ends with.
type headDoc struct {
	Applied   int64           `json:"a"`
	Readable  int64           `json:"r"`
	Ingest    int64           `json:"i"`
	Sum       string          `json:"s"`
	Rows      int64           `json:"n"`
	Parts     json.RawMessage `json:"p"`
	Digest    string          `json:"d"`
	LastApply string          `json:"l"`
	Semantic  int             `json:"v"`
}

func headDocOf(h graph.Head) ([]byte, error) {
	parts, err := partsJSON(h.Hash.Parts)
	if err != nil {
		return nil, err
	}
	return marshal(headDoc{Applied: h.AppliedSeq, Readable: h.ReadableThrough, Ingest: h.IngestSeq, Sum: h.Hash.Sum.String(), Rows: h.Hash.Rows,
		Parts: parts, Digest: h.InputDigest.String(), LastApply: h.LastApplyID, Semantic: h.AppliedSemanticVersion})
}

func headOfDoc(kg graph.KGID, raw []byte) (graph.Head, error) {
	var d headDoc
	if err := unmarshal(raw, &d); err != nil {
		return graph.Head{}, err
	}
	sum, err := sumOf(d.Sum)
	if err != nil {
		return graph.Head{}, err
	}
	parts, err := partsOf(d.Parts)
	if err != nil {
		return graph.Head{}, err
	}
	digest, err := kernel.ParseHash(d.Digest)
	if err != nil {
		return graph.Head{}, kernel.Wrap(kernel.CodeInternal, err, "a stored head is damaged")
	}
	return graph.Head{KG: kg, AppliedSeq: d.Applied, ReadableThrough: d.Readable, IngestSeq: d.Ingest, InputDigest: digest, LastApplyID: d.LastApply,
		AppliedSemanticVersion: d.Semantic, Hash: graph.HashState{Sum: sum, Rows: d.Rows, Parts: parts}}, nil
}

// kgLease is the lease row of a KG. held is false when no owner has it; alive says it has not run out, by the
// clock the store goes by.
type kgLease struct {
	held     bool
	alive    bool
	owner    string
	until    time.Time
	attempt  int64
	semantic int
}

// nowExpr is the SQL of the time a lease is judged by: the database's, or the caller's as the parameter at index.
func (s *StoreV2) nowExpr(index string) string {
	if s.clock == LeaseClockCaller {
		return index + "::timestamptz"
	}
	return "now()"
}

// loadLease reads the lease row and locks it as asked. now is the caller's time, used only with the caller's clock.
func (s *StoreV2) loadLease(ctx context.Context, tx *sql.Tx, a *kgAccess, lock lockMode, now time.Time) (kgLease, error) {
	query := `SELECT lease_owner, lease_until, attempt, semantic_version, COALESCE(lease_until > ` + s.nowExpr("$1") + `, false) FROM ` +
		a.t(pgschema.TableLease) + ` WHERE id = 1` + string(lock)
	var args []any
	if s.clock == LeaseClockCaller {
		args = append(args, pgTime(now))
	}
	var (
		l     kgLease
		owner sql.NullString
		until sql.NullTime
	)
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&owner, &until, &l.attempt, &l.semantic, &l.alive); err != nil {
		return kgLease{}, notFound(err, "read the lease", a.id)
	}
	if owner.Valid && until.Valid {
		l.held, l.owner, l.until = true, owner.String, until.Time.UTC()
	}
	l.alive = l.alive && l.held
	return l, nil
}

// holds checks that the caller still has the lease: the owner and the attempt are the stored ones and it has not
// run out.
func (l kgLease) holds(want graph.Lease) error {
	if !l.held || l.owner != want.Owner || l.attempt != want.Attempt || !l.alive {
		return kernel.Errorf(graph.CodeLeaseLost, "owner %s attempt %d does not hold the lease", want.Owner, want.Attempt)
	}
	return nil
}

// kgGuard is the check every write makes first, in this order: that no work is half done, the generation, the lease
// (and that its engine is not older than the one that applied), then the head the write was planned against. It
// returns the locked head.
func (s *StoreV2) kgGuard(ctx context.Context, tx *sql.Tx, a *kgAccess, p planned) (kgHeadRow, error) {
	cur, err := loadKGHead(ctx, tx, a, true)
	if err != nil {
		return kgHeadRow{}, err
	}
	if cur.work == workAbandoning {
		return kgHeadRow{}, kernel.Errorf(graph.CodeApplyInProgress, "the apply of KG %s is being abandoned: end the abandon first", a.id)
	}
	if p.generation != cur.generation {
		return kgHeadRow{}, kernel.Errorf(graph.CodeGenerationChanged, "generation %d, the KG is at %d", p.generation, cur.generation)
	}
	lease, err := s.loadLease(ctx, tx, a, lockShare, p.now)
	if err != nil {
		return kgHeadRow{}, err
	}
	if err := lease.holds(p.lease); err != nil {
		return kgHeadRow{}, err
	}
	if p.lease.Semantic < cur.head.AppliedSemanticVersion {
		return kgHeadRow{}, kernel.Errorf(graph.CodeSemanticTooLow, "engine %d is below the applied %d", p.lease.Semantic, cur.head.AppliedSemanticVersion)
	}
	if p.expected != cur.head.AppliedSeq {
		return kgHeadRow{}, kernel.Errorf(graph.CodeHeadMoved, "expected applied_seq %d, the head is at %d", p.expected, cur.head.AppliedSeq)
	}
	return cur, nil
}
