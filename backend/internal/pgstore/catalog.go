package pgstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// catalogLockKey is the key of the transaction-level lock that serializes the creation of KGs: the name of a KG
// and "one production KG" are rules across KGs, so they cannot be a unique index that starts with kg_id.
const catalogLockKey int64 = 0x7072617869756d32

const kgColumns = `kg_id, name, role, state, base_kg_id, definition_set_hash, generation, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanKG(sc scanner) (graph.KG, error) {
	var (
		kg                 graph.KG
		id, base, role, st string
		definition         []byte
		created            time.Time
	)
	if err := sc.Scan(&id, &kg.Name, &role, &st, &base, &definition, &kg.Generation, &created); err != nil {
		return graph.KG{}, err
	}
	var err error
	if kg.ID, err = parseID[graph.KGKind](id); err != nil {
		return graph.KG{}, err
	}
	if base != "" {
		if kg.BaseKGID, err = parseID[graph.KGKind](base); err != nil {
			return graph.KG{}, err
		}
	}
	if kg.DefinitionSetHash, err = hashOf(definition); err != nil {
		return graph.KG{}, err
	}
	kg.Role, kg.State, kg.CreatedAt = graph.Role(role), graph.State(st), created.UTC()
	return kg, nil
}

// CreateKG implements graph.Catalog. The KG gets generation 1, an empty head and a lease row nobody holds.
func (s *Store) CreateKG(ctx context.Context, kg graph.KG) (graph.KG, error) {
	if kg.ID.IsZero() || kg.Name == "" || !kg.Role.Valid() {
		return graph.KG{}, kernel.Errorf(kernel.CodeInvalidValue, "a KG needs an id, a name and a role")
	}
	if kg.State == "" {
		kg.State = graph.StateBuilding
	}
	kg.Generation = 1
	kg.CreatedAt = pgTime(kg.CreatedAt)
	sh, err := s.rt.locate(kg.ID)
	if err != nil {
		return graph.KG{}, err
	}
	err = sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error { return insertKG(ctx, tx, kg) })
	if err != nil {
		return graph.KG{}, err
	}
	return kg, nil
}

func insertKG(ctx context.Context, tx *sql.Tx, kg graph.KG) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, catalogLockKey); err != nil {
		return dbErr(err, "take the catalog lock")
	}
	var clash bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM kg WHERE kg_id = $1 OR name = $2 OR $2 = 'main' OR ($3 AND role = 'production'))`,
		kg.ID.String(), kg.Name, kg.Role == graph.RoleProduction).Scan(&clash)
	if err != nil {
		return dbErr(err, "look for a KG that conflicts")
	}
	if clash {
		return kernel.Errorf(kernel.CodeConflict, "KG %q conflicts with one that exists", kg.Name)
	}
	base := ""
	if !kg.BaseKGID.IsZero() {
		base = kg.BaseKGID.String()
	}
	steps := []struct {
		what, query string
		args        []any
	}{
		{"insert the KG", `INSERT INTO kg (kg_id, created_no, name, role, state, base_kg_id, definition_set_hash, generation, created_at)
			VALUES ($1, (SELECT COALESCE(MAX(created_no), 0) + 1 FROM kg), $2, $3, $4, $5, $6, 1, $7)`,
			[]any{kg.ID.String(), kg.Name, string(kg.Role), string(kg.State), base, hashBytes(kg.DefinitionSetHash), kg.CreatedAt}},
		{"insert the head", `INSERT INTO kg_head (kg_id, hash_sum, input_digest) VALUES ($1, $2, $3)`,
			[]any{kg.ID.String(), make([]byte, 32), make([]byte, 32)}},
		{"insert the lease row", `INSERT INTO kg_apply_lease (kg_id) VALUES ($1)`, []any{kg.ID.String()}},
		{"insert the first generation", `INSERT INTO generation_event (kg_id, generation, restored_to_seq, at) VALUES ($1, 1, 0, $2)`,
			[]any{kg.ID.String(), kg.CreatedAt}},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.query, st.args...); err != nil {
			return dbErr(err, st.what)
		}
	}
	return nil
}

// GetKG implements graph.Catalog: by id, by name, or "main" for the production KG, the first match by creation.
func (s *Store) GetKG(ctx context.Context, ref string) (graph.KG, error) {
	for _, sh := range s.rt.all() {
		var kg graph.KG
		err := sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
			row := tx.QueryRowContext(ctx, `SELECT `+kgColumns+` FROM kg WHERE kg_id = $1 OR name = $1 OR ($1 = 'main' AND role = 'production')
				ORDER BY created_no LIMIT 1`, ref)
			var err error
			kg, err = scanKG(row)
			return err
		})
		if err == nil {
			return kg, nil
		}
		if !isNoRows(err) {
			return graph.KG{}, dbErr(err, "look a KG up")
		}
	}
	return graph.KG{}, kernel.Errorf(graph.CodeKGNotFound, "no KG %q", ref)
}

// ListKGs implements graph.Catalog: the KGs in the order they were created.
func (s *Store) ListKGs(ctx context.Context) ([]graph.KG, error) {
	var out []graph.KG
	for _, sh := range s.rt.all() {
		err := sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT `+kgColumns+` FROM kg ORDER BY created_no`)
			if err != nil {
				return dbErr(err, "list the KGs")
			}
			defer rows.Close()
			for rows.Next() {
				kg, err := scanKG(rows)
				if err != nil {
					return dbErr(err, "read a KG")
				}
				out = append(out, kg)
			}
			return dbErr(rows.Err(), "list the KGs")
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Generations implements graph.Catalog.
func (s *Store) Generations(ctx context.Context, id graph.KGID) ([]graph.GenerationEvent, error) {
	sh, err := s.rt.locate(id)
	if err != nil {
		return nil, err
	}
	var out []graph.GenerationEvent
	err = sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		if _, err := loadHead(ctx, tx, id, false); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT generation, restored_to_seq, at FROM generation_event WHERE kg_id = $1 ORDER BY generation`, id.String())
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
