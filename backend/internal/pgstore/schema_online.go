package pgstore

import (
	"context"
	"database/sql"
	"regexp"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// searchIndexName is the name an index of the search set has (pgschema.SearchIndex): t_<type_no>_a<attr_no>.
var searchIndexName = regexp.MustCompile(`^t_[0-9]+_a[0-9]+$`)

// buildOnline is the second part of an adjustment: the indexes that run outside a transaction, one statement at a
// time. First every invalid index of the schema is dropped (a stopped run left it, and IF NOT EXISTS would
// keep it), then the steps of the change run, then the schema is compared with its manifest and what is still
// missing is built and what the manifest does not know is dropped. A failure puts the state back to
// index_pending, so that the next run does the rest.
func buildOnline(ctx context.Context, sh *shard, adj adjustment) error {
	err := doOnline(ctx, sh, adj)
	if err != nil {
		if perr := markIndexPending(ctx, sh, adj); perr != nil {
			return kernel.Wrap(kernel.CodeInternal, err, "build the indexes (and the state could not be set back to index_pending: "+perr.Error()+")")
		}
		return err
	}
	return locked(ctx, sh, adj.rec.KG, func(tx *sql.Tx) error {
		have, err := listIndexes(ctx, tx, adj.rec.Schema())
		if err != nil {
			return err
		}
		if bad := have.invalid(); len(bad) > 0 {
			return kernel.Errorf(kernel.CodeInternal, "the index %s is still invalid after it was built", bad[0])
		}
		done := adj.rec.Manifest
		done.State = pgschema.StateOK
		return writeRecord(ctx, tx, recordWrite{kg: adj.rec.KG, manifest: done, state: string(pgschema.StateOK)})
	})
}

func doOnline(ctx context.Context, sh *shard, adj adjustment) error {
	schema := adj.rec.Schema()
	have, err := readIndexes(ctx, sh, schema)
	if err != nil {
		return err
	}
	for _, name := range have.invalid() {
		if err := sh.pool.ExecOutsideTx(ctx, migrationRole, dropIndex(schema, name)); err != nil {
			return err
		}
	}
	for _, st := range adj.concurrent {
		if err := sh.pool.ExecOutsideTx(ctx, migrationRole, st.SQL); err != nil {
			return dbErr(err, "run the step that makes the "+st.What+" "+st.Name)
		}
	}
	if have, err = readIndexes(ctx, sh, schema); err != nil {
		return err
	}
	rest, err := reconcileSteps(adj.rec.Manifest, have)
	if err != nil {
		return err
	}
	for _, st := range rest {
		if err := sh.pool.ExecOutsideTx(ctx, migrationRole, st.SQL); err != nil {
			return dbErr(err, "run the step that makes the "+st.What+" "+st.Name)
		}
	}
	return nil
}

func dropIndex(schema, name string) string {
	return "DROP INDEX CONCURRENTLY IF EXISTS " + pgschema.Qualified(schema, name)
}

func markIndexPending(ctx context.Context, sh *shard, adj adjustment) error {
	return locked(ctx, sh, adj.rec.KG, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE kg_schema_state SET state = 'index_pending', online_since = NULL
			WHERE kg_id = $1 AND state = 'building'`, adj.rec.KG.String())
		return dbErr(err, "set the state back to index_pending")
	})
}

// indexSet is the indexes of a schema by name; the value says whether the index is valid.
type indexSet map[string]bool

// invalid lists the names of the indexes that are not valid, sorted.
func (x indexSet) invalid() []string {
	var out []string
	for name, ok := range x {
		if !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func listIndexes(ctx context.Context, tx *sql.Tx, schema string) (indexSet, error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.relname, i.indisvalid FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1`, schema)
	if err != nil {
		return nil, dbErr(err, "list the indexes of the storage schema")
	}
	defer func() { _ = rows.Close() }()
	out := indexSet{}
	for rows.Next() {
		var name string
		var valid bool
		if err := rows.Scan(&name, &valid); err != nil {
			return nil, dbErr(err, "read the indexes of the storage schema")
		}
		out[name] = valid
	}
	return out, dbErr(rows.Err(), "read the indexes of the storage schema")
}

func readIndexes(ctx context.Context, sh *shard, schema string) (indexSet, error) {
	var out indexSet
	err := sh.tx(ctx, migrationRole, false, func(tx *sql.Tx) error {
		var err error
		out, err = listIndexes(ctx, tx, schema)
		return err
	})
	return out, err
}

// reconcileSteps compares the indexes of the search set and the name indexes of the types that the schema has with those its
// manifest names: the ones that are missing are built (the steps come from pgschema.Diff, from the manifest without them to the
// manifest), the ones that the manifest does not name are dropped. A schema that matches needs none.
func reconcileSteps(m pgschema.Manifest, have indexSet) ([]pgschema.Step, error) {
	steps, named, err := missingSearchSteps(m, have)
	if err != nil {
		return nil, err
	}
	for _, t := range m.Types {
		for _, st := range pgschema.NameIndexSteps(pgschema.SchemaName(m.KGNo), t.TypeNo) {
			if !have[st.Name] {
				steps = append(steps, st)
			}
		}
	}
	return append(steps, extraSearchSteps(m, have, named)...), nil
}

// missingSearchSteps builds the search-set indexes that are not there or not valid, and gives the names the manifest has.
func missingSearchSteps(m pgschema.Manifest, have indexSet) ([]pgschema.Step, map[string]bool, error) {
	named := map[string]bool{}
	without := m
	without.Types = make([]pgschema.TypeSummary, len(m.Types))
	missing := false
	for i, t := range m.Types {
		without.Types[i] = t
		without.Types[i].Search = nil
		for _, a := range t.Search {
			name := pgschema.SearchIndex(t.TypeNo, a)
			named[name] = true
			if valid, ok := have[name]; ok && valid {
				without.Types[i].Search = append(without.Types[i].Search, a)
			} else {
				missing = true
			}
		}
	}
	if !missing {
		return nil, named, nil
	}
	delta, err := pgschema.Diff(without, m)
	return delta.Steps, named, err
}

// extraSearchSteps drops the search-set indexes that the manifest does not name.
func extraSearchSteps(m pgschema.Manifest, have indexSet, named map[string]bool) []pgschema.Step {
	var extra []string
	for name := range have {
		if searchIndexName.MatchString(name) && !named[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	var steps []pgschema.Step
	for _, name := range extra {
		steps = append(steps, pgschema.Step{Phase: pgschema.Concurrent, What: pgschema.WhatIndex, Name: name, SQL: dropIndex(pgschema.SchemaName(m.KGNo), name)})
	}
	return steps
}
