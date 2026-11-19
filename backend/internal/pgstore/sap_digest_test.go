package pgstore

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The digests of the staged apply are made in SQL, over the rows that are in the table, and the Go functions of
// adapter.pgschema are what they are checked against. Every name starts with sap.

// The hash of an identifier row and of a key row, made by the expressions of SQL, are those of the Go functions, for
// texts that are empty, plain, long and not Latin.
//
// [REQ-726]
func TestSapRowHashExpressionsGiveTheValuesOfTheGoFunctions(t *testing.T) {
	_, admin := sxOpen(t)
	long := string(bytes.Repeat([]byte("abc"), 200))
	for _, c := range [][3]string{{"is.serial", "SN-1", ""}, {"", "", ""}, {"is.name", "서버-01 \U0001F600", "scope"}, {"s", long, long}, {"é", "ß", "日本語"}} {
		want := pgschema.IdentRowHash(c[0], c[1], c[2])
		var got []byte
		q := `SELECT ` + pgschema.IdentRowHashExpr("x") + ` FROM (SELECT $1::text AS scheme, $2::text AS value, $3::text AS scope) x`
		if err := admin.QueryRowContext(context.Background(), q, c[0], c[1], c[2]).Scan(&got); err != nil {
			t.Fatalf("identifier hash of %q: %v", c, err)
		}
		if !bytes.Equal(got, want[:]) {
			t.Fatalf("identifier hash of %.20q: SQL %x, Go %x", c, got, want)
		}
	}
	sig := bytes.Repeat([]byte{0xAB}, pgschema.KeyHashSigSize)
	for _, c := range []struct {
		typeNo int16
		ctx    int64
	}{{0, 0}, {7, 1}, {32767, 1 << 40}} {
		want, err := pgschema.KeyRowHash(c.typeNo, c.ctx, sig)
		noErr(t, "key hash", err)
		var got []byte
		q := `SELECT ` + pgschema.KeyRowHashExpr("x") + ` FROM (SELECT $1::smallint AS type_no, $2::bigint AS ctx_no, $3::bytea AS sig_hash) x`
		noErr(t, "key hash in SQL", admin.QueryRowContext(context.Background(), q, c.typeNo, c.ctx, sig).Scan(&got))
		if !bytes.Equal(got, want[:]) {
			t.Fatalf("key hash of %+v: SQL %x, Go %x", c, got, want)
		}
	}
}

// sapLines reads the stored difference of a KG as the lines of the digest, by chunk.
func sapLines(t *testing.T, k *sapKG) [][]pgschema.DiffLine {
	t.Helper()
	ctx := context.Background()
	a, err := k.st.access(ctx, k.kg.ID)
	noErr(t, "access", err)
	var out [][]pgschema.DiffLine
	noErr(t, "lines", a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, a.stagedSQL(`SELECT {chunk_no}, {kind}, {type_no}, {entity_no}, {change_kind}, {old_hash}, {new_hash} FROM {ingest_diff} ORDER BY {ord}`))
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var chunk int
			var l pgschema.DiffLine
			if err := rows.Scan(&chunk, &l.Kind, &l.TypeNo, &l.EntityNo, &l.Change, &l.OldHash, &l.NewHash); err != nil {
				return err
			}
			for len(out) <= chunk {
				out = append(out, nil)
			}
			out[chunk] = append(out[chunk], l)
		}
		return rows.Err()
	}))
	return out
}

// The digest of every chunk that SQL makes from the stored lines is the digest the Go function makes from the same
// lines, the chain the caller carries is the chain of those digests, and the head keeps the input digest that takes
// that chain.
//
// [REQ-726]
func TestSapChunkDigestsOfSQLAreThoseOfGo(t *testing.T) {
	p := sapStaged(t)
	ctx := context.Background()
	k := p.staged
	end := sapRun(t, k, "sap-run")
	chunks := sapLines(t, k)
	if len(chunks) != end.ChunkCount || len(chunks) < 4 {
		t.Fatalf("%d chunks of lines, %d applied", len(chunks), end.ChunkCount)
	}
	a, err := k.st.access(ctx, k.kg.ID)
	noErr(t, "access", err)
	var chain [32]byte
	noErr(t, "digests", a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		for n, lines := range chunks {
			want, err := pgschema.ChunkDigest(lines)
			if err != nil {
				return err
			}
			var got []byte
			if err := tx.QueryRowContext(ctx, pgschema.ChunkDigestSQL(a.rec.KGNo), n).Scan(&got); err != nil {
				return err
			}
			if !bytes.Equal(got, want[:]) {
				t.Fatalf("chunk %d: SQL %x, Go %x", n, got, want)
			}
			chain = pgschema.ChainDigest(chain, want)
		}
		return nil
	}))
	if end.State != graph.StagedDone || end.Chain != chain {
		t.Fatalf("the chain at the end: %x, from the lines %x", end.Chain, chain)
	}
	got, err := chainBeforeTest(ctx, k, len(chunks))
	noErr(t, "chain from the stored lines", err)
	if got != chain {
		t.Fatalf("the chain a server that takes over makes: %x, wanted %x", got, chain)
	}
	if _, err := pgschema.InputDigest(chunks); err != nil {
		t.Fatalf("input digest: %v", err)
	}
}

func chainBeforeTest(ctx context.Context, k *sapKG, n int) ([32]byte, error) {
	a, err := k.st.access(ctx, k.kg.ID)
	if err != nil {
		return [32]byte{}, err
	}
	var out [32]byte
	err = a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		h, err := chainBefore(ctx, tx, a, n)
		out = h
		return err
	})
	return out, err
}
