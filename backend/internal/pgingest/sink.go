package pgingest

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// Sink is the receiving half of the workbench for one run (ingest.StageSink). A page is one transaction: the attempt of the run
// is checked first and the place of the receiving is written to the row of the run, then the rows of the page are compared with
// the raw records and go in with COPY. A page that fails leaves nothing, not even its place.
//
// What the tables of ADR-0095 keep of a StagePage, and how (ADR-0098):
//   - stage_record has a row for every record of the page (row_no from 0), with its key, its place (the range of the row on its
//     sheet, else its line number), whether a rule filtered it and what the receiving found of it. The hash of the raw row and
//     its cells are only on the rows that are new or changed. The problems of a record are the jsonb of its row. The problems of
//     the page (a line that could not be read, a header that changed, a dataset that was not read to its end) have no record:
//     they are on one row of the page whose row_no is -1.
//   - stage_key, stage_end and stage_claim have the rows of the records that are new or changed only. A key, an end or a claim
//     that is held (the feed sees the row as one a rule filtered out) has held set; its feed or definition number is always the
//     number itself, never a complement. A claim names its scheme by the number SchemeNumbers gives. The parents a context key
//     needs are the arrays on its row, nearest first.
type Sink struct {
	st       *Store
	lease    ingest.RunLease
	kgNo     int64
	srcNo    int64
	datasets map[string]int64
	schemes  map[string]int
	skipped  map[string]map[string]int
}

var _ ingest.StageSink = (*Sink)(nil)

// The columns each COPY gives. A test holds them to the columns the tables have.
var (
	recordCols = []string{pgschema.ColSrcNo, pgschema.ColDatasetNo, pgschema.ColPage, pgschema.ColRowNo, pgschema.ColKey, pgschema.ColPlace,
		pgschema.ColFiltered, pgschema.ColChangeKind, pgschema.ColRawHash, pgschema.ColFields, pgschema.ColProblems}
	keyCols = []string{pgschema.ColSrcNo, pgschema.ColDatasetNo, pgschema.ColPage, pgschema.ColRowNo, pgschema.ColFeedNo, pgschema.ColKeyOrd,
		pgschema.ColTypeNo, pgschema.ColSigHash, pgschema.ColCandRef, pgschema.ColAncTypes, pgschema.ColAncSigs, pgschema.ColHeld}
	endCols = []string{pgschema.ColSrcNo, pgschema.ColDatasetNo, pgschema.ColPage, pgschema.ColRowNo, pgschema.ColDefNo, pgschema.ColSide,
		pgschema.ColLookupHash, pgschema.ColHeld}
	claimCols = []string{pgschema.ColSrcNo, pgschema.ColDatasetNo, pgschema.ColPage, pgschema.ColRowNo, pgschema.ColSchemeNo, pgschema.ColFeedNo,
		pgschema.ColValueHash, pgschema.ColValue, pgschema.ColHeld}
)

// pageRow is the row_no of the row that holds the problems of a page.
const pageRow = -1

// Stage stores one page. It is one transaction, and does not keep the page after it returns.
func (k *Sink) Stage(ctx context.Context, page ingest.StagePage) error {
	dataset, ok := k.datasets[page.Dataset]
	if !ok {
		return kernel.Errorf(kernel.CodeInvalidValue, "the dataset %q is not one of the datasets put to the source", page.Dataset)
	}
	k.countSkipped(page)
	return k.st.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		if err := k.writePlace(ctx, tx, dataset, page); err != nil {
			return err
		}
		w := pageWriter{ctx: ctx, tx: tx, sink: k, dataset: dataset, page: page}
		return w.write()
	})
}

// countSkipped adds up the lines the reader passed over on purpose, for the report of the run: they are no records.
func (k *Sink) countSkipped(page ingest.StagePage) {
	for why, n := range page.Skipped {
		if k.skipped[page.Dataset] == nil {
			k.skipped[page.Dataset] = map[string]int{}
		}
		k.skipped[page.Dataset][why] += n
	}
}

// writePlace writes where the receiving stands to the row of the run, with the attempt as the condition, and lets the lease
// run on from now. A run that another attempt holds, or that is not receiving, changes nothing and the page is not written.
func (k *Sink) writePlace(ctx context.Context, tx *sql.Tx, dataset int64, page ingest.StagePage) error {
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColRecvData+" = $3, "+pgschema.ColRecvPage+" = $4, "+pgschema.ColRecvToken+
		" = $5, "+pgschema.ColLeaseUntil+" = "+lease("$6")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+
		string(ingest.RunReceiving)+"'", k.lease.No, k.lease.Attempt, dataset, page.Page, encodeToken(page.Token, page.Last), k.lease.Term.Seconds())
	if err != nil {
		return dbErr(err, "write the place of the receiving")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, k.lease)
	}
	return nil
}

// pageWriter writes the rows of one page in the transaction.
type pageWriter struct {
	ctx     context.Context
	tx      *sql.Tx
	sink    *Sink
	dataset int64
	page    ingest.StagePage
	change  []int16
}

func (w *pageWriter) write() error {
	if err := w.compare(); err != nil {
		return err
	}
	for _, step := range []func() error{w.records, w.keys, w.ends, w.claims} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// fresh says whether a record is one whose fields and keys go in: it is new or it changed.
func (w *pageWriter) fresh(no int) bool {
	return no >= 0 && no < len(w.change) && w.change[no] != pgschema.ReceiveSame
}

type rawState struct {
	hash []byte
	fate int16
}

// compare finds what the receiving makes of each record of the page: it is new when the raw records have no record of its key,
// it changed when the hash of its raw row is another one or when a rule filters it now and did not (or the other way round), and it is
// the same otherwise. Whether a record that is no longer shown is unseen, and which of two records that have a key is the one
// that is used, is not told here: they are for the step that compares the whole source.
func (w *pageWriter) compare() error {
	s := w.sink
	keys := make([]string, len(w.page.Records))
	for i, r := range w.page.Records {
		keys[i] = r.Key
	}
	rows, err := w.tx.QueryContext(w.ctx, "SELECT "+pgschema.ColKey+", "+pgschema.ColRawHash+", "+pgschema.ColFate+" FROM "+
		pgschema.TableRef(s.kgNo, pgschema.SourcePartition(pgschema.TableRawRecord, s.srcNo))+" WHERE "+pgschema.ColDatasetNo+" = $1 AND "+
		pgschema.ColKey+" = ANY($2::text[])", w.dataset, textArray(keys))
	if err != nil {
		return dbErr(err, "look for the raw records of the page")
	}
	defer func() { _ = rows.Close() }()
	old := make(map[string]rawState, len(keys))
	for rows.Next() {
		var key string
		var st rawState
		if err := rows.Scan(&key, &st.hash, &st.fate); err != nil {
			return dbErr(err, "read the raw records of the page")
		}
		old[key] = st
	}
	if err := rows.Err(); err != nil {
		return dbErr(err, "read the raw records of the page")
	}
	w.change = make([]int16, len(w.page.Records))
	for i, r := range w.page.Records {
		if r.No != i {
			return kernel.Errorf(kernel.CodeInternal, "the record %d of the page is numbered %d", i, r.No)
		}
		h, err := rawHashBytes(r)
		if err != nil {
			return err
		}
		w.change[i] = decide(r, h, old)
	}
	return nil
}

func rawHashBytes(r ingest.StageRecord) ([]byte, error) {
	h, err := kernel.ParseHash(r.RawHash)
	if err != nil {
		return nil, err
	}
	return h[:], nil
}

// decide is the change of one record against what the raw records keep of its key.
func decide(r ingest.StageRecord, hash []byte, old map[string]rawState) int16 {
	st, ok := old[r.Key]
	switch {
	case !ok:
		return pgschema.ReceiveNew
	case string(st.hash) != string(hash):
		return pgschema.ReceiveChanged
	case r.Filtered != (st.fate == pgschema.FateFiltered || st.fate == pgschema.FateUnused):
		return pgschema.ReceiveChanged
	}
	return pgschema.ReceiveSame
}

func (w *pageWriter) base(no int) []any {
	return []any{w.sink.srcNo, w.dataset, int64(w.page.Page), int64(no)}
}

// place is where a record stood in its source, for the person who looks for it: the range of the row on its sheet when it has
// one, else its line number.
func place(r ingest.StageRecord) string {
	switch {
	case r.Where != "":
		return r.Where
	case r.Line > 0:
		return strconv.Itoa(r.Line)
	}
	return ""
}

// records copies a row for every record of the page, and the row of the problems of the page when it has any.
func (w *pageWriter) records() error {
	problems, err := groupProblems(w.page.Problems)
	if err != nil {
		return err
	}
	c, err := openCopy(w.ctx, w.tx, w.sink.kgNo, w.sink.srcNo, pgschema.TableStageRecord, recordCols)
	if err != nil {
		return err
	}
	for _, r := range w.page.Records {
		var hash, fields any
		if w.fresh(r.No) {
			h, _ := rawHashBytes(r)
			hash, fields = h, textArray(r.Fields)
		}
		if err := c.add(append(w.base(r.No), r.Key, place(r), r.Filtered, int64(w.change[r.No]), hash, fields, problems[r.No])...); err != nil {
			return err
		}
	}
	if pg, ok := problems[pageRow]; ok {
		if err := c.add(append(w.base(pageRow), "", "", false, int64(pgschema.ReceiveSame), nil, nil, pg)...); err != nil {
			return err
		}
	}
	return c.finish()
}

// keys copies the keys of the records that are new or changed, each with the parents it needs on its row.
func (w *pageWriter) keys() error {
	type at struct{ no, feed, ord int }
	parents := map[at][]ingest.StageParent{}
	for _, p := range w.page.Parents {
		k := at{p.No, p.Feed, p.Ord}
		parents[k] = append(parents[k], p)
	}
	c, err := w.copyOf(pgschema.TableStageKey, keyCols, len(w.page.Keys))
	if err != nil || c == nil {
		return err
	}
	for _, k := range w.page.Keys {
		if !w.fresh(k.No) {
			continue
		}
		var types []int
		var sigs [][]byte
		for _, p := range parents[at{k.No, k.Feed, k.Ord}] {
			types, sigs = append(types, p.TypeNo), append(sigs, p.SigHash[:])
		}
		var cand, ancTypes, ancSigs any
		if k.Cand != "" {
			cand = k.Cand
		}
		if len(types) > 0 {
			ancTypes, ancSigs = smallArray(types), bytesArray(sigs)
		}
		if err := c.add(append(w.base(k.No), int64(k.Feed), int64(k.Ord), int64(k.TypeNo), k.SigHash[:], cand, ancTypes, ancSigs, k.Held)...); err != nil {
			return err
		}
	}
	return c.finish()
}

// ends copies the keys the ends of the links find their other end by, of the records that are new or changed.
func (w *pageWriter) ends() error {
	c, err := w.copyOf(pgschema.TableStageEnd, endCols, len(w.page.Ends))
	if err != nil || c == nil {
		return err
	}
	for _, e := range w.page.Ends {
		if !w.fresh(e.No) {
			continue
		}
		side := pgschema.RefRoleLinkFrom
		if e.Side == "to" {
			side = pgschema.RefRoleLinkTo
		}
		if err := c.add(append(w.base(e.No), int64(e.Def), int64(side), e.LookupHash[:], e.Held)...); err != nil {
			return err
		}
	}
	return c.finish()
}

// claims copies the identifiers the records that are new or changed say their objects have.
func (w *pageWriter) claims() error {
	c, err := w.copyOf(pgschema.TableStageClaim, claimCols, len(w.page.Claims))
	if err != nil || c == nil {
		return err
	}
	for _, cl := range w.page.Claims {
		if !w.fresh(cl.No) {
			continue
		}
		scheme, ok := w.sink.schemes[cl.Scheme]
		if !ok {
			c.abort()
			return kernel.Errorf(kernel.CodeInvalidValue, "the model has no identifier scheme %q", cl.Scheme)
		}
		if err := c.add(append(w.base(cl.No), int64(scheme), int64(cl.Feed), cl.ValueHash[:], cl.Value, cl.Held)...); err != nil {
			return err
		}
	}
	return c.finish()
}

// copyOf opens the COPY of a table, or nothing when the page has no row for it.
func (w *pageWriter) copyOf(table string, cols []string, rows int) (*copier, error) {
	if rows == 0 {
		return nil, nil
	}
	return openCopy(w.ctx, w.tx, w.sink.kgNo, w.sink.srcNo, table, cols)
}
