package pgingest

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// Which objects may be retired is decided here and nowhere else (ADR-0114): the expansion marks the candidates (ObjectWhyRetire in
// calc_object) and the computation of the objects (step 4) makes no row for them and reads only that mark. An object is a candidate when
//
//  1. no record of the run was bound to it (the identification wrote it to calc_object as ObjectWhyIdentified: a record that is bound to
//     an object stands under it, and so does a record whose row a rule filters out and whose wait is not over),
//  2. no record that is not in the run, or that is in it only because it is read again, and that is open, has a reference to it that is
//     solved: the references of the records that are in the run for the reason that they changed or are closed are not what stands now,
//     and a record that was set aside by the identification (read by a declaration it was not made by) keeps what it had, and
//  3. a reference to it that is kept now says that it is retirable: the feed of the record may retire what it makes.
//
// The third is where the old flow and this one differ: that one kept "retirable" for an object for good once a feed that retires saw it,
// this one reads it of the references there are when the run begins, that is, of the records that stood under the object until now.

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
	return "(NOT EXISTS (SELECT 1 FROM {objrun} i WHERE i.obj_no = " + obj + " AND i.why = " + whyIdent + ")" +
		" AND NOT EXISTS (SELECT 1 FROM {rref} s WHERE s.target_no = " + obj + " AND s.role = " + roleIdent + " AND s.state = " + stateFound + " AND NOT " + replaced + ")" +
		" AND EXISTS (SELECT 1 FROM {rref} t WHERE t.target_no = " + obj + " AND t.role = " + roleIdent + " AND t.retirable))"
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
