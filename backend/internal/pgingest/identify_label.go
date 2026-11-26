package pgingest

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The search for the groups of a round (ingestion-at-scale-identity.md 2.2): the pairs (a record and a feed) that share a key are one
// group, and the smallest number of a group reaches all of it, a turn at a time.

// labels does a range of the pairs of a turn of the search for the groups. Turn 0 gives every pair its own number as the label; a
// later turn gives it the smallest label of the pairs that have a key in common with it. The turns go on until none changes.
func (r *identRun) labels(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	var end sql.NullInt64
	endQuery := r.q("SELECT pid FROM {slot} WHERE round_no = {R} AND pid > $1 ORDER BY pid OFFSET $2 LIMIT 1")
	r.saw(tx, "labels end", endQuery, st.Cur, st.Size)
	err := tx.QueryRowContext(ctx, endQuery, st.Cur, st.Size).Scan(&end)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the unit")
	}
	hi := int64(math.MaxInt64)
	if !last {
		hi = end.Int64
	}
	if err := r.labelPairs(ctx, tx, hi); err != nil {
		return err
	}
	if !last {
		st.Cur = hi
		return nil
	}
	st.Cur = 0
	switch {
	case st.Turn == 0 || (st.Changed > 0 && st.Turn < st.Turns):
		st.Turn, st.Changed = st.Turn+1, 0
		st.Phase, st.Cur, st.Cur2, st.CurKey = phaseKLabels, -1, 0, ""
		return r.analyze(ctx, tx, pgschema.TableCalcLabel)
	case st.Changed > 0:
		// the turns did not end the search: Go ends it for the groups that are not whole. The labels of the last turn are measured first,
		// as at every turn: a page of Go's read is planned on them, and a plan made on a turn the planner does not know reads the labels
		// that are left after the page (ADR-0116)
		st.Phase, st.Cur, st.Cur2, st.Skipped = phaseSettle, 0, 0, false
		return r.analyze(ctx, tx, pgschema.TableCalcLabel)
	default:
		st.Unsettled = false
		st.Phase, st.Cur = phaseDecide, 0
		return r.analyze(ctx, tx, pgschema.TableCalcLabel, pgschema.TableCalcBind)
	}
}

// labelPairs gives the label of the pairs of a range for the turn.
func (r *identRun) labelPairs(ctx context.Context, tx *sql.Tx, hi int64) error {
	st := r.st
	if st.Turn == 0 {
		_, err := r.exec(ctx, tx, "label the pairs", r.q("INSERT INTO {label} (round_no, iter, pid, label) SELECT {R}, 0, pid, pid FROM {slot} "+
			"WHERE round_no = {R} AND pid > $1 AND pid <= $2 GROUP BY pid"), st.Cur, hi)
		return err
	}
	// the label a pair gets is the smallest label of its keys, and then the label that group had at the turn before (the label of a pair
	// is the number of a pair of the same group, so it is a place to jump to): a group that is a chain of keys is followed by the
	// jumps, so the turns it takes fall, but they depend on the diameter of the group (Go ends what they do not)
	last := "j" + strconv.Itoa(labelJumps)
	// the labels of the turn before the last one are not read again: a unit takes them away for its range, so that a round keeps at most
	// two labels of each pair
	if st.Turn >= 2 {
		if _, err := r.exec(ctx, tx, "take away the labels that are not read again", r.q("DELETE FROM {label} WHERE round_no = {R} AND iter = $3 AND pid > $1 AND pid <= $2"), st.Cur, hi, st.Turn-2); err != nil {
			return err
		}
	}
	changed, err := r.scanInt(ctx, tx, "label the pairs", r.q("WITH nl AS (SELECT s.pid, min(k.label) AS label FROM {slot} s"+
		probe("x.label", "{klabel}", "k", "x.round_no = {R} AND x.iter = $3 AND x.type_no = s.type_no AND x.ctx_no = s.ctx_no AND x.sig_hash = s.sig_hash")+
		" WHERE s.round_no = {R} AND s.pid > $1 AND s.pid <= $2 GROUP BY s.pid), "+
		"jl AS (SELECT nl.pid, "+last+".label AS label FROM nl"+jumpsSQL()+"), "+
		"ins AS (INSERT INTO {label} (round_no, iter, pid, label) SELECT {R}, $3, pid, label FROM jl RETURNING 1) "+
		"SELECT count(*) FROM jl"+probe("x.label", "{label}", "p", "x.round_no = {R} AND x.iter = $3 - 1 AND x.pid = jl.pid")+" WHERE p.label <> jl.label"), st.Cur, hi, st.Turn)
	st.Changed += int(changed)
	return err
}

// labelJumps is how many places a label is followed to at a turn of the search for the groups: the label a pair got is the number of a
// pair of its group, that pair has a label from the turn before, and that is the number of another pair of the group, and so on.
const labelJumps = 3

// jumpsSQL follows the label of a pair (nl.label) labelJumps places, each at the turn before (every place is one probe of an index, and
// a label is never larger than the number it is looked up by, so the last place is the smallest).
func jumpsSQL() string {
	var b strings.Builder
	from := "nl"
	for i := 1; i <= labelJumps; i++ {
		at := "j" + strconv.Itoa(i)
		b.WriteString(probe("x.label", "{label}", at, fmt.Sprintf("x.round_no = {R} AND x.iter = $3 - 1 AND x.pid = %s.label", from)))
		from = at
	}
	return b.String()
}

// keyLabels is the first step of a turn: the label of each key of a range of keys, the smallest label the records that have the key got
// at the turn before. A unit is the keys from the last one done to one that is size slots on, with all the slots of the keys in it.
func (r *identRun) keyLabels(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	var no, ctxNo sql.NullInt64
	var hash sql.NullString
	endQuery := r.q("SELECT type_no, ctx_no, encode(sig_hash, 'hex') FROM {slot} WHERE round_no = {R} AND (type_no, ctx_no, sig_hash) > ($1, $2, decode($3, 'hex')) " +
		"ORDER BY type_no, ctx_no, sig_hash OFFSET $4 LIMIT 1")
	r.saw(tx, "keys end", endQuery, st.Cur, st.Cur2, st.CurKey, st.Size)
	err := tx.QueryRowContext(ctx, endQuery, st.Cur, st.Cur2, st.CurKey, st.Size).Scan(&no, &ctxNo, &hash)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the unit")
	}
	cond := "(s.type_no, s.ctx_no, s.sig_hash) > ($1, $2, decode($3, 'hex'))"
	args := []any{st.Cur, st.Cur2, st.CurKey, st.Turn}
	if !last {
		cond += " AND (s.type_no, s.ctx_no, s.sig_hash) <= ($5, $6, decode($7, 'hex'))"
		args = append(args, no.Int64, ctxNo.Int64, hash.String)
	}
	if _, err := r.exec(ctx, tx, "label the keys", r.q("INSERT INTO {klabel} (round_no, iter, type_no, ctx_no, sig_hash, label) "+
		"SELECT {R}, $4, s.type_no, s.ctx_no, s.sig_hash, min(l.label) FROM {slot} s"+
		probe("x.label", "{label}", "l", "x.round_no = {R} AND x.iter = $4 - 1 AND x.pid = s.pid")+" WHERE s.round_no = {R} AND "+cond+
		" GROUP BY s.type_no, s.ctx_no, s.sig_hash"), args...); err != nil {
		return err
	}
	if last {
		st.Phase, st.Cur, st.Cur2, st.CurKey = phaseLabels, 0, 0, ""
		return r.analyze(ctx, tx, pgschema.TableCalcKLabel)
	}
	st.Cur, st.Cur2, st.CurKey = no.Int64, ctxNo.Int64, hash.String
	return nil
}

// pairsSQL is the pairs of the groups of a range of labels of the last turn of the round (pl), and the slot of each (ps). When the
// round leaves some groups (leftSQL), the first statement of a unit leaves them, and they are not in pl: a group that is left has a
// reference of each of its pairs, so one probe of the reference of the pair that is its label tells it. (When the search was stopped,
// the pair whose number is the label of a piece may have a smaller label: it is in a piece that is not whole either, whose label is
// smaller, so it was left by this unit or an earlier one.)
func pairsSQL(leaving bool) string {
	base := "SELECT l.pid, l.label FROM {label} l"
	cond := " WHERE l.round_no = {R} AND l.iter = {F} AND l.label > $1 AND l.label <= $2"
	if leaving {
		feeds := strconv.Itoa(ingest.MaxIdentifyFeeds)
		base += absent("{ref}", "lr", "x.rid = l.label / "+feeds+" AND x.role = "+strconv.Itoa(int(roleIdentity))+" AND x.def_no = l.label % "+feeds)
		cond += " AND lr.hit IS NULL"
	}
	return "pl AS (" + base + cond + "), " + psSQL
}

// leftSQL are the CTEs that name the labels of the range whose groups the round leaves (gone): the groups that are not known to be
// whole, when the search for the groups was stopped by its limit before it was done (bad), and the groups that share a key with a pair
// an earlier round left, when one did (nb). A group is left whole, so what shares a key with a pair of it is left with it whatever the
// round or the unit it is in (ADR-0116).
func leftSQL(unsettled, neighbors bool) string {
	ctes := keysSQL
	var from []string
	if unsettled {
		ctes += wholeSQL
		from = append(from, "SELECT label FROM bad")
	}
	if neighbors {
		ctes += neighborsSQL()
		from = append(from, "SELECT label FROM nb")
	}
	return ctes + "gone AS (" + strings.Join(from, " UNION ") + "), "
}

// psSQL is the slot of each pair of pl, by one probe of the slots for each (a range of the labels is a range of the pairs, and the
// slots of the round are as many as the run).
const psSQL = "ps AS (SELECT pl.label, s.rid, s.src_no, s.feed_no, s.pid, s.key_ord, s.type_no, s.ctx_no, s.sig_hash, s.found_no FROM pl" +
	" CROSS JOIN LATERAL (SELECT x.* FROM {slot} x WHERE x.round_no = {R} AND x.pid = pl.pid OFFSET 0) s)"

// keysSQL are the pairs of a range of labels with their slots (sk: a row for each key of a pair) and the keys among them (kk). The range
// of the labels is read by its index and each pair's slots by theirs, so what it reads is the pairs of the range.
const keysSQL = "sk AS (SELECT l.pid, l.label, s.rid, s.feed_no, s.type_no, s.ctx_no, s.sig_hash FROM {label} l" +
	" CROSS JOIN LATERAL (SELECT x.rid, x.feed_no, x.type_no, x.ctx_no, x.sig_hash FROM {slot} x WHERE x.round_no = {R} AND x.pid = l.pid OFFSET 0) s" +
	" WHERE l.round_no = {R} AND l.iter = {F} AND l.label > $1 AND l.label <= $2), " +
	"kk AS (SELECT DISTINCT type_no, ctx_no, sig_hash FROM sk), "

// wholeSQL are the CTEs, after keysSQL, that find the groups of the range that are not known to be whole: when the search for the
// groups was stopped by its limit before it was done, a group is a piece of a group that is not whole yet if one of its keys is also a
// key of a pair that has another label (sp: each key's pairs by the index of the keys, and the label of each by the index of the pairs),
// and bad is the labels of the groups that have such a key. Only the groups that no other group shares a key with are known to be whole.
const wholeSQL = "sp AS (SELECT kk.type_no, kk.ctx_no, kk.sig_hash FROM kk" +
	" CROSS JOIN LATERAL (SELECT x.pid FROM {slot} x WHERE x.round_no = {R} AND x.type_no = kk.type_no AND x.ctx_no = kk.ctx_no AND x.sig_hash = kk.sig_hash OFFSET 0) t" +
	" CROSS JOIN LATERAL (SELECT x.label FROM {label} x WHERE x.round_no = {R} AND x.iter = {F} AND x.pid = t.pid LIMIT 1) u" +
	" GROUP BY kk.type_no, kk.ctx_no, kk.sig_hash HAVING min(u.label) <> max(u.label)), " +
	"bad AS (SELECT DISTINCT sk.label FROM sk JOIN sp ON sp.type_no = sk.type_no AND sp.ctx_no = sk.ctx_no AND sp.sig_hash = sk.sig_hash), "

// unsettledSQL are the CTEs that find the groups of a range of labels that are not known to be whole, with the pairs of the range (sk).
const unsettledSQL = keysSQL + wholeSQL

// neighborsSQL are the CTEs, after keysSQL, that find the groups of the range that share a key with a pair an earlier round left: a key
// is looked at once for each earlier round (at most sixteen), by the index of the keys of that round's slots, for a slot whose pair has
// a reference of its identity (it was left, or no object could be told for it). The pairs of a round that share a key are one group and
// a group is in one unit, so a key is read once in a round. nb is the labels of the groups that have such a key.
func neighborsSQL() string {
	return "lk AS (SELECT DISTINCT kk.type_no, kk.ctx_no, kk.sig_hash FROM kk CROSS JOIN LATERAL generate_series(1, {R} - 1) pr(r)" +
		probe("1", "{slot}", "t", "x.round_no = pr.r AND x.type_no = kk.type_no AND x.ctx_no = kk.ctx_no AND x.sig_hash = kk.sig_hash AND EXISTS (SELECT 1 FROM {ref} z "+
			"WHERE z.rid = x.rid AND z.role = "+strconv.Itoa(int(roleIdentity))+" AND z.def_no = x.feed_no)") + "), " +
		"nb AS (SELECT DISTINCT sk.label FROM sk JOIN lk ON lk.type_no = sk.type_no AND lk.ctx_no = sk.ctx_no AND lk.sig_hash = sk.sig_hash), "
}
