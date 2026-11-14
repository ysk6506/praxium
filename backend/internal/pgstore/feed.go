package pgstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// feed runs a read of the feed in a reading transaction with the readable_through it saw.
func (s *Store) feed(ctx context.Context, kg graph.KGID, fn func(tx *sql.Tx, readable int64) error) error {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return err
	}
	return sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		cur, err := loadHead(ctx, tx, kg, false)
		if err != nil {
			return err
		}
		return fn(tx, cur.head.ReadableThrough)
	})
}

func scanBatch(sc scanner) (graph.ChangeBatch, error) {
	var b graph.ChangeBatch
	var kind string
	err := sc.Scan(&b.Seq, &kind, &b.SourceID, &b.ApplyID, &b.ChunkNo, &b.ChunkCount, &b.IngestSeq, &b.Attempt, &b.AppliedAt, &b.Objects, &b.Links)
	b.Kind, b.AppliedAt = graph.BatchKind(kind), b.AppliedAt.UTC()
	return b, err
}

func scanEntry(sc scanner) (graph.ChangeEntry, error) {
	var e graph.ChangeEntry
	var change string
	var oldHash, newHash []byte
	if err := sc.Scan(&e.Seq, &e.Entity, &e.ID, &change, &oldHash, &newHash); err != nil {
		return e, err
	}
	var err error
	e.Change = graph.ChangeKind(change)
	if e.OldHash, err = hashOf(oldHash); err != nil {
		return e, err
	}
	e.NewHash, err = hashOf(newHash)
	return e, err
}

// AppliedChanges implements graph.Feed: the chunks above after, up to readable_through.
func (s *Store) AppliedChanges(ctx context.Context, kg graph.KGID, after int64, limit int) ([]graph.AppliedChange, error) {
	var out []graph.AppliedChange
	err := s.feed(ctx, kg, func(tx *sql.Tx, readable int64) error {
		if limit <= 0 {
			return nil
		}
		batches, err := collect(ctx, tx, scanBatch, "read the applied changes", `SELECT change_seq, kind, source_id, apply_id, chunk_no, chunk_count,
			ingest_seq, attempt, applied_at, objects, links FROM change_batch WHERE kg_id = $1 AND change_seq > $2 AND change_seq <= $3
			ORDER BY change_seq LIMIT $4`, kg.String(), after, readable, limit)
		if err != nil {
			return err
		}
		if len(batches) == 0 {
			return nil
		}
		entries, err := collect(ctx, tx, scanEntry, "read the entries", `SELECT change_seq, entity_kind, entity_id, change_kind, old_hash, new_hash
			FROM change_entry WHERE kg_id = $1 AND change_seq >= $2 AND change_seq <= $3 ORDER BY change_seq, ord`,
			kg.String(), batches[0].Seq, batches[len(batches)-1].Seq)
		if err != nil {
			return err
		}
		out = make([]graph.AppliedChange, len(batches))
		at := map[int64]int{}
		for i, b := range batches {
			out[i].Batch, at[b.Seq] = b, i
		}
		for _, e := range entries {
			if i, ok := at[e.Seq]; ok {
				out[i].Entries = append(out[i].Entries, e)
			}
		}
		return nil
	})
	return out, err
}

// LightChanges implements graph.Feed.
func (s *Store) LightChanges(ctx context.Context, kg graph.KGID, after int64, limit int) ([]graph.LightChange, error) {
	var out []graph.LightChange
	err := s.feed(ctx, kg, func(tx *sql.Tx, readable int64) error {
		if limit <= 0 {
			return nil
		}
		var err error
		out, err = collect(ctx, tx, func(sc scanner) (graph.LightChange, error) {
			var l graph.LightChange
			var object string
			if err := sc.Scan(&l.Seq, &object, &l.AttrNo, &l.ValueMark); err != nil {
				return l, err
			}
			var err error
			l.Object, err = parseObject(object)
			return l, err
		}, "read the light changes", `SELECT change_seq, object_id, attr_no, value_mark FROM light_change
			WHERE kg_id = $1 AND change_seq > $2 AND change_seq <= $3 ORDER BY change_seq, object_id, attr_no LIMIT $4`, kg.String(), after, readable, limit)
		return err
	})
	return out, err
}

// OrderEvents implements graph.Feed. The ordering events are not bound by readable_through.
func (s *Store) OrderEvents(ctx context.Context, kg graph.KGID, after int64, limit int) ([]graph.OrderEvent, error) {
	var out []graph.OrderEvent
	err := s.feed(ctx, kg, func(tx *sql.Tx, _ int64) error {
		if limit <= 0 {
			return nil
		}
		var err error
		out, err = collect(ctx, tx, func(sc scanner) (graph.OrderEvent, error) {
			var e graph.OrderEvent
			var kind string
			var digest []byte
			if err := sc.Scan(&e.IngestSeq, &kind, &e.Ref, &digest, &e.ChangeSeq); err != nil {
				return e, err
			}
			var err error
			e.Kind = graph.OrderKind(kind)
			e.Digest, err = hashOf(digest)
			return e, err
		}, "read the ordering events", `SELECT ingest_seq, event_kind, ref, digest, change_seq FROM ingest_order
			WHERE kg_id = $1 AND ingest_seq > $2 ORDER BY ingest_seq LIMIT $3`, kg.String(), after, limit)
		return err
	})
	return out, err
}

// WaitReadable implements graph.Feed: it polls readable_through until it reaches minSeq. There is no
// notification channel; a wait costs one small read per poll interval.
func (s *Store) WaitReadable(ctx context.Context, kg graph.KGID, minSeq int64, waitTimeout time.Duration) (int64, error) {
	deadline := time.NewTimer(waitTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(s.poll)
	defer tick.Stop()
	for {
		readable, err := s.readable(ctx, kg)
		if err != nil || readable >= minSeq {
			return readable, err
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			return readable, kernel.Errorf(graph.CodeWaitTimeout, "readable_through is %d, wanted %d", readable, minSeq)
		case <-ctx.Done():
			return readable, kernel.Wrap(kernel.CodeInternal, ctx.Err(), "wait for readable_through was cancelled")
		}
	}
}

func (s *Store) readable(ctx context.Context, kg graph.KGID) (int64, error) {
	var out int64
	err := s.feed(ctx, kg, func(_ *sql.Tx, readable int64) error {
		out = readable
		return nil
	})
	return out, err
}
