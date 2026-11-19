package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// sadStore is a store that says what its adjustment of old schemas gives.
type sadStore struct {
	graph.Store
	done []pgstore.SchemaStartup
	err  error
}

func (s sadStore) AdjustOldSchemas(context.Context) ([]pgstore.SchemaStartup, error) {
	return s.done, s.err
}

// [REQ-685]
func TestAdjustSchemasLogsWhatItDidAndNeverStopsTheStart(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	kg, err := kernel.ParseID[graph.KGKind]("kg-0000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	other, err := kernel.ParseID[graph.KGKind]("kg-0000000000000002")
	if err != nil {
		t.Fatal(err)
	}
	adjustSchemas(context.Background(), sadStore{
		done: []pgstore.SchemaStartup{{KG: kg, Adjusted: true}, {KG: other, Reason: "the difference is not an addition"}},
		err:  errors.New("cannot adjust the storage schema of KG x"),
	}, log)
	out := buf.String()
	for _, want := range []string{"brought the storage schema of a KG up to this build", "left the storage schema",
		"the difference is not an addition", "level=WARN", "cannot adjust"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %q:\n%s", want, out)
		}
	}
	buf.Reset()
	adjustSchemas(context.Background(), struct{ graph.Store }{}, log)
	if buf.Len() != 0 {
		t.Errorf("a store that cannot adjust logged: %s", buf.String())
	}
}

// [REQ-685]
func TestAServerThatStartsOnAKGOfTheGeneratorBeforeAdjustsItAndServesIt(t *testing.T) {
	truth := writeWorld(t)
	dsn := pgstore.NewTestDatabase(t)
	_, stop, _ := servePG(t, dsn, "-dev-truth", truth)
	stop()

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var kgNo int64
	if err := db.QueryRow(`SELECT kg_no FROM kg_catalog WHERE kg_id IS NOT NULL ORDER BY kg_no LIMIT 1`).Scan(&kgNo); err != nil {
		t.Fatal(err)
	}
	schema := pgschema.SchemaName(kgNo)
	names := pgschema.IngestTables()
	for i := len(names) - 1; i >= 0; i-- {
		if _, err := db.Exec(`DROP TABLE IF EXISTS ` + pgschema.Qualified(schema, names[i]) + ` CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE kg_schema_state SET generator_version = 2, manifest = jsonb_set(manifest, '{generator_version}', '2')`); err != nil {
		t.Fatal(err)
	}

	client, _, logged := servePG(t, dsn)
	if code, out, errOut := client("kg", "get", "main"); code != 0 || !strings.Contains(out, "serving") {
		t.Fatalf("the KG of the generator before is not served: %d\n%s%s\n%s", code, out, errOut, logged())
	}
	if code, out, errOut := client("query", "find", "--type", "Rack", "--limit", "5"); code != 0 || !strings.Contains(out, "rack-") {
		t.Fatalf("the query on the adjusted KG: %d\n%s%s", code, out, errOut)
	}
	var generator int
	if err := db.QueryRow(`SELECT generator_version FROM kg_schema_state`).Scan(&generator); err != nil || generator != pgschema.GeneratorVersion {
		t.Errorf("the generator after the start is %d (%v), want %d", generator, err, pgschema.GeneratorVersion)
	}
	if !strings.Contains(logged(), "up to this build") {
		t.Errorf("the log does not say the schema was adjusted: %s", logged())
	}
}
