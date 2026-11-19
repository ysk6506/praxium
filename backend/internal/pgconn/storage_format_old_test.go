package pgconn

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// The database of the storage format 1 (the alpha releases: the tables of migration 0001 and nothing else) is met by
// this build, ADR-0094. The names of these tests and their helpers start with old.

// oldDatabase makes a database as the alpha releases left it: migration 0001 applied and recorded. withKG puts a KG in
// the table kg.
func oldDatabase(t *testing.T, withKG bool) (dsn string, admin *sql.DB) {
	t.Helper()
	dsn = NewTestDatabase(t)
	admin, err := sql.Open("postgres", dsn)
	noErr(t, "open", err)
	t.Cleanup(func() { _ = admin.Close() })
	steps, err := embedded()
	noErr(t, "migrations", err)
	first := steps[0]
	for _, q := range []string{first.sql,
		`CREATE TABLE schema_migrations (version int PRIMARY KEY, name text NOT NULL, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`} {
		_, err = admin.ExecContext(context.Background(), q)
		noErr(t, "prepare", err)
	}
	_, err = admin.ExecContext(context.Background(), `INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`, first.version, first.name, first.checksum)
	noErr(t, "record", err)
	if withKG {
		_, err = admin.ExecContext(context.Background(), `INSERT INTO kg (kg_id, created_no, name, role, state, definition_set_hash, generation, created_at)
			VALUES ('kg-0123456789abcdef', 1, 'main', 'production', 'serving', '\x00', 1, now())`)
		noErr(t, "kg", err)
	}
	return dsn, admin
}

func oldCount(t *testing.T, admin *sql.DB, query string) int {
	t.Helper()
	var n int
	noErr(t, query, admin.QueryRow(query).Scan(&n))
	return n
}

// [REQ-632]
func TestADatabaseWithKGsInTheOldTablesIsRefusedAndLeftAsItWas(t *testing.T) {
	dsn, admin := oldDatabase(t, true)
	_, err := Open(context.Background(), Config{DSN: dsn})
	wantCode(t, "the old tables hold a KG", err, CodeStorageFormatOld)
	for _, want := range []string{"storage format 1", "no way to move", "new database"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the reason does not say %q: %v", want, err)
		}
	}
	if oldCount(t, admin, `SELECT count(*) FROM kg`) != 1 || oldCount(t, admin, `SELECT count(*) FROM schema_migrations`) != 1 {
		t.Fatal("the refusal changed the database")
	}
	if oldCount(t, admin, `SELECT count(*) FROM pg_class WHERE relname IN ('storage_format', 'kg_catalog', 'identity_record')`) != 0 {
		t.Fatal("a migration ran before the refusal")
	}
}

// [REQ-632]
func TestADatabaseWithTheOldTablesAndNoKGGoesOnAndTheTablesAreDropped(t *testing.T) {
	dsn, admin := oldDatabase(t, false)
	p, err := Open(context.Background(), Config{DSN: dsn})
	noErr(t, "open", err)
	defer func() { _ = p.Close() }()
	if n := oldCount(t, admin, `SELECT count(*) FROM pg_class WHERE relkind = 'r' AND relnamespace = 'public'::regnamespace AND relname IN
		('kg', 'kg_head', 'kg_apply_lease', 'generation_event', 'object_live', 'object_hist', 'link_live', 'link_hist', 'attr_volatile', 'light_change',
		 'object_identifier', 'identity_event', 'ingest_order', 'change_batch', 'change_entry', 'kg_version', 'kg_version_hash_part')`); n != 0 {
		t.Fatalf("%d tables of the old storage are left", n)
	}
	if v := oldCount(t, admin, `SELECT version FROM storage_format WHERE id = 1`); v != StorageFormat {
		t.Fatalf("the storage format is %d, not %d", v, StorageFormat)
	}
	if n := oldCount(t, admin, `SELECT count(*) FROM identity_record`); n != 0 {
		t.Fatalf("%d identity records out of nowhere", n)
	}
}

// The step that drops the tables stops by itself when a KG is in them: a server that does not know the gate cannot
// throw the rows away by running it.
//
// [REQ-632]
func TestTheStepThatDropsTheOldTablesStopsWhenAKGIsInThem(t *testing.T) {
	_, admin := oldDatabase(t, true)
	steps, err := embedded()
	noErr(t, "migrations", err)
	var drop migration
	for _, m := range steps {
		if m.name == "drop_old_kg_tables" {
			drop = m
		}
	}
	if drop.version == 0 {
		t.Fatal("there is no step that drops the old tables")
	}
	_, err = admin.ExecContext(context.Background(), `CREATE TABLE storage_format (id smallint PRIMARY KEY, version integer NOT NULL); INSERT INTO storage_format VALUES (1, 1)`)
	noErr(t, "storage_format", err)
	_, err = admin.ExecContext(context.Background(), drop.sql)
	if err == nil || !strings.Contains(err.Error(), "storage format 1") {
		t.Fatalf("the step went on or stopped for another reason: %v", err)
	}
	if oldCount(t, admin, `SELECT count(*) FROM kg`) != 1 || oldCount(t, admin, `SELECT version FROM storage_format`) != 1 {
		t.Fatal("the step changed what it refused")
	}
}
