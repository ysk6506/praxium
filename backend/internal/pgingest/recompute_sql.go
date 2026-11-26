package pgingest

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The statements of the computation of the objects (ADR-0115). The lines of an object are the references of the identity of the records
// that stand under it: the ones that are kept (record_ref, found by the index of the targets) except the ones of the records the run
// read again and wrote the references of anew (the same judgement the retirement is decided by, expReplaced), and the ones of the run
// (calc_ref, found by its own index of the targets). Every statement reads the lines of a range of the objects, one object at a time
// by an index, so the work grows with the lines and not with the product of the objects and the lines.

func rcpReplacer(r *rcpRun) *strings.Replacer {
	return strings.NewReplacer(
		"{rec}", r.t(pgschema.TableCalcRecord), "{objrun}", r.t(pgschema.TableCalcObject), "{rref}", r.t(pgschema.TableRecordRef),
		"{cref}", r.t(pgschema.TableCalcRef), "{raw}", r.t(pgschema.TableRawRecord), "{ds}", pgschema.TableSourceDataset,
		"{obj}", r.t(pgschema.TableObject), "{newobj}", r.t(pgschema.TableCalcNewObj), "{ask}", r.t(pgschema.TableCalcAsk),
		"{prob}", r.t(pgschema.TableCalcProblem), "{stobj}", r.t(pgschema.TableStageObject), "{stvol}", r.t(pgschema.TableStageVolat),
		"{stid}", r.t(pgschema.TableStageIdent),
		"{RETIRE}", whyRetire, "{WHY}", strconv.Itoa(int(pgschema.CalcWhyExpanded)), "{IDENT}", roleIdent, "{CLAIM}", roleClaim, "{FOUND}", stateFound,
	)
}

// rcpKept is the lines of the references that are kept for the object o (the alias of a row with obj_no): the references of the identity
// that are solved, of the records that the run did not read again.
// Their cells are the raw record's; a record that is in the run only because it is read again (expanded) has its number and reason.
var rcpKept = "SELECT s.src_no, s.dataset_no, s.key, s.def_no AS feed, COALESCE(c.rid, 0) AS rid, COALESCE(c.why, -1) AS why, w.fields, w.opened_seq, w.decl_rev " +
	"FROM {rref} s JOIN {raw} w ON w.src_no = s.src_no AND w.dataset_no = s.dataset_no AND w.key = s.key " +
	"LEFT JOIN {rec} c ON c.src_no = s.src_no AND c.dataset_no = s.dataset_no AND c.key = s.key " +
	"WHERE s.target_no = o.obj_no AND s.role = {IDENT} AND s.state = {FOUND} AND NOT " + expReplaced("s")

// rcpOwn is the lines of the references of the run for the object o: the cells are the ones of the record of the run (the raw record
// has what the record was before).
const rcpOwn = "SELECT c.src_no, c.dataset_no, c.key, f.def_no AS feed, c.rid, c.why, c.fields, c.opened_seq, c.decl_rev FROM {cref} f JOIN {rec} c ON c.rid = f.rid " +
	"WHERE f.role = {IDENT} AND f.target_no = o.obj_no AND f.state = {FOUND}"

// rcpCountSQL gives the objects after a number, with how many lines each has (counted up to a limit: an object that has more than the
// limit is not read).
var rcpCountSQL = "SELECT o.obj_no, (SELECT count(*) FROM (SELECT 1 FROM {rref} s WHERE s.target_no = o.obj_no AND s.role = {IDENT} AND s.state = {FOUND} AND NOT " + expReplaced("s") +
	" UNION ALL SELECT 1 FROM {cref} f WHERE f.role = {IDENT} AND f.target_no = o.obj_no AND f.state = {FOUND} LIMIT $3) z) " +
	"FROM (SELECT obj_no FROM {objrun} WHERE obj_no > $1 AND why <> {RETIRE} ORDER BY obj_no LIMIT $2) o ORDER BY o.obj_no"

// rcpObjectsSQL are the objects of a range with what Go needs of each: the id, the type, the name and the first key of one the KG has,
// and for one the run makes the record and the feed that made it and the id of the parent that its key asks for.
const rcpObjectsSQL = "SELECT o.obj_no, COALESCE(x.ref, n.ref), COALESCE(x.type_no, n.type_no), x.obj_no IS NULL, COALESCE(x.name, ''), COALESCE(x.first_key::text, ''), " +
	"COALESCE(n.rid, 0), COALESCE(n.feed_no, 0), COALESCE(a.parent_ref, '') FROM {objrun} o LEFT JOIN {obj} x ON x.obj_no = o.obj_no LEFT JOIN {newobj} n ON n.obj_no = o.obj_no " +
	"LEFT JOIN LATERAL (SELECT q.parent_ref FROM {ask} q WHERE q.rid = n.rid AND q.feed_no = n.feed_no AND q.parent_ref IS NOT NULL LIMIT 1) a ON true " +
	"WHERE o.obj_no > $1 AND o.obj_no <= $2 AND o.why <> {RETIRE} AND NOT o.obj_no = ANY($3::text::bigint[]) ORDER BY o.obj_no"

// rcpLinesSQL are the lines of the objects of a range: the cells of the record as the run reads them (the ones of the run's record, or
// the raw record), NULL when they were kept under another declaration than the dataset has now.
var rcpLinesSQL = "SELECT o.obj_no, x.rid, x.why, x.src_no, x.dataset_no, d.dataset_id, x.key, " +
	"CASE WHEN x.decl_rev = d.decl_rev THEN x.fields END, x.opened_seq, x.feed " +
	"FROM {objrun} o CROSS JOIN LATERAL (" + rcpKept + " UNION ALL " + rcpOwn + ") x " +
	"JOIN {ds} d ON d.dataset_no = x.dataset_no " +
	"WHERE o.obj_no > $1 AND o.obj_no <= $2 AND o.why <> {RETIRE} AND NOT o.obj_no = ANY($3::text::bigint[]) " +
	"ORDER BY o.obj_no, x.src_no, x.dataset_no, x.key, x.feed"

// rcpClaimsSQL are the identifier claims of the lines that were decided for their object: from the references that are kept (of the
// records the run did not read again) and the ones of the run.
var rcpClaimsSQL = "SELECT u.obj, u.src, u.ds, u.key, y.feed, y.scheme, y.hash FROM unnest($1::text::bigint[], $2::text::bigint[], $3::text::bigint[], $4::text::text[], $5::text::bigint[]) " +
	"AS u(obj, src, ds, key, rid) CROSS JOIN LATERAL (" +
	"SELECT s.sub AS feed, s.def_no AS scheme, s.lookup_hash AS hash FROM {rref} s WHERE s.src_no = u.src AND s.dataset_no = u.ds AND s.key = u.key AND s.role = {CLAIM} " +
	"AND s.state = {FOUND} AND s.target_no = u.obj AND NOT " + expReplaced("s") +
	" UNION ALL SELECT f.sub, f.def_no, f.lookup_hash FROM {cref} f WHERE f.rid = u.rid AND f.role = {CLAIM} AND f.state = {FOUND} AND f.target_no = u.obj) y"

// The statements that write a page.
const (
	rcpRecordsSQL = "INSERT INTO {rec} (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why) SELECT w.src_no, w.dataset_no, w.key, w.fields, w.raw_hash, w.decl_rev, " +
		"w.opened_seq, w.fate, {WHY} FROM unnest($1::text::bigint[], $2::text::bigint[], $3::text::text[]) AS u(src, ds, key) " +
		"JOIN {raw} w ON w.src_no = u.src AND w.dataset_no = u.ds AND w.key = u.key ON CONFLICT (src_no, dataset_no, key) DO NOTHING"
	rcpProblemsSQL = "INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT c.rid, u.feed, u.field, u.kind, u.reason FROM " +
		"unnest($1::text::bigint[], $2::text::bigint[], $3::text::text[], $4::text::int[], $5::text::text[], $6::text::text[], $7::text::text[]) AS u(src, ds, key, feed, field, kind, reason) " +
		"JOIN {rec} c ON c.src_no = u.src AND c.dataset_no = u.ds AND c.key = u.key"
	rcpObjectsPutSQL = "INSERT INTO {stobj} (obj_no, ref, type_no, name, min_grade, doc, source_refs, content_hash, row_hash, first_key, is_new) SELECT u.obj, u.ref, u.typ, u.name, u.grade, " +
		"u.doc::jsonb, u.refs::jsonb, decode(u.ch, 'hex'), decode(u.rh, 'hex'), NULLIF(u.fk, '')::jsonb, u.isnew FROM " +
		"unnest($1::text::bigint[], $2::text::text[], $3::text::int[], $4::text::text[], $5::text::int[], $6::text::text[], $7::text::text[], $8::text::text[], $9::text::text[], $10::text::text[], $11::text::boolean[]) " +
		"AS u(obj, ref, typ, name, grade, doc, refs, ch, rh, fk, isnew)"
	rcpVolatileSQL = "INSERT INTO {stvol} (obj_no, type_no, attr_no, entry) SELECT u.obj, u.typ, u.attr, u.entry::jsonb FROM " +
		"unnest($1::text::bigint[], $2::text::int[], $3::text::int[], $4::text::text[]) AS u(obj, typ, attr, entry)"
	rcpIdentSQL = "INSERT INTO {stid} (obj_no, scheme, value, scope) SELECT u.obj, u.scheme, u.value, '' FROM " +
		"unnest($1::text::bigint[], $2::text::text[], $3::text::text[]) AS u(obj, scheme, value)"
)

func nums[T ~int | ~int64 | ~int16](items []T) string {
	out := make([]string, len(items))
	for i, n := range items {
		out[i] = strconv.FormatInt(int64(n), 10)
	}
	return textArray(out)
}

// exec runs one statement of a page and gives the rows it wrote.
func (r *rcpRun) exec(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int, error) {
	q := r.q(query)
	r.saw(tx, what, q, args...)
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, dbErr(err, what)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// put writes the rows of a page: the records that are records of the run now, the problems, the objects, the volatile values and the
// identifiers.
func (r *rcpRun) put(ctx context.Context, tx *sql.Tx, page ingest.RecomputedPage) error {
	c := &r.st.Counts
	var err error
	if len(page.Records) > 0 {
		var srcs, dss, keys []string
		for _, x := range page.Records {
			srcs, dss, keys = append(srcs, strconv.FormatInt(x.SrcNo, 10)), append(dss, strconv.FormatInt(x.DatasetNo, 10)), append(keys, x.Key)
		}
		var n int
		if n, err = r.exec(ctx, tx, "make the records that stand under the objects records of the run", rcpRecordsSQL, textArray(srcs), textArray(dss), textArray(keys)); err != nil {
			return err
		}
		c.Added += n
	}
	if err := r.putProblems(ctx, tx, page.Problems); err != nil {
		return err
	}
	return r.putObjects(ctx, tx, page.Objects)
}

func (r *rcpRun) putProblems(ctx context.Context, tx *sql.Tx, problems []ingest.RecomputedProblem) error {
	if len(problems) == 0 {
		return nil
	}
	var srcs, dss, keys, feeds, fields, kinds, reasons []string
	for _, p := range problems {
		srcs, dss, keys = append(srcs, strconv.FormatInt(p.SrcNo, 10)), append(dss, strconv.FormatInt(p.DatasetNo, 10)), append(keys, p.Key)
		feeds, fields, kinds, reasons = append(feeds, strconv.Itoa(p.Feed)), append(fields, p.Field), append(kinds, p.Kind), append(reasons, p.Reason)
	}
	n, err := r.exec(ctx, tx, "write the problems of the records", rcpProblemsSQL, textArray(srcs), textArray(dss), textArray(keys), textArray(feeds), textArray(fields), textArray(kinds), textArray(reasons))
	if err != nil {
		return err
	}
	if n != len(problems) {
		return kernel.Errorf(kernel.CodeInternal, "%d problems were made for records and %d were written", len(problems), n)
	}
	r.st.Counts.Problems += n
	return nil
}

// putObjects writes the objects, their volatile values and their identifiers.
func (r *rcpRun) putObjects(ctx context.Context, tx *sql.Tx, objects []ingest.RecomputedObject) error {
	if len(objects) == 0 {
		return nil
	}
	var nos, types, grades []int64
	var refs, names, docs, srcRefs, ch, rh, fk, isNew []string
	var vObj, vType, vAttr []int64
	var vEntry []string
	var iObj []int64
	var iScheme, iValue []string
	for _, o := range objects {
		grade, err := graph.GradeNo(o.MinGrade)
		if err != nil {
			return err
		}
		nos, types, grades = append(nos, o.ObjNo), append(types, int64(o.TypeNo)), append(grades, int64(grade))
		refs, names, docs, srcRefs = append(refs, o.Ref), append(names, o.Name), append(docs, string(o.Doc)), append(srcRefs, string(o.SourceRefs))
		ch, rh, fk, isNew = append(ch, o.ContentHash.String()), append(rh, o.RowHash.String()), append(fk, o.FirstKey), append(isNew, strconv.FormatBool(o.IsNew))
		for _, v := range o.Volatile {
			vObj, vType, vAttr, vEntry = append(vObj, o.ObjNo), append(vType, int64(o.TypeNo)), append(vAttr, int64(v.AttrNo)), append(vEntry, string(v.Entry))
		}
		for _, id := range o.Idents {
			iObj, iScheme, iValue = append(iObj, o.ObjNo), append(iScheme, id.Scheme), append(iValue, id.Value)
		}
	}
	n, err := r.exec(ctx, tx, "write the objects", rcpObjectsPutSQL, nums(nos), textArray(refs), nums(types), textArray(names), nums(grades), textArray(docs), textArray(srcRefs),
		textArray(ch), textArray(rh), textArray(fk), textArray(isNew))
	if err != nil {
		return err
	}
	r.st.Counts.Objects += n
	if len(vObj) > 0 {
		nv, err := r.exec(ctx, tx, "write the volatile values", rcpVolatileSQL, nums(vObj), nums(vType), nums(vAttr), textArray(vEntry))
		if err != nil {
			return err
		}
		r.st.Counts.Volatile += nv
	}
	if len(iObj) > 0 {
		ni, err := r.exec(ctx, tx, "write the identifiers", rcpIdentSQL, nums(iObj), textArray(iScheme), textArray(iValue))
		if err != nil {
			return err
		}
		r.st.Counts.Idents += ni
	}
	return nil
}
