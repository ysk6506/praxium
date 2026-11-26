package pgingest

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The writing of the pages of the fifth step: what is taken out is written to calc_isolated, the staged row is taken away, and the
// reference of the record that made it is not solved any more (with a problem, but for a link that goes with an object that retires).

// putIdents takes out the identifier lines that lost.
func (r *isoRun) putIdents(ctx context.Context, tx *sql.Tx, losses []ingest.IdentLoss) error {
	if len(losses) == 0 {
		return nil
	}
	var objs, schemes, values, scopes, nos, hashes, whys []string
	for _, l := range losses {
		why := pgschema.IsolatedIdentRun
		if l.ByKG {
			why = pgschema.IsolatedIdentKG
		}
		objs, schemes, values, scopes = append(objs, strconv.FormatInt(l.ObjNo, 10)), append(schemes, l.Scheme), append(values, l.Value), append(scopes, l.Scope)
		nos, hashes, whys = append(nos, strconv.Itoa(l.SchemeNo)), append(hashes, hexOf(l.Hash[:])), append(whys, isoNum(why))
	}
	args := []any{textArray(objs), textArray(schemes), textArray(values), textArray(scopes), textArray(nos), textArray(hashes), textArray(whys)}
	n, err := r.exec(ctx, tx, "write the identifiers that lost", isoIdentsSQL, args...)
	if err != nil {
		return err
	}
	if n != len(losses) {
		return kernel.Errorf(kernel.CodeInternal, "%d identifier lines lost and %d of them were staged lines", len(losses), n)
	}
	c := &r.st.Counts
	if n, err = r.exec(ctx, tx, "take the identifiers that lost out", isoIdentTakeSQL, args...); err != nil {
		return err
	}
	c.Idents += n
	for _, s := range []struct{ what, query string }{
		{"make the claims of the run that lost lost", isoClaimRunSQL},
		{"write the claims that are kept that lost", isoClaimKeptSQL},
	} {
		n, err := r.exec(ctx, tx, s.what, s.query, args...)
		if err != nil {
			return err
		}
		c.Refs += n
		c.Problems += n
	}
	return nil
}

// putEnds takes out the links of the range whose end is not there or retires.
func (r *isoRun) putEnds(ctx context.Context, tx *sql.Tx, page ingest.IsolatedPage) error {
	hi := page.Through.No
	var nos []string
	err := r.query(ctx, tx, "find the links whose end is not there or retires", isoEndsSQL, func(rows *sql.Rows) error {
		var no int64
		var why int16
		if err := rows.Scan(&no, &why); err != nil {
			return err
		}
		nos = append(nos, strconv.FormatInt(no, 10))
		if why == pgschema.IsolatedEndMissing {
			r.st.Counts.Missing++
		} else {
			r.st.Counts.Retiring++
		}
		return nil
	}, r.st.Cur.No, hi)
	if err != nil {
		return err
	}
	return r.takeLinks(ctx, tx, nos)
}

// takeLinks takes the links calc_isolated has (by their numbers) out of the staged ones: the problems, the references, the links.
func (r *isoRun) takeLinks(ctx context.Context, tx *sql.Tx, nos []string) error {
	if len(nos) == 0 {
		return nil
	}
	c := &r.st.Counts
	n, err := r.exec(ctx, tx, "write the problems of the links taken out", isoTakeProblemsSQL, textArray(nos))
	if err != nil {
		return err
	}
	c.Problems += n
	if n, err = r.exec(ctx, tx, "make the references of the links taken out not solved", isoTakeRefsSQL, textArray(nos)); err != nil {
		return err
	}
	c.Refs += n
	if n, err = r.exec(ctx, tx, "take the links out", isoTakeLinksSQL, textArray(nos)); err != nil {
		return err
	}
	if n != len(nos) {
		return kernel.Errorf(kernel.CodeInternal, "%d links were to be taken out and %d were", len(nos), n)
	}
	return nil
}

// putSeats writes the places the links of a page took and takes the links that lost out.
func (r *isoRun) putSeats(ctx context.Context, tx *sql.Tx, page ingest.IsolatedPage) error {
	c := &r.st.Counts
	if len(page.Seats) > 0 {
		var sides, ends, binds, nos []string
		for _, s := range page.Seats {
			sides, ends = append(sides, isoNum(s.Side)), append(ends, strconv.FormatInt(s.EndNo, 10))
			binds, nos = append(binds, s.BindID), append(nos, strconv.FormatInt(s.LinkNo, 10))
		}
		n, err := r.exec(ctx, tx, "write the places the links took", isoSeatsSQL, textArray(sides), textArray(ends), textArray(binds), textArray(nos))
		if err != nil {
			return err
		}
		c.Seats += n
	}
	if len(page.Lost) == 0 {
		return nil
	}
	var nos, refs, reasons []string
	for _, l := range page.Lost {
		nos, refs, reasons = append(nos, strconv.FormatInt(l.LinkNo, 10)), append(refs, l.Ref), append(reasons, l.Reason)
	}
	n, err := r.exec(ctx, tx, "write the links that lost a place", isoLostSQL, textArray(nos), textArray(refs), textArray(reasons))
	if err != nil {
		return err
	}
	if n != len(page.Lost) {
		return kernel.Errorf(kernel.CodeInternal, "%d links lost a place and %d of them were staged links", len(page.Lost), n)
	}
	c.Cardinality += n
	return r.takeLinks(ctx, tx, nos)
}
