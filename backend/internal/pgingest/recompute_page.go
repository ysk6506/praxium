package pgingest

import (
	"context"
	"database/sql"
	"math"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// rcpBatch is how many objects are counted at a time to find where a page ends: a page ends at the object that would take it past
// the number of lines, so the objects are counted a batch at a time until that is found, and at most one batch is counted twice.
const rcpBatch = 256

// RecomputePage reads the page the place says: see ingest.Recomputer. It changes nothing.
func (s *Store) RecomputePage(ctx context.Context, lease ingest.RunLease) (ingest.RecomputePage, error) {
	var page ingest.RecomputePage
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.loadRecompute(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		if r.st.Finished {
			return kernel.Errorf(ingest.CodeRunStage, "the computation of the objects of the run %d is done", lease.No)
		}
		page = ingest.RecomputePage{Unit: r.st.Unit}
		if page.Through, page.Last, page.TooMany, err = r.extent(ctx, tx); err != nil {
			return err
		}
		skip := nums(page.TooMany)
		if page.Objects, err = r.objects(ctx, tx, page.Through, skip); err != nil {
			return err
		}
		return r.lines(ctx, tx, page.Through, skip, page.Objects)
	})
	return page, err
}

// extent finds where the page ends: the last object of it, whether there are none after it, and the objects that have more lines than
// the limit (they are left out of the page and take no room in it).
func (r *rcpRun) extent(ctx context.Context, tx *sql.Tx) (through int64, last bool, tooMany []int64, err error) {
	st := r.st
	from, total, prev := st.Cursor, 0, st.Cursor
	for {
		query := r.q(rcpCountSQL)
		r.saw(tx, "count the lines of the objects", query, from, rcpBatch, st.Max+1)
		rows, qerr := tx.QueryContext(ctx, query, from, rcpBatch, st.Max+1)
		if qerr != nil {
			return 0, false, nil, dbErr(qerr, "count the lines of the objects")
		}
		seen := 0
		for rows.Next() {
			var no int64
			var n int
			if err := rows.Scan(&no, &n); err != nil {
				_ = rows.Close()
				return 0, false, nil, dbErr(err, "count the lines of an object")
			}
			seen++
			switch {
			case n > st.Max:
				tooMany = append(tooMany, no)
			case total > 0 && total+max(n, 1) > st.Size:
				_ = rows.Close()
				return prev, false, tooMany, nil
			default:
				total += max(n, 1)
			}
			prev, from = no, no
		}
		if err := rows.Err(); err != nil {
			return 0, false, nil, dbErr(err, "count the lines of the objects")
		}
		if seen < rcpBatch {
			return math.MaxInt64, true, tooMany, nil
		}
	}
}

// objects reads the objects of the page.
func (r *rcpRun) objects(ctx context.Context, tx *sql.Tx, through int64, skip string) ([]ingest.RecomputeObject, error) {
	query := r.q(rcpObjectsSQL)
	r.saw(tx, "read the objects", query, r.st.Cursor, through, skip)
	rows, err := tx.QueryContext(ctx, query, r.st.Cursor, through, skip)
	if err != nil {
		return nil, dbErr(err, "read the objects")
	}
	defer func() { _ = rows.Close() }()
	var out []ingest.RecomputeObject
	for rows.Next() {
		var o ingest.RecomputeObject
		var first string
		if err := rows.Scan(&o.ObjNo, &o.Ref, &o.TypeNo, &o.IsNew, &o.Name, &first, &o.OriginRid, &o.OriginFeed, &o.ParentRef); err != nil {
			return nil, dbErr(err, "read an object")
		}
		o.FirstKey = []byte(first)
		out = append(out, o)
	}
	return out, dbErr(rows.Err(), "read the objects")
}

// rcpLineKey names a line of an object, to find the claims of it.
type rcpLineKey struct {
	obj, src, ds int64
	key          string
	feed         int
}

// lines reads the lines of the objects of the page into them, and the claims each line was decided to have.
func (r *rcpRun) lines(ctx context.Context, tx *sql.Tx, through int64, skip string, objects []ingest.RecomputeObject) error {
	at := make(map[int64]*ingest.RecomputeObject, len(objects))
	for i := range objects {
		at[objects[i].ObjNo] = &objects[i]
	}
	query := r.q(rcpLinesSQL)
	r.saw(tx, "read the lines of the objects", query, r.st.Cursor, through, skip)
	rows, err := tx.QueryContext(ctx, query, r.st.Cursor, through, skip)
	if err != nil {
		return dbErr(err, "read the lines of the objects")
	}
	var objs, srcs, dss, rids []string
	var keys []string
	where := map[rcpLineKey]*ingest.RecomputeLine{}
	for rows.Next() {
		var obj int64
		var ln ingest.RecomputeLine
		var cells sql.NullString
		if err := rows.Scan(&obj, &ln.Rid, &ln.Why, &ln.SrcNo, &ln.DatasetNo, &ln.Dataset, &ln.Key, &cells, &ln.OpenedSeq, &ln.Feed); err != nil {
			_ = rows.Close()
			return dbErr(err, "read a line of an object")
		}
		if cells.Valid {
			if ln.Fields, err = parseTextArray(cells.String); err != nil {
				_ = rows.Close()
				return err
			}
		}
		o, ok := at[obj]
		if !ok {
			_ = rows.Close()
			return kernel.Errorf(kernel.CodeInternal, "a line of the object %d was read and the object was not", obj)
		}
		o.Lines = append(o.Lines, ln)
		objs, srcs, dss, rids = append(objs, strconv.FormatInt(obj, 10)), append(srcs, strconv.FormatInt(ln.SrcNo, 10)), append(dss, strconv.FormatInt(ln.DatasetNo, 10)), append(rids, strconv.FormatInt(ln.Rid, 10))
		keys = append(keys, ln.Key)
	}
	if err := rows.Err(); err != nil {
		return dbErr(err, "read the lines of the objects")
	}
	_ = rows.Close()
	for i := range objects {
		for j := range objects[i].Lines {
			ln := &objects[i].Lines[j]
			where[rcpLineKey{objects[i].ObjNo, ln.SrcNo, ln.DatasetNo, ln.Key, ln.Feed}] = ln
		}
	}
	if len(objs) == 0 {
		return nil
	}
	return r.claims(ctx, tx, where, objs, srcs, dss, keys, rids)
}

// claims reads the claims of the lines and gives each line the ones that were decided for its object.
func (r *rcpRun) claims(ctx context.Context, tx *sql.Tx, where map[rcpLineKey]*ingest.RecomputeLine, objs, srcs, dss, keys, rids []string) error {
	query := r.q(rcpClaimsSQL)
	r.saw(tx, "read the claims of the lines", query, textArray(objs), textArray(srcs), textArray(dss), textArray(keys), textArray(rids))
	rows, err := tx.QueryContext(ctx, query, textArray(objs), textArray(srcs), textArray(dss), textArray(keys), textArray(rids))
	if err != nil {
		return dbErr(err, "read the claims of the lines")
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k rcpLineKey
		var scheme int
		var hash []byte
		if err := rows.Scan(&k.obj, &k.src, &k.ds, &k.key, &k.feed, &scheme, &hash); err != nil {
			return dbErr(err, "read a claim of a line")
		}
		ln, ok := where[k]
		if !ok || len(hash) != len(ingest.KeyHash{}) {
			continue
		}
		cl := ingest.RecomputeClaim{Scheme: scheme}
		copy(cl.Hash[:], hash)
		ln.Won = append(ln.Won, cl)
	}
	return dbErr(rows.Err(), "read the claims of the lines")
}
