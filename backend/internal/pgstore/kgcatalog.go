package pgstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// catalogRow is a row of kg_catalog: what a KG is called and made from, which does not change.
type catalogRow struct {
	id         graph.KGID
	kgNo       int64
	name       string
	role       graph.Role
	base       graph.KGID
	definition kernel.Hash
	created    time.Time
}

const catalogColumns = `kg_id, kg_no, name, role, base_kg_id, definition_set_hash, created_at`

func scanCatalog(sc scanner) (catalogRow, error) {
	var (
		c                  catalogRow
		id, base, roleText string
		definition         []byte
	)
	if err := sc.Scan(&id, &c.kgNo, &c.name, &roleText, &base, &definition, &c.created); err != nil {
		return catalogRow{}, err
	}
	var err error
	if c.id, err = parseID[graph.KGKind](id); err != nil {
		return catalogRow{}, err
	}
	if base != "" {
		if c.base, err = parseID[graph.KGKind](base); err != nil {
			return catalogRow{}, err
		}
	}
	c.role, c.created = graph.Role(roleText), c.created.UTC()
	c.definition, err = hashOf(definition)
	return c, err
}

// CreateKG implements graph.Catalog: the row of the list, the storage schema made from the model, and the control
// rows of the KG (generation 1, an empty head, a lease row nobody holds), all in one transaction. A KG that was
// not made leaves nothing behind.
func (s *Store) CreateKG(ctx context.Context, kg graph.KG) (graph.KG, error) {
	if kg.ID.IsZero() || kg.Name == "" || !kg.Role.Valid() {
		return graph.KG{}, kernel.Errorf(kernel.CodeInvalidValue, "a KG needs an id, a name and a role")
	}
	if kg.State == "" {
		kg.State = graph.StateBuilding
	}
	kg.Generation, kg.CreatedAt = 1, pgTime(kg.CreatedAt)
	m, err := s.models(kg.DefinitionSetHash)
	if err != nil {
		return graph.KG{}, err
	}
	if m.Registry == nil || m.Registry.Hash() != kg.DefinitionSetHash {
		return graph.KG{}, kernel.Errorf(kernel.CodeInvalidValue, "the definition set of the KG is not the model the server holds")
	}
	sh, err := s.rt.locate(kg.ID)
	if err != nil {
		return graph.KG{}, err
	}
	err = locked(ctx, sh, kg.ID, func(tx *sql.Tx) error { return createKGInTx(ctx, tx, kg, m) })
	if err != nil {
		return graph.KG{}, err
	}
	return kg, nil
}

func createKGInTx(ctx context.Context, tx *sql.Tx, kg graph.KG, m SchemaModel) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, catalogLockKey); err != nil {
		return dbErr(err, "take the catalog lock")
	}
	var clash bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM kg_catalog WHERE kg_id = $1 OR name = $2 OR $2 = 'main' OR ($3 AND role = 'production'))`,
		kg.ID.String(), kg.Name, kg.Role == graph.RoleProduction).Scan(&clash)
	if err != nil {
		return dbErr(err, "look for a KG that conflicts")
	}
	if clash {
		return kernel.Errorf(kernel.CodeConflict, "KG %q conflicts with one that exists", kg.Name)
	}
	rec, err := createInTx(ctx, tx, kg.ID, m, buildPlan)
	if err != nil {
		return err
	}
	base := ""
	if !kg.BaseKGID.IsZero() {
		base = kg.BaseKGID.String()
	}
	schema := pgschema.QuoteIdent(rec.Schema())
	steps := []struct {
		what, query string
		args        []any
	}{
		{"insert the KG", `INSERT INTO kg_catalog (kg_id, kg_no, name, role, base_kg_id, definition_set_hash, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			[]any{kg.ID.String(), rec.KGNo, kg.Name, string(kg.Role), base, hashBytes(kg.DefinitionSetHash), kg.CreatedAt}},
		{"insert the head", `INSERT INTO ` + schema + `.head (id, state, hash_sum, input_digest) VALUES (1, $1, $2, $3)`,
			[]any{string(kg.State), make([]byte, 32), make([]byte, 32)}},
		{"insert the lease row", `INSERT INTO ` + schema + `.lease (id) VALUES (1)`, nil},
		{"insert the first generation", `INSERT INTO ` + schema + `.generation_event (generation, restored_to_seq, at) VALUES (1, 0, $1)`,
			[]any{kg.CreatedAt}},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.query, st.args...); err != nil {
			return dbErr(err, st.what)
		}
	}
	return nil
}

// DeleteKG deletes a KG: its storage schema with everything in it, its state record and its row of the list, in
// one transaction. A KG that is not there is graph.CodeKGNotFound. The number of the KG is not given again.
func (s *Store) DeleteKG(ctx context.Context, kg graph.KGID) error {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return err
	}
	defer s.forget(kg)
	return locked(ctx, sh, kg, func(tx *sql.Tx) error {
		rec, found, err := readRecord(ctx, tx, kg)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM kg_catalog WHERE kg_id = $1`, kg.String())
		if err != nil {
			return dbErr(err, "delete the KG from the list")
		}
		if n, _ := res.RowsAffected(); n == 0 && !found {
			return kernel.Errorf(graph.CodeKGNotFound, "no KG %q", kg)
		}
		if !found {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+pgschema.QuoteIdent(rec.Schema())+` CASCADE`); err != nil {
			return dbErr(err, "drop the storage schema")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM kg_schema_state WHERE kg_id = $1`, kg.String())
		return dbErr(err, "delete the state of the storage schema")
	})
}

// kgOf is the KG of a row of the list with the state and the generation of its head.
func (s *Store) kgOf(ctx context.Context, a *kgAccess) (graph.KG, error) {
	var h kgHeadRow
	err := a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		var err error
		h, err = loadKGHead(ctx, tx, a, false)
		return err
	})
	if err != nil && kernel.CodeOf(err) != CodeKGRestoring {
		return graph.KG{}, err
	}
	return graph.KG{ID: a.id, Name: a.cat.name, Role: a.cat.role, State: h.state, BaseKGID: a.cat.base,
		DefinitionSetHash: a.cat.definition, Generation: h.generation, CreatedAt: a.cat.created}, nil
}

// GetKG implements graph.Catalog: by id, by name, or "main" for the production KG, the first match by creation.
func (s *Store) GetKG(ctx context.Context, ref string) (graph.KG, error) {
	for _, sh := range s.rt.all() {
		var id string
		err := sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT kg_id FROM kg_catalog WHERE kg_id = $1 OR name = $1 OR ($1 = 'main' AND role = 'production')
				ORDER BY kg_no LIMIT 1`, ref).Scan(&id)
		})
		if isNoRows(err) {
			continue
		}
		if err != nil {
			return graph.KG{}, dbErr(err, "look a KG up")
		}
		kgID, err := parseID[graph.KGKind](id)
		if err != nil {
			return graph.KG{}, err
		}
		a, err := s.access(ctx, kgID)
		if err != nil {
			return graph.KG{}, err
		}
		return s.kgOf(ctx, a)
	}
	return graph.KG{}, kernel.Errorf(graph.CodeKGNotFound, "no KG %q", ref)
}

// ListKGs implements graph.Catalog: the KGs in the order they were created.
func (s *Store) ListKGs(ctx context.Context) ([]graph.KG, error) {
	var out []graph.KG
	for _, sh := range s.rt.all() {
		var ids []graph.KGID
		err := sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT kg_id FROM kg_catalog ORDER BY kg_no`)
			if err != nil {
				return dbErr(err, "list the KGs")
			}
			defer rows.Close()
			for rows.Next() {
				var text string
				if err := rows.Scan(&text); err != nil {
					return dbErr(err, "read a KG")
				}
				id, err := parseID[graph.KGKind](text)
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
			return dbErr(rows.Err(), "list the KGs")
		})
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			a, err := s.access(ctx, id)
			if err != nil {
				return nil, err
			}
			kg, err := s.kgOf(ctx, a)
			if err != nil {
				return nil, err
			}
			out = append(out, kg)
		}
	}
	return out, nil
}

// Generations implements graph.Catalog.
func (s *Store) Generations(ctx context.Context, id graph.KGID) ([]graph.GenerationEvent, error) {
	a, err := s.access(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []graph.GenerationEvent
	err = a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT generation, restored_to_seq, at FROM `+a.t(pgschema.TableGenerationEvent)+` ORDER BY generation`)
		if err != nil {
			return dbErr(err, "list the generations")
		}
		defer rows.Close()
		for rows.Next() {
			var g graph.GenerationEvent
			if err := rows.Scan(&g.Generation, &g.RestoredTo, &g.At); err != nil {
				return dbErr(err, "read a generation")
			}
			g.At = g.At.UTC()
			out = append(out, g)
		}
		return dbErr(rows.Err(), "list the generations")
	})
	return out, err
}
