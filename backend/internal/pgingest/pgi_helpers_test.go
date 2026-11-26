package pgingest

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// The tests of this unit run on a real server. Every helper, type and field that they add starts with pgi, so that none of them
// meets a name of another test file.

const pgiSourceJSON = `{"id":"src_pgi","name":"pgi","adapter":"file","datasets":[
 {"id":"ds_places","parser":"csv","natural_key":["row_id"],"columns":[{"name":"row_id"},{"name":"site"},{"name":"building"},{"name":"floor"},{"name":"room"},{"name":"rack"},{"name":"tag"}]},
 {"id":"ds_servers","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"},{"name":"rack_tag"},{"name":"power_state"}]},
 {"id":"ds_vms","parser":"json_lines","natural_key":["uuid"],"columns":[{"name":"uuid"},{"name":"name"}]}
]}`

const pgiMappingJSON = `{"feeds":[
 {"id":"f_rack","dataset":"ds_places","object_type":"Rack","creates_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack"},{"attribute":"asset_tag","column":"tag"}],
  "identity":[{"components":[{"context":"LOCATED_IN","parent":{"object_type":"Room","identity":{"components":[{"context":"LOCATED_IN","parent":{"object_type":"Floor","identity":{"components":[{"context":"LOCATED_IN","parent":{"object_type":"Building","identity":{"components":[{"context":"LOCATED_IN","parent":{"object_type":"Site","identity":{"components":[{"property":"site_code","column":"site"}]}}},{"property":"name","column":"building"}]}}},{"property":"name","column":"floor"}]}}},{"property":"name","column":"room"}]}}},{"property":"name","column":"rack"}],"priority":1},
   {"components":[{"property":"asset_tag","column":"tag"}],"priority":2}]},
 {"id":"f_server","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"serial_number","column":"serial"},{"attribute":"manufacturer","column":"manufacturer"},{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
 {"id":"f_vm","dataset":"ds_vms","object_type":"VirtualMachine","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"name"}],
  "identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}]}
],
"links":[{"id":"l_mounted","link_type":"MOUNTED_IN","from":{"feed":"f_server","columns":["rack_tag"]},"to":{"feed":"f_rack","columns":["tag"]},"condition":"same_key","basis":"declared"}]}`

const (
	pgiPlaces  = "row_id,site,building,floor,room,rack,tag\n1,HQ,B1,F1,R1,rack a,RK1\n2,HQ,B1,F1,R1,rack b,RK2\n3,HQ,B1,F2,R2,rack c,RK3\n"
	pgiServers = "asset_id,serial,manufacturer,rack_tag,power_state\nSV1,sn1,Zorvane,RK1,on\nSV2,sn2,Zorvane,RK2,off\n"
	pgiVMs     = "{\"uuid\":\"u-1\",\"name\":\"vm one\"}\n{\"uuid\":\"u-2\",\"name\":\"vm two\"}\n"
)

// pgiTexts is a reader of the datasets of a source from texts; a dataset it has no text of is not given.
type pgiTexts map[string]string

func (m pgiTexts) ReadPages(ctx context.Context, _ string, ds ingest.Dataset, opts ingest.PageOptions) iter.Seq2[ingest.Page, error] {
	return func(yield func(ingest.Page, error) bool) {
		text, ok := m[ds.ID]
		if !ok {
			yield(ingest.Page{}, kernel.Errorf(kernel.CodeNotFound, "not given"))
			return
		}
		for p, err := range ingest.StreamPages(ctx, strings.NewReader(text), ds, opts) {
			if !yield(p, err) {
				return
			}
		}
	}
}

func (m pgiTexts) Check(ctx context.Context, source string, ds ingest.Dataset) ingest.CheckResult {
	return ingest.CheckFirstPage(ctx, m, source, ds)
}

func pgiAll() pgiTexts {
	return pgiTexts{"ds_places": pgiPlaces, "ds_servers": pgiServers, "ds_vms": pgiVMs}
}

func pgiPlan(t *testing.T, source, mapping string) *ingest.Plan {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ingest.ParseSource(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ingest.ParseMapping(strings.NewReader(mapping))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ingest.CompileScale(reg, src, m)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func pgiDatasets(plan *ingest.Plan) []string {
	var out []string
	for _, d := range plan.Source.Datasets {
		out = append(out, d.ID)
	}
	return out
}

// pgiEnv is a test server with a store of KGs, a connection pool and the Store of this unit.
type pgiEnv struct {
	ctx   context.Context
	pst   *pgstore.Store
	pool  *pgconn.Pool
	st    *Store
	admin *sql.DB
}

func pgiOpen(t *testing.T) *pgiEnv {
	t.Helper()
	ctx := context.Background()
	dsn := pgconn.NewTestDatabase(t)
	models, err := pgstore.TestOnlyModelsWithoutQuerySchema()
	if err != nil {
		t.Fatal(err)
	}
	pst, err := pgstore.Open(ctx, pgstore.Config{DSN: dsn, Models: models})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pst.Close() })
	pool, err := pgconn.Open(ctx, pgconn.Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	return &pgiEnv{ctx: ctx, pst: pst, pool: pool, st: New(pool), admin: admin}
}

// kg makes a KG of the standard model.
func (e *pgiEnv) kg(t *testing.T, n int) graph.KG {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	id, err := kernel.ParseID[graph.KGKind](fmt.Sprintf("kg-%016x", n))
	if err != nil {
		t.Fatal(err)
	}
	kg, err := e.pst.CreateKG(e.ctx, graph.KG{ID: id, Name: fmt.Sprintf("pgi%d", n), Role: graph.RoleTest, DefinitionSetHash: reg.Hash(), CreatedAt: time.Unix(1000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return kg
}

// attach puts the source of a plan to the KG.
func (e *pgiEnv) attach(t *testing.T, kg graph.KG, plan *ingest.Plan) SourceRef {
	t.Helper()
	ref, err := e.st.AttachSource(e.ctx, kg.ID, plan.Source.ID, pgiDatasets(plan))
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func (e *pgiEnv) open(t *testing.T, kg graph.KG, source, owner string) ingest.RunLease {
	t.Helper()
	l, err := e.st.OpenRun(e.ctx, ingest.RunOpen{KG: kg.ID, Source: source, Owner: owner, Term: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (e *pgiEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.admin.QueryRowContext(e.ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (e *pgiEnv) text(t *testing.T, query string, args ...any) string {
	t.Helper()
	var s sql.NullString
	if err := e.admin.QueryRowContext(e.ctx, query, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return s.String
}

func (e *pgiEnv) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := e.admin.ExecContext(e.ctx, query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// table is the name of a table of the source's partition in the KG, quoted, for a query of the test.
func pgiTable(ref SourceRef, table string) string {
	return fmt.Sprintf(`"s_%d"."%s_p%d"`, ref.KGNo, table, ref.SrcNo)
}

func pgiCodeIs(err error, code kernel.Code) bool { return err != nil && kernel.CodeOf(err) == code }

// pgiExpireLease makes the lease of a run run out, as the passing of time does.
func (e *pgiEnv) expire(t *testing.T, run int64) {
	t.Helper()
	e.exec(t, `UPDATE sync_run SET lease_until = now() - interval '1 second' WHERE run_no = $1`, run)
}

// pgiTee is a sink that gives the pages to the sink of the unit and keeps them for the test to hold the rows against.
type pgiTee struct {
	inner ingest.StageSink
	pages []ingest.StagePage
}

func (s *pgiTee) Stage(ctx context.Context, p ingest.StagePage) error {
	s.pages = append(s.pages, p)
	return s.inner.Stage(ctx, p)
}

func nullText(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
