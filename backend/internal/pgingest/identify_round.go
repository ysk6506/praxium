package pgingest

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// A round of the identification (ingestion-at-scale-identity.md 2.2): the keys whose parents are found become slots, the records that
// share a slot's key become groups (the smallest number of a group reaches all of it, a turn at a time), and every group gets an
// object. A round ends with the objects of the records that did not have a parent when it began, so the next round finds them.

// q writes the names of the tables and the numbers of the unit into a statement.
func (r *identRun) q(query string) string {
	pairs := []string{
		"{slot}", r.t(pgschema.TableCalcSlot), "{label}", r.t(pgschema.TableCalcLabel), "{klabel}", r.t(pgschema.TableCalcKLabel), "{bind}", r.t(pgschema.TableCalcBind),
		"{key}", r.t(pgschema.TableCalcKey), "{newkey}", r.t(pgschema.TableCalcNewKey), "{dropped}", r.t(pgschema.TableCalcDropped),
		"{newobj}", r.t(pgschema.TableCalcNewObj), "{ask}", r.t(pgschema.TableCalcAsk), "{ref}", r.t(pgschema.TableCalcRef),
		"{prob}", r.t(pgschema.TableCalcProblem), "{obj}", r.t(pgschema.TableObject), "{okey}", r.t(pgschema.TableObjectKey),
		"{rec}", r.t(pgschema.TableCalcRecord), "{claim}", r.t(pgschema.TableCalcClaim), "{ident}", r.t(pgschema.TableObjectIdentifier),
		"{R}", strconv.Itoa(r.st.Round), "{F}", strconv.Itoa(r.st.Turn), "{feeds}", r.feedSQL(),
	}
	return strings.NewReplacer(pairs...).Replace(query)
}

// lookupSQL is the object the index has for a key: the identification index, else a key bound in an earlier round of the run.
func lookupSQL(typ, ctx, sig string) string {
	return "COALESCE((SELECT o.obj_no FROM {okey} o WHERE o.type_no = " + typ + " AND o.ctx_no = " + ctx + " AND o.sig_hash = " + sig + "), " +
		"(SELECT n.obj_no FROM {newkey} n WHERE n.type_no = " + typ + " AND n.ctx_no = " + ctx + " AND n.sig_hash = " + sig + "))"
}

// chainSQL is the lateral joins that find the parents of a key (alias k, with anc_types and anc_sigs), from the farthest down to the
// nearest: the parent of the key is the object the nearest one finds, and it is NULL when one of them is not found. A key with no
// parent has the context 0. The columns d.n (how many parents) and ctx are the result.
func chainSQL() string {
	var b strings.Builder
	b.WriteString(" CROSS JOIN LATERAL (SELECT COALESCE(cardinality(k.anc_types), 0) AS n) d")
	for i := 4; i >= 1; i-- {
		ctx := "0"
		if i < 4 {
			ctx = "COALESCE(l" + strconv.Itoa(i+1) + ".o, 0)"
		}
		cond := "d.n >= " + strconv.Itoa(i)
		if i < 4 {
			cond += " AND (d.n < " + strconv.Itoa(i+1) + " OR l" + strconv.Itoa(i+1) + ".o IS NOT NULL)"
		}
		idx := strconv.Itoa(i)
		fmt.Fprintf(&b, " LEFT JOIN LATERAL (SELECT %s AS o WHERE %s) l%d ON true", lookupSQL("k.anc_types["+idx+"]", ctx, "k.anc_sigs["+idx+"]"), cond, i)
	}
	return b.String()
}

// ctxSQL is the context of the key that chainSQL found, NULL when it is not found.
const ctxSQL = "(CASE WHEN d.n = 0 THEN 0 WHEN d.n > 4 THEN NULL ELSE l1.o END)"

// absent is a join that asks, with one probe of the index of a table, whether a row with the condition is there: the alias has the
// column hit, and it is NULL when there is none. The condition is written on x (the table) and on what is to the left of it. A
// statement of a unit asks the big tables of the run this way and no other, so that what it reads is the probes of its own rows: a
// join the server is free to take the other way is the join that reads the whole of the table (ADR-0108).
func absent(table, alias, cond string) string {
	return " LEFT JOIN LATERAL (SELECT 1 AS hit FROM " + table + " x WHERE " + cond + " LIMIT 1) " + alias + " ON true"
}

// probe is a join with the one row of a table that the condition finds by its index (cols are written on x), the same way as absent.
func probe(cols, table, alias, cond string) string {
	return " CROSS JOIN LATERAL (SELECT " + cols + " FROM " + table + " x WHERE " + cond + " LIMIT 1) " + alias
}

// scan is probe for a table that may give more than one row for the condition.
func scan(cols, table, alias, cond string) string {
	return " CROSS JOIN LATERAL (SELECT " + cols + " FROM " + table + " x WHERE " + cond + " OFFSET 0) " + alias
}

// slot makes the slots of a range of the records: the keys of the records that are not bound yet and whose feed makes objects, with
// the object the index has for each. A record is ready when all its keys have their parents, or, in a round that takes what it can,
// when one has.
func (r *identRun) slot(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	if st.Cur >= st.MaxRid || st.Round > st.Rounds {
		return r.endSlots(ctx, tx)
	}
	hi := st.Cur + int64(st.Size)
	ready := "bool_and(c IS NOT NULL)"
	if st.Relaxed {
		ready = "bool_or(c IS NOT NULL)"
	}
	id := strconv.Itoa(int(roleIdentity))
	if st.LeftKeys && st.Round > 1 {
		if err := r.leaveNeighbors(ctx, tx, hi); err != nil {
			return err
		}
	}
	// whether a pair is ready is told by a window over its keys and not by a join with them: a join of the two is the one the server may
	// take as a loop of the one over the other when it expects few rows, which is the square of the unit
	n, err := r.exec(ctx, tx, "make the slots", r.q("WITH {feeds}, ks AS (SELECT k.rid, rc.src_no, k.feed_no, k.key_ord, k.type_no, k.sig_hash, "+ctxSQL+" AS c FROM {key} k"+
		probe("x.src_no", "{rec}", "rc", "x.rid = k.rid")+" JOIN feeds f ON f.src_no = rc.src_no AND f.feed_no = k.feed_no AND f.creates"+chainSQL()+
		absent("{bind}", "hb", "x.rid = k.rid AND x.feed_no = k.feed_no")+absent("{ref}", "hr", "x.rid = k.rid AND x.def_no = k.feed_no AND x.role = "+id)+
		" WHERE NOT k.held AND k.rid > $1 AND k.rid <= $2 AND hb.hit IS NULL AND hr.hit IS NULL), "+
		"rk AS (SELECT ks.*, "+ready+" OVER (PARTITION BY rid, feed_no) AS ready FROM ks), "+
		"found AS (SELECT rk.*, "+lookupSQL("rk.type_no", "rk.c", "rk.sig_hash")+" AS f FROM rk WHERE rk.ready AND rk.c IS NOT NULL) "+
		"INSERT INTO {slot} (rid, src_no, feed_no, pid, key_ord, type_no, ctx_no, sig_hash, round_no, found_no) "+
		"SELECT found.rid, found.src_no, found.feed_no, found.rid * 1024 + found.feed_no, found.key_ord, found.type_no, found.c, found.sig_hash, {R}, found.f FROM found"+
		absent("{dropped}", "dd", "x.type_no = found.type_no AND x.ctx_no = found.c AND x.sig_hash = found.sig_hash")+" WHERE found.f IS NOT NULL OR dd.hit IS NULL"),
		st.Cur, hi)
	if err != nil {
		return err
	}
	st.RoundSlots += n
	st.Cur = hi
	return nil
}

// endSlots is the end of the slots of a round: a round that made none is tried again with what it can take, and then the rounds are
// over; a round that made some goes on to the groups.
func (r *identRun) endSlots(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	switch {
	case st.Round > st.Rounds:
		return r.toLookup()
	case st.RoundSlots == 0 && !st.Relaxed:
		st.Relaxed, st.Cur = true, 0
	case st.RoundSlots == 0:
		return r.toLookup()
	default:
		st.Phase, st.Cur, st.Turn, st.Changed = phaseLabels, 0, 0, 0
		st.Counts.Rounds = st.Round
		return r.analyze(ctx, tx, pgschema.TableCalcSlot, pgschema.TableCalcBind)
	}
	return nil
}

// analyzeStage measures the rows the receiving staged for the source of the run.
func (r *identRun) analyzeStage(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{pgschema.TableStageKey, pgschema.TableStageEnd, pgschema.TableStageClaim} {
		if _, err := tx.ExecContext(ctx, "ANALYZE "+r.t(pgschema.SourcePartition(table, r.srcNo))); err != nil {
			return dbErr(err, "measure the rows the receiving staged")
		}
	}
	return nil
}

// analyze gives the planner the sizes of tables the run has just filled: a table that is only inserted into is not looked at by the
// server in time, and a plan made on a size that is not there reads the whole of a table for each unit.
func (r *identRun) analyze(ctx context.Context, tx *sql.Tx, tables ...string) error {
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, "ANALYZE "+r.t(table)); err != nil {
			return dbErr(err, "measure the tables of the run")
		}
	}
	return nil
}

func (r *identRun) toLookup() error {
	st := r.st
	st.Phase, st.Cur, st.Turn = phaseLookup, 0, 0
	return nil
}

// nextRound closes a round and begins the next one.
func (r *identRun) nextRound(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	st.Round, st.Cur, st.Turn, st.RoundSlots, st.Relaxed, st.Unsettled, st.AskLeft = st.Round+1, 0, 0, 0, false, false, 0
	st.Phase = phaseSlot
	if st.Round > st.Rounds {
		return r.toLookup()
	}
	return nil
}

// leaveNeighbors leaves, in a round after the one that left some groups, the pairs that have a key one of the pairs left has: they
// are in the same group as the pairs left, and the next run binds them together. A pair is left when one of its keys, whose parent is
// found, is the key of a slot of an earlier round whose pair is left (a probe of the slots of each earlier round, which are at most
// sixteen). The pairs it leaves have no slot of their own, so the pairs that share a key with them only are not found by this: that is
// the limit of one run (ADR-0108).
func (r *identRun) leaveNeighbors(ctx context.Context, tx *sql.Tx, hi int64) error {
	st := r.st
	id := strconv.Itoa(int(roleIdentity))
	n, err := r.exec(ctx, tx, "leave the pairs that share a key with the groups left", r.q("WITH {feeds}, q AS (SELECT DISTINCT k.rid, k.feed_no FROM {key} k"+
		probe("x.src_no", "{rec}", "rc", "x.rid = k.rid")+" JOIN feeds f ON f.src_no = rc.src_no AND f.feed_no = k.feed_no AND f.creates"+chainSQL()+
		absent("{bind}", "hb", "x.rid = k.rid AND x.feed_no = k.feed_no")+absent("{ref}", "hr", "x.rid = k.rid AND x.def_no = k.feed_no AND x.role = "+id)+
		" CROSS JOIN LATERAL generate_series(1, {R} - 1) pr(r)"+
		probe("1", "{slot}", "t", "x.round_no = pr.r AND x.type_no = k.type_no AND x.ctx_no = "+ctxSQL+" AND x.sig_hash = k.sig_hash AND EXISTS (SELECT 1 FROM {ref} z "+
			"WHERE z.rid = x.rid AND z.role = "+id+" AND z.def_no = x.feed_no)")+
		" WHERE NOT k.held AND k.rid > $1 AND k.rid <= $2 AND hb.hit IS NULL AND hr.hit IS NULL AND "+ctxSQL+" IS NOT NULL) "+refUnresolved("q")), st.Cur, hi)
	if err != nil {
		return err
	}
	st.Counts.Unsettled += n
	st.Counts.Unresolved += n
	return nil
}
