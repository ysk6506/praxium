package pgingest

import (
	"context"
	"database/sql"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The pages of the computation of the links. The ends of the run are read by an index in the order (link definition, key, record), a page
// of them after the place; so the records that share a key are next to each other, and what the To side has for a key is counted once, by
// the first page that has the key, and kept (calc_tokey).

// RecomputeLinksPage reads the page the place says: see ingest.LinkRecomputer. It changes nothing.
func (s *Store) RecomputeLinksPage(ctx context.Context, lease ingest.RunLease) (ingest.RecomputeLinksPage, error) {
	var page ingest.RecomputeLinksPage
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.must(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		if r.st.Finished {
			return kernel.Errorf(ingest.CodeRunStage, "the computation of the links of the run %d is done", lease.No)
		}
		page = ingest.RecomputeLinksPage{Unit: r.st.Unit, Phase: r.st.Phase}
		switch r.st.Phase {
		case ingest.LinksPhaseEnds:
			page.Through, page.Last, err = r.extentOfRecords(ctx, tx)
		case ingest.LinksPhaseTo:
			page.Through, page.Last, err = r.extentOfEnds(ctx, tx, pgschema.RefRoleLinkTo)
		case ingest.LinksPhaseFrom:
			if page.Through, page.Last, err = r.extentOfEnds(ctx, tx, pgschema.RefRoleLinkFrom); err == nil {
				err = r.ends(ctx, tx, &page)
			}
		case ingest.LinksPhaseConflicts:
			page.Through, page.Last, err = r.extentOfLinks(ctx, tx, rclConflictRefsSQL)
		default:
			page.Through, page.Last, err = r.extentOfLinks(ctx, tx, rclCellRefsSQL)
		}
		return err
	})
	return page, err
}

// extentOfRecords finds where a page of the records that the run only reads again ends: the last of the next ones by their number.
func (r *rclRun) extentOfRecords(ctx context.Context, tx *sql.Tx) (ingest.LinkCursor, bool, error) {
	cur := r.st.Cur
	query := r.q(rclRecordsExtentSQL)
	r.saw(tx, "find the end of the page of the records that are read again", query, cur.Rid, r.st.Size)
	rows, err := tx.QueryContext(ctx, query, cur.Rid, r.st.Size)
	if err != nil {
		return cur, false, dbErr(err, "find the end of the page of the records that are read again")
	}
	defer func() { _ = rows.Close() }()
	through, n := cur, 0
	for rows.Next() {
		if err := rows.Scan(&through.Rid); err != nil {
			return cur, false, dbErr(err, "read the end of the page of the records that are read again")
		}
		n++
	}
	return through, n < r.st.Size, dbErr(rows.Err(), "find the end of the page of the records that are read again")
}

// extentOfEnds finds where the page ends: the last of the next ends of a side by the order of the index, and whether there are none after it.
func (r *rclRun) extentOfEnds(ctx context.Context, tx *sql.Tx, side int16) (ingest.LinkCursor, bool, error) {
	cur := r.st.Cur
	query := r.q(rclExtentSQL)
	r.saw(tx, "find the end of the page of the ends", query, side, cur.Def, cur.Hash, cur.Rid, r.st.Size)
	rows, err := tx.QueryContext(ctx, query, side, cur.Def, cur.Hash, cur.Rid, r.st.Size)
	if err != nil {
		return cur, false, dbErr(err, "find the end of the page of the ends")
	}
	defer func() { _ = rows.Close() }()
	through, n := cur, 0
	for rows.Next() {
		through = ingest.LinkCursor{Hash: []byte{}}
		if err := rows.Scan(&through.Def, &through.Hash, &through.Rid); err != nil {
			return cur, false, dbErr(err, "read the end of the page of the ends")
		}
		n++
	}
	return through, n < r.st.Size, dbErr(rows.Err(), "find the end of the page of the ends")
}

// extentOfLinks is the same for the phases that go through the links by their id.
func (r *rclRun) extentOfLinks(ctx context.Context, tx *sql.Tx, query string) (ingest.LinkCursor, bool, error) {
	cur := r.st.Cur
	q := r.q(query)
	r.saw(tx, "find the end of the page of the links", q, cur.Ref, r.st.Size)
	rows, err := tx.QueryContext(ctx, q, cur.Ref, r.st.Size)
	if err != nil {
		return cur, false, dbErr(err, "find the end of the page of the links")
	}
	defer func() { _ = rows.Close() }()
	through, n := cur, 0
	for rows.Next() {
		if err := rows.Scan(&through.Ref); err != nil {
			return cur, false, dbErr(err, "read the end of the page of the links")
		}
		n++
	}
	return through, n < r.st.Size, dbErr(rows.Err(), "find the end of the page of the links")
}

// ends reads the ends of the From side of the page, with the object of each one's record, and counts what the To side has for the keys that
// no page counted (these are given to Go to be kept, and filled into the ends that have them).
func (r *rclRun) ends(ctx context.Context, tx *sql.Tx, page *ingest.RecomputeLinksPage) error {
	cur, to := r.st.Cur, page.Through
	args := []any{cur.Def, cur.Hash, cur.Rid, to.Def, to.Hash, to.Rid}
	keys, err := r.countKeys(ctx, tx, args)
	if err != nil {
		return err
	}
	page.Keys = keys
	query := r.q(rclEndsSQL)
	r.saw(tx, "read the ends of the From side", query, args...)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return dbErr(err, "read the ends of the From side")
	}
	defer func() { _ = rows.Close() }()
	counted := map[rclKey]ingest.LinkKeyCount{}
	for _, k := range keys {
		counted[rclKey{k.SrcNo, k.Def, k.Hash}] = k
	}
	for rows.Next() {
		var e ingest.RecomputeEnd
		var hash []byte
		var fields sql.NullString
		var targets sql.NullInt64
		var toObj sql.NullInt64
		var toRef sql.NullString
		if err := rows.Scan(&e.Rid, &e.SrcNo, &e.DatasetNo, &e.Dataset, &e.Key, &fields, &e.Def, &hash, &e.Held, &e.FromObj, &e.FromRef, &targets, &toObj, &toRef); err != nil {
			return dbErr(err, "read an end of the From side")
		}
		if len(hash) != len(e.Hash) {
			return kernel.Errorf(kernel.CodeInternal, "the key of the end of the record %d is not a hash", e.Rid)
		}
		copy(e.Hash[:], hash)
		if fields.Valid {
			if e.Fields, err = parseTextArray(fields.String); err != nil {
				return err
			}
		}
		if targets.Valid {
			e.Targets, e.ToObj, e.ToRef = int(targets.Int64), toObj.Int64, toRef.String
		} else if k, ok := counted[rclKey{e.SrcNo, e.Def, e.Hash}]; ok {
			e.Targets, e.ToObj, e.ToRef = k.Targets, k.Obj, k.Ref
		}
		if e.Targets == 1 && e.ToRef == "" {
			e.Targets = 0 // an object that no table knows cannot be linked to
		}
		page.Ends = append(page.Ends, e)
	}
	return dbErr(rows.Err(), "read the ends of the From side")
}

// rclKey names a link end key: the source, the link definition and the hash.
type rclKey struct {
	src int64
	def int
	key ingest.KeyHash
}

// countKeys counts, for the keys of the ends of the page that were not counted before, how many objects the To side has for each.
func (r *rclRun) countKeys(ctx context.Context, tx *sql.Tx, args []any) ([]ingest.LinkKeyCount, error) {
	query := r.q(rclCountSQL)
	r.saw(tx, "count the objects of the To side of the keys", query, args...)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, dbErr(err, "count the objects of the To side of the keys")
	}
	defer func() { _ = rows.Close() }()
	var out []ingest.LinkKeyCount
	for rows.Next() {
		var k ingest.LinkKeyCount
		var hash []byte
		var obj sql.NullInt64
		var ref sql.NullString
		if err := rows.Scan(&k.SrcNo, &k.Def, &hash, &k.Targets, &obj, &ref); err != nil {
			return nil, dbErr(err, "read the count of a key")
		}
		if len(hash) != len(k.Hash) {
			return nil, kernel.Errorf(kernel.CodeInternal, "a key of an end is not a hash")
		}
		copy(k.Hash[:], hash)
		k.Obj, k.Ref = obj.Int64, ref.String
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr(err, "count the objects of the To side of the keys")
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.SrcNo != b.SrcNo {
			return a.SrcNo < b.SrcNo
		}
		if a.Def != b.Def {
			return a.Def < b.Def
		}
		return string(a.Hash[:]) < string(b.Hash[:])
	})
	return out, nil
}
