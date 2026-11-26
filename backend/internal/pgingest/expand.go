package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The expansion of the set of a run, on PostgreSQL (ingest.Expander, ADR-0114). The run record keeps where it stands: the job in calc_job
// (CalcJobExpand), the units done in calc_part, and under "expand" in the counts the place below. A unit is one range of one rule,
// and it writes its rows and moves the place in one transaction, as a unit of the identification does.

var _ ingest.Expander = (*Store)(nil)

// The rules of the expansion in the order they are done (a rule is a path of the rules of ingestion-at-scale-identity.md 3: a rule
// that has two paths has two names here, so that a test can leave one out). The last is where a finished expansion stands.
const (
	ruleAll       = "all"        // the computation of everything: every open record of the KG is a record of the run
	ruleOld       = "old"        // the objects the records of the run were bound to, and which of them may be retired
	ruleRetireOut = "retire_out" // the records that made the links that start at an object to retire
	ruleRetireIn  = "retire_in"  // the records that made the links that end at an object to retire
	ruleKeyOld    = "key_old"    // the records that read a link end key that a record of the run had
	ruleKeyNew    = "key_new"    // the records that read a link end key that a record of the run gives
	ruleLimitOut  = "limit_out"  // the records of the links that compete for the start of a link type that holds it to one
	ruleLimitIn   = "limit_in"   // the records of the links that compete for the end of a link type that holds it to one
	ruleClaim     = "claim"      // the objects that claim an identifier that a record of the run claims
	ruleDone      = "done"
)

// expRules lists the rules in order.
func expRules() []string {
	return []string{ruleAll, ruleOld, ruleRetireOut, ruleRetireIn, ruleKeyOld, ruleKeyNew, ruleLimitOut, ruleLimitIn, ruleClaim}
}

// expHook is what the tests of the expansion watch it by: a rule that Skip says is not done (its units still go by, and nothing is
// written), and the hook that is told every statement before it runs, as for the identification. It belongs to a Store; the code that
// runs leaves it nil.
type expHook struct {
	Skip func(rule string) bool
	Seen identObserver
}

// expState is the place of the expansion and what it counts, kept under "expand" in the counts of the run.
type expState struct {
	Full  bool                `json:"full"`
	Size  int                 `json:"size"`
	Cards []ingest.ExpandCard `json:"cards"`
	// Feeds are the feeds of the sources whose plan the run was given (ExpandStart.Feeds), kept so that a server that goes on knows the same.
	Feeds []ingest.ExpandFeed `json:"feeds"`
	Rule  string              `json:"rule"`
	// Cur is the place in the range of the rule (the last record number or object number done; for the rule all, the last source and
	// dataset and key), and Hi is where the range of the rule ends: the records of the run that the rules before it made are in it.
	Cur    int64               `json:"cur"`
	Cur2   int64               `json:"cur2"`
	CurKey string              `json:"cur_key"`
	Hi     int64               `json:"hi"`
	Units  int                 `json:"units"`
	Counts ingest.ExpandCounts `json:"counts"`
}

type expRun struct {
	kgNo  int64
	runNo int64
	st    *expState
	hook  *expHook
	// unsettled is how many pairs the identification of the run left (read of the counts of the run, not kept in the place): a run that
	// left some makes no candidate for retirement.
	unsettled int
}

func (e *expRun) t(table string) string { return pgschema.TableRef(e.kgNo, table) }

// feeds is the feeds of the sources the run was given, as the CTE feeds, and sources the sources they are of, as an array.
func (e *expRun) feeds() (cte, sources string) {
	feeds := append([]ingest.ExpandFeed(nil), e.st.Feeds...)
	sort.Slice(feeds, func(i, j int) bool {
		if feeds[i].SrcNo != feeds[j].SrcNo {
			return feeds[i].SrcNo < feeds[j].SrcNo
		}
		return feeds[i].Feed < feeds[j].Feed
	})
	var srcs, nos, creates, planned []string
	seen := map[int64]bool{}
	for _, f := range feeds {
		srcs, nos, creates = append(srcs, strconv.FormatInt(f.SrcNo, 10)), append(nos, strconv.Itoa(f.Feed)), append(creates, strconv.FormatBool(f.Creates))
		if !seen[f.SrcNo] {
			seen[f.SrcNo] = true
			planned = append(planned, strconv.FormatInt(f.SrcNo, 10))
		}
	}
	arr := func(items []string, typ string) string { return "'{" + strings.Join(items, ",") + "}'::" + typ + "[]" }
	return "feeds AS (SELECT src_no, feed_no, creates FROM unnest(" + arr(srcs, "int") + ", " + arr(nos, "int") + ", " + arr(creates, "boolean") + ") AS f(src_no, feed_no, creates))",
		arr(planned, "int")
}

// q writes the names of the tables and the numbers of the unit into a statement, and the link types that hold an end to one, the feeds
// and whether an object may be a candidate for retirement.
func (e *expRun) q(query string) string {
	var types, from, to []string
	for _, c := range e.st.Cards {
		types, from, to = append(types, strconv.Itoa(c.LinkTypeNo)), append(from, strconv.FormatBool(c.FromOne)), append(to, strconv.FormatBool(c.ToOne))
	}
	arr := func(items []string, typ string) string { return "'{" + strings.Join(items, ",") + "}'::" + typ + "[]" }
	cards := "cards AS (SELECT type_no, from_one, to_one FROM unnest(" + arr(types, "int") + ", " + arr(from, "boolean") + ", " + arr(to, "boolean") + ") AS c(type_no, from_one, to_one))"
	feeds, sources := e.feeds()
	return strings.NewReplacer(
		"{rec}", e.t(pgschema.TableCalcRecord), "{objrun}", e.t(pgschema.TableCalcObject), "{bind}", e.t(pgschema.TableCalcBind),
		"{end}", e.t(pgschema.TableCalcEnd), "{claim}", e.t(pgschema.TableCalcClaim), "{rref}", e.t(pgschema.TableRecordRef), "{cref}", e.t(pgschema.TableCalcRef),
		"{raw}", e.t(pgschema.TableRawRecord), "{link}", e.t(pgschema.TableLink), "{ds}", pgschema.TableSourceDataset, "{cards}", cards,
		"{retire}", e.t(pgschema.TableObjectRetire), "{obj}", e.t(pgschema.TableObject), "{xkey}", e.t(pgschema.TableCalcXKey), "{feeds}", feeds, "{sources}", sources,
		"{canretire}", strconv.FormatBool(e.unsettled == 0), "{WHY}", strconv.Itoa(int(pgschema.CalcWhyExpanded)),
	).Replace(query)
}

func (e *expRun) saw(tx *sql.Tx, what, query string, args ...any) {
	if e.hook != nil && e.hook.Seen != nil {
		e.hook.Seen(tx, what, query, args)
	}
}

func (e *expRun) skipped(rule string) bool {
	return e.hook != nil && e.hook.Skip != nil && e.hook.Skip(rule)
}

func (e *expRun) exec(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int, error) {
	q := e.q(query)
	e.saw(tx, what, q, args...)
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, dbErr(err, what)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (e *expRun) scanTwo(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int, int, error) {
	q := e.q(query)
	e.saw(tx, what, q, args...)
	var a, b sql.NullInt64
	err := tx.QueryRowContext(ctx, q, args...).Scan(&a, &b)
	return int(a.Int64), int(b.Int64), dbErr(err, what)
}

// BeginExpand starts the expansion: see ingest.Expander.
func (s *Store) BeginExpand(ctx context.Context, lease ingest.RunLease, start ingest.ExpandStart) error {
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
		if run.job == pgschema.CalcJobExpand {
			return kernel.Errorf(ingest.CodeRunStage, "the expansion of the run %d has begun already", lease.No)
		}
		if run.job != pgschema.CalcJobIdentify {
			return kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d is not done, so its records cannot be expanded", lease.No)
		}
		id, err := identOf(run.counts)
		if err != nil {
			return err
		}
		if id.Phase != phaseDone {
			return kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d is at %s, so its records cannot be expanded", lease.No, id.Phase)
		}
		st := expState{Full: start.Full, Size: orInt(start.Rows, ingest.DefaultExpandRows), Cards: start.Cards, Feeds: start.Feeds}
		e := &expRun{kgNo: kgNo, runNo: lease.No, st: &st, hook: s.expTest, unsettled: id.Counts.Unsettled}
		if err := e.enter(ctx, tx, ""); err != nil {
			return err
		}
		return writeExpand(ctx, tx, lease, &st, true)
	})
}

// writeExpand writes the place to the run, moves the number of the units on and lets the lease run on. The attempt is the condition.
func writeExpand(ctx context.Context, tx *sql.Tx, l ingest.RunLease, st *expState, begin bool) error {
	text, _ := json.Marshal(map[string]any{"expand": st}) // a place of numbers and words cannot fail to be written as text
	set := pgschema.ColCalcPart + " = " + pgschema.ColCalcPart + " + 1"
	if begin {
		set = pgschema.ColCalcJob + " = " + strconv.Itoa(int(pgschema.CalcJobExpand)) + ", " + pgschema.ColCalcPart + " = 0"
	}
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+set+", "+pgschema.ColCounts+" = "+pgschema.ColCounts+" || $3::jsonb, "+
		pgschema.ColLeaseUntil+" = "+lease("$4")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+
		string(ingest.RunComputing)+"'", l.No, l.Attempt, string(text), l.Term.Seconds())
	if err != nil {
		return dbErr(err, "write the place of the expansion")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

// loadExpand reads the place of the run in the transaction, checks the lease (the attempt) and that the tables of the computation are
// the run's, and that the expansion has begun.
func (s *Store) loadExpand(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (*expRun, error) {
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
	if run.job != pgschema.CalcJobExpand {
		return nil, kernel.Errorf(ingest.CodeRunStage, "the expansion of the run %d has not begun", lease.No)
	}
	var in struct {
		Expand expState `json:"expand"`
	}
	if err := json.Unmarshal(run.counts, &in); err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "read the place of the expansion")
	}
	// what the identification left is read of its counts every time (the place of the expansion does not keep it), so that a server that
	// goes on judges the candidates as the one that began did
	id, err := identOf(run.counts)
	if err != nil {
		return nil, err
	}
	return &expRun{kgNo: kgNo, runNo: lease.No, st: &in.Expand, hook: s.expTest, unsettled: id.Counts.Unsettled}, nil
}

// ExpandPlace says where the expansion stands: see ingest.Expander.
func (s *Store) ExpandPlace(ctx context.Context, lease ingest.RunLease) (ingest.ExpandPlace, error) {
	place := ingest.ExpandPlace{Stage: ingest.RunComputing}
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		run, err := readRun(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		if run.job != pgschema.CalcJobExpand {
			return nil
		}
		e, err := s.loadExpand(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		place.Begun, place.Finished, place.Rule, place.Unit = true, e.st.Rule == ruleDone, e.st.Rule, e.st.Units
		return nil
	})
	return place, err
}

// ExpandReport adds up what the expansion did: see ingest.Expander.
func (s *Store) ExpandReport(ctx context.Context, lease ingest.RunLease) (ingest.ExpandCounts, error) {
	var out ingest.ExpandCounts
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		e, err := s.loadExpand(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		out = e.st.Counts
		out.Unsettled = e.unsettled
		return nil
	})
	return out, err
}

// ExpandStep does the unit the place says: see ingest.Expander.
func (s *Store) ExpandStep(ctx context.Context, lease ingest.RunLease) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		e, err := s.loadExpand(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		if e.st.Rule == ruleDone {
			return kernel.Errorf(ingest.CodeRunStage, "the expansion of the run %d is done", lease.No)
		}
		if err := e.unit(ctx, tx); err != nil {
			return fmt.Errorf("%s: %w", e.st.Rule, err)
		}
		e.st.Units++
		return writeExpand(ctx, tx, lease, e.st, false)
	})
}

// enter puts the place at the start of the rule after the one given (the first rule for ""). A rule that goes through the records of the
// run ends at the last of them there are when it begins, so that the records it makes are not walked again by itself.
func (e *expRun) enter(ctx context.Context, tx *sql.Tx, after string) error {
	rules := expRules()
	next := 0
	for i, r := range rules {
		if r == after {
			next = i + 1
		}
	}
	for ; next < len(rules); next++ {
		if rules[next] == ruleAll && !e.st.Full {
			continue
		}
		break
	}
	st := e.st
	st.Cur, st.Cur2, st.CurKey, st.Hi = 0, 0, "", 0
	if next >= len(rules) {
		st.Rule = ruleDone
		return nil
	}
	st.Rule = rules[next]
	switch st.Rule {
	case ruleAll:
		st.Cur, st.Cur2 = -1, -1
	case ruleRetireOut, ruleRetireIn:
	default:
		var hi sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT max(rid) FROM "+e.t(pgschema.TableCalcRecord)).Scan(&hi); err != nil {
			return dbErr(err, "read the last record of the run")
		}
		st.Hi = hi.Int64
	}
	return nil
}

// unit does one unit of the rule the place is at and moves the place.
func (e *expRun) unit(ctx context.Context, tx *sql.Tx) error {
	switch e.st.Rule {
	case ruleAll:
		return e.everything(ctx, tx)
	case ruleRetireOut, ruleRetireIn:
		return e.retireUnit(ctx, tx)
	}
	st := e.st
	hi, last, err := e.recordEnd(ctx, tx)
	if err != nil {
		return err
	}
	if !e.skipped(st.Rule) {
		if err := e.recordRange(ctx, tx, st.Cur, hi); err != nil {
			return err
		}
	}
	if last {
		return e.enter(ctx, tx, st.Rule)
	}
	st.Cur = hi
	return nil
}

// recordEnd gives the end of the unit of a rule that goes through the records of the run: the number of the record that is Size records on
// from the place, and whether there are no more than that before the end of the rule. The unit is a number of records, not of numbers:
// the numbers of the records have gaps (a transaction that is rolled back leaves some), and a unit that is a range of numbers would
// hold fewer records than it says.
func (e *expRun) recordEnd(ctx context.Context, tx *sql.Tx) (hi int64, last bool, err error) {
	st := e.st
	var end sql.NullInt64
	query := e.q("SELECT rid FROM {rec} WHERE rid > $1 AND rid <= $2 ORDER BY rid OFFSET $3 LIMIT 1")
	e.saw(tx, "record range end", query, st.Cur, st.Hi, st.Size-1)
	err = tx.QueryRowContext(ctx, query, st.Cur, st.Hi, st.Size-1).Scan(&end)
	if isNoRows(err) {
		return st.Hi, true, nil
	}
	if err != nil {
		return 0, false, dbErr(err, "find the end of the unit")
	}
	// the record that ends the unit may be the last one of the rule
	return end.Int64, end.Int64 >= st.Hi, nil
}

// recordRange does the rule the place is at for the records of the run whose numbers are in the range.
func (e *expRun) recordRange(ctx context.Context, tx *sql.Tx, lo, hi int64) error {
	c := &e.st.Counts
	switch e.st.Rule {
	case ruleOld:
		retiring, objects, err := e.scanTwo(ctx, tx, "the objects the records were bound to", expOldSQL, lo, hi)
		c.Retiring += retiring
		c.OldObjects += objects
		return err
	case ruleKeyOld:
		return e.addTo(ctx, tx, &c.KeyRecords, "the records that read an end key the records had", expKeyOldSQL, lo, hi)
	case ruleKeyNew:
		return e.addTo(ctx, tx, &c.KeyRecords, "the records that read an end key the records give", expKeyNewSQL, lo, hi)
	case ruleLimitOut:
		return e.addTo(ctx, tx, &c.LimitRecords, "the records of the links that compete for a start", expLimitOutSQL, lo, hi)
	case ruleLimitIn:
		return e.addTo(ctx, tx, &c.LimitRecords, "the records of the links that compete for an end", expLimitInSQL, lo, hi)
	case ruleClaim:
		return e.addTo(ctx, tx, &c.ClaimObjects, "the objects that claim the same identifiers", expClaimSQL, lo, hi)
	}
	return kernel.Errorf(kernel.CodeInternal, "the expansion has no unit for %q", e.st.Rule)
}

// addTo runs a statement that adds rows and counts them, as the rows it changes.
func (e *expRun) addTo(ctx context.Context, tx *sql.Tx, to *int, what, query string, args ...any) error {
	n, err := e.exec(ctx, tx, what, query, args...)
	*to += n
	return err
}

// everything makes the records of a range of the raw records records of the run, for the computation of everything.
func (e *expRun) everything(ctx context.Context, tx *sql.Tx) error {
	st := e.st
	var src, ds sql.NullInt64
	var key sql.NullString
	endQuery := e.q("SELECT src_no, dataset_no, key FROM {raw} WHERE (src_no, dataset_no, key) > ($1, $2, $3) ORDER BY src_no, dataset_no, key OFFSET $4 LIMIT 1")
	e.saw(tx, "everything end", endQuery, st.Cur, st.Cur2, st.CurKey, st.Size)
	err := tx.QueryRowContext(ctx, endQuery, st.Cur, st.Cur2, st.CurKey, st.Size).Scan(&src, &ds, &key)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the unit")
	}
	cond, args := "(w.src_no, w.dataset_no, w.key) > ($1, $2, $3)", []any{st.Cur, st.Cur2, st.CurKey}
	if !last {
		cond += " AND (w.src_no, w.dataset_no, w.key) <= ($4, $5, $6)"
		args = append(args, src.Int64, ds.Int64, key.String)
	}
	// the records the identification reads: of the sources whose plan was given, kept under the declaration the dataset has now (the
	// ones of another declaration are not read by a declaration they were not made by, and a source without a plan is not touched)
	from, where := "{raw} w JOIN {ds} d ON d.dataset_no = w.dataset_no AND d.decl_rev = w.decl_rev", "w.src_no = ANY({sources}) AND "+cond
	if !e.skipped(ruleAll) {
		if err := e.addTo(ctx, tx, &st.Counts.Everything, "make every record a record of the run", expAddRecords("w", from, where), args...); err != nil {
			return err
		}
	}
	if last {
		return e.enter(ctx, tx, ruleAll)
	}
	st.Cur, st.Cur2, st.CurKey = src.Int64, ds.Int64, key.String
	return nil
}

// objectRange gives the end of the unit of a rule that goes through the objects of the run: the object that is Size objects on from the
// place, and whether there are no more than that.
func (e *expRun) objectRange(ctx context.Context, tx *sql.Tx) (hi int64, last bool, err error) {
	var end sql.NullInt64
	query := e.q("SELECT obj_no FROM {objrun} WHERE obj_no > $1 ORDER BY obj_no OFFSET $2 LIMIT 1")
	e.saw(tx, "object range end", query, e.st.Cur, e.st.Size)
	err = tx.QueryRowContext(ctx, query, e.st.Cur, e.st.Size).Scan(&end)
	if isNoRows(err) {
		return math.MaxInt64, true, nil
	}
	return end.Int64, false, dbErr(err, "find the end of the unit")
}
