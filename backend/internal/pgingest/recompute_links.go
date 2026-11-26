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

// The computation of the links of a run, on PostgreSQL (ingest.LinkRecomputer, ADR-0122). The run record keeps where it stands under
// "recompute_links" in the counts: the phase, the place in it, the pages done. A page is the next ends (or links) after the place; it is
// read in one transaction (changing nothing), Go makes the rows of it, and one transaction writes them and moves the place on, so a page
// is done once or not at all. The job stays CalcJobRecompute: the step is the second half of the fourth step.

var _ ingest.LinkRecomputer = (*Store)(nil)

// rclState is the place of the computation of the links and what it counts.
type rclState struct {
	Size     int                         `json:"size"`
	Defs     []ingest.RecomputeLinkDef   `json:"defs"`
	Phase    string                      `json:"phase"`
	Cur      ingest.LinkCursor           `json:"cur"`
	Unit     int                         `json:"unit"`
	Finished bool                        `json:"finished"`
	Counts   ingest.RecomputeLinksCounts `json:"counts"`
}

// rclFirst is the place before the first end: no definition has the number -1.
func rclFirst() ingest.LinkCursor { return ingest.LinkCursor{Def: -1, Hash: []byte{}} }

type rclRun struct {
	kgNo  int64
	runNo int64
	st    *rclState
	seen  identObserver
}

func (r *rclRun) saw(tx *sql.Tx, what, query string, args ...any) {
	if r.seen != nil {
		r.seen(tx, what, query, args)
	}
}

// BeginRecomputeLinks starts the computation of the links: see ingest.LinkRecomputer.
func (s *Store) BeginRecomputeLinks(ctx context.Context, lease ingest.RunLease, start ingest.RecomputeLinksStart) error {
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
		case in.Links != nil:
			return kernel.Errorf(ingest.CodeRunStage, "the computation of the links of the run %d has begun already", lease.No)
		case run.job != pgschema.CalcJobRecompute || !in.Recompute.Finished:
			return kernel.Errorf(ingest.CodeRunStage, "the computation of the objects of the run %d is not done, so its links cannot be computed", lease.No)
		}
		st := rclState{Size: orInt(start.Lines, ingest.DefaultRecomputeLinkLines), Defs: start.Defs, Phase: ingest.LinksPhaseEnds, Cur: rclFirst()}
		return writeLinks(ctx, tx, lease, &st)
	})
}

// writeLinks writes the place to the run, moves the number of the pages on and lets the lease run on. The attempt is the condition.
func writeLinks(ctx context.Context, tx *sql.Tx, l ingest.RunLease, st *rclState) error {
	text, _ := json.Marshal(map[string]any{"recompute_links": st}) // a place of numbers and words cannot fail to be written as text
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColCalcPart+" = "+pgschema.ColCalcPart+" + 1, "+pgschema.ColCounts+" = "+
		pgschema.ColCounts+" || $3::jsonb, "+pgschema.ColLeaseUntil+" = "+lease("$4")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+
		string(ingest.RunComputing)+"'", l.No, l.Attempt, string(text), l.Term.Seconds())
	if err != nil {
		return dbErr(err, "write the place of the computation of the links")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

// loadLinks reads the place of the run in the transaction, checks the lease (the attempt) and that the tables of the computation are the
// run's. The second value is false when the computation of the links has not begun.
func (s *Store) loadLinks(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (*rclRun, bool, error) {
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
	var in struct {
		Links *rclState `json:"recompute_links"`
	}
	if err := json.Unmarshal(run.counts, &in); err != nil {
		return nil, false, kernel.Wrap(kernel.CodeInternal, err, "read the place of the computation of the links")
	}
	if in.Links == nil {
		return nil, false, nil
	}
	return &rclRun{kgNo: kgNo, runNo: lease.No, st: in.Links, seen: s.rclSeen}, true, nil
}

// must is loadLinks for a call that needs the computation to have begun.
func (s *Store) must(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (*rclRun, error) {
	r, begun, err := s.loadLinks(ctx, tx, lease, lock)
	if err == nil && !begun {
		return nil, kernel.Errorf(ingest.CodeRunStage, "the computation of the links of the run %d has not begun", lease.No)
	}
	return r, err
}

// RecomputeLinksPlace says where the computation stands: see ingest.LinkRecomputer.
func (s *Store) RecomputeLinksPlace(ctx context.Context, lease ingest.RunLease) (ingest.RecomputeLinksPlace, error) {
	place := ingest.RecomputeLinksPlace{Stage: ingest.RunComputing}
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		r, begun, err := s.loadLinks(ctx, tx, lease, false)
		if err != nil || !begun {
			return err
		}
		place.Begun, place.Finished, place.Phase, place.Unit, place.Cursor = true, r.st.Finished, r.st.Phase, r.st.Unit, r.st.Cur
		place.Defs = ingest.RecomputeLinksStart{Defs: r.st.Defs}.DefsText()
		return nil
	})
	return place, err
}

// RecomputeLinksReport adds up what the computation did: see ingest.LinkRecomputer.
func (s *Store) RecomputeLinksReport(ctx context.Context, lease ingest.RunLease) (ingest.RecomputeLinksCounts, error) {
	var out ingest.RecomputeLinksCounts
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		r, err := s.must(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		out = r.st.Counts
		return nil
	})
	return out, err
}

// nextPhase is the phase after one, and where it begins.
func nextPhase(phase string) string {
	switch phase {
	case ingest.LinksPhaseEnds:
		return ingest.LinksPhaseTo
	case ingest.LinksPhaseTo:
		return ingest.LinksPhaseFrom
	case ingest.LinksPhaseFrom:
		return ingest.LinksPhaseConflicts
	case ingest.LinksPhaseConflicts:
		return ingest.LinksPhaseCells
	}
	return ingest.LinksPhaseDone
}

// advance moves the place past a page that was put: to the end of the page, or to the start of the next phase when it was the last page of
// the phase (the step is finished after the last page of the last phase).
func (st *rclState) advance(page ingest.RecomputedLinksPage) {
	st.Unit++
	if !page.Last {
		st.Cur = page.Through
		return
	}
	st.Phase = nextPhase(st.Phase)
	st.Cur = rclFirst()
	if st.Phase == ingest.LinksPhaseDone {
		st.Finished = true
	}
}

// PutRecomputedLinks writes what Go made of the page the place says and moves the place past it: see ingest.LinkRecomputer.
func (s *Store) PutRecomputedLinks(ctx context.Context, lease ingest.RunLease, page ingest.RecomputedLinksPage) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.must(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		st := r.st
		switch {
		case st.Finished:
			return kernel.Errorf(ingest.CodeRunStage, "the computation of the links of the run %d is done", lease.No)
		case page.Unit != st.Unit || page.Phase != st.Phase:
			return kernel.Errorf(ingest.CodeRunStage, "the page %d of the phase %s of the computation of the links of the run %d is not the one the run is at (%d, %s)", page.Unit, page.Phase, lease.No, st.Unit, st.Phase)
		case !page.Last && !rclAfter(page.Through, st.Cur):
			return kernel.Errorf(kernel.CodeInvalidValue, "the page ends at %v, not after the place %v", page.Through, st.Cur)
		}
		if err := r.put(ctx, tx, page); err != nil {
			return err
		}
		st.advance(page)
		return writeLinks(ctx, tx, lease, st)
	})
}

// rclAfter says whether the place a is after the place b: in the order the ends are read (the definition, the key, the record), or by the
// id of the link.
func rclAfter(a, b ingest.LinkCursor) bool {
	switch {
	case a.Ref != b.Ref:
		return a.Ref > b.Ref
	case a.Def != b.Def:
		return a.Def > b.Def
	case string(a.Hash) != string(b.Hash):
		return string(a.Hash) > string(b.Hash)
	}
	return a.Rid > b.Rid
}

// put writes the rows of a page of the phase the place is in.
func (r *rclRun) put(ctx context.Context, tx *sql.Tx, page ingest.RecomputedLinksPage) error {
	switch r.st.Phase {
	case ingest.LinksPhaseEnds:
		return r.putEnds(ctx, tx, page)
	case ingest.LinksPhaseTo:
		return r.putTo(ctx, tx, page)
	case ingest.LinksPhaseFrom:
		return r.putFrom(ctx, tx, page)
	case ingest.LinksPhaseConflicts:
		return r.putConflicts(ctx, tx, page)
	}
	return r.putCells(ctx, tx, page)
}

// rclDefs is the link definitions of the run, as the CTE defs.
func (r *rclRun) defs() string {
	var srcs, defs, from, to []string
	for _, d := range r.st.Defs {
		srcs, defs = append(srcs, strconv.FormatInt(d.SrcNo, 10)), append(defs, strconv.Itoa(d.Def))
		from, to = append(from, strconv.Itoa(d.FromFeed)), append(to, strconv.Itoa(d.ToFeed))
	}
	arr := func(items []string) string { return "'{" + strings.Join(items, ",") + "}'::int[]" }
	return "defs AS (SELECT src_no, def_no, from_feed, to_feed FROM unnest(" + arr(srcs) + ", " + arr(defs) + ", " + arr(from) + ", " + arr(to) + ") AS d(src_no, def_no, from_feed, to_feed))"
}

func (r *rclRun) t(table string) string { return pgschema.TableRef(r.kgNo, table) }

// q writes the names of the tables and the numbers into a statement.
func (r *rclRun) q(query string) string {
	return strings.NewReplacer(
		"{rec}", r.t(pgschema.TableCalcRecord), "{end}", r.t(pgschema.TableCalcEnd), "{bind}", r.t(pgschema.TableCalcBind), "{cref}", r.t(pgschema.TableCalcRef),
		"{rref}", r.t(pgschema.TableRecordRef), "{obj}", r.t(pgschema.TableObject), "{newobj}", r.t(pgschema.TableCalcNewObj), "{link}", r.t(pgschema.TableLink),
		"{tokey}", r.t(pgschema.TableCalcToKey), "{win}", r.t(pgschema.TableCalcLinkWin), "{lost}", r.t(pgschema.TableCalcLinkLost), "{stlink}", r.t(pgschema.TableStageLink),
		"{stobj}", r.t(pgschema.TableStageObject), "{prob}", r.t(pgschema.TableCalcProblem), "{ds}", pgschema.TableSourceDataset, "{defs}", r.defs(),
		"{WHY}", strconv.Itoa(int(pgschema.CalcWhyExpanded)), "{FROM}", strconv.Itoa(int(pgschema.RefRoleLinkFrom)), "{TO}", strconv.Itoa(int(pgschema.RefRoleLinkTo)), "{IDENT}", roleIdent,
		"{RES}", strconv.Itoa(int(pgschema.RefResolved)), "{UNRES}", strconv.Itoa(int(pgschema.RefUnresolved)), "{HELD}", strconv.Itoa(int(pgschema.RefHeld)),
		// a problem of a link keeps the number of its definition as a link definition's (ADR-0128)
		"{DEFY}", pgschema.ProblemDefLinkSQL("y.def_no"), "{DEFW}", pgschema.ProblemDefLinkSQL("w.def_no"),
	).Replace(query)
}

// exec runs one statement of a page and gives the rows it wrote.
func (r *rclRun) exec(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int, error) {
	q := r.q(query)
	r.saw(tx, what, q, args...)
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, dbErr(err, what)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
