package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The fifth step of a run, its first part, on PostgreSQL (ingest.Isolator, ADR-0123): the rows the KG cannot have are taken out of what
// the run staged. The run record keeps where it stands: the job in calc_job (CalcJobIsolate), the pages done in calc_part, and under
// "isolate" in the counts the phase, the place in it and what was done. A page is read in one transaction (changing nothing), Go decides
// it, and one transaction writes what it decided and moves the place on, so a page is done once or not at all.

var _ ingest.Isolator = (*Store)(nil)

// isoState is the place of the step and what it counts. Schemes are the numbers of the schemes the computation of the objects was given
// (the references of the claims name a scheme by its number).
type isoState struct {
	Size     int                  `json:"size"`
	Cards    []ingest.ExpandCard  `json:"cards"`
	Schemes  map[string]int       `json:"schemes"`
	Phase    string               `json:"phase"`
	Cur      ingest.IsolateCursor `json:"cur"`
	Unit     int                  `json:"unit"`
	Finished bool                 `json:"finished"`
	Counts   ingest.IsolateCounts `json:"counts"`
}

type isoRun struct {
	kgNo  int64
	runNo int64
	st    *isoState
	seen  identObserver
}

func (r *isoRun) saw(tx *sql.Tx, what, query string, args ...any) {
	if r.seen != nil {
		r.seen(tx, what, query, args)
	}
}

// BeginIsolate starts the step: see ingest.Isolator.
func (s *Store) BeginIsolate(ctx context.Context, lease ingest.RunLease, start ingest.IsolateStart) error {
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
		var in struct {
			Recompute rcpState  `json:"recompute"`
			Links     *rclState `json:"recompute_links"`
		}
		if err := json.Unmarshal(run.counts, &in); err != nil {
			return kernel.Wrap(kernel.CodeInternal, err, "read the place of the computation")
		}
		switch {
		case run.job == pgschema.CalcJobIsolate:
			return kernel.Errorf(ingest.CodeRunStage, "the fifth step of the run %d has begun already", lease.No)
		case run.job != pgschema.CalcJobRecompute || !in.Recompute.Finished || in.Links == nil || !in.Links.Finished:
			// the step reads what the fourth step staged, the objects and the links: it begins when both are done
			return kernel.Errorf(ingest.CodeRunStage, "the computation of the objects and the links of the run %d is not done, so its fifth step cannot begin", lease.No)
		}
		st := isoState{Size: orInt(start.Lines, ingest.DefaultIsolateLines), Cards: start.Cards, Schemes: in.Recompute.Schemes, Phase: ingest.IsolatePhaseIdents}
		return writeIsolate(ctx, tx, lease, &st, true)
	})
}

// writeIsolate writes the place to the run, moves the number of the pages on and lets the lease run on. The attempt is the condition.
func writeIsolate(ctx context.Context, tx *sql.Tx, l ingest.RunLease, st *isoState, begin bool) error {
	text, _ := json.Marshal(map[string]any{"isolate": st}) // a place of numbers and words cannot fail to be written as text
	set := pgschema.ColCalcPart + " = " + pgschema.ColCalcPart + " + 1"
	if begin {
		set = pgschema.ColCalcJob + " = " + strconv.Itoa(int(pgschema.CalcJobIsolate)) + ", " + pgschema.ColCalcPart + " = 0"
	}
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+set+", "+pgschema.ColCounts+" = "+pgschema.ColCounts+" || $3::jsonb, "+
		pgschema.ColLeaseUntil+" = "+lease("$4")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+
		string(ingest.RunComputing)+"'", l.No, l.Attempt, string(text), l.Term.Seconds())
	if err != nil {
		return dbErr(err, "write the place of the fifth step")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

// loadIsolate reads the place of the run in the transaction, checks the lease (the attempt) and that the tables of the computation are the
// run's. The second value is false when the step has not begun.
func (s *Store) loadIsolate(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (*isoRun, bool, error) {
	run, err := readRun(ctx, tx, lease, lock)
	if err != nil {
		return nil, false, err
	}
	kgNo, err := kgNoOfRun(ctx, tx, run.kgID)
	if err != nil {
		return nil, false, err
	}
	if err := ownsCalc(ctx, tx, kgNo, lease.No); err != nil {
		return nil, false, err
	}
	if run.job != pgschema.CalcJobIsolate {
		return nil, false, nil
	}
	var in struct {
		Isolate isoState `json:"isolate"`
	}
	if err := json.Unmarshal(run.counts, &in); err != nil {
		return nil, false, kernel.Wrap(kernel.CodeInternal, err, "read the place of the fifth step")
	}
	return &isoRun{kgNo: kgNo, runNo: lease.No, st: &in.Isolate, seen: s.isoSeen}, true, nil
}

// mustIsolate is loadIsolate for a call that needs the step to have begun.
func (s *Store) mustIsolate(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (*isoRun, error) {
	r, begun, err := s.loadIsolate(ctx, tx, lease, lock)
	if err == nil && !begun {
		return nil, kernel.Errorf(ingest.CodeRunStage, "the fifth step of the run %d has not begun", lease.No)
	}
	return r, err
}

// IsolatePlace says where the step stands: see ingest.Isolator.
func (s *Store) IsolatePlace(ctx context.Context, lease ingest.RunLease) (ingest.IsolatePlace, error) {
	place := ingest.IsolatePlace{Stage: ingest.RunComputing}
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		r, begun, err := s.loadIsolate(ctx, tx, lease, false)
		if err != nil || !begun {
			return err
		}
		place.Begun, place.Finished, place.Phase, place.Unit, place.Cursor = true, r.st.Finished, r.st.Phase, r.st.Unit, r.st.Cur
		return nil
	})
	return place, err
}

// IsolateReport adds up what the step did: see ingest.Isolator.
func (s *Store) IsolateReport(ctx context.Context, lease ingest.RunLease) (ingest.IsolateCounts, error) {
	var out ingest.IsolateCounts
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		r, err := s.mustIsolate(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		out = r.st.Counts
		return nil
	})
	return out, err
}

// isoNextPhase is the phase after one.
func isoNextPhase(phase string) string {
	switch phase {
	case ingest.IsolatePhaseIdents:
		return ingest.IsolatePhaseEnds
	case ingest.IsolatePhaseEnds:
		return ingest.IsolatePhaseKept
	case ingest.IsolatePhaseKept:
		return ingest.IsolatePhaseFresh
	}
	return ingest.IsolatePhaseDone
}

// isoAfter says whether the place a is after the place b in the phase: by the identifier, the link number or the link id.
func isoAfter(phase string, a, b ingest.IsolateCursor) bool {
	switch phase {
	case ingest.IsolatePhaseIdents:
		// the identifiers are read in the order of their bytes (COLLATE "C", the order of the index stage_ident_value), which is Go's
		if a.Scheme != b.Scheme {
			return a.Scheme > b.Scheme
		}
		if a.Value != b.Value {
			return a.Value > b.Value
		}
		return a.Scope > b.Scope
	case ingest.IsolatePhaseEnds:
		return a.No > b.No
	}
	return a.Ref > b.Ref
}

// PutIsolated writes what Go decided of the page the place says and moves the place past it: see ingest.Isolator.
func (s *Store) PutIsolated(ctx context.Context, lease ingest.RunLease, page ingest.IsolatedPage) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.mustIsolate(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		st := r.st
		switch {
		case st.Finished:
			return kernel.Errorf(ingest.CodeRunStage, "the fifth step of the run %d is done", lease.No)
		case page.Unit != st.Unit || page.Phase != st.Phase:
			return kernel.Errorf(ingest.CodeRunStage, "the page %d of the phase %s of the fifth step of the run %d is not the one the run is at (%d, %s)", page.Unit, page.Phase, lease.No, st.Unit, st.Phase)
		case !page.Last && !isoAfter(st.Phase, page.Through, st.Cur):
			return kernel.Errorf(kernel.CodeInvalidValue, "the page ends at %v, not after the place %v", page.Through, st.Cur)
		}
		if err := r.put(ctx, tx, page); err != nil {
			return err
		}
		st.Unit++
		if page.Last {
			st.Phase, st.Cur = isoNextPhase(st.Phase), ingest.IsolateCursor{}
			st.Finished = st.Phase == ingest.IsolatePhaseDone
		} else {
			st.Cur = page.Through
		}
		return writeIsolate(ctx, tx, lease, st, false)
	})
}

// put writes what a page of the phase the place is in decided.
func (r *isoRun) put(ctx context.Context, tx *sql.Tx, page ingest.IsolatedPage) error {
	switch r.st.Phase {
	case ingest.IsolatePhaseIdents:
		r.st.Counts.Keys += page.Keys
		return r.putIdents(ctx, tx, page.Idents)
	case ingest.IsolatePhaseEnds:
		return r.putEnds(ctx, tx, page)
	}
	return r.putSeats(ctx, tx, page)
}

func (r *isoRun) t(table string) string { return pgschema.TableRef(r.kgNo, table) }

// q writes the names of the tables and the numbers into a statement.
func (r *isoRun) q(query string) string {
	return strings.NewReplacer(
		"{rec}", r.t(pgschema.TableCalcRecord), "{cref}", r.t(pgschema.TableCalcRef), "{rref}", r.t(pgschema.TableRecordRef), "{obj}", r.t(pgschema.TableObject),
		"{objrun}", r.t(pgschema.TableCalcObject), "{link}", r.t(pgschema.TableLink), "{ident}", r.t(pgschema.TableObjectIdentifier),
		"{win}", r.t(pgschema.TableCalcLinkWin), "{stlink}", r.t(pgschema.TableStageLink), "{stobj}", r.t(pgschema.TableStageObject), "{stid}", r.t(pgschema.TableStageIdent),
		"{prob}", r.t(pgschema.TableCalcProblem), "{iso}", r.t(pgschema.TableCalcIsolated), "{seat}", r.t(pgschema.TableCalcSeat), "{ds}", pgschema.TableSourceDataset,
		"{RETIRE}", whyRetire, "{IDENT}", roleIdent, "{CLAIM}", roleClaim, "{FOUND}", stateFound,
		"{FROM}", strconv.Itoa(int(pgschema.RefRoleLinkFrom)), "{UNRES}", strconv.Itoa(int(pgschema.RefUnresolved)), "{LOST}", strconv.Itoa(int(pgschema.RefLostClaim)),
		"{HELD}", strconv.Itoa(int(pgschema.RefHeld)), "{KLINK}", strconv.Itoa(int(pgschema.DiffKindLink)), "{KIDENT}", strconv.Itoa(int(pgschema.DiffKindIdentifier)),
		"{PIDENT}", isoNum(pgschema.IsolatePhaseIdents), "{PENDS}", isoNum(pgschema.IsolatePhaseEnds), "{PCARD}", isoNum(pgschema.IsolatePhaseCard),
		"{IKG}", isoNum(pgschema.IsolatedIdentKG), "{MISSING}", isoNum(pgschema.IsolatedEndMissing), "{RETIRES}", isoNum(pgschema.IsolatedEndRetires),
		"{CARD}", isoNum(pgschema.IsolatedCardinality), "{OUT}", isoNum(ingest.SeatOut), "{IN}", isoNum(ingest.SeatIn),
		// the kinds of problem are the list the step owns, by its place in it (an identifier, an end, the cardinality)
		"{KCONFLICT}", isoKinds[0], "{KMISSING}", isoKinds[1], "{KCARD}", isoKinds[2],
	).Replace(query)
}

// isoKinds are the kinds of problem the step writes (pgschema.StepFiveProblemKinds).
var isoKinds = pgschema.StepFiveProblemKinds()

func isoNum(v int16) string { return strconv.Itoa(int(v)) }

// exec runs one statement and gives the rows it wrote.
func (r *isoRun) exec(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int, error) {
	q := r.q(query)
	r.saw(tx, what, q, args...)
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, dbErr(err, what)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// query runs one statement that reads, and gives its rows to scan.
func (r *isoRun) query(ctx context.Context, tx *sql.Tx, what, query string, scan func(*sql.Rows) error, args ...any) error {
	q := r.q(query)
	r.saw(tx, what, q, args...)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return dbErr(err, what)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return dbErr(err, what)
		}
	}
	return dbErr(rows.Err(), what)
}
