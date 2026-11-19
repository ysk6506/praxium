package pgsource

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The tests read a real PostgreSQL server over TCP on the loopback. Every value in them is made up.
const (
	pgsSecretID = "sec_pgs"
	pgsSourceID = "src_pgs"
)

var pgsCtx = context.Background()

// pgsRig is a test database, a secret that names the test role, an egress dialer and the definition of a source.
type pgsRig struct {
	t        *testing.T
	host     string
	port     int
	user     string
	pass     string
	dbName   string
	admin    *sql.DB
	db       *sql.DB
	svc      *secrets.Service
	dialer   *egress.Dialer
	mu       sync.Mutex
	events   []ingest.Event
	sslmode  string
	secretID string
	docPort  int
}

func pgsEnv(t *testing.T, name string) string {
	t.Helper()
	v, ok := kernel.LookupEnv("PRAXIUM_TEST_PG_TCP_" + name)
	if !ok || v == "" {
		t.Fatalf("PRAXIUM_TEST_PG_TCP_%s is not set: run the tests through scripts/with-postgres.sh", name)
	}
	return v
}

func pgsDSN(r *pgsRig, db string) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", r.host, r.port, r.user, r.pass, db)
}

func pgsNewRig(t *testing.T) *pgsRig {
	t.Helper()
	port, err := strconv.Atoi(pgsEnv(t, "PORT"))
	if err != nil {
		t.Fatalf("PRAXIUM_TEST_PG_TCP_PORT is not a number")
	}
	r := &pgsRig{t: t, host: pgsEnv(t, "HOST"), port: port, user: pgsEnv(t, "USER"), pass: pgsEnv(t, "PASSWORD"), sslmode: "disable", secretID: pgsSecretID}
	r.dbName = "pgs_" + strings.ToLower(kernel.NewToken(8))
	admin := pgsOpen(t, pgsDSN(r, "postgres"))
	r.admin = admin
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(pgsCtx, `CREATE DATABASE "`+r.dbName+`"`); err != nil {
		t.Fatalf("cannot create the test database")
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(pgsCtx, `DROP DATABASE IF EXISTS "`+r.dbName+`" WITH (FORCE)`) })
	r.db = pgsOpen(t, pgsDSN(r, r.dbName))
	t.Cleanup(func() { _ = r.db.Close() })

	svc, err := secrets.New(secrets.Config{
		Store: secrets.NewMemoryStore(), Keys: secrets.StaticKey(secrets.GenerateMasterKey(kernel.NewSeededBytes(7))),
		Clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), Bytes: kernel.NewSeededBytes(8),
	})
	if err != nil {
		t.Fatal(err)
	}
	r.svc = svc
	r.addSecret(pgsSecretID, r.user, r.pass)
	d, err := egress.New(egress.Config{})
	if err != nil {
		t.Fatal(err)
	}
	r.dialer = d
	return r
}

func (r *pgsRig) addSecret(id, user, pass string) { r.addSecretAt(id, user, pass, r.port) }

func (r *pgsRig) addSecretAt(id, user, pass string, port int) {
	r.t.Helper()
	_, err := r.svc.Create(pgsCtx, secrets.CreateInput{
		ID: id, Kind: secrets.KindPassword,
		Fields: secrets.NewFields(map[string]string{"username": user, "password": pass}),
		Scope:  []string{pgsSourceID}, AllowedHosts: []secrets.Address{{Host: r.host, Port: port}},
	})
	if err != nil {
		r.t.Fatalf("cannot create the secret: %v", err)
	}
}

func (r *pgsRig) exec(stmt string, args ...any) {
	r.t.Helper()
	if _, err := r.db.ExecContext(pgsCtx, stmt, args...); err != nil {
		r.t.Fatalf("setup statement failed: %v", err)
	}
}

// pgsDS is the JSON of a dataset: table or sql, key and columns (names only).
func pgsDS(id, from, key string, cols ...string) string {
	cs := make([]string, len(cols))
	for i, c := range cols {
		cs[i] = `{"name":"` + c + `"}`
		if name, path, ok := strings.Cut(c, "="); ok {
			cs[i] = `{"name":"` + name + `","paths":[` + strconv.Quote(path) + `]}`
		}
	}
	ks := strings.Split(key, ",")
	for i := range ks {
		ks[i] = `"` + ks[i] + `"`
	}
	return fmt.Sprintf(`{"id":%q,%s,"natural_key":[%s],"columns":[%s]}`, id, from, strings.Join(ks, ","), strings.Join(cs, ","))
}

func pgsTable(name string) string { return `"table":"` + name + `"` }
func pgsSQL(stmt string) string   { return `"sql":` + strconv.Quote(stmt) }

// pgsSourceDoc is a source definition with the datasets; extra is more fields of the connection.
func (r *pgsRig) sourceDoc(extra string, datasets ...string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"version":1,"id":%q,"adapter":"postgres","connection":{"host":%q,"port":%d,"database":%q,"credential":{"secret_id":%q},"sslmode":%q%s},"datasets":[%s]}`,
		pgsSourceID, r.host, r.portInDoc(), r.dbName, r.secretID, r.sslmode, extra, strings.Join(datasets, ","))
}

func (r *pgsRig) portInDoc() int {
	if r.docPort != 0 {
		return r.docPort
	}
	return r.port
}

func (r *pgsRig) parse(doc string) ingest.Source {
	r.t.Helper()
	src, err := ingest.ParseAdapterSource(strings.NewReader(doc))
	if err != nil {
		r.t.Fatalf("the test definition does not parse: %v", err)
	}
	return src
}

func (r *pgsRig) observe(e ingest.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *pgsRig) eventsSeen() []ingest.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ingest.Event(nil), r.events...)
}

func (r *pgsRig) reader(doc string) (*Reader, ingest.Source) {
	r.t.Helper()
	src := r.parse(doc)
	rd, err := New(Config{Source: src, Secrets: r.svc.Opener(), Dialer: r.dialer, Observer: ingest.ObserverFunc(r.observe), Clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))})
	if err != nil {
		r.t.Fatalf("New: %v", err)
	}
	return rd, src
}

// one makes the reader of a source with one dataset and returns it with that dataset.
func (r *pgsRig) one(ds string, extra string) (*Reader, ingest.Dataset) {
	r.t.Helper()
	rd, src := r.reader(r.sourceDoc(extra, ds))
	return rd, src.Datasets[0]
}

// pgsResult is everything a read gave.
type pgsResult struct {
	pages []ingest.Page
	err   error
}

func (p pgsResult) rows() []ingest.Row {
	var out []ingest.Row
	for _, pg := range p.pages {
		out = append(out, pg.Rows...)
	}
	return out
}

func (p pgsResult) keys() []string {
	var out []string
	for _, row := range p.rows() {
		out = append(out, row.Key)
	}
	return out
}

func pgsRead(rd *Reader, ds ingest.Dataset, opts ingest.PageOptions) pgsResult {
	var res pgsResult
	for pg, err := range rd.ReadPages(pgsCtx, pgsSourceID, ds, opts) {
		if err != nil {
			res.err = err
			return res
		}
		res.pages = append(res.pages, pg)
	}
	return res
}

func (r *pgsRig) read(ds string, opts ingest.PageOptions) pgsResult {
	r.t.Helper()
	rd, d := r.one(ds, "")
	return pgsRead(rd, d, opts)
}

func pgsSrcErr(t *testing.T, err error, kind string) *ingest.SourceError {
	t.Helper()
	se, ok := err.(*ingest.SourceError)
	if !ok {
		t.Fatalf("want a SourceError of kind %s, got %T", kind, err)
	}
	if se.Kind != kind {
		t.Fatalf("want kind %s, got %s (%s)", kind, se.Kind, se.Message)
	}
	return se
}

const pgsKeySep = "\x1f"

func pgsOpen(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	c, err := pq.NewConnector(dsn)
	if err != nil {
		t.Fatalf("cannot read the test DSN")
	}
	return sql.OpenDB(c)
}
