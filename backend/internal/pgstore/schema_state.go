package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// CodeSchemaMismatch is a storage schema that was made from another model than the one the server holds
// (ADR-0085 decision 6). A KG in that state is not written to or queried until the schema is brought up to the
// model by AdjustStorageSchema, or the server holds the model the schema was made from.
const CodeSchemaMismatch kernel.Code = "pgstore_schema_mismatch"

// The states of a row of kg_schema_state. They are the states of the manifest (pgschema.State) and one more that
// only the row has: a run that is building indexes right now.
const (
	stateBuilding = "building"
)

// buildingStale is how long a run may say it is building indexes before another run takes the work over. A
// build of an index on a large table takes long; the claim is only there to keep two runs from dropping each
// other's half-built index.
const buildingStale = 30 * time.Minute

// schemaLockPrefix names the transaction lock of one KG's schema work, so that two runs on the same KG take
// turns and runs on other KGs do not wait.
const schemaLockPrefix = "praxium:schema:"

// SchemaModel is what the executor makes a storage schema from. The caller gives it (the store does not hold a
// model yet): the registry, the search set (what gets an index), and whether the statistics objects are made
// together with the tables or left for after the first load.
type SchemaModel struct {
	Registry  *model.Registry
	Search    pgschema.SearchSet
	WithStats bool
	// token is the token of the installation that the executor reads from the database before it plans, for the names of the roles of
	// the query schema (ADR-0113). A caller does not give it.
	token string
	// noQuery says that the plan has no query schema (views, functions and roles). Only StandardModels(false) (with no statistics
	// objects either) and TestOnlyModelsWithoutQuerySchema (with them) set it, for the tests that make a KG for each case and are not
	// about the query schema (it costs about two seconds for each KG); a server never holds such a model (a test locks that no file of a
	// server calls either of them with anything that makes it, or sets this field).
	noQuery bool
}

// SchemaRecord is the row of kg_schema_state of one KG: what its storage schema was made from and how far it has
// got.
type SchemaRecord struct {
	KG               graph.KGID
	KGNo             int64
	GeneratorVersion int
	RegistryHash     string
	NumberingHash    string
	SearchHash       string
	Manifest         pgschema.Manifest
	State            string
	AppliedAt        time.Time
	// Stale is true for a record that says it is building indexes and has not said anything for longer than
	// buildingStale.
	Stale bool
}

// Schema is the name of the storage schema of the KG.
func (r SchemaRecord) Schema() string { return pgschema.SchemaName(r.KGNo) }

const recordColumns = `kg_no, generator_version, registry_hash, numbering_hash, search_hash, manifest, state, applied_at,
	COALESCE(online_since < now() - make_interval(secs => $2), false)`

// readRecord reads the row of a KG; ok is false when it has none.
func readRecord(ctx context.Context, tx *sql.Tx, kg graph.KGID) (rec SchemaRecord, ok bool, err error) {
	var manifest []byte
	row := tx.QueryRowContext(ctx, `SELECT `+recordColumns+` FROM kg_schema_state WHERE kg_id = $1`, kg.String(), buildingStale.Seconds())
	err = row.Scan(&rec.KGNo, &rec.GeneratorVersion, &rec.RegistryHash, &rec.NumberingHash, &rec.SearchHash, &manifest,
		&rec.State, &rec.AppliedAt, &rec.Stale)
	if isNoRows(err) {
		return SchemaRecord{}, false, nil
	}
	if err != nil {
		return SchemaRecord{}, false, dbErr(err, "read the state of the storage schema")
	}
	if rec.Manifest, err = pgschema.ParseManifest(manifest); err != nil {
		return SchemaRecord{}, false, err
	}
	rec.KG, rec.AppliedAt = kg, rec.AppliedAt.UTC()
	return rec, true, nil
}

// locked runs fn in one transaction of the migration role under the lock of the schema work of a KG, and runs it
// again, a little later, while the lock cannot be had within the lock timeout of the role.
func locked(ctx context.Context, sh *shard, kg graph.KGID, fn func(tx *sql.Tx) error) error {
	return pgconn.RetryLockWaits(ctx, func() error {
		return sh.tx(ctx, migrationRole, false, func(tx *sql.Tx) error {
			if err := lockSchema(ctx, tx, kg); err != nil {
				return err
			}
			return fn(tx)
		})
	})
}

// lockSchema takes the transaction lock of the schema work of a KG: it ends with the transaction.
func lockSchema(ctx context.Context, tx *sql.Tx, kg graph.KGID) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, schemaLockPrefix+kg.String()); err != nil {
		return dbErr(err, "take the lock of the storage schema")
	}
	return nil
}

// recordWrite is what writeRecord writes: the row of a KG for a manifest in a state. claim says that the run is
// building indexes from now on; insert that the row is new.
type recordWrite struct {
	kg            graph.KGID
	manifest      pgschema.Manifest
	state         string
	claim, insert bool
}

// writeRecord writes the row of a KG for a manifest in the given state (insert when it is new, otherwise update).
// claim says that the run is building indexes from now on.
func writeRecord(ctx context.Context, tx *sql.Tx, w recordWrite) error {
	kg, m, state, claim, insert := w.kg, w.manifest, w.state, w.claim, w.insert
	raw, err := m.JSON()
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "write the manifest")
	}
	online := "NULL"
	if claim {
		online = "now()"
	}
	query := `UPDATE kg_schema_state SET generator_version = $2, registry_hash = $3, numbering_hash = $4, search_hash = $5,
		manifest = $6::jsonb, state = $7, online_since = ` + online + `, applied_at = now() WHERE kg_id = $1`
	if insert {
		query = `INSERT INTO kg_schema_state (kg_no, kg_id, generator_version, registry_hash, numbering_hash, search_hash, manifest, state, online_since, applied_at)
			VALUES ($8, $1, $2, $3, $4, $5, $6::jsonb, $7, ` + online + `, now())`
	}
	args := []any{kg.String(), m.GeneratorVersion, m.RegistryHash, m.NumberingHash, m.SearchHash, string(raw), state}
	if insert {
		args = append(args, m.KGNo)
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return dbErr(err, "write the state of the storage schema")
	}
	return nil
}

// hashesOf are the generator version and the three hashes a model gives a schema.
type hashesOf struct {
	generator                   int
	registry, numbering, search string
}

func hashesOfModel(m SchemaModel) (hashesOf, error) {
	if m.Registry == nil {
		return hashesOf{}, kernel.Errorf(kernel.CodeInvalidValue, "the storage schema needs a registry")
	}
	numbering, err := m.Registry.NumberingHash()
	if err != nil {
		return hashesOf{}, err
	}
	search, err := pgschema.NewSearchSet(m.Search...).Hash()
	if err != nil {
		return hashesOf{}, err
	}
	return hashesOf{pgschema.GeneratorVersion, m.Registry.Hash().String(), numbering.String(), search.String()}, nil
}

// CheckSchemaMatch is the judgement of D19: nil when the schema of the record was made from the model, otherwise
// CodeSchemaMismatch with what differs. It is a pure function of the record and the model, so the writers and
// the readers of a KG can all ask it.
func CheckSchemaMatch(rec SchemaRecord, m SchemaModel) error {
	want, err := hashesOfModel(m)
	if err != nil {
		return err
	}
	var differs []string
	for _, c := range []struct {
		what      string
		have, got string
	}{
		{"the generator version", fmt.Sprint(rec.GeneratorVersion), fmt.Sprint(want.generator)},
		{"the model", rec.RegistryHash, want.registry},
		{"the numbering of the model", rec.NumberingHash, want.numbering},
		{"the search set", rec.SearchHash, want.search},
	} {
		if c.have != c.got {
			differs = append(differs, c.what)
		}
	}
	if len(differs) == 0 {
		return nil
	}
	return kernel.Errorf(CodeSchemaMismatch, "the storage schema of the KG %s was not made from what the server holds (it differs in: %s); "+
		"bring the schema up to the model or use the model it was made from", rec.KG, strings.Join(differs, ", "))
}

// StorageSchema reads the record of the storage schema of a KG. A KG without one is kernel.CodeNotFound.
func (s *Store) StorageSchema(ctx context.Context, kg graph.KGID) (SchemaRecord, error) {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return SchemaRecord{}, err
	}
	var rec SchemaRecord
	err = sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		var ok bool
		var err error
		if rec, ok, err = readRecord(ctx, tx, kg); err == nil && !ok {
			err = kernel.Errorf(kernel.CodeNotFound, "the KG %s has no storage schema", kg)
		}
		return err
	})
	return rec, err
}

// CheckStorageSchema reads the record of a KG and judges it against the model (CheckSchemaMatch). The paths of
// the apply and of the queries call it before they work on a KG (a later change hangs them on it).
func (s *Store) CheckStorageSchema(ctx context.Context, kg graph.KGID, m SchemaModel) error {
	rec, err := s.StorageSchema(ctx, kg)
	if err != nil {
		return err
	}
	return CheckSchemaMatch(rec, m)
}

// migrationRole is the role the executor runs under: it changes the shape of the database, so it has the limits
// of the migration runner.
const migrationRole = pgconn.RoleMigration
