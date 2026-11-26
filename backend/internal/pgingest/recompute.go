package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The computation of the objects of a run again, on PostgreSQL (ingest.Recomputer, ADR-0115). The run record keeps where it stands: the
// job in calc_job (CalcJobRecompute), the pages done in calc_part, and under "recompute" in the counts the place below. A page is the
// objects after the cursor up to a number of lines; it is read in one transaction (changing nothing), Go makes the rows of it, and one
// transaction writes them and moves the place on, so a page is done once or not at all.

var _ ingest.Recomputer = (*Store)(nil)

// rcpState is the place of the computation and what it counts, kept under "recompute" in the counts of the run.
type rcpState struct {
	Size    int            `json:"size"`
	Max     int            `json:"max"`
	Seen    string         `json:"seen"`
	Schemes map[string]int `json:"schemes"`
	// Unit is how many pages are done, Cursor the last object they ended at and Finished says that the last page is done.
	Unit     int                    `json:"unit"`
	Cursor   int64                  `json:"cursor"`
	Finished bool                   `json:"finished"`
	Counts   ingest.RecomputeCounts `json:"counts"`
}

type rcpRun struct {
	kgNo  int64
	runNo int64
	st    *rcpState
	seen  identObserver
}

func (r *rcpRun) t(table string) string { return pgschema.TableRef(r.kgNo, table) }

// q writes the names of the tables into a statement.
func (r *rcpRun) q(query string) string {
	return rcpReplacer(r).Replace(query)
}

func (r *rcpRun) saw(tx *sql.Tx, what, query string, args ...any) {
	if r.seen != nil {
		r.seen(tx, what, query, args)
	}
}

// BeginRecompute starts the computation of the objects: see ingest.Recomputer.
func (s *Store) BeginRecompute(ctx context.Context, lease ingest.RunLease, start ingest.RecomputeStart) error {
	if err := start.Validate(); err != nil {
		return err
	}
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		run, err := readRun(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		kgNo, err := kgNoOfRun(ctx, tx, run.kgID)
		if err != nil {
			return err
		}
		if err := ownsCalc(ctx, tx, kgNo, lease.No); err != nil {
			return err
		}
		if run.job == pgschema.CalcJobRecompute {
			return kernel.Errorf(ingest.CodeRunStage, "the computation of the objects of the run %d has begun already", lease.No)
		}
		if run.job != pgschema.CalcJobExpand {
			return kernel.Errorf(ingest.CodeRunStage, "the expansion of the run %d is not done, so its objects cannot be computed", lease.No)
		}
		var in struct {
			Expand expState `json:"expand"`
		}
		if err := json.Unmarshal(run.counts, &in); err != nil {
			return kernel.Wrap(kernel.CodeInternal, err, "read the place of the expansion")
		}
		if in.Expand.Rule != ruleDone {
			return kernel.Errorf(ingest.CodeRunStage, "the expansion of the run %d is at %s, so its objects cannot be computed", lease.No, in.Expand.Rule)
		}
		st := rcpState{Size: orInt(start.Lines, ingest.DefaultRecomputeLines), Max: orInt(start.ObjectMax, ingest.DefaultRecomputeObjectMax), Seen: start.Seen, Schemes: start.Schemes}
		r := &rcpRun{kgNo: kgNo, runNo: lease.No, st: &st, seen: s.rcpSeen}
		var retiring int
		query := r.q("SELECT count(*) FROM {objrun} WHERE why = {RETIRE}")
		r.saw(tx, "count the objects to retire", query)
		if err := tx.QueryRowContext(ctx, query).Scan(&retiring); err != nil {
			return dbErr(err, "count the objects to retire")
		}
		st.Counts.Retiring = retiring
		return writeRecompute(ctx, tx, lease, &st, true)
	})
}

// writeRecompute writes the place to the run, moves the number of the pages on and lets the lease run on. The attempt is the condition.
func writeRecompute(ctx context.Context, tx *sql.Tx, l ingest.RunLease, st *rcpState, begin bool) error {
	text, _ := json.Marshal(map[string]any{"recompute": st}) // a place of numbers and words cannot fail to be written as text
	set := pgschema.ColCalcPart + " = " + pgschema.ColCalcPart + " + 1"
	if begin {
		set = pgschema.ColCalcJob + " = " + strconv.Itoa(int(pgschema.CalcJobRecompute)) + ", " + pgschema.ColCalcPart + " = 0"
	}
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+set+", "+pgschema.ColCounts+" = "+pgschema.ColCounts+" || $3::jsonb, "+
		pgschema.ColLeaseUntil+" = "+lease("$4")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+
		string(ingest.RunComputing)+"'", l.No, l.Attempt, string(text), l.Term.Seconds())
	if err != nil {
		return dbErr(err, "write the place of the computation of the objects")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

// loadRecompute reads the place of the run in the transaction, checks the lease (the attempt) and that the tables of the computation
// are the run's, and that the computation has begun.
func (s *Store) loadRecompute(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (*rcpRun, error) {
	run, err := readRun(ctx, tx, lease, lock)
	if err != nil {
		return nil, err
	}
	kgNo, err := kgNoOfRun(ctx, tx, run.kgID)
	if err != nil {
		return nil, err
	}
	if err := ownsCalc(ctx, tx, kgNo, lease.No); err != nil {
		return nil, err
	}
	if run.job != pgschema.CalcJobRecompute {
		return nil, kernel.Errorf(ingest.CodeRunStage, "the computation of the objects of the run %d has not begun", lease.No)
	}
	var in struct {
		Recompute rcpState `json:"recompute"`
	}
	if err := json.Unmarshal(run.counts, &in); err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "read the place of the computation of the objects")
	}
	return &rcpRun{kgNo: kgNo, runNo: lease.No, st: &in.Recompute, seen: s.rcpSeen}, nil
}

// RecomputePlace says where the computation stands: see ingest.Recomputer.
func (s *Store) RecomputePlace(ctx context.Context, lease ingest.RunLease) (ingest.RecomputePlace, error) {
	place := ingest.RecomputePlace{Stage: ingest.RunComputing}
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		run, err := readRun(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		if run.job != pgschema.CalcJobRecompute {
			return nil
		}
		r, err := s.loadRecompute(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		place.Begun, place.Finished, place.Unit, place.Cursor = true, r.st.Finished, r.st.Unit, r.st.Cursor
		place.Seen, place.Schemes = r.st.Seen, ingest.RecomputeStart{Schemes: r.st.Schemes}.SchemesText()
		return nil
	})
	return place, err
}

// RecomputeReport adds up what the computation did: see ingest.Recomputer.
func (s *Store) RecomputeReport(ctx context.Context, lease ingest.RunLease) (ingest.RecomputeCounts, error) {
	var out ingest.RecomputeCounts
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		r, err := s.loadRecompute(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		out = r.st.Counts
		return nil
	})
	return out, err
}

// PutRecomputed writes what Go made of the page the place says and moves the place past it: see ingest.Recomputer.
func (s *Store) PutRecomputed(ctx context.Context, lease ingest.RunLease, page ingest.RecomputedPage) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.loadRecompute(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		st := r.st
		if st.Finished {
			return kernel.Errorf(ingest.CodeRunStage, "the computation of the objects of the run %d is done", lease.No)
		}
		if page.Unit != st.Unit {
			return kernel.Errorf(ingest.CodeRunStage, "the page %d of the computation of the objects of the run %d is not the one the run is at (%d)", page.Unit, lease.No, st.Unit)
		}
		if !page.Last && page.Through <= st.Cursor {
			return kernel.Errorf(kernel.CodeInvalidValue, "the page ends at the object %d, not after the cursor %d", page.Through, st.Cursor)
		}
		if err := r.put(ctx, tx, page); err != nil {
			return err
		}
		c := &st.Counts
		c.Lines += page.Lines
		c.TooMany += page.TooMany
		c.Empty += len(page.Empty)
		c.Unreadable += len(page.Unreadable)
		c.Left += page.TooMany + len(page.Unreadable)
		st.Unit++
		if st.Finished = page.Last; page.Last {
			st.Cursor = math.MaxInt64
		} else {
			st.Cursor = page.Through
		}
		return writeRecompute(ctx, tx, lease, st, false)
	})
}
