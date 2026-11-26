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
// rows of the KG (generation 1, an empty head, a lease row nobody holds) in one transaction, and then the query
// schemas, one to a transaction (ADR-0126); the KG is closed and out of the list until the last. A KG that was not
// made leaves nothing behind. A KG that is being deleted and has the id, the name or the production role of the new
// one is deleted to the end first (ADR-0125), and so is one whose making a server left and whose term is over; one
// that another server is making is a conflict, as a KG that is there.
func (s *Store) CreateKG(ctx context.Context, kg graph.KG) (graph.KG, error) {
	if kg.ID.IsZero() || kg.Name == "" || !kg.Role.Valid() {
		return graph.KG{}, kernel.Errorf(kernel.CodeInvalidValue, "a KG needs an id, a name and a role")
	}
	kg = madeKG(kg)
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
	if err := s.endClashingDeletes(ctx, sh, kg); err != nil {
		return graph.KG{}, err
	}
	if err := s.endClashingMakings(ctx, sh, kg.ID, kg.Name, kg.Role == graph.RoleProduction); err != nil {
		return graph.KG{}, err
	}
	var out made
	err = locked(ctx, sh, kg.ID, func(tx *sql.Tx) error {
		var err error
		if out, err = createKGInTx(ctx, tx, kg, m); err != nil {
			return err
		}
		return s.schemaStep(ctx, schemaStepMake, tx)
	})
	if err != nil {
		return graph.KG{}, err
	}
	if err := s.makeRest(ctx, sh, kg.ID, out.plan); err != nil {
		return graph.KG{}, err
	}
	return kg, nil
}

// madeKG is the KG that CreateKG makes, and returns, from the one it is given.
func madeKG(kg graph.KG) graph.KG {
	if kg.State == "" {
		kg.State = graph.StateBuilding
	}
	kg.Generation, kg.CreatedAt = 1, pgTime(kg.CreatedAt)
	return kg
}

// createKGInTx is the first transaction of CreateKG: the storage schema and the mark of the making (createInTx), and the row of
// the list and the control rows.
func createKGInTx(ctx context.Context, tx *sql.Tx, kg graph.KG, m SchemaModel) (made, error) {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, catalogLockKey); err != nil {
		return made{}, dbErr(err, "take the catalog lock")
	}
	var clash bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM kg_catalog WHERE kg_id = $1 OR name = $2 OR $2 = 'main' OR ($3 AND role = 'production'))`,
		kg.ID.String(), kg.Name, kg.Role == graph.RoleProduction).Scan(&clash)
	if err != nil {
		return made{}, dbErr(err, "look for a KG that conflicts")
	}
	if clash {
		return made{}, kernel.Errorf(kernel.CodeConflict, "KG %q conflicts with one that exists", kg.Name)
	}
	out, err := createInTx(ctx, tx, kg.ID, m, buildPlan)
	if err != nil {
		return made{}, err
	}
	rec := out.rec
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
			return made{}, dbErr(err, st.what)
		}
	}
	return out, nil
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
			return tx.QueryRowContext(ctx, `SELECT kg_id FROM kg_catalog c WHERE (kg_id = $1 OR name = $1 OR ($1 = 'main' AND role = 'production'))
				AND NOT EXISTS (SELECT 1 FROM `+closedKGs+` d WHERE d.kg_id = c.kg_id) ORDER BY kg_no LIMIT 1`, ref).Scan(&id)
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

// ListKGs implements graph.Catalog: the KGs in the order they were created. A KG that is being deleted (ADR-0125) or made
// (ADR-0126) is not in it: it is closed to reads and writes until its delete or its making ends.
func (s *Store) ListKGs(ctx context.Context) ([]graph.KG, error) {
	var out []graph.KG
	for _, sh := range s.rt.all() {
		var ids []graph.KGID
		err := sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT kg_id FROM kg_catalog c WHERE NOT EXISTS (SELECT 1 FROM `+closedKGs+` d WHERE d.kg_id = c.kg_id)
				ORDER BY kg_no`)
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

// Generations implements graph.Catalog. A KG that is being deleted or made is not found (ADR-0125, ADR-0126).
func (s *Store) Generations(ctx context.Context, id graph.KGID) ([]graph.GenerationEvent, error) {
	a, err := s.access(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []graph.GenerationEvent
	err = a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		var closed bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM `+closedKGs+` d WHERE d.kg_id = $1)`, id.String()).Scan(&closed); err != nil {
			return dbErr(err, "look for the mark of a KG being made or deleted")
		}
		if closed {
			return errKGClosed(id)
		}
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
