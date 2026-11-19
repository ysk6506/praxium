package pgstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// feed runs a read of the feed in a reading transaction with the readable_through it saw.
func (s *Store) feed(ctx context.Context, kg graph.KGID, fn func(tx *sql.Tx, a *kgAccess, readable int64) error) error {
	a, err := s.access(ctx, kg)
	if err != nil {
		return err
	}
	if err := a.checked(); err != nil {
		return err
	}
	return a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		cur, err := loadKGHead(ctx, tx, a, false)
		if err != nil {
			return err
		}
		return fn(tx, a, cur.head.ReadableThrough)
	})
}

// AppliedChanges implements graph.Feed: the chunks above after, up to readable_through.
func (s *Store) AppliedChanges(ctx context.Context, kg graph.KGID, after int64, limit int) ([]graph.AppliedChange, error) {
	var out []graph.AppliedChange
	err := s.feed(ctx, kg, func(tx *sql.Tx, a *kgAccess, readable int64) error {
		if limit <= 0 {
			return nil
		}
		batches, err := collect(ctx, tx, scanBatch, "read the applied changes", `SELECT change_seq, kind, source_id, apply_id, chunk_no, chunk_count,
			ingest_seq, attempt, applied_at, objects, links FROM `+a.t(pgschema.TableChangeBatch)+` WHERE change_seq > $1 AND change_seq <= $2
			ORDER BY change_seq LIMIT $3`, after, readable, limit)
		if err != nil || len(batches) == 0 {
			return err
		}
		entries, err := collect(ctx, tx, scanKGEntry, "read the entries", kgEntrySQL(a), batches[0].Seq, batches[len(batches)-1].Seq)
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

// kgEntrySQL reads the entries of a range of change numbers with the refs of what they name: the registry for an
// object, the open links and then the history for a link (a retired link is only in the history).
func kgEntrySQL(a *kgAccess) string {
	return `SELECT e.change_seq, e.entity_kind, CASE WHEN e.entity_kind = 0 THEN o.ref ELSE COALESCE(l.ref, h.ref) END, e.change_kind, e.old_hash, e.new_hash
		FROM ` + a.t(pgschema.TableChangeEntry) + ` e
		LEFT JOIN ` + a.t(pgschema.TableObject) + ` o ON e.entity_kind = 0 AND o.obj_no = e.entity_no
		LEFT JOIN ` + a.t(pgschema.TableLink) + ` l ON e.entity_kind = 1 AND l.link_no = e.entity_no
		LEFT JOIN LATERAL (SELECT ref FROM ` + a.t(pgschema.TableLinkHist) + ` WHERE link_no = e.entity_no LIMIT 1) h ON e.entity_kind = 1 AND l.link_no IS NULL
		WHERE e.change_seq >= $1 AND e.change_seq <= $2 ORDER BY e.change_seq, e.ord`
}

func scanKGEntry(sc scanner) (graph.ChangeEntry, error) {
	var (
		e                graph.ChangeEntry
		kind, change     int16
		ref              sql.NullString
		oldHash, newHash []byte
	)
	if err := sc.Scan(&e.Seq, &kind, &ref, &change, &oldHash, &newHash); err != nil {
		return e, err
	}
	if !ref.Valid {
		return e, kernel.Errorf(kernel.CodeInternal, "a change entry of change %d names an entity that is not there", e.Seq)
	}
	e.Entity, e.ID = entityNameObject, ref.String
	if kind == pgschema.EntityKindLink {
		e.Entity = entityNameLink
	}
	var err error
	if e.Change, err = changeKindOf(change); err != nil {
		return e, err
	}
	if e.OldHash, err = hashOf(oldHash); err != nil {
		return e, err
	}
	e.NewHash, err = hashOf(newHash)
	return e, err
}

// LightChanges implements graph.Feed.
func (s *Store) LightChanges(ctx context.Context, kg graph.KGID, after int64, limit int) ([]graph.LightChange, error) {
	var out []graph.LightChange
	err := s.feed(ctx, kg, func(tx *sql.Tx, a *kgAccess, readable int64) error {
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
		}, "read the light changes", `SELECT l.change_seq, o.ref, l.attr_no, l.value_mark FROM `+a.t(pgschema.TableLightChange)+` l JOIN `+a.t(pgschema.TableObject)+
			` o ON o.obj_no = l.obj_no WHERE l.change_seq > $1 AND l.change_seq <= $2 ORDER BY l.change_seq, o.ref, l.attr_no LIMIT $3`, after, readable, limit)
		return err
	})
	return out, err
}

// OrderEvents implements graph.Feed. The ordering events are not bound by readable_through.
func (s *Store) OrderEvents(ctx context.Context, kg graph.KGID, after int64, limit int) ([]graph.OrderEvent, error) {
	var out []graph.OrderEvent
	err := s.feed(ctx, kg, func(tx *sql.Tx, a *kgAccess, _ int64) error {
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
		}, "read the ordering events", `SELECT ingest_seq, event_kind, ref, digest, change_seq FROM `+a.t(pgschema.TableIngestOrder)+
			` WHERE ingest_seq > $1 ORDER BY ingest_seq LIMIT $2`, after, limit)
		return err
	})
	return out, err
}

// WaitReadable implements graph.Feed: it polls readable_through until it reaches minSeq. There is no notification
// channel; a wait costs one small read per poll interval.
func (s *Store) WaitReadable(ctx context.Context, kg graph.KGID, minSeq int64, waitTimeout time.Duration) (int64, error) {
	deadline := time.NewTimer(waitTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(s.poll)
	defer tick.Stop()
	for {
		var readable int64
		err := s.feed(ctx, kg, func(_ *sql.Tx, _ *kgAccess, r int64) error {
			readable = r
			return nil
		})
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
