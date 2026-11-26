package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The writing of the pages of the computation of the links.

func (r *rclRun) endArgs(page ingest.RecomputedLinksPage) []any {
	cur, to := r.st.Cur, page.Through
	return []any{cur.Def, cur.Hash, cur.Rid, to.Def, to.Hash, to.Rid}
}

// putEnds gives the records of the page's range that the run only reads again the ends their references say.
func (r *rclRun) putEnds(ctx context.Context, tx *sql.Tx, page ingest.RecomputedLinksPage) error {
	n, err := r.exec(ctx, tx, "give the records that are read again their ends", rclCarrySQL, r.st.Cur.Rid, page.Through.Rid)
	r.st.Counts.Carried += n
	return err
}

// putTo writes the references of the To ends of the page's range.
func (r *rclRun) putTo(ctx context.Context, tx *sql.Tx, page ingest.RecomputedLinksPage) error {
	n, err := r.exec(ctx, tx, "write the references of the To ends", rclToSQL, r.endArgs(page)...)
	r.st.Counts.Refs += n
	return err
}

// putConflicts says the records that made a link with other attributes than the next record that made it, for the links of the range.
func (r *rclRun) putConflicts(ctx context.Context, tx *sql.Tx, page ingest.RecomputedLinksPage) error {
	n, err := r.exec(ctx, tx, "say which records made a link with other attributes", rclConflictsSQL, r.st.Cur.Ref, page.Through.Ref)
	r.st.Counts.Conflicts += n
	r.st.Counts.Problems += n
	return err
}

// putCells says the cells of the records that are used for the links of the range that gave no value.
func (r *rclRun) putCells(ctx context.Context, tx *sql.Tx, page ingest.RecomputedLinksPage) error {
	n, err := r.exec(ctx, tx, "say the cells of the links that gave no value", rclCellsSQL, r.st.Cur.Ref, page.Through.Ref)
	r.st.Counts.Cells += n
	r.st.Counts.Problems += n
	return err
}

// putFrom writes a page of the From side: the keys that were counted, the problems of the ends that found no object or too many, the links
// (the ones that are used now, written in place of the one used before when another record's end made the same link and comes after it),
// the records that lost, and the reference each end gets.
func (r *rclRun) putFrom(ctx context.Context, tx *sql.Tx, page ingest.RecomputedLinksPage) error {
	c := &r.st.Counts
	c.Ends += page.Ends
	c.Skipped += page.Skipped
	if err := r.putKeys(ctx, tx, page.Keys); err != nil {
		return err
	}
	if err := r.putEndProblems(ctx, tx, page.Problems); err != nil {
		return err
	}
	refs := map[string]bool{}
	for _, cand := range page.Candidates {
		refs[cand.Row.Ref] = true
	}
	holders, err := r.holders(ctx, tx, sortedKeys(refs))
	if err != nil {
		return err
	}
	election := ingest.ElectLinks(holders, page.Candidates)
	nos, err := r.numbers(ctx, tx, election, holders)
	if err != nil {
		return err
	}
	if err := r.putWins(ctx, tx, election, nos); err != nil {
		return err
	}
	if err := r.putLost(ctx, tx, election.Lost); err != nil {
		return err
	}
	c.Lost += len(election.Lost)
	return r.putFromRefs(ctx, tx, page, election, nos)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// putKeys keeps what the To side has for the keys the page counted.
func (r *rclRun) putKeys(ctx context.Context, tx *sql.Tx, keys []ingest.LinkKeyCount) error {
	if len(keys) == 0 {
		return nil
	}
	var srcs, defs, hashes, targets, objs []string
	for _, k := range keys {
		srcs, defs, hashes = append(srcs, strconv.FormatInt(k.SrcNo, 10)), append(defs, strconv.Itoa(k.Def)), append(hashes, hexOf(k.Hash[:]))
		targets, objs = append(targets, strconv.Itoa(k.Targets)), append(objs, strconv.FormatInt(k.Obj, 10))
	}
	n, err := r.exec(ctx, tx, "keep what the To side has for the keys", rclKeysSQL, textArray(srcs), textArray(defs), textArray(hashes), textArray(targets), textArray(objs))
	r.st.Counts.Keys += n
	return err
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, x := range b {
		out = append(out, digits[x>>4], digits[x&15])
	}
	return string(out)
}

// putEndProblems writes the problems of the ends that found no object or more than one (the records are the ones of the run).
func (r *rclRun) putEndProblems(ctx context.Context, tx *sql.Tx, problems []ingest.RecomputedProblem) error {
	if len(problems) == 0 {
		return nil
	}
	var srcs, dss, keys, feeds, fields, kinds, reasons []string
	c := &r.st.Counts
	for _, p := range problems {
		srcs, dss, keys = append(srcs, strconv.FormatInt(p.SrcNo, 10)), append(dss, strconv.FormatInt(p.DatasetNo, 10)), append(keys, p.Key)
		// the problem of an end is of its link definition (ADR-0128)
		feeds, fields, kinds, reasons = append(feeds, strconv.Itoa(pgschema.ProblemDefOfLink(p.Feed))), append(fields, p.Field), append(kinds, p.Kind), append(reasons, p.Reason)
		switch p.Kind {
		case ingest.ProblemLinkTarget:
			c.Missing++
		case ingest.ProblemLinkAmbiguous:
			c.Ambiguous++
		default:
			return kernel.Errorf(kernel.CodeInternal, "the problem kind %q is not one the computation of the links gives for an end", p.Kind)
		}
	}
	n, err := r.exec(ctx, tx, "write the problems of the ends", rcpProblemsSQL, textArray(srcs), textArray(dss), textArray(keys), textArray(feeds), textArray(fields), textArray(kinds), textArray(reasons))
	if err != nil {
		return err
	}
	if n != len(problems) {
		return kernel.Errorf(kernel.CodeInternal, "%d problems were made for the ends and %d were written", len(problems), n)
	}
	c.Problems += n
	return nil
}

// holders reads the records that are used for the links of a list, as the earlier pages left them.
func (r *rclRun) holders(ctx context.Context, tx *sql.Tx, refs []string) (map[string]ingest.LinkHolder, error) {
	out := map[string]ingest.LinkHolder{}
	if len(refs) == 0 {
		return out, nil
	}
	query := r.q(rclHoldersSQL)
	r.saw(tx, "read the records that are used for the links", query, textArray(refs))
	rows, err := tx.QueryContext(ctx, query, textArray(refs))
	if err != nil {
		return nil, dbErr(err, "read the records that are used for the links")
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ref string
		var h ingest.LinkHolder
		o := &h.Who.Order
		if err := rows.Scan(&ref, &h.Who.Rid, &o.Def, &o.SrcNo, &o.DatasetNo, &o.Key, &o.Held, &h.Who.Sig, &h.Who.Shown, &h.Who.LinkName, &h.Who.DefID, &h.Who.Dataset, &h.No); err != nil {
			return nil, dbErr(err, "read a record that is used for a link")
		}
		out[ref] = h
	}
	return out, dbErr(rows.Err(), "read the records that are used for the links")
}

// numbers gives the links that are written their numbers: the one a link has (it is written by an earlier page, or the KG has it open),
// else a new one, in the order of the ids of the links.
func (r *rclRun) numbers(ctx context.Context, tx *sql.Tx, e ingest.LinkElection, holders map[string]ingest.LinkHolder) (map[string]int64, error) {
	nos := map[string]int64{}
	var asked []string
	for _, w := range e.Wins {
		if h, ok := holders[w.Row.Ref]; ok {
			nos[w.Row.Ref] = h.No
		} else {
			asked = append(asked, w.Row.Ref)
		}
	}
	if len(asked) == 0 {
		return nos, nil
	}
	query := r.q(rclRegistrySQL)
	r.saw(tx, "find the numbers the open links have", query, textArray(asked))
	rows, err := tx.QueryContext(ctx, query, textArray(asked))
	if err != nil {
		return nil, dbErr(err, "find the numbers the open links have")
	}
	for rows.Next() {
		var ref string
		var no int64
		if err := rows.Scan(&ref, &no); err != nil {
			_ = rows.Close()
			return nil, dbErr(err, "read the number of an open link")
		}
		nos[ref] = no
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, dbErr(err, "find the numbers the open links have")
	}
	_ = rows.Close()
	var fresh []string
	for _, ref := range asked {
		if _, ok := nos[ref]; !ok {
			fresh = append(fresh, ref)
		}
	}
	return nos, r.allocate(ctx, tx, fresh, nos)
}

// allocate gives the links that have no number the next numbers, in the order of their ids.
func (r *rclRun) allocate(ctx context.Context, tx *sql.Tx, fresh []string, nos map[string]int64) error {
	if len(fresh) == 0 {
		return nil
	}
	sort.Strings(fresh)
	r.saw(tx, "take the numbers of new links", rclNumbersSQL, len(fresh))
	rows, err := tx.QueryContext(ctx, rclNumbersSQL, len(fresh))
	if err != nil {
		return dbErr(err, "take the numbers of new links")
	}
	defer func() { _ = rows.Close() }()
	i := 0
	for rows.Next() {
		if i >= len(fresh) {
			return kernel.Errorf(kernel.CodeInternal, "more numbers of links were given than were asked for")
		}
		var no int64
		if err := rows.Scan(&no); err != nil {
			return dbErr(err, "read a number of a link")
		}
		nos[fresh[i]] = no
		i++
	}
	if err := rows.Err(); err != nil {
		return dbErr(err, "take the numbers of new links")
	}
	if i != len(fresh) {
		return kernel.Errorf(kernel.CodeInternal, "%d numbers of links were asked for and %d were given", len(fresh), i)
	}
	return nil
}

// rclWhoCols are the columns the records that make links have, one list for each, to write them with a statement.
type rclWhoCols struct{ ref, rid, def, src, ds, key, held, sig, shown, lname, defid, dsid []string }

func (c *rclWhoCols) add(ref string, w ingest.LinkWho) {
	o := w.Order
	c.ref, c.rid, c.def, c.src, c.ds = append(c.ref, ref), append(c.rid, strconv.FormatInt(w.Rid, 10)), append(c.def, strconv.Itoa(o.Def)), append(c.src, strconv.FormatInt(o.SrcNo, 10)), append(c.ds, strconv.FormatInt(o.DatasetNo, 10))
	c.key, c.held, c.sig, c.shown = append(c.key, o.Key), append(c.held, strconv.FormatBool(o.Held)), append(c.sig, w.Sig), append(c.shown, w.Shown)
	c.lname, c.defid, c.dsid = append(c.lname, w.LinkName), append(c.defid, w.DefID), append(c.dsid, w.Dataset)
}

func (c *rclWhoCols) args() []any {
	var out []any
	for _, l := range [][]string{c.ref, c.rid, c.def, c.src, c.ds, c.key, c.held, c.sig, c.shown, c.lname, c.defid, c.dsid} {
		out = append(out, textArray(l))
	}
	return out
}

// putWins writes the links that are used now to stage_link and to the table of the records that make them. A link that an earlier page
// wrote is written over (the record that is used for it is another one now). The links it counts are the ones that were not written.
func (r *rclRun) putWins(ctx context.Context, tx *sql.Tx, e ingest.LinkElection, nos map[string]int64) error {
	if len(e.Wins) == 0 {
		return nil
	}
	var no, def, typ, src, dst []string
	var grade, basis, doc, refs, ch, rh, left []string
	var who rclWhoCols
	for _, w := range e.Wins {
		row := w.Row
		no, def, typ = append(no, strconv.FormatInt(nos[row.Ref], 10)), append(def, strconv.Itoa(row.Def)), append(typ, strconv.Itoa(row.TypeNo))
		src, dst = append(src, strconv.FormatInt(row.Src, 10)), append(dst, strconv.FormatInt(row.Dst, 10))
		grade, basis, doc, refs = append(grade, row.Grade), append(basis, row.Basis), append(doc, string(row.Doc)), append(refs, string(row.SourceRefs))
		ch, rh = append(ch, row.ContentHash.String()), append(rh, row.RowHash.String())
		cells, _ := json.Marshal(append([]string{}, w.Who.Left...)) // a list of words cannot fail to be written as text
		left = append(left, string(cells))
		who.add(row.Ref, w.Who)
	}
	refList := who.ref
	rows, err := r.insertedRows(ctx, tx, "write the links", rclLinksSQL, textArray(no), textArray(refList), textArray(def), textArray(typ), textArray(src), textArray(dst),
		textArray(grade), textArray(basis), textArray(doc), textArray(refs), textArray(ch), textArray(rh))
	if err != nil {
		return err
	}
	r.st.Counts.Links += rows
	args := append(who.args(), textArray(no), textArray(left))
	_, err = r.exec(ctx, tx, "write the records that make the links", rclWinSQL, args...)
	return err
}

// insertedRows runs a statement that gives (xmax = 0) for every row it wrote, and counts the rows that were new.
func (r *rclRun) insertedRows(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int, error) {
	q := r.q(query)
	r.saw(tx, what, q, args...)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, dbErr(err, what)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var fresh bool
		if err := rows.Scan(&fresh); err != nil {
			return 0, dbErr(err, what)
		}
		if fresh {
			n++
		}
	}
	return n, dbErr(rows.Err(), what)
}

// putLost writes the records that lost.
func (r *rclRun) putLost(ctx context.Context, tx *sql.Tx, lost []ingest.LinkLost) error {
	if len(lost) == 0 {
		return nil
	}
	var who rclWhoCols
	for _, l := range lost {
		who.add(l.Ref, l.Who)
	}
	n, err := r.exec(ctx, tx, "write the records that lost", rclLostSQL, who.args()...)
	if err != nil {
		return err
	}
	if n != len(lost) {
		return kernel.Errorf(kernel.CodeInternal, "%d records lost a link and %d were written", len(lost), n)
	}
	return nil
}

// putFromRefs writes the reference of each end of the page and makes the references of the records that were used before not solved.
func (r *rclRun) putFromRefs(ctx context.Context, tx *sql.Tx, page ingest.RecomputedLinksPage, e ingest.LinkElection, nos map[string]int64) error {
	state := func(held bool) int16 {
		if held {
			return pgschema.RefHeld
		}
		return pgschema.RefUnresolved
	}
	var rids, defs, hashes, links, states []string
	for _, res := range page.Results {
		var link int64
		st := state(res.Held)
		if res.Ref != "" && e.Used[ingest.LinkUse{Rid: res.Rid, Def: res.Def}] {
			link, st = nos[res.Ref], pgschema.RefResolved
		}
		rids, defs, hashes = append(rids, strconv.FormatInt(res.Rid, 10)), append(defs, strconv.Itoa(res.Def)), append(hashes, hexOf(res.Hash[:]))
		links, states = append(links, strconv.FormatInt(link, 10)), append(states, strconv.Itoa(int(st)))
	}
	if len(rids) > 0 {
		n, err := r.exec(ctx, tx, "write the references of the From ends", rclFromRefsSQL, textArray(rids), textArray(defs), textArray(hashes), textArray(links), textArray(states))
		if err != nil {
			return err
		}
		r.st.Counts.Refs += n
	}
	if len(e.Displaced) == 0 {
		return nil
	}
	var frids, fdefs, fstates []string
	for _, d := range e.Displaced {
		frids, fdefs, fstates = append(frids, strconv.FormatInt(d.Who.Rid, 10)), append(fdefs, strconv.Itoa(d.Who.Order.Def)), append(fstates, strconv.Itoa(int(state(d.Who.Order.Held))))
	}
	n, err := r.exec(ctx, tx, "make the references of the records that are not used any more not solved", rclFlipSQL, textArray(frids), textArray(fdefs), textArray(fstates))
	if err == nil && n != len(e.Displaced) {
		err = kernel.Errorf(kernel.CodeInternal, "%d records were used for a link and are not now, and %d references were changed", len(e.Displaced), n)
	}
	return err
}
