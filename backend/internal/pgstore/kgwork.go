package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// Abandoning an apply and restoring a KG are one kind of work: take the rows of one change number back out, a
// transaction for each number and going backwards, driven by the change entries of the number (so the work is the
// size of the change and not of the KG). The head keeps how far it got (work, work_seq), the number it stops at
// (work_floor) and the head it ends with (work_head), so a server that dies in the middle leaves nothing that the
// next one cannot end (ResumeWork). An abandon does not touch what readers see (it works above readable_through);
// a restore raises the generation in its first transaction and closes the KG to reads and writes until its last.

// AbandonApply implements graph.Writer: the chunks past readable_through go and what they closed is open again.
func (s *Store) AbandonApply(ctx context.Context, w graph.AbandonWrite) error {
	a, err := s.access(ctx, w.KG)
	if err != nil {
		return err
	}
	if err := a.checked(); err != nil {
		return err
	}
	err = a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		raw, err := loadKGHeadRaw(ctx, tx, a, true)
		if err != nil || raw.work == workAbandoning {
			return err
		}
		cur, err := s.kgGuard(ctx, tx, a, planned{w.KG, w.Generation, w.ExpectedSeq, w.Lease, w.Now})
		if err != nil {
			return err
		}
		if w.Head.AppliedSeq != cur.head.ReadableThrough {
			return kernel.Errorf(kernel.CodeInvalidValue, "an abandoned apply goes back to readable_through %d", cur.head.ReadableThrough)
		}
		return startWork(ctx, tx, a, kgWorkStart{workAbandoning, cur.head.AppliedSeq, cur.head.ReadableThrough, w.Head})
	})
	if err != nil {
		return s.resolveWork(ctx, a, workAbandoning, err)
	}
	return s.runWork(ctx, a)
}

// resolveWork decides an error of the first transaction of a work: when its COMMIT got no answer the head says
// whether the work began.
func (s *Store) resolveWork(ctx context.Context, a *kgAccess, kind string, err error) error {
	if kernel.CodeOf(err) != pgconn.CodeOutcomeUnknown {
		return err
	}
	var cur kgHeadRow
	if rerr := a.sh.tx(ctx, RoleApplier, true, func(tx *sql.Tx) error {
		var e error
		cur, e = loadKGHeadRaw(ctx, tx, a, false)
		return e
	}); rerr != nil || cur.work != kind {
		return err
	}
	return s.runWork(ctx, a)
}

// kgWorkStart is what the work needs to be resumed: its kind, where it is, where it stops and the head it ends with.
type kgWorkStart struct {
	kind        string
	from, floor int64
	end         graph.Head
}

// startWork writes the start of a work on the head.
func startWork(ctx context.Context, tx *sql.Tx, a *kgAccess, w kgWorkStart) error {
	doc, err := headDocOf(w.end)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, a.sub(`UPDATE @ SET work = $1, work_seq = $2, work_floor = $3, work_head = $4::jsonb WHERE id = 1`, pgschema.TableHead),
		w.kind, w.from, w.floor, string(doc))
	return dbErr(err, "start the work on the head")
}

// ResumeWork ends the abandon or the restore that a head says is half done; it does nothing for a KG that has none.
// A server that finds the KG in that state calls it, and any server can: the progress is in the head. The server
// calls it for every KG when it starts (ResumeAllWork), and AcquireLease calls it before it hands out a lease of a KG
// in that state (ADR-0094).
func (s *Store) ResumeWork(ctx context.Context, kg graph.KGID) error {
	a, err := s.access(ctx, kg)
	if err != nil {
		return err
	}
	return s.runWork(ctx, a)
}

// ResumeAllWork ends the work that any KG's head says is half done and returns the KGs it ended it for. A KG whose
// work cannot be ended (the database failed, the KG is damaged) does not stop the others: its error is in the one
// returned, which joins them, and the KG stays closed or refusing writes as the head says. The deletes of KGs that a
// server left half done are ended first (ADR-0125), and those KGs are among the ones returned.
func (s *Store) ResumeAllWork(ctx context.Context) ([]graph.KGID, error) {
	resumed, deleteErr := s.endDeletes(ctx)
	kgs, err := s.ListKGs(ctx)
	if err != nil {
		return resumed, errors.Join(deleteErr, err)
	}
	failed := []error{deleteErr}
	for _, kg := range kgs {
		done, err := s.resumeOne(ctx, kg.ID)
		if err != nil {
			failed = append(failed, kernel.Wrap(kernel.CodeOf(err), err, "cannot end the work half done on KG "+kg.ID.String()))
		}
		if done {
			resumed = append(resumed, kg.ID)
		}
	}
	return resumed, errors.Join(failed...)
}

// resumeOne ends the work of one KG if its head has any, and says whether it did.
func (s *Store) resumeOne(ctx context.Context, kg graph.KGID) (bool, error) {
	a, err := s.access(ctx, kg)
	if err != nil {
		return false, err
	}
	pending, err := s.workPending(ctx, a)
	if err != nil || !pending {
		return false, err
	}
	if err := s.runWork(ctx, a); err != nil {
		return false, err
	}
	return true, nil
}

// workPending says whether the head of the KG is in the middle of an abandon or a restore.
func (s *Store) workPending(ctx context.Context, a *kgAccess) (bool, error) {
	var cur kgHeadRow
	err := a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		var e error
		cur, e = loadKGHeadRaw(ctx, tx, a, false)
		return e
	})
	return cur.work != workNone, err
}

// runWork runs the transactions of the work the head names until it ends.
func (s *Store) runWork(ctx context.Context, a *kgAccess) error {
	for {
		var done bool
		var seq int64
		err := a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
			var err error
			done, seq, err = s.workStep(ctx, tx, a)
			return err
		})
		if err != nil || done {
			return err
		}
		if s.hook != nil {
			if err := s.hook("work", seq); err != nil {
				return err
			}
		}
	}
}

// workStep does one transaction of the work: takes the change number the head is at out, or ends the work.
func (s *Store) workStep(ctx context.Context, tx *sql.Tx, a *kgAccess) (bool, int64, error) {
	cur, err := loadKGHeadRaw(ctx, tx, a, true)
	if err != nil {
		return false, 0, err
	}
	if cur.work == workNone {
		return true, 0, nil
	}
	if cur.workSeq <= cur.workFloor {
		return true, 0, endWork(ctx, tx, a, cur)
	}
	if err := revertChange(ctx, tx, a, cur.workSeq, cur.work == workAbandoning); err != nil {
		return false, 0, err
	}
	_, err = tx.ExecContext(ctx, a.sub(`UPDATE @ SET work_seq = work_seq - 1, applied_seq = work_seq - 1 WHERE id = 1`, pgschema.TableHead))
	return false, cur.workSeq, dbErr(err, "move the work on")
}

// endWork puts the head the work was given in place of the one it kept, and clears the work.
func endWork(ctx context.Context, tx *sql.Tx, a *kgAccess, cur kgHeadRow) error {
	end, err := headOfDoc(a.id, cur.workHead)
	if err != nil {
		return err
	}
	if cur.work == workRestoring {
		if err := dropLaterKG(ctx, tx, a, cur.workFloor); err != nil {
			return err
		}
	}
	if err := saveKGHead(ctx, tx, a, end); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, a.sub(`UPDATE @ SET work = '', work_seq = 0, work_floor = 0, work_head = NULL WHERE id = 1`, pgschema.TableHead))
	return dbErr(err, "end the work on the head")
}

// dropLaterKG removes the ordering events and the versions that came after the restored change number.
func dropLaterKG(ctx context.Context, tx *sql.Tx, a *kgAccess, seq int64) error {
	for _, st := range []struct{ what, query string }{
		{"drop the later ordering events", a.sub(`DELETE FROM @ WHERE change_seq > $1`, pgschema.TableIngestOrder)},
		{"drop the later version parts", a.sub(`DELETE FROM @ p USING `, pgschema.TableVersionHashPart) + a.t(pgschema.TableVersion) +
			` v WHERE v.version_no = p.version_no AND v.sealed_seq > $1`},
		{"drop the later versions", a.sub(`DELETE FROM @ WHERE sealed_seq > $1`, pgschema.TableVersion)},
	} {
		if _, err := tx.ExecContext(ctx, st.query, seq); err != nil {
			return dbErr(err, st.what)
		}
	}
	return nil
}

// Restore implements graph.Catalog: the KG goes back to change number toSeq, the generation goes up by one and the
// restore is kept as an event. The first transaction raises the generation and closes the KG; the rest take the
// change numbers back out one by one; the last puts the head in place and opens the KG.
func (s *Store) Restore(ctx context.Context, id graph.KGID, toSeq int64, head graph.Head, now time.Time) (graph.KG, error) {
	a, err := s.access(ctx, id)
	if err != nil {
		return graph.KG{}, err
	}
	if err := a.checked(); err != nil {
		return graph.KG{}, err
	}
	err = a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		return beginRestore(ctx, tx, a, toSeq, head, now)
	})
	if err != nil {
		if rerr := s.resolveWork(ctx, a, workRestoring, err); rerr != nil {
			return graph.KG{}, rerr
		}
		return s.kgOf(ctx, a)
	}
	if err := s.runWork(ctx, a); err != nil {
		return graph.KG{}, err
	}
	return s.kgOf(ctx, a)
}

// beginRestore is the first transaction of a restore: it raises the generation, keeps the event and starts the work.
func beginRestore(ctx context.Context, tx *sql.Tx, a *kgAccess, toSeq int64, head graph.Head, now time.Time) error {
	cur, err := loadKGHeadRaw(ctx, tx, a, true)
	if err != nil || cur.work == workRestoring {
		return err
	}
	if cur.work != workNone {
		return kernel.Errorf(graph.CodeApplyInProgress, "the apply of KG %s is being abandoned: end the abandon first", a.id)
	}
	if toSeq < 0 || toSeq > cur.head.ReadableThrough {
		return kernel.Errorf(graph.CodeNotReadable, "cannot restore to %d", toSeq)
	}
	gen := cur.generation + 1
	if _, err := tx.ExecContext(ctx, a.sub(`UPDATE @ SET generation = $1 WHERE id = 1`, pgschema.TableHead), gen); err != nil {
		return dbErr(err, "raise the generation")
	}
	if _, err := tx.ExecContext(ctx, a.sub(`INSERT INTO @ (generation, restored_to_seq, at) VALUES ($1, $2, $3)`, pgschema.TableGenerationEvent),
		gen, toSeq, pgTime(now)); err != nil {
		return dbErr(err, "keep the restore")
	}
	return startWork(ctx, tx, a, kgWorkStart{workRestoring, cur.head.AppliedSeq, toSeq, head})
}
