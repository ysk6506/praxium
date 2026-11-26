package pgstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// TestDSNEnv names the environment variable that gives the DSN of the PostgreSQL server the tests use. It is
// set by scripts/with-postgres.sh (a throwaway server) and by the CI job (a service container).
const TestDSNEnv = pgconn.TestDSNEnv

// TB is the part of testing.TB the test helpers need; a test passes its *testing.T.
type TB = pgconn.TB

// NewTestDatabase makes an empty database on the test server and returns its DSN. The database is dropped when
// the test ends. A missing TestDSNEnv fails the test: a test that needs PostgreSQL never passes by being skipped.
func NewTestDatabase(tb TB) string {
	tb.Helper()
	return pgconn.NewTestDatabase(tb)
}

// withDatabase is the DSN with another database name (see pgconn.WithDatabase).
func withDatabase(dsn, name string) string { return pgconn.WithDatabase(dsn, name) }

// NewTestDatabaseWithKG makes a test database that holds kg and returns its DSN: the migrations ran on it, CreateKG
// made kg, and then, when it is not nil, did what the test needs on top. The model never has a query schema: its roles
// belong to the cluster, so every copy would share them (ADR-0113). withStats picks the model of a server less its query
// schema (TestOnlyModelsWithoutQuerySchema), the other the light model of NewTestStore (StandardModels(false)). The
// database is a copy of a template made once on the test server (pgconn.NewTestDatabaseFrom, ADR-0120), whose key holds
// the migrations, which of the two models it is, the plan of the storage schema (generator version, model, statistics
// objects), kg and thenKey: thenKey must name everything then does, as two calls with the same KG, model and thenKey get
// the same database. then gets the DSN of the template while it is built and closes every connection it opens. It returns
// the KG as CreateKG returned it too. The database is dropped when the test ends.
func NewTestDatabaseWithKG(tb TB, withStats bool, kg graph.KG, thenKey string, then func(dsn string) error) (string, graph.KG) {
	tb.Helper()
	models, err := testKGModels(withStats)
	key := ""
	if err == nil {
		key, err = testKGKey(models, kg, thenKey)
	}
	if err != nil {
		tb.Fatalf("cannot make the key of the template of the KG: %v", err)
		return "", graph.KG{}
	}
	return pgconn.NewTestDatabaseFrom(tb, key, func(dsn string) error {
		if err := makeTestKG(dsn, models, kg); err != nil {
			return err
		}
		if then == nil {
			return nil
		}
		return then(dsn)
	}), madeKG(kg)
}

// testKGModels is the model of the template of a KG: with the statistics objects, the model of a server less its query
// schema; without, the light model.
func testKGModels(withStats bool) (ModelProvider, error) {
	if withStats {
		return TestOnlyModelsWithoutQuerySchema()
	}
	return StandardModels(false)
}

// testKGKey is the key of the template of a KG: the migrations, which model it is, the statements and the manifest of the
// storage schema the model makes for the first KG of a database, every field of kg, and what the caller does after. A
// model that makes a query schema has no template.
func testKGKey(models ModelProvider, kg graph.KG, thenKey string) (string, error) {
	migrations := pgconn.TestMigrationsKey()
	m, err := models(kg.DefinitionSetHash)
	if err != nil {
		return "", err
	}
	if m.Registry == nil || m.Registry.Hash() != kg.DefinitionSetHash {
		return "", kernel.Errorf(kernel.CodeInvalidValue, "the definition set of the KG is not the model of the template")
	}
	if !m.noQuery {
		return "", kernel.Errorf(kernel.CodeInvalidValue, "a template of a KG has no query schema: the roles of one would be shared by every copy")
	}
	// a plan that cannot be made gives its error with the key, which the caller does not use then
	plan, err := buildPlan(m, 1)
	var b strings.Builder
	b.WriteString(migrations + fmt.Sprintf("\nmodel: statistics objects %t, query schema %t", m.WithStats, !m.noQuery))
	b.WriteString("\nkg " + fmt.Sprintf("%+v", kg) + "\nmanifest " + fmt.Sprintf("%+v", plan.Manifest))
	for _, st := range append(append([]pgschema.Step(nil), plan.Steps...), plan.Deferred...) {
		b.WriteString(fmt.Sprintf("\nstep %+v", st))
	}
	b.WriteString("\nthen " + thenKey)
	return b.String(), err
}

func makeTestKG(dsn string, models ModelProvider, kg graph.KG) error {
	st, err := Open(context.Background(), Config{DSN: dsn, Models: models})
	if err != nil {
		return err
	}
	_, err = st.CreateKG(context.Background(), kg)
	if cerr := st.Close(); err == nil {
		err = cerr
	}
	return err
}

// TestOnlyModelsWithoutQuerySchema is the provider of the model of a server (StandardModels(true), with the statistics objects) less the
// query schema, for the tests of other packages that make KGs and are not about the query schema (ADR-0113): a test passes it as
// Config.Models of Open, and the KGs it makes have the tables and the statistics objects a server makes, so the plans those tests watch
// are the plans of a server, and no query schema, which costs about two seconds for each KG. Only a _test.go file or this file calls it;
// a test locks that no other file does, or sets SchemaModel.noQuery, and no setting of a server (variable, flag or file) reaches it.
func TestOnlyModelsWithoutQuerySchema() (ModelProvider, error) {
	std, err := StandardModels(true)
	if err != nil {
		return nil, err
	}
	return func(h kernel.Hash) (SchemaModel, error) {
		m, err := std(h)
		m.noQuery = true
		return m, err
	}, nil
}

// NewTestStore opens a Store on a new test database with the standard model (without the statistics objects), on which
// the migrations have run (a copy of a template with a token of the installation of its own, pgconn.NewMigratedTestDatabase).
// The lease runs out by the clock of the caller, as the tests move a clock of their own. The store is closed and the
// database dropped when the test ends. The KGs it makes have the tables and no query schema (views, functions and roles;
// ADR-0113, see StandardModels): a test of the query schema opens the Store with Open and makes it.
func NewTestStore(tb TB) *Store { return NewTestStoreWith(tb, LeaseClockCaller) }

// NewTestStoreWith is NewTestStore with the clock that decides when a lease has run out (LeaseClockDB: the
// database's, the one a server uses).
func NewTestStoreWith(tb TB, clock LeaseClock) *Store {
	tb.Helper()
	dsn := pgconn.NewMigratedTestDatabase(tb)
	if dsn == "" {
		return nil
	}
	models, err := StandardModels(false)
	if err != nil {
		tb.Fatalf("cannot load the standard model: %v", err)
		return nil
	}
	st, err := Open(context.Background(), Config{DSN: dsn, Models: models, LeaseClock: clock})
	if err != nil {
		tb.Fatalf("cannot open the test store: %v", err)
		return nil
	}
	tb.Cleanup(func() { _ = st.Close() })
	return st
}

// RecordChunks makes this Store hand every chunk that ApplyChunk wrote to fn once it is written, in the order they were
// written. The tests of the staged apply use it to take the rows the rows path wrote (ADR-0099). Only tests use it, and
// they set it before the calls that write.
func (s *Store) RecordChunks(fn func(graph.ChunkWrite)) { s.record = fn }

// StopWorkAfter makes an abandon or a restore of this Store stop with err once it has done n steps (n of at least 1),
// as a server that dies in the middle does: the head keeps the work and ResumeWork ends it. Only tests use it, and
// they set it before the call that does the work.
func (s *Store) StopWorkAfter(n int, err error) {
	steps := 0
	s.hook = func(string, int64) error {
		if steps++; steps >= n {
			return err
		}
		return nil
	}
}
