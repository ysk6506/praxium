package pgingest

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The reading of the pages of the fifth step. A page reads and changes nothing; the place says where it begins.

// IsolatePage reads the page the place says: see ingest.Isolator.
func (s *Store) IsolatePage(ctx context.Context, lease ingest.RunLease) (ingest.IsolatePage, error) {
	var page ingest.IsolatePage
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.mustIsolate(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		st := r.st
		if st.Finished {
			return kernel.Errorf(ingest.CodeRunStage, "the fifth step of the run %d is done", lease.No)
		}
		page = ingest.IsolatePage{Unit: st.Unit, Phase: st.Phase, Schemes: st.Schemes}
		switch st.Phase {
		case ingest.IsolatePhaseIdents:
			return r.identPage(ctx, tx, &page)
		case ingest.IsolatePhaseEnds:
			return r.endsPage(ctx, tx, &page)
		}
		return r.seatPage(ctx, tx, &page)
	})
	return page, err
}

// identPage reads the identifiers of a page that more than one object has.
func (r *isoRun) identPage(ctx context.Context, tx *sql.Tx, page *ingest.IsolatePage) error {
	cur := r.st.Cur
	var end ingest.IsolateCursor
	q := r.q(isoIdentEndSQL)
	r.saw(tx, "find the end of the page of the identifiers", q, cur.Scheme, cur.Value, cur.Scope, r.st.Size-1)
	err := tx.QueryRowContext(ctx, q, cur.Scheme, cur.Value, cur.Scope, r.st.Size-1).Scan(&end.Scheme, &end.Value, &end.Scope)
	page.Last = isNoRows(err)
	if err != nil && !page.Last {
		return dbErr(err, "find the end of the page of the identifiers")
	}
	page.Through = end
	var key *ingest.IdentKey
	return r.query(ctx, tx, "read the identifiers that more than one object has", isoIdentKeysSQL, func(rows *sql.Rows) error {
		var scheme, value, scope string
		var c ingest.IdentClaimant
		if err := rows.Scan(&scheme, &value, &scope, &c.ObjNo, &c.Ref, &c.Staged, &c.Holds); err != nil {
			return err
		}
		if key == nil || key.Scheme != scheme || key.Value != value || key.Scope != scope {
			page.Keys = append(page.Keys, ingest.IdentKey{Scheme: scheme, Value: value, Scope: scope})
			key = &page.Keys[len(page.Keys)-1]
		}
		key.Claimants = append(key.Claimants, c)
		return nil
	}, cur.Scheme, cur.Value, cur.Scope, end.Scheme, end.Value, end.Scope, page.Last)
}

// endsPage reads where a page of the links by their number ends.
func (r *isoRun) endsPage(ctx context.Context, tx *sql.Tx, page *ingest.IsolatePage) error {
	var n int
	var last int64
	q := r.q(isoLinkExtentSQL)
	r.saw(tx, "find the end of the page of the links", q, r.st.Cur.No, r.st.Size)
	if err := tx.QueryRowContext(ctx, q, r.st.Cur.No, r.st.Size).Scan(&n, &last); err != nil {
		return dbErr(err, "find the end of the page of the links")
	}
	page.Last = n < r.st.Size
	page.Through = ingest.IsolateCursor{No: max(last, r.st.Cur.No)}
	return nil
}

// isoCardTypes are the link types that hold an end to one: all of them (the ones whose links the check looks at), the ones that hold the
// start, and the ones that hold the end.
func (r *isoRun) cardTypes() (all []string, out, in map[int]bool) {
	out, in = map[int]bool{}, map[int]bool{}
	for _, c := range r.st.Cards {
		all = append(all, strconv.Itoa(c.LinkTypeNo))
		out[c.LinkTypeNo], in[c.LinkTypeNo] = c.FromOne, c.ToOne
	}
	return all, out, in
}

func isoScanLink(rows *sql.Rows) (ingest.SeatLink, error) {
	var l ingest.SeatLink
	err := rows.Scan(&l.LinkNo, &l.Ref, &l.TypeNo, &l.Src, &l.Dst, &l.SrcType, &l.DstType, &l.SrcName, &l.DstName)
	return l, err
}

// seatPage reads a page of the links of the run of the pass (the ones the KG has, or the others) in the order of their ids, the links the
// KG keeps at their ends, and the places earlier pages took there.
func (r *isoRun) seatPage(ctx context.Context, tx *sql.Tx, page *ingest.IsolatePage) error {
	all, out, in := r.cardTypes()
	kept := r.st.Phase == ingest.IsolatePhaseKept
	err := r.query(ctx, tx, "read the links of a page", isoSeatLinksSQL, func(rows *sql.Rows) error {
		l, err := isoScanLink(rows)
		page.Links = append(page.Links, l)
		return err
	}, r.st.Cur.Ref, r.st.Size, textArray(all), kept)
	if err != nil {
		return err
	}
	page.Last = len(page.Links) < r.st.Size
	page.Through = r.st.Cur
	if len(page.Links) > 0 {
		page.Through = ingest.IsolateCursor{Ref: page.Links[len(page.Links)-1].Ref}
	}
	var oT, oO, iT, iO, starts, ends []string
	seen := map[string]bool{}
	for _, l := range page.Links {
		src, dst, typ := strconv.FormatInt(l.Src, 10), strconv.FormatInt(l.Dst, 10), strconv.Itoa(l.TypeNo)
		if out[l.TypeNo] && !seen["o"+typ+"/"+src] {
			seen["o"+typ+"/"+src] = true
			oT, oO, starts = append(oT, typ), append(oO, src), append(starts, src)
		}
		if in[l.TypeNo] && !seen["i"+typ+"/"+dst] {
			seen["i"+typ+"/"+dst] = true
			iT, iO, ends = append(iT, typ), append(iO, dst), append(ends, dst)
		}
	}
	if len(starts)+len(ends) == 0 {
		return nil
	}
	err = r.query(ctx, tx, "read the links the KG keeps at the ends", isoFixedSQL, func(rows *sql.Rows) error {
		l, err := isoScanLink(rows)
		page.Fixed = append(page.Fixed, l)
		return err
	}, textArray(oT), textArray(oO), textArray(iT), textArray(iO))
	if err != nil {
		return err
	}
	return r.query(ctx, tx, "read the places earlier pages took", isoTakenSQL, func(rows *sql.Rows) error {
		var t ingest.Seat
		err := rows.Scan(&t.Side, &t.EndNo, &t.BindID, &t.LinkNo)
		page.Taken = append(page.Taken, t)
		return err
	}, textArray(starts), textArray(ends))
}
