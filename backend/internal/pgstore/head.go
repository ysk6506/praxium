package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// headRow is the head of a KG with what a write checks beside it.
type headRow struct {
	generation int
	state      graph.State
	head       graph.Head
}

const headColumns = `h.applied_seq, h.readable_through, h.applied_through, h.hash_sum, h.hash_rows, h.hash_parts,
	h.input_digest, h.apply_id, h.next_chunk_no, h.chunk_count, h.rollback, h.last_apply_id, h.applied_semantic_version`

// loadHead reads the head row of a KG; with lock it takes the row lock that every write starts with. A KG that
// is not there is graph.CodeKGNotFound.
func loadHead(ctx context.Context, tx *sql.Tx, kg graph.KGID, lock bool) (headRow, error) {
	query := `SELECT k.generation, k.state, ` + headColumns + ` FROM kg_head h JOIN kg k ON k.kg_id = h.kg_id WHERE h.kg_id = $1`
	if lock {
		query += ` FOR UPDATE OF h`
	}
	out := headRow{head: graph.Head{KG: kg}}
	h := &out.head
	var (
		state                         string
		sum, parts, digest, rollbackJ []byte
		rows                          int64
	)
	err := tx.QueryRowContext(ctx, query, kg.String()).Scan(&out.generation, &state, &h.AppliedSeq, &h.ReadableThrough,
		&h.IngestSeq, &sum, &rows, &parts, &digest, &h.ApplyID, &h.NextChunkNo, &h.ChunkCount, &rollbackJ, &h.LastApplyID,
		&h.AppliedSemanticVersion)
	if err != nil {
		return headRow{}, notFound(err, "read the head", kg)
	}
	out.state = graph.State(state)
	if h.Hash, err = hashStateOf(sum, rows, parts); err != nil {
		return headRow{}, err
	}
	if h.InputDigest, err = hashOf(digest); err != nil {
		return headRow{}, err
	}
	if h.Rollback, err = rollbackOf(rollbackJ); err != nil {
		return headRow{}, err
	}
	return out, nil
}

// hashStateOf reads the three columns of a hash state.
func hashStateOf(sum []byte, rows int64, parts []byte) (graph.HashState, error) {
	s, err := sumFrom(sum)
	if err != nil {
		return graph.HashState{}, err
	}
	p, err := partsOf(parts)
	if err != nil {
		return graph.HashState{}, err
	}
	return graph.HashState{Sum: s, Rows: rows, Parts: p}, nil
}

// rollbackDoc is the stored form of the hash state before the first chunk of an open apply.
type rollbackDoc struct {
	Sum   string          `json:"s"`
	Rows  int64           `json:"n"`
	Parts json.RawMessage `json:"p"`
}

// rollbackJSON is the value of the rollback column: null, or the state as a document.
func rollbackJSON(r *graph.HashState) (any, error) {
	if r == nil {
		return nil, nil
	}
	parts, err := partsJSON(r.Parts)
	if err != nil {
		return nil, err
	}
	raw, err := marshal(rollbackDoc{Sum: r.Sum.String(), Rows: r.Rows, Parts: parts})
	return string(raw), err
}

func rollbackOf(raw []byte) (*graph.HashState, error) {
	if raw == nil {
		return nil, nil
	}
	var doc rollbackDoc
	if err := unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	sum, err := sumOf(doc.Sum)
	if err != nil {
		return nil, err
	}
	parts, err := partsOf(doc.Parts)
	if err != nil {
		return nil, err
	}
	return &graph.HashState{Sum: sum, Rows: doc.Rows, Parts: parts}, nil
}

// saveHead replaces the head row with the head the applier computed.
func saveHead(ctx context.Context, tx *sql.Tx, kg graph.KGID, h graph.Head) error {
	parts, err := partsJSON(h.Hash.Parts)
	if err != nil {
		return err
	}
	rollback, err := rollbackJSON(h.Rollback)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE kg_head SET applied_seq = $2, readable_through = $3, applied_through = $4,
		hash_sum = $5, hash_rows = $6, hash_parts = $7::jsonb, input_digest = $8, apply_id = $9, next_chunk_no = $10,
		chunk_count = $11, rollback = $12::jsonb, last_apply_id = $13, applied_semantic_version = $14 WHERE kg_id = $1`,
		kg.String(), h.AppliedSeq, h.ReadableThrough, h.IngestSeq, sumBytes(h.Hash.Sum), h.Hash.Rows, string(parts),
		hashBytes(h.InputDigest), h.ApplyID, h.NextChunkNo, h.ChunkCount, rollback, h.LastApplyID, h.AppliedSemanticVersion)
	return dbErr(err, "write the head")
}

// leaseRow is the lease row of a KG. held is false when no owner has it.
type leaseRow struct {
	held     bool
	owner    string
	until    time.Time
	attempt  int64
	semantic int
}

// lockMode says how a read of the lease row locks it: not at all (a reader), shared for a write that must see a
// lease that stays as it is until the write commits, exclusive for the calls that change the lease.
type lockMode string

const (
	lockNone      lockMode = ""
	lockShare     lockMode = " FOR SHARE"
	lockExclusive lockMode = " FOR UPDATE"
)

// loadLease reads the lease row of a KG and locks it as asked.
func loadLease(ctx context.Context, tx *sql.Tx, kg graph.KGID, lock lockMode) (leaseRow, error) {
	query := `SELECT lease_owner, lease_until, attempt, semantic_version FROM kg_apply_lease WHERE kg_id = $1` + string(lock)
	var (
		l     leaseRow
		owner sql.NullString
		until sql.NullTime
	)
	if err := tx.QueryRowContext(ctx, query, kg.String()).Scan(&owner, &until, &l.attempt, &l.semantic); err != nil {
		return leaseRow{}, notFound(err, "read the lease", kg)
	}
	if owner.Valid && until.Valid {
		l.held, l.owner, l.until = true, owner.String, until.Time.UTC()
	}
	return l, nil
}

// holds checks that the caller still has the lease: the owner and the attempt are the stored ones and it has not
// run out at now.
func (l leaseRow) holds(want graph.Lease, now time.Time) error {
	if !l.held || l.owner != want.Owner || l.attempt != want.Attempt || !now.Before(l.until) {
		return kernel.Errorf(graph.CodeLeaseLost, "owner %s attempt %d does not hold the lease", want.Owner, want.Attempt)
	}
	return nil
}

// planned is what a write names about the state it was planned against.
type planned struct {
	kg         graph.KGID
	generation int
	expected   int64
	lease      graph.Lease
	now        time.Time
}

// guard is the check every write makes first, in this order: the generation, the lease (and that its engine is
// not older than the one that applied), then the head the write was planned against. It returns the locked head.
func guard(ctx context.Context, tx *sql.Tx, p planned) (headRow, error) {
	cur, err := loadHead(ctx, tx, p.kg, true)
	if err != nil {
		return headRow{}, err
	}
	if p.generation != cur.generation {
		return headRow{}, kernel.Errorf(graph.CodeGenerationChanged, "generation %d, the KG is at %d", p.generation, cur.generation)
	}
	lease, err := loadLease(ctx, tx, p.kg, lockShare)
	if err != nil {
		return headRow{}, err
	}
	if err := lease.holds(p.lease, p.now); err != nil {
		return headRow{}, err
	}
	if p.lease.Semantic < cur.head.AppliedSemanticVersion {
		return headRow{}, kernel.Errorf(graph.CodeSemanticTooLow, "engine %d is below the applied %d", p.lease.Semantic, cur.head.AppliedSemanticVersion)
	}
	if p.expected != cur.head.AppliedSeq {
		return headRow{}, kernel.Errorf(graph.CodeHeadMoved, "expected applied_seq %d, the head is at %d", p.expected, cur.head.AppliedSeq)
	}
	return cur, nil
}
