package pgingest

import (
	"context"
	"database/sql"
	"math"
	"sort"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The end of a search for the groups that its turns did not end (ADR-0108): Go reads the pairs of the groups that are not whole and
// writes the labels of the groups they make (a turn more, whose label for each pair is the smallest pair of its group). The pairs of
// the groups that were whole are copied to that turn by the database, and the round goes on as if the search had ended.

// settleAt checks that the place is at the end of a search and gives the run.
func (s *Store) settleAt(ctx context.Context, tx *sql.Tx, lease ingest.RunLease) (*identRun, error) {
	r, err := s.loadIdent(ctx, tx, lease)
	if err != nil {
		return nil, err
	}
	if r.st.Phase != phaseSettle {
		return nil, kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d is at %s, not at the end of a search for the groups", lease.No, r.st.Phase)
	}
	return r, nil
}

// SettleRead gives the pairs of the groups with the labels of a range, that are not whole, and the keys of each: see ingest.Identifier.
func (s *Store) SettleRead(ctx context.Context, lease ingest.RunLease, after int64) (ingest.SettlePage, error) {
	page := ingest.SettlePage{}
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.settleAt(ctx, tx, lease)
		if err != nil {
			return err
		}
		page.Written = r.st.Cur2
		var end sql.NullInt64
		endQuery := r.q("SELECT label FROM {label} WHERE round_no = {R} AND iter = {F} AND label > $1 ORDER BY label OFFSET $2 LIMIT 1")
		r.saw(tx, "settle end", endQuery, after, r.st.Size)
		err = tx.QueryRowContext(ctx, endQuery, after, r.st.Size).Scan(&end)
		page.Done = isNoRows(err)
		if err != nil && !page.Done {
			return dbErr(err, "find the end of the unit")
		}
		hi := int64(math.MaxInt64)
		if !page.Done {
			hi = end.Int64
		}
		page.Next = hi
		query := r.q("WITH " + unsettledSQL + "up AS (SELECT l.pid, s.type_no, s.ctx_no, encode(s.sig_hash, 'hex') AS h FROM bad" +
			scan("x.pid", "{label}", "l", "x.round_no = {R} AND x.iter = {F} AND x.label = bad.label") +
			scan("x.type_no, x.ctx_no, x.sig_hash", "{slot}", "s", "x.round_no = {R} AND x.pid = l.pid") + ") SELECT pid, type_no, ctx_no, h FROM up ORDER BY pid")
		r.saw(tx, "read the pairs of the groups that are not whole", query, after, hi)
		rows, err := tx.QueryContext(ctx, query, after, hi)
		if err != nil {
			return dbErr(err, "read the pairs of the groups that are not whole")
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var pid, typ, ctxNo int64
			var h string
			if err := rows.Scan(&pid, &typ, &ctxNo, &h); err != nil {
				return dbErr(err, "read a pair")
			}
			key := strconv.FormatInt(typ, 10) + ":" + strconv.FormatInt(ctxNo, 10) + ":" + h
			if n := len(page.Pairs); n > 0 && page.Pairs[n-1].Pid == pid {
				page.Pairs[n-1].Keys = append(page.Pairs[n-1].Keys, key)
			} else {
				page.Pairs = append(page.Pairs, ingest.SettlePair{Pid: pid, Keys: []string{key}})
			}
		}
		return dbErr(rows.Err(), "read the pairs of the groups that are not whole")
	})
	return page, err
}

// PutSettled writes the labels of a page of pairs and, with the last page, goes on to the copy of the others: see ingest.Identifier.
func (s *Store) PutSettled(ctx context.Context, lease ingest.RunLease, rows []ingest.SettledPair, end ingest.SettleEnd) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.settleAt(ctx, tx, lease)
		if err != nil {
			return err
		}
		st := r.st
		if len(rows) > 0 {
			sort.Slice(rows, func(i, j int) bool { return rows[i].Pid < rows[j].Pid })
			pids, labels := make([]string, len(rows)), make([]string, len(rows))
			for i, row := range rows {
				pids[i], labels[i] = strconv.FormatInt(row.Pid, 10), strconv.FormatInt(row.Label, 10)
			}
			n, err := r.exec(ctx, tx, "write the labels of the groups that were not whole", r.q("INSERT INTO {label} (round_no, iter, pid, label) "+
				"SELECT {R}, {F} + 1, u.pid, u.label FROM unnest($1::text::bigint[], $2::text::bigint[]) AS u(pid, label)"), textArray(pids), textArray(labels))
			if err != nil {
				return err
			}
			if n != len(rows) {
				return kernel.Errorf(kernel.CodeInternal, "%d of %d labels were written", n, len(rows))
			}
			st.Cur2 = rows[len(rows)-1].Pid
		}
		if end.Last {
			st.Skipped, st.Phase, st.Cur = end.Skipped, phaseCopy, 0
		}
		return writeIdent(ctx, tx, lease, st, false)
	})
}

// copyLabels is a unit of the copy of the labels that Go did not write to the turn it wrote: the pairs of the groups that were whole.
// When Go wrote none (the pairs were more than it holds) the groups are not whole still, and the round decides as one that was stopped.
func (r *identRun) copyLabels(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	var end sql.NullInt64
	endQuery := r.q("SELECT pid FROM {label} WHERE round_no = {R} AND iter = {F} AND pid > $1 ORDER BY pid OFFSET $2 LIMIT 1")
	r.saw(tx, "copy end", endQuery, st.Cur, st.Size)
	err := tx.QueryRowContext(ctx, endQuery, st.Cur, st.Size).Scan(&end)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the unit")
	}
	hi := int64(math.MaxInt64)
	if !last {
		hi = end.Int64
	}
	if _, err := r.exec(ctx, tx, "copy the labels of the groups that were whole", r.q("INSERT INTO {label} (round_no, iter, pid, label) SELECT {R}, {F} + 1, o.pid, o.label FROM {label} o"+
		absent("{label}", "h", "x.round_no = {R} AND x.iter = {F} + 1 AND x.pid = o.pid")+
		" WHERE o.round_no = {R} AND o.iter = {F} AND o.pid > $1 AND o.pid <= $2 AND h.hit IS NULL"), st.Cur, hi); err != nil {
		return err
	}
	// the labels of the turn before the last are not read again: two turns are kept, as the turns keep them
	if _, err := r.exec(ctx, tx, "take away the labels that are not read again", r.q("DELETE FROM {label} WHERE round_no = {R} AND iter = {F} - 1 AND pid > $1 AND pid <= $2"), st.Cur, hi); err != nil {
		return err
	}
	if !last {
		st.Cur = hi
		return nil
	}
	st.Turn, st.Unsettled, st.Phase, st.Cur = st.Turn+1, st.Skipped, phaseDecide, 0
	return r.analyze(ctx, tx, pgschema.TableCalcLabel)
}
