package pgingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The order of the difference (ADR-0127). The rows are in the order the apply writes them: the kinds in the order of their numbers (the
// objects, the keys, the links, the identifiers), the types in the order of theirs, and in a type the id of the object or the link in the
// order of its bytes (COLLATE "C") and then the tie. The order is numbered from 1 without a gap and the chunks from 0 without a gap; a
// chunk has the rows of one kind and one type, at most the rows of a chunk, so a chunk touches the tables of one type (ADR-0099). The
// numbers are worked out from what the rows are, so they do not hang on the order the run read or numbered anything in.

// diffPlanned is the plan of a kind and a type: how many rows, the number in the order before its first row and its first chunk.
type diffPlanned struct {
	kind, typ   int16
	rows        int
	ord         int64
	first, many int
}

// diffPlanOf works out the plan from the counts: the kinds and the types in order, each with the rows the counts give it.
func diffPlanOf(tally map[string]int, chunkRows int) ([]diffPlanned, error) {
	rows := map[[2]int16]int{}
	for key, n := range tally {
		v, err := diffTallyParts(key)
		if err != nil {
			return nil, err
		}
		rows[[2]int16{v[0], v[1]}] += n
	}
	keys := make([][2]int16, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b [2]int16) int {
		if a[0] != b[0] {
			return int(a[0]) - int(b[0])
		}
		return int(a[1]) - int(b[1])
	})
	var out []diffPlanned
	var ord int64
	chunk := 0
	for _, k := range keys {
		n := rows[k]
		many := (n + chunkRows - 1) / chunkRows
		out = append(out, diffPlanned{kind: k[0], typ: k[1], rows: n, ord: ord, first: chunk, many: many})
		ord += int64(n)
		chunk += many
	}
	return out, nil
}

// diffTallyParts reads a key of the counts (diffTallyKey): the kind, the type and the change.
func diffTallyParts(key string) ([3]int16, error) {
	var v [3]int16
	parts := strings.Split(key, "/")
	if len(parts) != 3 {
		return v, kernel.Errorf(kernel.CodeInternal, "the count %q of the difference is not of a kind, a type and a change", key)
	}
	for i, p := range parts {
		x, err := strconv.ParseInt(p, 10, 16)
		if err != nil {
			return v, kernel.Wrap(kernel.CodeInternal, err, "read the count "+key+" of the difference")
		}
		v[i] = int16(x)
	}
	return v, nil
}

// planUnit writes the plan of the order from the counts of the rows phase. A difference that has no row has no plan and no chunk, and its
// fingerprint is the chain of no chunk: the step is done.
func (r *diffRun) planUnit(ctx context.Context, tx *sql.Tx) error {
	plan, err := diffPlanOf(r.st.Tally, r.st.Chunk)
	if err != nil {
		return err
	}
	var kinds, types, lines, ords, firsts []string
	for _, p := range plan {
		kinds, types, lines = append(kinds, strconv.Itoa(int(p.kind))), append(types, strconv.Itoa(int(p.typ))), append(lines, strconv.Itoa(p.rows))
		ords, firsts = append(ords, strconv.FormatInt(p.ord, 10)), append(firsts, strconv.Itoa(p.first))
		r.st.Rows += p.rows
		r.st.Chunks += p.many
	}
	if len(plan) > 0 {
		if _, err := r.exec(ctx, tx, "write the plan of the order", diffPlanSQL, textArray(kinds), textArray(types), textArray(lines), textArray(ords), textArray(firsts)); err != nil {
			return err
		}
	}
	r.st.Phase, r.st.Kind, r.st.Type, r.st.Chain = ingest.DiffPhaseOrder, -1, -1, hex.EncodeToString(make([]byte, sha256.Size))
	if len(plan) == 0 {
		r.st.Phase, r.st.Finished = ingest.DiffPhaseDone, true
		return nil
	}
	return r.nextPlanned(ctx, tx)
}

// nextPlanned moves the order to the next kind and type of the plan, or to the print when there is none.
func (r *diffRun) nextPlanned(ctx context.Context, tx *sql.Tx) error {
	var kind, typ int16
	var lines int
	q := r.q(diffNextSQL)
	r.saw(tx, "find the next kind and type of the plan", q, r.st.Kind, r.st.Type)
	err := tx.QueryRowContext(ctx, q, r.st.Kind, r.st.Type).Scan(&kind, &typ, &lines)
	if isNoRows(err) {
		r.st.Phase = ingest.DiffPhasePrint
		return nil
	}
	if err != nil {
		return dbErr(err, "find the next kind and type of the plan")
	}
	r.st.Kind, r.st.Type, r.st.Did, r.st.Ref, r.st.Tie = kind, typ, 0, "", ""
	return nil
}

// orderUnit numbers the next rows of the kind and the type the order is at, a unit of them, and moves on to the next kind and type when
// the plan says they are all numbered. The rows the plan counted and the rows there are must agree.
func (r *diffRun) orderUnit(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	tie, err := hex.DecodeString(st.Tie)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "read the place in the order")
	}
	var lines int
	q := r.q("SELECT lines FROM {dplan} WHERE kind = $1 AND type_no = $2")
	r.saw(tx, "read the plan of the kind and the type", q, st.Kind, st.Type)
	if err := tx.QueryRowContext(ctx, q, st.Kind, st.Type).Scan(&lines); err != nil {
		return dbErr(err, "read the plan of the kind and the type")
	}
	var endRef string
	var endTie []byte
	q = r.q(diffEndSQL)
	r.saw(tx, "find the end of the range of the order", q, st.Kind, st.Type, st.Ref, tie, st.Size-1)
	err = tx.QueryRowContext(ctx, q, st.Kind, st.Type, st.Ref, tie, st.Size-1).Scan(&endRef, &endTie)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the range of the order")
	}
	args := []any{st.Kind, st.Type, st.Ref, tie, st.Did, st.Chunk}
	if !last {
		args = append(args, endRef, endTie)
	}
	n, err := r.exec(ctx, tx, "number the rows of the difference", diffOrderSQL(last), args...)
	if err != nil {
		return err
	}
	st.Did += n
	switch {
	case st.Did > lines || (last && st.Did != lines):
		return kernel.Errorf(kernel.CodeInternal, "the difference has %d rows of the kind %d and the type %d and its plan counted %d", st.Did, st.Kind, st.Type, lines)
	case last:
		return r.nextPlanned(ctx, tx)
	}
	st.Ref, st.Tie = endRef, hex.EncodeToString(endTie)
	return nil
}

// printUnit makes the digest of the next chunk (ChunkDigestSQL, the statement the apply makes it with) and chains it to the ones before.
// After the last chunk the chain is the fingerprint of the change.
func (r *diffRun) printUnit(ctx context.Context, tx *sql.Tx) error {
	before, err := kernel.ParseHash(r.st.Chain)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "read the chain of the print")
	}
	var chunk [sha256.Size]byte
	var d []byte
	q := pgschema.ChunkDigestSQL(r.kgNo)
	r.saw(tx, "make the digest of a chunk", q, r.st.Next)
	if err := tx.QueryRowContext(ctx, q, r.st.Next).Scan(&d); err != nil {
		return dbErr(err, "make the digest of a chunk")
	}
	if len(d) != sha256.Size {
		return kernel.Errorf(kernel.CodeInternal, "the difference has no chunk %d of the %d it planned", r.st.Next, r.st.Chunks)
	}
	copy(chunk[:], d)
	chain := pgschema.ChainDigest(before, chunk)
	r.st.Chain, r.st.Next = hex.EncodeToString(chain[:]), r.st.Next+1
	if r.st.Next == r.st.Chunks {
		r.st.Phase, r.st.Finished = ingest.DiffPhaseDone, true
	}
	return nil
}

// diffKindWords and diffChangeWords are the words the report names the codes by.
var (
	diffKindWords   = map[int16]string{pgschema.DiffKindObject: ingest.DiffObject, pgschema.DiffKindKey: ingest.DiffKey, pgschema.DiffKindLink: ingest.DiffLink, pgschema.DiffKindIdentifier: ingest.DiffIdentifier}
	diffChangeWords = map[int16]string{pgschema.ChangeKindAdded: ingest.DiffAdded, pgschema.ChangeKindChanged: ingest.DiffChanged, pgschema.ChangeKindRetired: ingest.DiffRetired}
)

// report is what the place says the step made.
func (st *diffState) report() (ingest.DiffCounts, error) {
	out := ingest.DiffCounts{Rows: st.Rows, Chunks: st.Chunks}
	h, err := kernel.ParseHash(st.Chain)
	if err != nil {
		return out, kernel.Wrap(kernel.CodeInternal, err, "read the fingerprint of the change")
	}
	out.Fingerprint = h
	for key, n := range st.Tally {
		v, err := diffTallyParts(key)
		if err != nil {
			return out, err
		}
		out.Tally = append(out.Tally, ingest.DiffTally{Kind: diffKindWords[v[0]], TypeNo: int(v[1]), Change: diffChangeWords[v[2]], Rows: n})
	}
	ingest.SortTally(out.Tally)
	return out, nil
}
