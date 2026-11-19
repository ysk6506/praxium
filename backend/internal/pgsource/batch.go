package pgsource

import (
	"context"
	"database/sql"
	"slices"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// dbRow is a row of the source as a record the row builder reads, with the text of its key.
type dbRow struct {
	doc  map[string]any
	keys []string
}

// batchResult is what one read of a batch gives: the rows, of whole keys only; whether the source has no row after them;
// and whether one key has more rows than a page may hold.
type batchResult struct {
	rows   []dbRow
	bytes  int
	end    bool
	tooBig bool
	// changed is a batch that cannot be read as the batch of one state of the source: the rows of a key that filled the
	// last batch are gone when they are asked for (the statement of the dataset gives other rows in one transaction than
	// in another).
	changed bool
}

// describe reads the names and the types of the columns of the statement without reading a row, and refuses what the
// dataset cannot be read from: a column that is not there, a binary column, a key of a type that has no order the
// pages can follow.
func (p *plan) describe(ctx context.Context, tx *sql.Tx) error {
	rs, err := runStmt(ctx, tx, p.describeSQL())
	if err != nil {
		return err
	}
	cts, err := rs.ColumnTypes()
	if err != nil {
		_ = rs.Close()
		return err
	}
	p.types = make(map[string]string, len(cts))
	for _, ct := range cts {
		p.types[ct.Name()] = ct.DatabaseTypeName()
	}
	if err := rs.Close(); err != nil {
		return err
	}
	return p.describeKeys(ctx, tx)
}

// describeKeys asks the catalog of the source what the type of each key column is. The static type of the column is the
// type of a scalar sub query that reads it and no row (pg_typeof of it), so it is known for an empty table as well; a
// domain is followed down to its base.
func (p *plan) describeKeys(ctx context.Context, tx *sql.Tx) error {
	p.keyKinds = make(map[string]string, len(p.keys))
	for _, k := range p.keys {
		if _, ok := p.types[k]; !ok {
			continue // validate says it
		}
		kind, err := p.keyKind(ctx, tx, k)
		if err != nil {
			return err
		}
		p.keyKinds[k] = kind
	}
	return nil
}

const typeBySubquery = "pg_catalog.pg_typeof((SELECT %s FROM %s LIMIT 0))"

func (p *plan) keyKind(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	const cols = "t.typname::pg_catalog.text, t.typtype::pg_catalog.text, t.typnamespace = 'pg_catalog'::pg_catalog.regnamespace, t.typbasetype::pg_catalog.int8"
	query := "SELECT " + cols + " FROM pg_catalog.pg_type t WHERE t.oid = " + strings.Replace(strings.Replace(typeBySubquery, "%s", quoted(key), 1), "%s", p.from, 1)
	var args []any
	for depth := 0; depth < 8; depth++ {
		rs, err := runStmt(ctx, tx, query, args...)
		if err != nil {
			return "", err
		}
		var (
			name, typtype string
			inCatalog     bool
			base          int64
		)
		found := false
		if rs.Next() {
			found = true
			if err := rs.Scan(&name, &typtype, &inCatalog, &base); err != nil {
				_ = rs.Close()
				return "", err
			}
		}
		if err := rs.Close(); err != nil {
			return "", err
		}
		switch {
		case !found:
			return "unknown", nil
		case typtype == "e":
			return "enum", nil
		case typtype == "d":
			query = "SELECT " + cols + " FROM pg_catalog.pg_type t WHERE t.oid = $1::pg_catalog.oid"
			args = []any{base}
		case typtype == "b" && inCatalog && catalogKeyTypes[name]:
			return "listed", nil
		default:
			return cleanDetail(name), nil
		}
	}
	return "a domain that is too deep", nil
}

// validate says whether the described columns fit the dataset. It is the check of the "key columns" step.
func (p *plan) validate() error {
	for _, c := range p.columns {
		typ, ok := p.types[c]
		switch {
		case !ok:
			return planError("a column that the dataset reads is not in the result of the statement")
		case typ == "BYTEA":
			return planError("a column that the dataset reads is binary, and a binary column cannot be read as text")
		}
	}
	for _, k := range p.keys {
		typ, ok := p.types[k]
		if !ok {
			return planError("a natural key column is not in the result of the statement")
		}
		switch kind := p.keyKinds[k]; {
		case kind == "enum" && typ == "":
		case kind == "listed" && keyTypes[typ]:
		default:
			return planError("a natural key column has a type that has no order the pages can follow (a whole number, a numeric, a text, a uuid, a date or time, a boolean, an enum, or a domain over one of them can)").
				WithDetail("the key column has the type " + typeWords(kind, typ))
		}
	}
	return nil
}

// nullKeys counts the rows that have a NULL in a key column: the pages cannot place them in the order, so they are told
// apart here, and the page flow gives each as a row that has no natural key.
func (p *plan) nullKeys(ctx context.Context, tx *sql.Tx) ([]nullGroup, error) {
	rs, err := runStmt(ctx, tx, p.nullKeySQL())
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []nullGroup
	for rs.Next() {
		var g nullGroup
		if err := rs.Scan(&g.first, &g.all, &g.n); err != nil {
			return nil, err
		}
		if g.first < 0 || g.first >= len(p.keys) || g.n < 0 {
			return nil, sourceErr(ingest.SrcResponseFormat, "the answer of the source does not fit the statement that was sent", nil)
		}
		out = append(out, g)
	}
	return out, rs.Err()
}

// nullGroup is the rows whose first NULL key column is the one at first, and which are NULL in every key column if all.
type nullGroup struct {
	first int
	all   bool
	n     int64
}

// batch reads up to limit rows after a key, and not much more than maxBytes of them. A key whose rows may go on past the
// end of the batch is read whole or not at all: the rows of its end are left for the next batch, and a key that fills a
// batch by itself is read by the key alone.
func (p *plan) batch(ctx context.Context, tx *sql.Tx, after []string, limit, maxBytes int) (batchResult, error) {
	rs, err := runStmt(ctx, tx, p.pageSQL(after != nil, limit), stringArgs(after)...)
	if err != nil {
		return batchResult{}, err
	}
	rows, size, capped, err := p.scanRows(rs, limit, maxBytes)
	if cerr := rs.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return batchResult{}, err
	}
	if len(rows) < limit && !capped {
		return batchResult{rows: rows, bytes: size, end: true}, nil
	}
	last := rows[len(rows)-1].keys
	keep := len(rows)
	for keep > 0 && slices.Equal(rows[keep-1].keys, last) {
		keep--
	}
	if keep > 0 {
		return batchResult{rows: rows[:keep], bytes: size}, nil
	}
	return p.group(ctx, tx, last, limit)
}

// group reads all the rows of one key. More than limit of them is more than a page holds, and the read is cut there.
func (p *plan) group(ctx context.Context, tx *sql.Tx, key []string, limit int) (batchResult, error) {
	rs, err := runStmt(ctx, tx, p.groupSQL(limit+1), stringArgs(key)...)
	if err != nil {
		return batchResult{}, err
	}
	rows, size, _, err := p.scanRows(rs, limit+1, 0)
	if cerr := rs.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return batchResult{}, err
	}
	if len(rows) == 0 {
		return batchResult{changed: true}, nil
	}
	if len(rows) > limit {
		return batchResult{tooBig: true}, nil
	}
	return batchResult{rows: rows, bytes: size}, nil
}

func stringArgs(keys []string) []any {
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}

// scanRows reads the rows of a statement until limit rows, or until the cells are maxBytes (0 is no such limit). The
// rest of the rows of the statement is not read: closing the rows ends it.
func (p *plan) scanRows(rs *stmtRows, limit, maxBytes int) (rows []dbRow, size int, capped bool, err error) {
	cts, err := rs.ColumnTypes()
	if err != nil {
		return nil, 0, false, err
	}
	if len(cts) != len(p.columns)+len(p.keys) {
		return nil, 0, false, sourceErr(ingest.SrcResponseFormat, "the answer of the source has other columns than the statement that was sent", nil)
	}
	names := make([]string, len(cts))
	for i, ct := range cts {
		names[i] = ct.DatabaseTypeName()
	}
	for len(rows) < limit && rs.Next() {
		row, n, serr := p.scanRow(rs, names)
		if serr != nil {
			return nil, 0, false, serr
		}
		rows = append(rows, row)
		size += n
		if maxBytes > 0 && size >= maxBytes {
			return rows, size, true, nil
		}
	}
	return rows, size, false, rs.Err()
}

// scanRow reads the current row: the columns of the dataset as the values of a record, then the text of each key.
func (p *plan) scanRow(rs *stmtRows, names []string) (dbRow, int, error) {
	dest := make([]any, len(names))
	ptrs := make([]any, len(names))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := rs.Scan(ptrs...); err != nil {
		return dbRow{}, 0, err
	}
	row := dbRow{doc: make(map[string]any, len(p.columns)), keys: make([]string, len(p.keys))}
	size := 0
	for i, c := range p.columns {
		size += rawSize(dest[i])
		v, serr := convertValue(names[i], dest[i])
		if serr != nil {
			return dbRow{}, 0, serr
		}
		row.doc[c] = v
	}
	for i := range p.keys {
		text, ok := keyText(dest[len(p.columns)+i])
		if !ok {
			return dbRow{}, 0, sourceErr(ingest.SrcInternal, "the source gave a row without the text of its key", nil)
		}
		row.keys[i] = text
	}
	return row, size, nil
}

func keyText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		return string(x), true
	}
	return "", false
}

// rawSize is about how many bytes a value takes, for the limit of a batch's size.
func rawSize(v any) int {
	switch x := v.(type) {
	case string:
		return len(x)
	case []byte:
		return len(x)
	}
	return 8
}

func typeWords(kind, driverName string) string {
	if kind == "" || kind == "listed" || kind == "enum" {
		kind = strings.ToLower(driverName)
	}
	if kind == "" {
		kind = "that the server does not name"
	}
	return kind
}
