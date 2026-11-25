package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The identification of the records of a run, on PostgreSQL (ingest.Identifier, ADR-0108). The run record keeps where it stands: the
// job in calc_job (CalcJobIdentify), the units done in calc_part, and under "identify" in the counts the whole of the place below.
// A unit writes its rows and moves the place in one transaction, so there is no statement that takes rows away to go on after a stop:
// the server that takes the run over finds the place at the next unit.

var _ ingest.Identifier = (*Store)(nil)

// The phases of the identification, in the order they are done.
const (
	phaseSeed    = "seed"    // the records whose references are not solved join the records of the run
	phaseKeys    = "keys"    // the keys, ends and claims of the rows received go to the computation
	phaseRebuild = "rebuild" // Go makes the keys of the records that have none from their cells
	phaseSlot    = "slot"    // a round: the keys whose parents are found
	phaseKLabels = "klabels" // a round: the label of each key at a turn of the search for the groups
	phaseLabels  = "labels"  // a round: the groups of records that share a key
	phaseDecide  = "decide"  // a round: the object of every group
	phaseAsk     = "ask"     // a round: the ids of the new objects that have a parent, from Go
	phaseLookup  = "lookup"  // the records that only find an object
	phaseClaims  = "claims"  // the identifiers the objects are said to have
	phaseFinish  = "finish"  // what is left, the references and the objects of the run
	phaseDone    = "done"
)

// identState is the place of the identification and what it counts, kept under "identify" in the counts of the run.
type identState struct {
	Feeds    []ingest.IdentifyFeed `json:"feeds"`
	Schemes  map[string]int        `json:"schemes"`
	Size     int                   `json:"size"`
	Pages    int                   `json:"pages"`
	Rounds   int                   `json:"rounds"`
	Turns    int                   `json:"turns"`
	Datasets []identDataset        `json:"datasets"`
	Phase    string                `json:"phase"`
	// Cur and Cur2 and CurKey are the place in the range the phase goes through: the last record, page or group done.
	Cur    int64  `json:"cur"`
	Cur2   int64  `json:"cur2"`
	CurKey string `json:"cur_key"`
	MaxRid int64  `json:"max_rid"`
	Round  int    `json:"round"`
	Turn   int    `json:"turn"`
	// Relaxed says that the round takes the records that have a key whose parent is not found, with the keys they can give.
	Relaxed    bool                  `json:"relaxed"`
	RoundSlots int                   `json:"round_slots"`
	Changed    int                   `json:"changed"`
	Unsettled  bool                  `json:"unsettled"`
	AskLeft    int                   `json:"ask_left"`
	Counts     ingest.IdentifyCounts `json:"counts"`
}

type identDataset struct {
	DatasetNo int64 `json:"dataset_no"`
	Pages     int   `json:"pages"`
}

// need is what the unit the state is at waits for.
func (s *identState) need() ingest.IdentifyNeed {
	switch s.Phase {
	case phaseRebuild:
		return ingest.NeedRebuild
	case phaseAsk:
		return ingest.NeedAsk
	}
	return ingest.NeedNothing
}

// identRun is what a unit works for.
type identRun struct {
	kgNo  int64
	srcNo int64
	runNo int64
	st    *identState
}

// t is the name of a table of the KG of the run.
func (r *identRun) t(table string) string { return pgschema.TableRef(r.kgNo, table) }

// feedSQL are the literals of the feeds that the statements of the identification use: the feeds that make objects, and the order of
// the keys of the model.
func (r *identRun) feedSQL() string {
	var nos, creates, kf, ko, km []string
	feeds := append([]ingest.IdentifyFeed(nil), r.st.Feeds...)
	sort.Slice(feeds, func(i, j int) bool { return feeds[i].Feed < feeds[j].Feed })
	for _, f := range feeds {
		nos, creates = append(nos, strconv.Itoa(f.Feed)), append(creates, strconv.FormatBool(f.Creates))
		for _, k := range f.Keys {
			kf, ko, km = append(kf, strconv.Itoa(f.Feed)), append(ko, strconv.Itoa(k.Ord)), append(km, strconv.Itoa(k.ModelOrd))
		}
	}
	arr := func(items []string, typ string) string { return "'{" + strings.Join(items, ",") + "}'::" + typ + "[]" }
	return "feeds AS (SELECT feed_no, creates FROM unnest(" + arr(nos, "int") + ", " + arr(creates, "boolean") + ") AS f(feed_no, creates)), " +
		"kord AS (SELECT feed_no, key_ord, model_ord FROM unnest(" + arr(kf, "int") + ", " + arr(ko, "int") + ", " + arr(km, "int") + ") AS k(feed_no, key_ord, model_ord))"
}

// BeginIdentify starts the identification: see ingest.Identifier.
func (s *Store) BeginIdentify(ctx context.Context, lease ingest.RunLease, start ingest.IdentifyStart) error {
	if err := start.Validate(); err != nil {
		return err
	}
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		run, err := readRun(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		var rd struct {
			Receive struct {
				Datasets []struct {
					DatasetNo int64 `json:"dataset_no"`
					Pages     int   `json:"pages"`
				} `json:"datasets"`
			} `json:"receive"`
		}
		if err := json.Unmarshal(run.counts, &rd); err != nil {
			return kernel.Wrap(kernel.CodeInternal, err, "read the counts of the run")
		}
		st := identState{Feeds: start.Feeds, Schemes: start.Schemes, Size: orInt(start.BucketRows, ingest.DefaultRawDiffBucketRows),
			Pages: orInt(start.Pages, ingest.DefaultIdentifyPages), Rounds: orInt(start.Rounds, ingest.DefaultIdentifyRounds),
			Turns: orInt(start.Turns, ingest.DefaultIdentifyTurns), Phase: phaseSeed, Round: 1}
		sort.Slice(rd.Receive.Datasets, func(i, j int) bool { return rd.Receive.Datasets[i].DatasetNo < rd.Receive.Datasets[j].DatasetNo })
		for _, d := range rd.Receive.Datasets {
			st.Datasets = append(st.Datasets, identDataset{DatasetNo: d.DatasetNo, Pages: d.Pages})
		}
		st.Cur, st.Cur2 = -1, -1
		return writeIdent(ctx, tx, lease, &st, true)
	})
}

func orInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// writeIdent writes the place to the run, moves the number of the units on and lets the lease run on. The attempt is the condition.
func writeIdent(ctx context.Context, tx *sql.Tx, l ingest.RunLease, st *identState, begin bool) error {
	text, err := json.Marshal(map[string]any{"identify": st})
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "write the place of the identification")
	}
	set := pgschema.ColCalcPart + " = " + pgschema.ColCalcPart + " + 1"
	if begin {
		set = pgschema.ColCalcJob + " = " + strconv.Itoa(int(pgschema.CalcJobIdentify)) + ", " + pgschema.ColCalcPart + " = 0"
	}
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+set+", "+pgschema.ColCounts+" = "+pgschema.ColCounts+" || $3::jsonb, "+
		pgschema.ColLeaseUntil+" = "+lease("$4")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+
		string(ingest.RunComputing)+"'", l.No, l.Attempt, string(text), l.Term.Seconds())
	if err != nil {
		return dbErr(err, "write the place of the identification")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

// loadIdent reads the place of the run in the transaction and checks that the identification has begun.
func loadIdent(ctx context.Context, tx *sql.Tx, lease ingest.RunLease) (*identRun, error) {
	run, err := readRun(ctx, tx, lease, true)
	if err != nil {
		return nil, err
	}
	if run.job != pgschema.CalcJobIdentify {
		return nil, kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d has not begun", lease.No)
	}
	st, err := identOf(run.counts)
	if err != nil {
		return nil, err
	}
	kgNo, err := kgNoOfRun(ctx, tx, run.kgID)
	if err != nil {
		return nil, err
	}
	return &identRun{kgNo: kgNo, srcNo: run.srcNo, runNo: lease.No, st: &st}, nil
}

func identOf(counts []byte) (identState, error) {
	var in struct {
		Identify identState `json:"identify"`
	}
	if err := json.Unmarshal(counts, &in); err != nil {
		return identState{}, kernel.Wrap(kernel.CodeInternal, err, "read the place of the identification")
	}
	return in.Identify, nil
}

// IdentifyPlace says where the identification stands: see ingest.Identifier.
func (s *Store) IdentifyPlace(ctx context.Context, no int64) (ingest.IdentifyPlace, error) {
	var (
		place  ingest.IdentifyPlace
		stage  string
		job    int16
		counts []byte
	)
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT "+pgschema.ColStage+", "+pgschema.ColCalcJob+", "+pgschema.ColCounts+" FROM "+
			pgschema.TableSyncRun+" WHERE "+pgschema.ColRunNo+" = $1", no).Scan(&stage, &job, &counts)
	})
	if isNoRows(err) {
		return place, kernel.Errorf(kernel.CodeNotFound, "no run %d", no)
	}
	if err != nil {
		return place, dbErr(err, "read where the identification stands")
	}
	place.Stage = ingest.RunStage(stage)
	if job != pgschema.CalcJobIdentify {
		return place, nil
	}
	st, err := identOf(counts)
	if err != nil {
		return place, err
	}
	place.Begun, place.Finished = true, st.Phase == phaseDone
	place.Phase, place.Round, place.Turn, place.Need = st.Phase, st.Round, st.Turn, st.need()
	return place, nil
}

// IdentifyReport adds up what the identification did: see ingest.Identifier.
func (s *Store) IdentifyReport(ctx context.Context, no int64) (ingest.IdentifyCounts, error) {
	var counts []byte
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT "+pgschema.ColCounts+" FROM "+pgschema.TableSyncRun+" WHERE "+pgschema.ColRunNo+" = $1", no).Scan(&counts)
	})
	if isNoRows(err) {
		return ingest.IdentifyCounts{}, kernel.Errorf(kernel.CodeNotFound, "no run %d", no)
	}
	if err != nil {
		return ingest.IdentifyCounts{}, dbErr(err, "read the counts of the identification")
	}
	st, err := identOf(counts)
	return st.Counts, err
}

// IdentifyStep does the unit the place says: see ingest.Identifier.
func (s *Store) IdentifyStep(ctx context.Context, lease ingest.RunLease) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := loadIdent(ctx, tx, lease)
		if err != nil {
			return err
		}
		if need := r.st.need(); need != ingest.NeedNothing || r.st.Phase == phaseDone {
			return kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d is at %s, which is not a unit the database does alone", lease.No, r.st.Phase)
		}
		if err := r.unit(ctx, tx); err != nil {
			return fmt.Errorf("%s: %w", r.st.Phase, err)
		}
		return writeIdent(ctx, tx, lease, r.st, false)
	})
}

// unit does one unit of the phase the place is at and moves the place.
func (r *identRun) unit(ctx context.Context, tx *sql.Tx) error {
	switch r.st.Phase {
	case phaseSeed:
		return r.seed(ctx, tx)
	case phaseKeys:
		return r.copyKeys(ctx, tx)
	case phaseSlot:
		return r.slot(ctx, tx)
	case phaseKLabels:
		return r.keyLabels(ctx, tx)
	case phaseLabels:
		return r.labels(ctx, tx)
	case phaseDecide:
		return r.decide(ctx, tx)
	case phaseLookup:
		return r.lookup(ctx, tx)
	case phaseClaims:
		return r.claims(ctx, tx)
	case phaseFinish:
		return r.finish(ctx, tx)
	}
	return kernel.Errorf(kernel.CodeInternal, "the identification has no unit for %q", r.st.Phase)
}

// identSeen is a hook of the tests: it is told each statement of a unit, in the transaction of the unit, before it runs (the tests of
// the plans ask the server how it will run it). It is nil in the code that runs.
var identSeen func(tx *sql.Tx, what, query string, args []any)

// identSaw tells the hook of the tests a statement.
func identSaw(tx *sql.Tx, what, query string, args ...any) {
	if identSeen != nil {
		identSeen(tx, what, query, args)
	}
}

// exec runs a statement of a unit and says how many rows it changed.
func exec(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int, error) {
	identSaw(tx, what, query, args...)
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, dbErr(err, what)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// scanInt runs a statement that gives one number.
func scanInt(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int64, error) {
	identSaw(tx, what, query, args...)
	var n sql.NullInt64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, dbErr(err, what)
	}
	return n.Int64, nil
}
