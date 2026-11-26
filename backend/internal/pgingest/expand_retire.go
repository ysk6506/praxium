package pgingest

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// Which objects may be retired is decided here and nowhere else (ADR-0114, ADR-0118): the expansion marks the candidates
// (ObjectWhyRetire in calc_object) and the computation of the objects (step 4) makes no row for them and reads only that mark. An object
// is a candidate when
//
//  1. no record of the run was bound to it by a feed that makes objects (the bindings of the identification, read as the references it
//     wrote of them, calc_ref: a record that is bound to an object stands under it, and so does a record whose row a rule filters out
//     and whose wait is not over, but the record of a feed that only adds values does not, even when it changed and is bound again
//     (ADR-0121); a binding of a feed that is not known stands under it),
//  2. no record that is not in the run, or that is in it only because it is read again, and that is open, has a reference to it that is
//     solved and that a feed that makes objects gave: the references of the records that are in the run for the reason that they changed
//     or are closed are not what stands now, a record that was set aside by the identification (read by a declaration it was not made
//     by) keeps what it had, and the record of a feed that only adds values does not keep an object (REQ-643). A reference of a feed that
//     is not known (the source has no plan in the run) is kept as a support: what it stands for is not known, and the object is not
//     retired for want of knowing, and
//  3. the object is marked retirable for good (it is in object_retire: a feed that retires made it once), or a reference to it that is
//     kept now says so.
//
// The third is the old flow's "once on, always on": the mark is of the object, so that an object that two feeds make, only one of which
// retires, is retired when the last record of the other feed is gone, whichever feed lets go of it first (ADR-0118). The references that
// are kept (the ones there are when the run begins) say it for the object that was made in a run that has not been applied yet.
// MarkRetirableObjects writes the mark; the step that applies the run calls it.
//
// The identification puts every object a record of the run is bound to in calc_object (ObjectWhyIdentified), whatever the feed of the
// record, and an object is in the table once. So an object that only records of feeds that add values were bound to is judged too: the
// rule old judges such an object once (the key of xkIdent says it was) and, when it is a candidate, turns its reason into
// ObjectWhyRetire, so that the computation of the objects makes no row for it (ADR-0121).

// expReplaced is the condition that the record of the reference named by the alias (a row of the references with the key of a record)
// is a record that the run read again and wrote the references of anew (or ended): what it had does not stand now. The computation of
// the objects (step 4) reads the references that stand by the same judgement.
func expReplaced(alias string) string {
	changed := strconv.Itoa(int(pgschema.CalcWhyChanged))
	isNew := strconv.Itoa(int(pgschema.CalcWhyNew))
	closed := strconv.Itoa(int(pgschema.CalcWhyClosed))
	released := strconv.Itoa(int(pgschema.CalcWhyReleased))
	expanded := strconv.Itoa(int(pgschema.CalcWhyExpanded))
	a := alias + "."
	return "EXISTS (SELECT 1 FROM {rec} c JOIN {ds} d ON d.dataset_no = c.dataset_no WHERE c.src_no = " + a + "src_no AND c.dataset_no = " + a + "dataset_no AND c.key = " + a + "key " +
		"AND c.why <> " + expanded + " AND (c.why IN (" + changed + ", " + isNew + ", " + closed + ", " + released + ") OR c.decl_rev = d.decl_rev))"
}

// expRetirable is the condition that the object named by the column obj (an expression of the statement) is a candidate for retirement.
func expRetirable(obj string) string {
	replaced := expReplaced("s")
	// the feed of a reference (the number of the feed is the def_no of an identity reference) of the source src is known not to make objects
	valueOnly := func(ref, src string) string {
		return "EXISTS (SELECT 1 FROM feeds f WHERE f.src_no = " + src + " AND f.feed_no = " + ref + ".def_no AND NOT f.creates)"
	}
	return "(NOT EXISTS (SELECT 1 FROM {cref} b JOIN {rec} c ON c.rid = b.rid WHERE b.role = " + roleIdent + " AND b.target_no = " + obj + " AND b.state = " + stateFound +
		" AND NOT " + valueOnly("b", "c.src_no") + ")" +
		" AND NOT EXISTS (SELECT 1 FROM {rref} s WHERE s.target_no = " + obj + " AND s.role = " + roleIdent + " AND s.state = " + stateFound + " AND NOT " + replaced + " AND NOT " + valueOnly("s", "s.src_no") + ")" +
		" AND (EXISTS (SELECT 1 FROM {retire} p WHERE p.obj_no = " + obj + ")" +
		" OR EXISTS (SELECT 1 FROM {rref} t WHERE t.target_no = " + obj + " AND t.role = " + roleIdent + " AND t.retirable)))"
}

// markRetirableSQL marks the objects that a retirable reference of the run points at (the references the identification wrote,
// calc_ref), for good.
const markRetirableSQL = "INSERT INTO {retire} (obj_no) SELECT DISTINCT r.target_no FROM {ref} r JOIN {obj} o ON o.obj_no = r.target_no WHERE r.role = {role} " +
	"AND r.state = {state} AND r.retirable ON CONFLICT DO NOTHING"

// MarkRetirableObjects puts the mark (a row of object_retire) on every object that a retirable reference of the run points at, and says
// how many it marked. The step that applies the run calls it in the transaction that writes the registry, after the objects the run made
// are in it (so that the next run reads the mark). A mark stays: nothing takes it away.
func MarkRetirableObjects(ctx context.Context, tx *sql.Tx, kgNo int64) (int, error) {
	q := strings.NewReplacer("{retire}", pgschema.TableRef(kgNo, pgschema.TableObjectRetire), "{obj}", pgschema.TableRef(kgNo, pgschema.TableObject), "{ref}", pgschema.TableRef(kgNo, pgschema.TableCalcRef),
		"{role}", roleIdent, "{state}", stateFound).Replace(markRetirableSQL)
	res, err := tx.ExecContext(ctx, q)
	if err != nil {
		return 0, dbErr(err, "mark the objects that may be retired")
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// retireOutSQL and retireInSQL are the records that made the links that start (or end) at an object to retire: the objects go with their
// links, and the records of the links are read again, so that the ends they asked for are not found any more (the step after reads
// them as references that are not solved). The links are found by the index of the ends of the links, the records by the one of the
// references from a link.
func retireSQL(end string) string {
	return "WITH o AS (SELECT obj_no FROM {objrun} WHERE why = " + whyRetire + " AND obj_no > $1 AND obj_no <= $2), " +
		"ls AS (SELECT l.link_no FROM o CROSS JOIN LATERAL (SELECT x.link_no FROM {link} x WHERE x." + end + " = o.obj_no OFFSET 0) l), " + expProducers + " " + expAddProducers()
}

var (
	retireOutSQL = retireSQL("src")
	retireInSQL  = retireSQL("dst")
)

// retireUnit does a unit of a rule that goes through the objects of the run, a range of their numbers at a time.
func (e *expRun) retireUnit(ctx context.Context, tx *sql.Tx) error {
	st := e.st
	hi, last, err := e.objectRange(ctx, tx)
	if err != nil {
		return err
	}
	if !e.skipped(st.Rule) {
		query, what := retireOutSQL, "the records that made the links that start at an object to retire"
		if st.Rule == ruleRetireIn {
			query, what = retireInSQL, "the records that made the links that end at an object to retire"
		}
		if err := e.addTo(ctx, tx, &st.Counts.RetireRecords, what, query, st.Cur, hi); err != nil {
			return err
		}
	}
	if last {
		return e.enter(ctx, tx, st.Rule)
	}
	st.Cur = hi
	return nil
}
