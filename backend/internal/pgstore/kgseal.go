package pgstore

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// Seal implements graph.Writer: it records the version and its ordering event and moves the head.
func (s *StoreV2) Seal(ctx context.Context, w graph.SealWrite) error {
	a, err := s.access(ctx, w.KG)
	if err != nil {
		return err
	}
	if err := a.checked(); err != nil {
		return err
	}
	err = a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		cur, err := s.kgGuard(ctx, tx, a, planned{w.KG, w.Generation, w.ExpectedSeq, w.Lease, w.Now})
		if err != nil {
			return err
		}
		if cur.head.AppliedSeq != cur.head.ReadableThrough || cur.head.Open() {
			return kernel.Errorf(graph.CodeSealNotReady, "applied_seq %d, readable_through %d", cur.head.AppliedSeq, cur.head.ReadableThrough)
		}
		var sealed int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+a.t(pgschema.TableVersion)).Scan(&sealed); err != nil {
			return dbErr(err, "count the versions")
		}
		if w.Version.No != sealed+1 || w.Version.SealedSeq != cur.head.ReadableThrough {
			return kernel.Errorf(kernel.CodeInvalidValue, "version %d at %d does not follow the %d sealed versions", w.Version.No, w.Version.SealedSeq, sealed)
		}
		c := &kgChunk{writer: newWriter(ctx, tx, w.KG), a: a}
		c.version(w.Version)
		c.order(w.Order)
		if c.err != nil {
			return c.err
		}
		return saveKGHead(ctx, tx, a, w.Head)
	})
	return s.resolve(ctx, a, w.Head, err)
}

func (c *kgChunk) version(v graph.Version) {
	prev := ""
	if !v.Prev.IsZero() {
		prev = v.Prev.String()
	}
	c.exec("write the version", c.a.sub(`INSERT INTO @ (version_no, version_id, sealed_seq, generation, prev_version_id, definition_set_hash,
		engine_version, hash_rule_version, input_digest, graph_hash, row_count, sealed_at, seal_kind)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`, pgschema.TableVersion),
		v.No, v.ID.String(), v.SealedSeq, v.Generation, prev, hashBytes(v.DefinitionSetHash), v.EngineVersion, v.HashRuleVersion,
		hashBytes(v.InputDigest), sumBytes(v.GraphHash), v.Rows, pgTime(v.SealedAt), string(v.Kind))
	var entities, types []string
	var sums [][]byte
	var rows []int64
	for k, p := range v.Parts {
		entities, types, sums, rows = append(entities, k.Entity), append(types, k.TypeID), append(sums, sumBytes(p.Sum)), append(rows, p.Rows)
	}
	if len(entities) > 0 {
		c.exec("write the version parts", c.a.sub(`INSERT INTO @ (version_no, entity_kind, type_id, partial_sum, row_count)
			SELECT $1, u.entity_kind, u.type_id, u.partial_sum, u.row_count
			FROM unnest($2::text[], $3::text[], $4::bytea[], $5::bigint[]) AS u(entity_kind, type_id, partial_sum, row_count)`, pgschema.TableVersionHashPart),
			v.No, pq.Array(entities), pq.Array(types), pq.ByteaArray(sums), pq.Array(rows))
	}
}
