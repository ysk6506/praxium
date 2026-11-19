package pgsource

import (
	"context"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// pgsCountResolver counts the names looked up: a dial of a name is one lookup, so no lookup is no connection.
type pgsCountResolver struct{ n atomic.Int64 }

func (r *pgsCountResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.n.Add(1)
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func (r *pgsRig) countDialer() *pgsCountResolver {
	r.t.Helper()
	res := &pgsCountResolver{}
	d, err := egress.New(egress.Config{Resolver: res})
	if err != nil {
		r.t.Fatal(err)
	}
	r.dialer = d
	return res
}

// [REQ-676] A preview of 20 pages and 1000 rows of a table of 5000 rows has 1000 rows: the rows are shared out over the
// pages and the preview ends at the rows, and a request of more pages than the most is cut to the most.
func TestPgsPreviewEndsAtTheRowsAndCapsThePages(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE big (k int primary key, v text)`)
	r.exec(`INSERT INTO big SELECT i, 'v'||i FROM generate_series(1,5000) i`)
	rd, ds := r.one(pgsDS("ds", pgsTable("big"), "k", "k", "v"), "")
	for _, opts := range []ingest.PreviewOptions{{MaxPages: 20, MaxRows: 1000}, {MaxPages: 100000, MaxRows: 1000}} {
		pv, err := rd.Preview(pgsCtx, pgsSourceID, ds, opts)
		if err != nil {
			t.Fatal(err)
		}
		rows := 0
		for _, pg := range pv.Pages {
			rows += len(pg.Rows)
		}
		if rows != 1000 || len(pv.Pages) > ingest.MaxPreviewPages {
			t.Fatalf("%d pages and %d rows for %+v", len(pv.Pages), rows, opts)
		}
	}
}

// [REQ-677] The type of a key column is read from the catalog of the source: whole numbers, numeric, text, uuid, date and
// time types, an enum and a domain over one of them are keys; any other type is a definition error that names the type.
func TestPgsKeyTypesAreVerifiedThroughTheCatalog(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TYPE mood AS ENUM ('a','b')`)
	r.exec(`CREATE DOMAIN pos AS int CHECK (VALUE > 0)`)
	r.exec(`CREATE DOMAIN deep AS pos`)
	r.exec(`CREATE DOMAIN fl AS float8`)
	r.exec(`CREATE TABLE kt (e mood, d deep, f fl, n inet, j jsonb, x float8)`)
	r.exec(`INSERT INTO kt VALUES ('a', 1, 1.5, '10.0.0.1', '{}', 2.5)`)
	for _, ok := range []string{"e", "d"} {
		if res := r.read(pgsDS("ds", pgsTable("kt"), ok, ok), ingest.PageOptions{}); res.err != nil || len(res.rows()) != 1 {
			t.Fatalf("key %s: %v", ok, res.err)
		}
	}
	for key, name := range map[string]string{"f": "float8", "n": "inet", "j": "jsonb", "x": "float8"} {
		res := r.read(pgsDS("ds", pgsTable("kt"), key, key), ingest.PageOptions{})
		se := pgsSrcErr(t, res.err, ingest.SrcRequestRejected)
		if !strings.Contains(se.Detail, name) {
			t.Fatalf("key %s: the detail does not name the type %s: %q", key, name, se.Detail)
		}
	}
}

// [REQ-672] A control character in the user name or the password of the secret is a secret that is not used, and one in
// the database name or the host of the definition is a definition that is refused: no connection is made for either.
func TestPgsControlCharactersMakeNoConnection(t *testing.T) {
	r := pgsNewRig(t)
	res := r.countDialer()
	r.addSecret("sec_ctl_user", "u\x01x", r.pass)
	r.addSecret("sec_ctl_pass", r.user, "p\x01x")
	for _, id := range []string{"sec_ctl_user", "sec_ctl_pass"} {
		r.secretID = id
		pgsSrcErr(t, r.read(pgsOneDS(), ingest.PageOptions{}).err, ingest.SrcSecretUnusable)
	}
	r.secretID = pgsSecretID
	for _, field := range []string{"database", "host"} {
		src := r.parse(r.sourceDoc("", pgsOneDS()))
		if field == "database" {
			src.Connection.Database = "d\x00x"
		} else {
			src.Connection.Host = "h\x00x"
		}
		rd, err := New(Config{Source: src, Secrets: r.svc.Opener(), Dialer: r.dialer})
		if err != nil {
			t.Fatal(err)
		}
		var got error
		for _, err := range rd.ReadPages(pgsCtx, pgsSourceID, src.Datasets[0], ingest.PageOptions{}) {
			got = err
			break
		}
		if got == nil {
			t.Fatalf("a read with a NUL in the %s was made", field)
		}
	}
	if n := res.n.Load(); n != 0 {
		t.Fatalf("%d connections were tried", n)
	}
}

// [REQ-679] The sentence of the server that goes with a refused statement is cut at a kibibyte and has no control
// character.
func TestPgsServerSentenceIsCutAndHasNoControlCharacters(t *testing.T) {
	r := pgsNewRig(t)
	res := r.read(pgsDS("ds", pgsSQL(`SELECT (E'a\001b\033c' || repeat('y', 5000))::int AS k`), "k", "k"), ingest.PageOptions{})
	se := pgsSrcErr(t, res.err, ingest.SrcRequestRejected)
	if len(se.Detail) == 0 || len(se.Detail) > 1024 {
		t.Fatalf("the detail has %d bytes", len(se.Detail))
	}
	for _, c := range se.Detail {
		if c < 0x20 || c == 0x7f {
			t.Fatalf("the detail has the control character %q", c)
		}
	}
}

// [REQ-678] With PQGO_DEBUG set no source is read and no connection is made: the driver would write the password and the
// rows to the standard error.
func TestPgsPqgoDebugStopsTheRead(t *testing.T) {
	r := pgsNewRig(t)
	res := r.countDialer()
	t.Setenv("PQGO_DEBUG", "1")
	se := pgsSrcErr(t, r.read(pgsOneDS(), ingest.PageOptions{}).err, ingest.SrcInternal)
	if !strings.Contains(se.Message, "PQGO_DEBUG") || res.n.Load() != 0 {
		t.Fatalf("%q, %d connections", se.Message, res.n.Load())
	}
}
