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

// The fifth step of a run, its second part, on PostgreSQL (ingest.Differ, ADR-0127): the difference of what the run staged with what the
// KG has. The run record keeps where it stands: the job in calc_job (CalcJobDiff), the units done in calc_part, and under "diff" in the
// counts the place below. A unit reads a range of one source (or of the rows of the difference) and writes what it made and the place in one
// transaction, so a unit is done once or not at all.
//
// The phases, each once and in this order: the rows (each source of what can change is read a range at a time by its key, and what differs
// from the KG is written to calc_diffrow, whose key is the order of the difference), the plan (the counts of the rows give each kind and type
// the number its order and its chunks begin at, in calc_diffplan), the order (the rows of a kind and a type are numbered a range at a time in
// the order of their id and written to ingest_diff with their chunk) and the print (the digest of each chunk, chained).

var _ ingest.Differ = (*Store)(nil)

// The sources the rows phase reads, in order: the objects the run staged (and their identifiers), the objects it retires (and the links at
// them), the links it staged, the links of the KG that a record of the run made, the links the isolation took out, and the keys it binds.
const (
	diffPartObjects = "objects"
	diffPartRetire  = "retire"
	diffPartLinks   = "links"
	diffPartRefs    = "refs"
	diffPartIso     = "iso"
	diffPartKeys    = "keys"
)

// diffParts lists the sources in the order they are read.
func diffParts() []string {
	return []string{diffPartObjects, diffPartRetire, diffPartLinks, diffPartRefs, diffPartIso, diffPartKeys}
}

// diffKeyCur is the place in the keys: the last key done.
type diffKeyCur struct {
	TypeNo int16  `json:"type_no"`
	CtxNo  int64  `json:"ctx_no"`
	Sig    string `json:"sig"`
}

// diffState is the place of the step and what it counts. Tally is how many rows each kind, type and change has ("kind/type/change" by the
// codes of pgschema), counted from the rows each unit wrote.
type diffState struct {
	Size  int    `json:"size"`
	Chunk int    `json:"chunk"`
	Phase string `json:"phase"`
	// Part and Cur are the source of the rows phase and the last number of it done (Key in the keys).
	Part string     `json:"part"`
	Cur  int64      `json:"cur"`
	Key  diffKeyCur `json:"key"`
	// Kind and Type are the rows the order is at, Did how many of them are numbered and Ref and Tie the last of them (Tie in hex).
	Kind int16  `json:"kind"`
	Type int16  `json:"type"`
	Did  int    `json:"did"`
	Ref  string `json:"ref"`
	Tie  string `json:"tie"`
	// Next is the chunk the print is at and Chain the chain of the digests of the chunks before it (hex).
	Next     int            `json:"next"`
	Chain    string         `json:"chain"`
	Unit     int            `json:"unit"`
	Finished bool           `json:"finished"`
	Rows     int            `json:"rows"`
	Chunks   int            `json:"chunks"`
	Tally    map[string]int `json:"tally"`
}

type diffRun struct {
	kgNo int64
	st   *diffState
	seen identObserver
}

func (r *diffRun) saw(tx *sql.Tx, what, query string, args ...any) {
	if r.seen != nil {
		r.seen(tx, what, query, args)
	}
}

// q writes the names of the tables and the codes into a statement: the step's own, and then those of the isolation, whose condition of a
// link of the KG that stays the step shares (isoKGLinkStays).
func (r *diffRun) q(query string) string {
	t := func(table string) string { return pgschema.TableRef(r.kgNo, table) }
	code := func(v int16) string { return strconv.Itoa(int(v)) }
	own := strings.NewReplacer(
		"{drow}", t(pgschema.TableCalcDiffRow), "{dplan}", t(pgschema.TableCalcDiffPlan), "{diff}", t(pgschema.TableIngestDiff),
		"{okey}", t(pgschema.TableObjectKey), "{newkey}", t(pgschema.TableCalcNewKey),
		"{KO}", code(pgschema.DiffKindObject), "{KK}", code(pgschema.DiffKindKey), "{KL}", code(pgschema.DiffKindLink), "{KI}", code(pgschema.DiffKindIdentifier),
		"{CA}", code(pgschema.ChangeKindAdded), "{CC}", code(pgschema.ChangeKindChanged), "{CR}", code(pgschema.ChangeKindRetired),
	).Replace(query)
	return (&isoRun{kgNo: r.kgNo}).q(own)
}

// exec runs one statement and gives the rows it changed.
func (r *diffRun) exec(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int, error) {
	q := r.q(query)
	r.saw(tx, what, q, args...)
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, dbErr(err, what)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// query runs one statement that gives rows, and gives them to scan.
func (r *diffRun) query(ctx context.Context, tx *sql.Tx, what, query string, scan func(*sql.Rows) error, args ...any) error {
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

// written runs a statement that writes rows of the difference and gives back (kind, type, change) for each, and counts them in the tally.
func (r *diffRun) written(ctx context.Context, tx *sql.Tx, what, query string, args ...any) error {
	return r.query(ctx, tx, what, query, func(rows *sql.Rows) error {
		var kind, typ, change int16
		if err := rows.Scan(&kind, &typ, &change); err != nil {
			return err
		}
		r.st.Tally[diffTallyKey(kind, typ, change)]++
		return nil
	}, args...)
}

func diffTallyKey(kind, typ, change int16) string {
	return strconv.Itoa(int(kind)) + "/" + strconv.Itoa(int(typ)) + "/" + strconv.Itoa(int(change))
}

// BeginDiff starts the step: see ingest.Differ.
func (s *Store) BeginDiff(ctx context.Context, lease ingest.RunLease, start ingest.DiffStart) error {
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
			Isolate isoState `json:"isolate"`
		}
		if err := json.Unmarshal(run.counts, &in); err != nil {
			return kernel.Wrap(kernel.CodeInternal, err, "read the place of the computation")
		}
		switch {
		case run.job == pgschema.CalcJobDiff:
			return kernel.Errorf(ingest.CodeRunStage, "the difference of the run %d has begun already", lease.No)
		case run.job != pgschema.CalcJobIsolate || !in.Isolate.Finished:
			// the difference reads what the isolation left: it begins when every check of it is done
			return kernel.Errorf(ingest.CodeRunStage, "the isolation of the run %d is not done, so its difference cannot begin", lease.No)
		}
		st := diffState{Size: orInt(start.Rows, ingest.DefaultDiffRows), Chunk: orInt(start.ChunkRows, ingest.DefaultDiffChunkRows),
			Phase: ingest.DiffPhaseRows, Part: diffPartObjects, Key: diffKeyCur{TypeNo: -1}, Kind: -1, Tally: map[string]int{}}
		return writeDiff(ctx, tx, lease, &st, true)
	})
}

// writeDiff writes the place to the run, moves the number of the units on and lets the lease run on; the unit that finishes the step
// writes the fingerprint of the change to the run (change_fingerprint). The attempt is the condition.
func writeDiff(ctx context.Context, tx *sql.Tx, l ingest.RunLease, st *diffState, begin bool) error {
	text, _ := json.Marshal(map[string]any{"diff": st}) // a place of numbers and words cannot fail to be written as text
	set := pgschema.ColCalcPart + " = " + pgschema.ColCalcPart + " + 1"
	if begin {
		set = pgschema.ColCalcJob + " = " + strconv.Itoa(int(pgschema.CalcJobDiff)) + ", " + pgschema.ColCalcPart + " = 0"
	}
	args := []any{l.No, l.Attempt, string(text), l.Term.Seconds()}
	if st.Finished {
		set += ", " + pgschema.ColFingerpr + " = decode($5, 'hex')"
		args = append(args, st.Chain)
	}
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+set+", "+pgschema.ColCounts+" = "+pgschema.ColCounts+" || $3::jsonb, "+
		pgschema.ColLeaseUntil+" = "+lease("$4")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+
		string(ingest.RunComputing)+"'", args...)
	if err != nil {
		return dbErr(err, "write the place of the difference")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

// loadDiff reads the place of the run in the transaction, checks the lease (the attempt) and that the tables of the computation are the
// run's. The second value is false when the step has not begun.
func (s *Store) loadDiff(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (*diffRun, bool, error) {
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
	if run.job != pgschema.CalcJobDiff {
		return nil, false, nil
	}
	var in struct {
		Diff diffState `json:"diff"`
	}
	if err := json.Unmarshal(run.counts, &in); err != nil {
		return nil, false, kernel.Wrap(kernel.CodeInternal, err, "read the place of the difference")
	}
	if in.Diff.Tally == nil {
		in.Diff.Tally = map[string]int{}
	}
	return &diffRun{kgNo: kgNo, st: &in.Diff, seen: s.diffSeen}, true, nil
}

// mustDiff is loadDiff for a call that needs the step to have begun.
func (s *Store) mustDiff(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (*diffRun, error) {
	r, begun, err := s.loadDiff(ctx, tx, lease, lock)
	if err == nil && !begun {
		return nil, kernel.Errorf(ingest.CodeRunStage, "the difference of the run %d has not begun", lease.No)
	}
	return r, err
}

// DiffPlace says where the step stands: see ingest.Differ.
func (s *Store) DiffPlace(ctx context.Context, lease ingest.RunLease) (ingest.DiffPlace, error) {
	place := ingest.DiffPlace{Stage: ingest.RunComputing}
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		r, begun, err := s.loadDiff(ctx, tx, lease, false)
		if err != nil || !begun {
			return err
		}
		place.Begun, place.Finished, place.Phase, place.Unit = true, r.st.Finished, r.st.Phase, r.st.Unit
		return nil
	})
	return place, err
}

// DiffReport says what the step made: see ingest.Differ. The fingerprint is there once the step is done.
func (s *Store) DiffReport(ctx context.Context, lease ingest.RunLease) (ingest.DiffCounts, error) {
	var out ingest.DiffCounts
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		r, err := s.mustDiff(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		if !r.st.Finished {
			return kernel.Errorf(ingest.CodeRunStage, "the difference of the run %d is not done", lease.No)
		}
		out, err = r.st.report()
		return err
	})
	return out, err
}

// DiffStep does the unit the place says and moves the place past it: see ingest.Differ.
func (s *Store) DiffStep(ctx context.Context, lease ingest.RunLease) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.mustDiff(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		if r.st.Finished {
			return kernel.Errorf(ingest.CodeRunStage, "the difference of the run %d is done", lease.No)
		}
		switch r.st.Phase {
		case ingest.DiffPhaseRows:
			err = r.rowsUnit(ctx, tx)
		case ingest.DiffPhasePlan:
			err = r.planUnit(ctx, tx)
		case ingest.DiffPhaseOrder:
			err = r.orderUnit(ctx, tx)
		default:
			err = r.printUnit(ctx, tx)
		}
		if err != nil {
			return err
		}
		r.st.Unit++
		return writeDiff(ctx, tx, lease, r.st, false)
	})
}
