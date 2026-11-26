package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The making of a KG in steps (ADR-0126). The query schema of a KG (ADR-0113) locks about as much as its storage schema, and
// the two in one transaction do not fit in 80% of the lock table of the default settings; one query schema does, by far. So a
// making is a chain of transactions, each under the lock of the KG's schema work:
//
//  1. make: the storage schema, its record and the mark of the making (kg_making, migration 0010), and for CreateKG the row of
//     the list and the control rows. From then on the KG is closed to reads, writes and ingestion and left out of the list,
//     as one that is being deleted is (closedKGs).
//  2. query: each query schema of the plan (pgschema.QuerySchemaNames), in the order of the plan; the roles are made in the
//     first, and the last removes the mark.
//
// The caller still gets all or nothing: when a transaction of the query schema fails, what was made is deleted by the steps of
// a delete (ADR-0125) and the error is returned. A making whose server died leaves its mark; it is not ended but deleted, by a
// server that starts or by the CreateKG of the same id or name, once the term of the mark is over. Every transaction of the
// making moves the term on, so a making that a live server is doing is never taken for one that stopped.
//
// An adjustment (AdjustStorageSchema) and a server that starts make the query schemas again by the same steps, one schema to a
// transaction. Each query schema keeps, as its comment, the mark of the steps it was made by (queryPart.mark): a schema whose
// mark is the one of the plan is left as it is, so a schema that a stopped adjustment left stale is found and made again, and
// one that is right is not touched.

// makingTerm is how long the mark of a making stays good after its last transaction. A transaction of the making takes a second
// or two; the term is only there to tell a making that stopped from one that is going on.
const makingTerm = 10 * time.Minute

// The steps of a making or an adjustment, as schemaSeen is told them.
const (
	schemaStepMake   = "make"
	schemaStepAdjust = "adjust"
	schemaStepQuery  = "query"
	schemaStepFresh  = "fresh"
)

// queryMarkPrefix starts the comment of a query schema that this executor made: the rest is the hash of its steps.
const queryMarkPrefix = "praxium query plan "

// schemaStep tells the hook of the tests, if there is one, that a transaction of a making or an adjustment is at its end.
func (s *Store) schemaStep(ctx context.Context, step string, tx *sql.Tx) error {
	if s.schemaSeen == nil {
		return nil
	}
	return s.schemaSeen(ctx, step, tx)
}

// made is what the first transaction of a making leaves for the rest: the record, and the plan whose query schema is still to be
// made (none when there is nothing left to make).
type made struct {
	rec  SchemaRecord
	plan pgschema.Plan
}

// queryPart is one query schema of a plan: its name, the steps from the one that drops it to the one of its rights, the mark its
// comment carries once they ran, and the roles of the plan, which the steps give rights to.
type queryPart struct {
	name  string
	steps []pgschema.Step
	mark  string
	roles []string
}

// queryParts cuts the steps of the query schema of a plan into its schemas: a part starts at a step of a schema of another name, and
// the steps before the first of them (the filling of the description of the model) go with the first. A plan whose steps name no
// schema, or name one twice apart, is a mistake of the planner.
func queryParts(p pgschema.Plan) ([]queryPart, error) {
	var parts []queryPart
	var lead []pgschema.Step
	seen := map[string]bool{}
	for _, st := range p.Query {
		if st.What == pgschema.WhatSchema && (len(parts) == 0 || parts[len(parts)-1].name != st.Name) {
			if seen[st.Name] {
				return nil, kernel.Errorf(kernel.CodeInternal, "the steps of the query schema %s are not together", st.Name)
			}
			seen[st.Name] = true
			parts = append(parts, queryPart{name: st.Name, steps: lead})
			lead = nil
		}
		if len(parts) == 0 {
			lead = append(lead, st)
			continue
		}
		last := &parts[len(parts)-1]
		last.steps = append(last.steps, st)
	}
	if len(lead) > 0 {
		return nil, kernel.Errorf(kernel.CodeInternal, "the steps of the query schema name no schema")
	}
	for i := range parts {
		var text strings.Builder
		for _, st := range parts[i].steps {
			text.WriteString(st.What + "\x00" + st.Name + "\x00" + st.SQL + "\x00")
		}
		parts[i].mark = queryMarkPrefix + kernel.HashBytes([]byte(text.String())).String()
		parts[i].roles = p.Roles
	}
	return parts, nil
}

// queryMark reads the comment of a query schema; a schema that is not there has none.
func queryMark(ctx context.Context, tx *sql.Tx, name string) (string, error) {
	var mark string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(obj_description(oid, 'pg_namespace'), '') FROM pg_namespace WHERE nspname = $1`, name).Scan(&mark)
	if isNoRows(err) {
		return "", nil
	}
	return mark, dbErr(err, "read the mark of a query schema")
}

// makeQuerySchemas gives a KG the query schemas of a plan, one schema to a transaction, in the order of the plan. A schema whose
// mark is the one of its steps is left as it is (another run made it, or nothing changed), so a run again makes only what is
// missing or stale. A making (making) moves the term of its mark on in every transaction and removes the mark in the last.
func (s *Store) makeQuerySchemas(ctx context.Context, sh *shard, kg graph.KGID, p pgschema.Plan, making bool) error {
	parts, err := queryParts(p)
	if err != nil {
		return err
	}
	for i, part := range parts {
		last := making && i == len(parts)-1
		if err := locked(ctx, sh, kg, func(tx *sql.Tx) error { return s.makeQueryPart(ctx, tx, kg, part, making, last) }); err != nil {
			return err
		}
	}
	return nil
}

// makeQueryPart is the transaction of one query schema: the roles (made when the cluster has none of the name), the steps and
// the mark, unless the schema has its mark already.
func (s *Store) makeQueryPart(ctx context.Context, tx *sql.Tx, kg graph.KGID, part queryPart, making, last bool) error {
	if err := partGoesOn(ctx, tx, kg, making); err != nil {
		return err
	}
	have, err := queryMark(ctx, tx, part.name)
	if err != nil {
		return err
	}
	step := schemaStepFresh
	if have != part.mark {
		step = schemaStepQuery
		if err := runQuerySteps(ctx, tx, pgschema.Plan{Query: part.steps, Roles: part.roles}); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `COMMENT ON SCHEMA `+pgschema.QuoteIdent(part.name)+` IS `+pgschema.QuoteLiteral(part.mark)); err != nil {
			return dbErr(err, "mark a query schema")
		}
	}
	if last {
		if _, err := tx.ExecContext(ctx, `DELETE FROM kg_making WHERE kg_id = $1`, kg.String()); err != nil {
			return dbErr(err, "end the making of the KG")
		}
	}
	return s.schemaStep(ctx, step, tx)
}

// partGoesOn says whether a transaction of the query schema of a KG may go on: the KG still has its record and is not being
// deleted (or nothing would drop what this makes). For a making, a mark whose term is over is the end of the making (a server is
// deleting it); a mark that is good gets its term moved on, and no mark means another run ended the making.
func partGoesOn(ctx context.Context, tx *sql.Tx, kg graph.KGID, making bool) error {
	var there, deleting bool
	var over sql.NullBool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM kg_schema_state WHERE kg_id = $1), EXISTS (SELECT 1 FROM kg_deleting WHERE kg_id = $1),
		(SELECT expires_at < now() FROM kg_making WHERE kg_id = $1)`, kg.String()).Scan(&there, &deleting, &over)
	switch {
	case err != nil:
		return dbErr(err, "look at the KG before its query schema")
	case !there || deleting:
		return kernel.Errorf(graph.CodeKGNotFound, "the KG %s was deleted while its query schema was made", kg)
	case !making || !over.Valid:
		return nil
	case over.Bool:
		return kernel.Errorf(kernel.CodeConflict, "the making of the KG %s ran past its term and is deleted", kg)
	}
	_, err = tx.ExecContext(ctx, `UPDATE kg_making SET expires_at = now() + make_interval(secs => $2) WHERE kg_id = $1`, kg.String(), makingTerm.Seconds())
	return dbErr(err, "move the term of the making on")
}

// makeRest is the rest of a making after its first transaction: the query schemas. When one of them fails, what was made is deleted
// and the error of the making is returned; a delete that fails too leaves the mark, and the KG is deleted once its term is over.
func (s *Store) makeRest(ctx context.Context, sh *shard, kg graph.KGID, p pgschema.Plan) error {
	err := s.makeQuerySchemas(ctx, sh, kg, p, true)
	if err == nil {
		return nil
	}
	if _, undo := s.unmake(ctx, sh, kg, false); undo != nil {
		return errors.Join(err, kernel.Wrap(kernel.CodeOf(undo), undo, "the KG made in part stays closed until a server deletes it"))
	}
	return err
}

// unmake deletes what a making that did not end made, by the steps of a delete (ADR-0125), when its mark is still there (a making
// that another run ended is left as it is). stale asks for a mark whose term is over: the making of a server that stopped. A KG
// that has a row of the list is marked as being deleted in the transaction that finds the mark, so nothing is made under it from
// then on; one that has none (CreateStorageSchema) is dropped as DropStorageSchema drops it. It says whether it deleted.
func (s *Store) unmake(ctx context.Context, sh *shard, kg graph.KGID, stale bool) (bool, error) {
	var found, listed bool
	err := locked(ctx, sh, kg, func(tx *sql.Tx) error {
		var over bool
		err := tx.QueryRowContext(ctx, `SELECT m.expires_at < now(), EXISTS (SELECT 1 FROM kg_catalog c WHERE c.kg_id = m.kg_id) FROM kg_making m WHERE m.kg_id = $1`,
			kg.String()).Scan(&over, &listed)
		if isNoRows(err) {
			return nil
		}
		if err != nil {
			return dbErr(err, "read the mark of a KG being made")
		}
		found = over || !stale
		if !found || !listed {
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO kg_deleting (kg_id, started_at) VALUES ($1, now()) ON CONFLICT (kg_id) DO NOTHING`, kg.String())
		return dbErr(err, "mark the KG as being deleted")
	})
	if err != nil || !found {
		return false, err
	}
	defer s.forget(kg)
	if listed {
		return true, s.dropInSteps(ctx, sh, kg, deleteListed(ctx, kg))
	}
	return true, s.dropInSteps(ctx, sh, kg, func(tx *sql.Tx) error { return dropSchemaRest(ctx, tx, kg) })
}

// endStaleMakings deletes every KG whose making a server left and whose term is over, and returns them (a server that starts
// does it). A KG that cannot be deleted does not stop the others: its error is in the one returned.
func (s *Store) endStaleMakings(ctx context.Context) ([]graph.KGID, error) {
	return s.endMarked(ctx, `SELECT kg_id FROM kg_making WHERE expires_at < now() ORDER BY kg_id`, "the making",
		func(sh *shard, id graph.KGID) (bool, error) { return s.unmake(ctx, sh, id, true) })
}

// endClashingMakings deletes the KGs whose making a server left, whose term is over, and that a new KG would clash with: its id,
// its name (none for a storage schema alone) or, for the production KG, the one production KG.
func (s *Store) endClashingMakings(ctx context.Context, sh *shard, id graph.KGID, name string, production bool) error {
	ids, err := markedKGs(ctx, sh, `SELECT m.kg_id FROM kg_making m LEFT JOIN kg_catalog c ON c.kg_id = m.kg_id
		WHERE m.expires_at < now() AND (m.kg_id = $1 OR c.name = $2 OR ($3 AND c.role = 'production')) ORDER BY m.kg_id`,
		id.String(), name, production)
	if err != nil {
		return err
	}
	for _, kg := range ids {
		if _, err := s.unmake(ctx, sh, kg, true); err != nil {
			return err
		}
	}
	return nil
}
