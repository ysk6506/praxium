package pgingest

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// COPY into a table of the KG, the way the driver does it. A statement that begins with COPY, prepared inside a transaction,
// opens a COPY FROM STDIN; each Exec with the cells of one row sends the row, and an Exec with no cell ends the COPY. The
// cells are the types the driver encodes for COPY: strings, byte slices (bytea), integers, booleans and nil (NULL). An array
// or a document is a string in the text form PostgreSQL reads (textArray, bytesArray, and the JSON text of a jsonb).
//
// This file is the only one of the unit that prepares a statement or executes one on something that is not a transaction:
// the COPY of a transaction is not a prepared statement of the server and leaves nothing on the session.

// copyStatement is the text of the statement that copies the columns of a table of a source's partition. The partition is named
// itself so that the rows go straight in and no other partition is locked.
func copyStatement(kgNo, srcNo int64, table string, cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pgschema.QuoteIdent(c)
	}
	return "COPY " + pgschema.TableRef(kgNo, pgschema.SourcePartition(table, srcNo)) + " (" + strings.Join(quoted, ", ") + ") FROM STDIN"
}

// copier is one COPY that is open in a transaction.
type copier struct {
	ctx  context.Context
	stmt *sql.Stmt
	rows int
}

// openCopy starts a COPY of the columns into the partition of the source.
func openCopy(ctx context.Context, tx *sql.Tx, kgNo, srcNo int64, table string, cols []string) (*copier, error) {
	stmt, err := tx.PrepareContext(ctx, copyStatement(kgNo, srcNo, table, cols))
	if err != nil {
		return nil, dbErr(err, "start the COPY into "+table)
	}
	return &copier{ctx: ctx, stmt: stmt}, nil
}

// add sends one row.
func (c *copier) add(cells ...any) error {
	if _, err := c.stmt.ExecContext(c.ctx, cells...); err != nil {
		_ = c.stmt.Close()
		return dbErr(err, "send a row of the COPY")
	}
	c.rows++
	return nil
}

// finish ends the COPY, which is when the server takes the rows, and closes the statement.
func (c *copier) finish() error {
	if _, err := c.stmt.ExecContext(c.ctx); err != nil {
		_ = c.stmt.Close()
		return dbErr(err, "end the COPY")
	}
	return dbErr(c.stmt.Close(), "close the COPY")
}

// abort closes a COPY that is not finished, so that the connection can be given back; the transaction is rolled back by its
// owner and nothing of the COPY stays.
func (c *copier) abort() { _ = c.stmt.Close() }

// smallArray is the text of an array of smallint.
func smallArray(items []int) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(it))
	}
	b.WriteByte('}')
	return b.String()
}

// bytesArray is the text of an array of bytea: every element in double quotes, in the hex form with its backslash escaped.
func bytesArray(items [][]byte) string {
	const hex = "0123456789abcdef"
	var b strings.Builder
	b.WriteByte('{')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"\\x`)
		for _, v := range it {
			b.WriteByte(hex[v>>4])
			b.WriteByte(hex[v&15])
		}
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}
