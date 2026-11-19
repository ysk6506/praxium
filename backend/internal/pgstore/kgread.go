package pgstore

import (
	"context"
	"database/sql"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// kgSnapshot is one state of a KG: the change number it shows is fixed when it is taken. Each read is a
// read-only repeatable-read transaction of its own that first reads the head, to check that the KG is still in
// the generation the snapshot was taken in (a restore removes rows that the snapshot's state may have had), that
// it is not being restored, and whether the state the snapshot shows is the newest one, which is read from the
// open rows alone.
type kgSnapshot struct {
	a    *kgAccess
	info graph.SnapshotInfo
}

var (
	_ graph.Snapshot         = (*kgSnapshot)(nil)
	_ graph.IdentifierLister = (*kgSnapshot)(nil)
	_ graph.BulkReader       = (*kgSnapshot)(nil)
)

// Snapshot implements graph.Reader.
func (s *Store) Snapshot(ctx context.Context, rp graph.ReadPoint) (graph.Snapshot, error) {
	a, err := s.access(ctx, rp.KG)
	if err != nil {
		return nil, err
	}
	if err := a.checked(); err != nil {
		return nil, err
	}
	var info graph.SnapshotInfo
	err = a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		cur, err := loadKGHead(ctx, tx, a, false)
		if err != nil {
			return err
		}
		if rp.Generation != 0 && rp.Generation != cur.generation {
			return kernel.Errorf(graph.CodeGenerationChanged, "generation %d, the KG is at %d", rp.Generation, cur.generation)
		}
		info = graph.SnapshotInfo{KG: rp.KG, Generation: cur.generation, Volatile: graph.VolatileOmitted}
		return kgReadPoint(ctx, tx, a, rp, cur, &info)
	})
	if err != nil {
		return nil, err
	}
	return &kgSnapshot{a: a, info: info}, nil
}

// kgReadPoint fills in the change number and the volatile mode a read point means.
func kgReadPoint(ctx context.Context, tx *sql.Tx, a *kgAccess, rp graph.ReadPoint, cur kgHeadRow, info *graph.SnapshotInfo) error {
	head := cur.head
	switch rp.Kind {
	case graph.ReadLatest:
		info.Seq, info.Volatile = head.ReadableThrough, graph.VolatileLatest
	case graph.ReadApplied:
		info.Seq, info.Volatile = head.AppliedSeq, graph.VolatileLatest
	case graph.ReadSeq:
		if rp.Seq < 0 || rp.Seq > head.ReadableThrough {
			return kernel.Errorf(graph.CodeNotReadable, "change number %d is not readable: readable_through is %d", rp.Seq, head.ReadableThrough)
		}
		info.Seq = rp.Seq
	case graph.ReadVersion:
		err := tx.QueryRowContext(ctx, `SELECT sealed_seq FROM `+a.t(pgschema.TableVersion)+` WHERE version_id = $1`, rp.Version.String()).Scan(&info.Seq)
		if isNoRows(err) {
			return kernel.Errorf(graph.CodeKGNotFound, "no version %q", rp.Version)
		}
		if err != nil {
			return dbErr(err, "find the version")
		}
		info.Version = rp.Version
	default:
		return kernel.Errorf(kernel.CodeInvalidValue, "unknown read kind %d", rp.Kind)
	}
	return kgFloor(cur, info.Seq)
}

// kgFloor refuses a read under the floor of the history: what is under it is not kept (ADR-0089 decision 5).
func kgFloor(cur kgHeadRow, seq int64) error {
	if seq < cur.floor {
		return kernel.Errorf(graph.CodeNotReadable, "change number %d is under the floor of the history, which is %d", seq, cur.floor)
	}
	return nil
}

func (sn *kgSnapshot) Info() graph.SnapshotInfo { return sn.info }

// read runs fn in a reading transaction after the checks of the head. newest says that the snapshot shows the
// change number the head is at, so the open rows alone are what it shows.
func (sn *kgSnapshot) read(ctx context.Context, fn func(tx *sql.Tx, newest bool) error) error {
	return sn.a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		cur, err := loadKGHead(ctx, tx, sn.a, false)
		if err != nil {
			return err
		}
		if cur.generation != sn.info.Generation {
			return kernel.Errorf(graph.CodeGenerationChanged, "the KG moved to generation %d", cur.generation)
		}
		return fn(tx, sn.info.Seq == cur.head.AppliedSeq)
	})
}

// Volatile returns the current value; a snapshot that is not latest answers CodeVolatileOmitted.
func (sn *kgSnapshot) Volatile(ctx context.Context, id graph.ObjectID, attrNo int) (graph.VolatileRow, bool, error) {
	var row graph.VolatileRow
	var found bool
	err := sn.read(ctx, func(tx *sql.Tx, newest bool) error {
		if sn.info.Volatile != graph.VolatileLatest {
			return kernel.Errorf(graph.CodeVolatileOmitted, "volatile values are only in latest reads")
		}
		reg, err := sn.registry(ctx, tx, []string{id.String()})
		if err != nil || len(reg) == 0 {
			return err
		}
		live, err := sn.liveAt(ctx, tx, reg, newest)
		if err != nil || !live[reg[0].objNo] {
			return err
		}
		row, found, err = sn.volatileOf(ctx, tx, reg[0], id, attrNo)
		return err
	})
	return row, found, err
}

// Head implements graph.Reader.
func (s *Store) Head(ctx context.Context, kg graph.KGID) (graph.Head, error) {
	a, err := s.access(ctx, kg)
	if err != nil {
		return graph.Head{}, err
	}
	var out graph.Head
	err = a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		cur, err := loadKGHead(ctx, tx, a, false)
		out = cur.head
		return err
	})
	return out, err
}
