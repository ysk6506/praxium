package pgsource

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// plan is what a dataset of a postgres source is read by: the columns of the source it needs, the columns its natural key
// is ordered by, and the statements that are made from them. It holds nothing of the source's data and no secret.
type plan struct {
	ds        ingest.Dataset
	builder   *ingest.RowBuilder
	from      string   // what the statements read from: the table, or the sql as a sub query, named q
	columns   []string // the columns of the source that the dataset's columns read, once each, in the order they come
	keys      []string // the column of the source of each natural key column
	collation string
	fp        string
	// types is the type of each column of the source (the name the driver gives), known after the description.
	types map[string]string
	// keyKinds says of each key column what the catalog of the source says its type is: "listed" (a type of the list, or a
	// domain over one), "enum", or the name of the type that is neither (known after the description).
	keyKinds map[string]string
}

// keyColumnName is the name of the column that carries the text of key column i, which the next page starts after.
func keyColumnName(i int) string { return "__praxium_key_" + strconv.Itoa(i) }

func planError(msg string) *ingest.SourceError {
	return sourceErr(ingest.SrcRequestRejected, msg, nil)
}

// newPlan reads the dataset. A natural key column has to be one column of the source exactly (one path with one step),
// because the order of the pages is the order of the source's column, and a path into a json column has no order of its own.
func newPlan(ds ingest.Dataset) (*plan, error) {
	if (ds.Table == "") == (ds.SQL == "") {
		return nil, planError("a dataset of a postgres source gives table or sql, one of them")
	}
	if len(ds.NaturalKey) == 0 {
		return nil, planError("a dataset of a postgres source needs a natural key: the pages are cut by it")
	}
	builder, err := ingest.NewRowBuilder(ds)
	if err != nil {
		return nil, planError("a column of the dataset has a path that is not a path")
	}
	p := &plan{ds: ds, builder: builder, collation: ds.KeyCollation}
	if err := p.readColumns(); err != nil {
		return nil, err
	}
	if err := p.readKeys(); err != nil {
		return nil, err
	}
	p.from = fromOf(ds)
	p.fp = fingerprint(p)
	return p, nil
}

// readColumns collects the columns of the source that the paths of the dataset's columns start at, once each.
func (p *plan) readColumns() error {
	seen := map[string]bool{}
	for _, c := range p.ds.Columns {
		paths := c.Paths
		if len(paths) == 0 {
			paths = []string{c.Name}
		}
		for _, text := range paths {
			path, err := ingest.ParsePath(text)
			if err != nil || len(path) == 0 {
				return planError("a column of the dataset has a path that is not a path")
			}
			if !seen[path[0]] {
				seen[path[0]] = true
				p.columns = append(p.columns, path[0])
			}
		}
	}
	return nil
}

func (p *plan) readKeys() error {
	for _, name := range p.ds.NaturalKey {
		col, ok := columnNamed(p.ds, name)
		if !ok {
			return planError("a natural key column is not a column of the dataset")
		}
		paths := col.Paths
		if len(paths) == 0 {
			paths = []string{col.Name}
		}
		if len(paths) != 1 {
			return planError("a natural key column reads more than one path; a postgres dataset orders its pages by one column of the source for each key column")
		}
		path, err := ingest.ParsePath(paths[0])
		if err != nil || len(path) != 1 {
			return planError("a natural key column reads a path into a value; it has to read one column of the source")
		}
		p.keys = append(p.keys, path[0])
	}
	return nil
}

func columnNamed(ds ingest.Dataset, name string) (ingest.Column, bool) {
	for _, c := range ds.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return ingest.Column{}, false
}

// fromOf is what a statement reads from. A table is quoted as the identifiers it is (the definition allows only plain
// identifiers, and they are used as written). The sql of the user is put in a sub query on lines of its own, so that a
// comment at its end does not reach the closing parenthesis, and one closing semicolon, which a sub query does not take,
// is dropped. What the sql holds beyond that is the server's to refuse: the statement is always prepared.
func fromOf(ds ingest.Dataset) string {
	if ds.Table != "" {
		parts := strings.Split(ds.Table, ".")
		for i, part := range parts {
			parts[i] = pq.QuoteIdentifier(part)
		}
		return strings.Join(parts, ".") + " AS q"
	}
	text := strings.TrimSpace(ds.SQL)
	text = strings.TrimSpace(strings.TrimSuffix(text, ";"))
	return "(\n" + text + "\n) AS q"
}

// fingerprint says which dataset a token belongs to: the table or the sql, the key columns and the collation. A
// token of another fingerprint is not read (the definition changed, so the position means nothing).
func fingerprint(p *plan) string {
	h := sha256.New()
	for _, part := range []string{"v1", p.ds.ID, p.ds.Table, "\x00", p.ds.SQL, "\x00", strings.Join(p.keys, "\x00"), p.collation} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// quoted is the column q."name".
func quoted(name string) string { return "q." + pq.QuoteIdentifier(name) }

// The types a column of the key may have: they sort the way a person expects, and the text the server prints for them
// gives the same value back when it is sent as the argument of the next page (the prototype of ADR-0093 sent each of
// them back). The driver gives an enum, and every type it does not know (an extension's, a composite), the empty name, so
// the name tells nothing there: the catalog of the source says which it is (describeKeys), and only an enum and a domain
// over a type of the list pass.
var keyTypes = map[string]bool{
	"INT2": true, "INT4": true, "INT8": true, "NUMERIC": true, "TEXT": true, "VARCHAR": true, "BPCHAR": true, "UUID": true,
	"DATE": true, "TIMESTAMP": true, "TIMESTAMPTZ": true, "TIME": true, "BOOL": true,
}

// catalogKeyTypes are the names of the types of pg_catalog that a key may have (the names of the catalog, which are not
// those of the driver).
var catalogKeyTypes = map[string]bool{
	"int2": true, "int4": true, "int8": true, "numeric": true, "text": true, "varchar": true, "bpchar": true, "uuid": true,
	"date": true, "timestamp": true, "timestamptz": true, "time": true, "bool": true,
}

var collatedTypes = map[string]bool{"TEXT": true, "VARCHAR": true, "BPCHAR": true}

// keyExpr is the column of key i as the pages order and compare it: with the collation of the dataset when the column is text.
func (p *plan) keyExpr(i int) string {
	expr := quoted(p.keys[i])
	if p.collation != "" && collatedTypes[p.types[p.keys[i]]] {
		expr += " COLLATE " + pq.QuoteIdentifier(p.collation)
	}
	return expr
}

// keyText is the text of key i that goes into the token. Equal values have one text (a numeric has its trailing zeros
// trimmed), so that the rows of one key are found by comparing texts.
func (p *plan) keyText(i int) string {
	if p.types[p.keys[i]] == "NUMERIC" {
		return "pg_catalog.trim_scale(" + quoted(p.keys[i]) + ")::text"
	}
	return quoted(p.keys[i]) + "::text"
}

// selectList is the columns of a page: the columns of the dataset (an array is turned into json by the server, because the
// driver gives it as a text that is no json) and then the text of each key.
func (p *plan) selectList() string {
	items := make([]string, 0, len(p.columns)+len(p.keys))
	for _, c := range p.columns {
		if strings.HasPrefix(p.types[c], "_") {
			items = append(items, "pg_catalog.to_jsonb("+quoted(c)+") AS "+pq.QuoteIdentifier(c))
			continue
		}
		items = append(items, quoted(c))
	}
	for i := range p.keys {
		items = append(items, p.keyText(i)+" AS "+keyColumnName(i))
	}
	return strings.Join(items, ", ")
}

func (p *plan) notNull() string {
	parts := make([]string, len(p.keys))
	for i, k := range p.keys {
		parts[i] = quoted(k) + " IS NOT NULL"
	}
	return strings.Join(parts, " AND ")
}

func (p *plan) keyExprs() string {
	parts := make([]string, len(p.keys))
	for i := range p.keys {
		parts[i] = p.keyExpr(i)
	}
	return strings.Join(parts, ", ")
}

func (p *plan) placeholders() string {
	parts := make([]string, len(p.keys))
	for i := range p.keys {
		parts[i] = "$" + strconv.Itoa(i+1)
	}
	return strings.Join(parts, ", ")
}

// describeSQL reads no row; it gives the names and types of the columns the statement has.
func (p *plan) describeSQL() string { return "SELECT q.* FROM " + p.from + " LIMIT 0" }

// pageSQL is one batch of rows in the order of the key, after a key when there is one. The rows whose key is NULL are
// not in it (they cannot be placed in the order); they are counted by nullKeySQL.
func (p *plan) pageSQL(after bool, limit int) string {
	where := p.notNull()
	if after {
		where += " AND (" + p.keyExprs() + ") > (" + p.placeholders() + ")"
	}
	return "SELECT " + p.selectList() + " FROM " + p.from + " WHERE " + where + " ORDER BY " + p.keyExprs() + " LIMIT " + strconv.Itoa(limit)
}

// groupSQL is the rows of one key, all of them (up to limit), for a key that fills a whole batch.
func (p *plan) groupSQL(limit int) string {
	return "SELECT " + p.selectList() + " FROM " + p.from + " WHERE " + p.notNull() + " AND (" + p.keyExprs() + ") = (" + p.placeholders() + ") LIMIT " + strconv.Itoa(limit)
}

// nullKeySQL counts the rows that have a NULL in a key column, by the first key column that is NULL and by whether every
// key column is: the two things a row without a natural key is told by.
func (p *plan) nullKeySQL() string {
	first := make([]string, 0, len(p.keys))
	nulls := make([]string, len(p.keys))
	for i, k := range p.keys {
		first = append(first, "WHEN "+quoted(k)+" IS NULL THEN "+strconv.Itoa(i))
		nulls[i] = quoted(k) + " IS NULL"
	}
	return "SELECT CASE " + strings.Join(first, " ") + " END, (" + strings.Join(nulls, " AND ") + "), pg_catalog.count(*) FROM " + p.from +
		" WHERE " + strings.Join(nulls, " OR ") + " GROUP BY 1, 2"
}
