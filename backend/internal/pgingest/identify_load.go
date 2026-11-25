package pgingest

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The first phases of the identification put the keys of the records in the computation: the records of the run get the ones whose
// references are not solved, the keys the receiving made are copied, and Go makes the keys of the records that have none.

const (
	whyUnresolved = pgschema.CalcWhyUnresolved
	roleIdentity  = pgschema.RefRoleIdentity
)

// seed adds the records whose references are not solved to the records of the run, a range of the list of them at a time. A record
// that is in the run already (it changed, or it is closed) stays as the comparison made it. A record whose cells were kept under
// another declaration than the dataset has now is left out: it is not read by a declaration it was not made by, and the solved
// references of it are not touched (ADR-0108).
func (r *identRun) seed(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	ref := r.t(pgschema.TableRecordRef)
	var end struct {
		src, ds sql.NullInt64
		key     sql.NullString
	}
	err := tx.QueryRowContext(ctx, "SELECT src_no, dataset_no, key FROM "+ref+" WHERE state <> "+strconv.Itoa(int(pgschema.RefResolved))+
		" AND (src_no, dataset_no, key) > ($1, $2, $3) ORDER BY src_no, dataset_no, key OFFSET $4 LIMIT 1", st.Cur, st.Cur2, st.CurKey, st.Size).
		Scan(&end.src, &end.ds, &end.key)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the unit")
	}
	cond := "(src_no, dataset_no, key) > ($1, $2, $3)"
	args := []any{st.Cur, st.Cur2, st.CurKey}
	if !last {
		cond += " AND (src_no, dataset_no, key) <= ($4, $5, $6)"
		args = append(args, end.src.Int64, end.ds.Int64, end.key.String)
	}
	raw, rec := r.t(pgschema.TableRawRecord), r.t(pgschema.TableCalcRecord)
	n, err := exec(ctx, tx, "add the records that are not solved", "WITH u AS (SELECT DISTINCT src_no, dataset_no, key FROM "+ref+" WHERE state <> "+
		strconv.Itoa(int(pgschema.RefResolved))+" AND "+cond+") INSERT INTO "+rec+" (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why) "+
		"SELECT w.src_no, w.dataset_no, w.key, w.fields, w.raw_hash, w.decl_rev, w.opened_seq, w.fate, "+strconv.Itoa(int(whyUnresolved))+
		" FROM u JOIN "+raw+" w ON w.src_no = u.src_no AND w.dataset_no = u.dataset_no AND w.key = u.key JOIN "+pgschema.TableSourceDataset+
		" d ON d.dataset_no = w.dataset_no AND d.decl_rev = w.decl_rev ON CONFLICT (src_no, dataset_no, key) DO NOTHING", args...)
	if err != nil {
		return err
	}
	st.Counts.Seeded += n
	if last {
		st.Phase, st.Cur, st.Cur2, st.CurKey = phaseKeys, 0, 0, ""
		return nil
	}
	st.Cur, st.Cur2, st.CurKey = end.src.Int64, end.ds.Int64, end.key.String
	return nil
}

// copyKeys copies the keys, the ends and the claims of the rows received that a record of the run was made of, a range of the pages
// of a dataset at a time. The rows received are read by the range of their pages (their index is a BRIN, which is how they were put
// in) and met with the records of the run by the place of the row (ADR-0108).
func (r *identRun) copyKeys(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	if int(st.Cur) >= len(st.Datasets) {
		return r.endKeys(ctx, tx)
	}
	d := st.Datasets[st.Cur]
	hi := st.Cur2 + int64(st.Pages)
	src := strconv.FormatInt(r.srcNo, 10)
	join := " JOIN " + r.t(pgschema.TableCalcRecord) + " c ON c.src_no = " + src + " AND c.dataset_no = k.dataset_no AND c.page = k.page AND c.row_no = k.row_no" +
		" WHERE k.dataset_no = $1 AND k.page >= $2 AND k.page < $3"
	part := func(table string) string { return r.t(pgschema.SourcePartition(table, r.srcNo)) }
	steps := []struct{ what, query string }{
		{"copy the keys", "INSERT INTO " + r.t(pgschema.TableCalcKey) + " (rid, feed_no, key_ord, type_no, sig_hash, cand_ref, anc_types, anc_sigs, held) " +
			"SELECT c.rid, k.feed_no, k.key_ord, k.type_no, k.sig_hash, k.cand_ref, k.anc_types, k.anc_sigs, k.held FROM " + part(pgschema.TableStageKey) + " k" + join},
		{"copy the ends", "INSERT INTO " + r.t(pgschema.TableCalcEnd) + " (rid, def_no, side, lookup_hash, held) " +
			"SELECT c.rid, k.def_no, k.side, k.lookup_hash, k.held FROM " + part(pgschema.TableStageEnd) + " k" + join},
		{"copy the claims", "INSERT INTO " + r.t(pgschema.TableCalcClaim) + " (rid, scheme_no, feed_no, value_hash, value, held) " +
			"SELECT c.rid, k.scheme_no, k.feed_no, k.value_hash, k.value, k.held FROM " + part(pgschema.TableStageClaim) + " k" + join},
	}
	for _, s := range steps {
		if _, err := exec(ctx, tx, s.what, s.query, d.DatasetNo, st.Cur2, hi); err != nil {
			return err
		}
	}
	if hi >= int64(d.Pages) {
		st.Cur, st.Cur2 = st.Cur+1, 0
	} else {
		st.Cur2 = hi
	}
	return nil
}

// endKeys is the end of the copy: the records are all in, so the largest number of one fixes the range the later phases go through.
func (r *identRun) endKeys(ctx context.Context, tx *sql.Tx) error {
	max, err := scanInt(ctx, tx, "read the last record", "SELECT max(rid) FROM "+r.t(pgschema.TableCalcRecord))
	if err != nil {
		return err
	}
	st := r.st
	st.MaxRid, st.Cur, st.Cur2, st.Phase = max, 0, 0, phaseRebuild
	return nil
}

// RebuildPage gives the records of the unit that have no key: see ingest.Identifier. A record that the comparison closed has no cells
// and none is made of it.
func (s *Store) RebuildPage(ctx context.Context, lease ingest.RunLease) (ingest.RebuildPage, error) {
	page := ingest.RebuildPage{}
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := loadIdent(ctx, tx, lease)
		if err != nil {
			return err
		}
		if r.st.Phase != phaseRebuild {
			return kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d is at %s, not at the keys of Go", lease.No, r.st.Phase)
		}
		page.Unit = int(r.st.Cur)
		rows, err := tx.QueryContext(ctx, "SELECT c.rid, c.src_no, d.dataset_id, c.key, c.fields FROM "+r.t(pgschema.TableCalcRecord)+" c JOIN "+
			pgschema.TableSourceDataset+" d ON d.dataset_no = c.dataset_no AND d.decl_rev = c.decl_rev WHERE c.rid > $1 AND c.rid <= $2 AND c.fields IS NOT NULL "+
			"AND NOT EXISTS (SELECT 1 FROM "+r.t(pgschema.TableCalcKey)+" k WHERE k.rid = c.rid) ORDER BY c.rid", r.st.Cur, r.st.Cur+int64(r.st.Size))
		if err != nil {
			return dbErr(err, "read the records that have no key")
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var row ingest.RebuildRow
			var cells string
			if err := rows.Scan(&row.Rid, &row.SrcNo, &row.Dataset, &row.Key, &cells); err != nil {
				return dbErr(err, "read a record")
			}
			if row.Fields, err = parseTextArray(cells); err != nil {
				return err
			}
			page.Rows = append(page.Rows, row)
		}
		return dbErr(rows.Err(), "read the records that have no key")
	})
	return page, err
}

// PutRebuilt takes the keys Go made and moves to the next unit: see ingest.Identifier.
func (s *Store) PutRebuilt(ctx context.Context, lease ingest.RunLease, unit int, rows []ingest.RebuiltRow) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := loadIdent(ctx, tx, lease)
		if err != nil {
			return err
		}
		st := r.st
		if st.Phase != phaseRebuild || int(st.Cur) != unit {
			return kernel.Errorf(ingest.CodeRunStage, "the run %d is at %s %d, not at the unit %d of the keys of Go", lease.No, st.Phase, st.Cur, unit)
		}
		if err := r.putKeys(ctx, tx, rows); err != nil {
			return err
		}
		skipped, err := scanInt(ctx, tx, "count the records left out", "SELECT count(*) FROM "+r.t(pgschema.TableCalcRecord)+" c JOIN "+pgschema.TableSourceDataset+
			" d ON d.dataset_no = c.dataset_no AND d.decl_rev <> c.decl_rev WHERE c.rid > $1 AND c.rid <= $2 AND c.fields IS NOT NULL AND c.why <> "+
			strconv.Itoa(int(pgschema.CalcWhyNew))+" AND c.why <> "+strconv.Itoa(int(pgschema.CalcWhyChanged)), st.Cur, st.Cur+int64(st.Size))
		if err != nil {
			return err
		}
		st.Counts.Skipped += int(skipped)
		st.Counts.Rebuilt += len(rows)
		st.Cur += int64(st.Size)
		if st.Cur >= st.MaxRid {
			st.Phase, st.Cur, st.Cur2, st.Round, st.Turn = phaseSlot, 0, 0, 1, 0
		}
		return writeIdent(ctx, tx, lease, st, false)
	})
}

// putKeys writes the keys, the ends and the claims Go made, by COPY.
func (r *identRun) putKeys(ctx context.Context, tx *sql.Tx, rows []ingest.RebuiltRow) error {
	var keys, ends, claims *copier
	open := func(c **copier, table string, cols []string) error {
		if *c != nil {
			return nil
		}
		quoted := make([]string, len(cols))
		for i, col := range cols {
			quoted[i] = pgschema.QuoteIdent(col)
		}
		stmt, err := tx.PrepareContext(ctx, "COPY "+r.t(table)+" ("+strings.Join(quoted, ", ")+") FROM STDIN")
		if err != nil {
			return dbErr(err, "start the COPY into "+table)
		}
		*c = &copier{ctx: ctx, stmt: stmt}
		return nil
	}
	abort := func() {
		for _, c := range []*copier{keys, ends, claims} {
			if c != nil {
				c.abort()
			}
		}
	}
	for _, row := range rows {
		parents := map[[2]int][]ingest.StageParent{}
		for _, p := range row.Parents {
			parents[[2]int{p.Feed, p.Ord}] = append(parents[[2]int{p.Feed, p.Ord}], p)
		}
		for _, k := range row.Keys {
			if err := open(&keys, pgschema.TableCalcKey, []string{pgschema.ColRid, pgschema.ColFeedNo, pgschema.ColKeyOrd, pgschema.ColTypeNo, pgschema.ColSigHash,
				pgschema.ColCandRef, pgschema.ColAncTypes, pgschema.ColAncSigs, pgschema.ColHeld}); err != nil {
				abort()
				return err
			}
			var types []int
			var sigs [][]byte
			for _, p := range parents[[2]int{k.Feed, k.Ord}] {
				types, sigs = append(types, p.TypeNo), append(sigs, append([]byte(nil), p.SigHash[:]...))
			}
			var cand, at, as any
			if k.Cand != "" {
				cand = k.Cand
			}
			if len(types) > 0 {
				at, as = smallArray(types), bytesArray(sigs)
			}
			if err := keys.add(row.Rid, int64(k.Feed), int64(k.Ord), int64(k.TypeNo), k.SigHash[:], cand, at, as, k.Held); err != nil {
				abort()
				return err
			}
		}
		for _, e := range row.Ends {
			if err := open(&ends, pgschema.TableCalcEnd, []string{pgschema.ColRid, pgschema.ColDefNo, pgschema.ColSide, pgschema.ColLookupHash, pgschema.ColHeld}); err != nil {
				abort()
				return err
			}
			side := pgschema.RefRoleLinkFrom
			if e.Side == "to" {
				side = pgschema.RefRoleLinkTo
			}
			if err := ends.add(row.Rid, int64(e.Def), int64(side), e.LookupHash[:], e.Held); err != nil {
				abort()
				return err
			}
		}
		for _, c := range row.Claims {
			scheme, ok := r.st.Schemes[c.Scheme]
			if !ok {
				abort()
				return kernel.Errorf(kernel.CodeInvalidValue, "the identifier scheme %q has no number", c.Scheme)
			}
			if err := open(&claims, pgschema.TableCalcClaim, []string{pgschema.ColRid, pgschema.ColSchemeNo, pgschema.ColFeedNo, pgschema.ColValueHash, pgschema.ColValue, pgschema.ColHeld}); err != nil {
				abort()
				return err
			}
			if err := claims.add(row.Rid, int64(scheme), int64(c.Feed), c.ValueHash[:], c.Value, c.Held); err != nil {
				abort()
				return err
			}
		}
	}
	for _, c := range []*copier{keys, ends, claims} {
		if c != nil {
			if err := c.finish(); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseTextArray reads the text form of an array of text that the database gives.
func parseTextArray(text string) ([]string, error) {
	if len(text) < 2 || text[0] != '{' || text[len(text)-1] != '}' {
		return nil, kernel.Errorf(kernel.CodeInternal, "the cells of a record are not an array: %q", text)
	}
	body := text[1 : len(text)-1]
	out := []string{}
	for i := 0; i < len(body); {
		if body[i] == '"' {
			var b strings.Builder
			i++
			for ; i < len(body) && body[i] != '"'; i++ {
				if body[i] == '\\' && i+1 < len(body) {
					i++
				}
				b.WriteByte(body[i])
			}
			out = append(out, b.String())
			i += 2
			continue
		}
		j := strings.IndexByte(body[i:], ',')
		if j < 0 {
			j = len(body) - i
		}
		cell := body[i : i+j]
		if cell == "NULL" {
			cell = ""
		}
		out = append(out, cell)
		i += j + 1
	}
	return out, nil
}
