package pgstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ApplyChunk implements graph.Writer. One transaction: lock the head, verify (generation, lease, engine, head),
// write every row, move the head. readable_through moves only because the head the applier computed says so.
func (s *Store) ApplyChunk(ctx context.Context, w graph.ChunkWrite) error {
	sh, err := s.rt.locate(w.KG)
	if err != nil {
		return err
	}
	return sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error { return applyChunk(ctx, tx, w) })
}

func applyChunk(ctx context.Context, tx *sql.Tx, w graph.ChunkWrite) error {
	cur, err := guard(ctx, tx, planned{w.KG, w.Generation, w.ExpectedSeq, w.Lease, w.Now})
	if err != nil {
		return err
	}
	seq := int64(0)
	if w.Batch != nil {
		seq = w.Batch.Seq
		if seq != cur.head.AppliedSeq+1 || w.Head.AppliedSeq != seq {
			return kernel.Errorf(graph.CodeHeadMoved, "chunk %d does not follow applied_seq %d", seq, cur.head.AppliedSeq)
		}
	} else if w.Head.AppliedSeq != cur.head.AppliedSeq {
		return kernel.Errorf(graph.CodeHeadMoved, "a chunk without rows may not move applied_seq")
	}
	if err := checkOpenings(w, seq); err != nil {
		return err
	}
	wr := newWriter(ctx, tx, w.KG)
	wr.objects(w.Objects, seq)
	wr.links(w.Links, seq)
	wr.volatile(w.Volatile)
	wr.light(w.Light)
	wr.identifiers(w.Identifiers, seq)
	wr.events(w.Events)
	if w.Batch != nil {
		wr.batch(*w.Batch)
		wr.entries(w.Entries)
	}
	if w.Order != nil {
		wr.order(*w.Order)
	}
	if wr.err != nil {
		return wr.err
	}
	if err := saveHead(ctx, tx, w.KG, w.Head); err != nil {
		return err
	}
	if w.Serve {
		_, err := tx.ExecContext(ctx, `UPDATE kg SET state = 'serving' WHERE kg_id = $1`, w.KG.String())
		return dbErr(err, "serve the KG")
	}
	return nil
}

// checkOpenings verifies that the rows the chunk opens begin at the chunk's change number.
func checkOpenings(w graph.ChunkWrite, seq int64) error {
	for _, c := range w.Objects {
		if c.New != nil && c.New.SeqFrom != seq {
			return kernel.Errorf(kernel.CodeInvalidValue, "object row %s opens at %d, the chunk is %d", c.New.ID, c.New.SeqFrom, seq)
		}
	}
	return nil
}

// AbandonApply implements graph.Writer: the chunks past readable_through go and what they closed is open again.
func (s *Store) AbandonApply(ctx context.Context, w graph.AbandonWrite) error {
	sh, err := s.rt.locate(w.KG)
	if err != nil {
		return err
	}
	return sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		cur, err := guard(ctx, tx, planned{w.KG, w.Generation, w.ExpectedSeq, w.Lease, w.Now})
		if err != nil {
			return err
		}
		if w.Head.AppliedSeq != cur.head.ReadableThrough {
			return kernel.Errorf(kernel.CodeInvalidValue, "an abandoned apply goes back to readable_through %d", cur.head.ReadableThrough)
		}
		if err := truncate(ctx, tx, w.KG, cur.head.ReadableThrough); err != nil {
			return err
		}
		return saveHead(ctx, tx, w.KG, w.Head)
	})
}

// truncateSteps take a KG back to change number $2: rows that began later go, rows that ended later are open
// again. The volatile values are the current ones and have no history, so they stay.
var truncateSteps = []struct{ what, query string }{
	{"drop the later objects", `DELETE FROM object_live WHERE kg_id = $1 AND seq_from > $2`},
	{"drop the later object history", `DELETE FROM object_hist WHERE kg_id = $1 AND seq_from > $2`},
	{"reopen objects", `WITH re AS (DELETE FROM object_hist WHERE kg_id = $1 AND seq_to > $2
			RETURNING object_id, type_no, name, min_grade, attrs, source_refs, content_hash, seq_from)
		INSERT INTO object_live (kg_id, object_id, type_no, name, min_grade, attrs, source_refs, content_hash, seq_from)
		SELECT $1, object_id, type_no, name, min_grade, attrs, source_refs, content_hash, seq_from FROM re`},
	{"drop the later links", `DELETE FROM link_live WHERE kg_id = $1 AND seq_from > $2`},
	{"drop the later link history", `DELETE FROM link_hist WHERE kg_id = $1 AND seq_from > $2`},
	{"reopen links", `WITH re AS (DELETE FROM link_hist WHERE kg_id = $1 AND seq_to > $2 RETURNING ` + linkColumns + `)
		INSERT INTO link_live (kg_id, ` + linkColumns + `) SELECT $1, ` + linkColumns + ` FROM re`},
	{"drop the later light changes", `DELETE FROM light_change WHERE kg_id = $1 AND change_seq > $2`},
	{"drop the later entries", `DELETE FROM change_entry WHERE kg_id = $1 AND change_seq > $2`},
	{"drop the later batches", `DELETE FROM change_batch WHERE kg_id = $1 AND change_seq > $2`},
	{"drop the later identifiers", `DELETE FROM object_identifier WHERE kg_id = $1 AND seq_from > $2`},
	{"reopen identifiers", `UPDATE object_identifier SET seq_to = NULL WHERE kg_id = $1 AND seq_to > $2`},
	{"drop the later identity events", `DELETE FROM identity_event WHERE kg_id = $1 AND change_seq > $2`},
}

func truncate(ctx context.Context, tx *sql.Tx, kg graph.KGID, seq int64) error {
	for _, st := range truncateSteps {
		if _, err := tx.ExecContext(ctx, st.query, kg.String(), seq); err != nil {
			return dbErr(err, st.what)
		}
	}
	return nil
}

// Seal implements graph.Writer: it records the version and its ordering event and moves the head.
func (s *Store) Seal(ctx context.Context, w graph.SealWrite) error {
	sh, err := s.rt.locate(w.KG)
	if err != nil {
		return err
	}
	return sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		cur, err := guard(ctx, tx, planned{w.KG, w.Generation, w.ExpectedSeq, w.Lease, w.Now})
		if err != nil {
			return err
		}
		if cur.head.AppliedSeq != cur.head.ReadableThrough || cur.head.Open() {
			return kernel.Errorf(graph.CodeSealNotReady, "applied_seq %d, readable_through %d", cur.head.AppliedSeq, cur.head.ReadableThrough)
		}
		var sealed int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM kg_version WHERE kg_id = $1`, w.KG.String()).Scan(&sealed); err != nil {
			return dbErr(err, "count the versions")
		}
		if w.Version.No != sealed+1 || w.Version.SealedSeq != cur.head.ReadableThrough {
			return kernel.Errorf(kernel.CodeInvalidValue, "version %d at %d does not follow the %d sealed versions", w.Version.No, w.Version.SealedSeq, sealed)
		}
		wr := newWriter(ctx, tx, w.KG)
		wr.version(w.Version)
		wr.order(w.Order)
		if wr.err != nil {
			return wr.err
		}
		return saveHead(ctx, tx, w.KG, w.Head)
	})
}

const versionSQL = `INSERT INTO kg_version (kg_id, version_no, version_id, sealed_seq, generation, prev_version_id, definition_set_hash,
	engine_version, hash_rule_version, input_digest, graph_hash, row_count, sealed_at, seal_kind)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

const versionPartSQL = `INSERT INTO kg_version_hash_part (kg_id, version_no, entity_kind, type_id, partial_sum, row_count)
SELECT $1, $2, u.entity_kind, u.type_id, u.partial_sum, u.row_count
FROM unnest($3::text[], $4::text[], $5::bytea[], $6::bigint[]) AS u(entity_kind, type_id, partial_sum, row_count)`

func (w *writer) version(v graph.Version) {
	prev := ""
	if !v.Prev.IsZero() {
		prev = v.Prev.String()
	}
	w.exec("write the version", versionSQL, w.kg, v.No, v.ID.String(), v.SealedSeq, v.Generation, prev, hashBytes(v.DefinitionSetHash),
		v.EngineVersion, v.HashRuleVersion, hashBytes(v.InputDigest), sumBytes(v.GraphHash), v.Rows, pgTime(v.SealedAt), string(v.Kind))
	var entities, types []string
	var sums [][]byte
	var rows []int64
	for k, p := range v.Parts {
		entities, types, sums, rows = append(entities, k.Entity), append(types, k.TypeID), append(sums, sumBytes(p.Sum)), append(rows, p.Rows)
	}
	if len(entities) > 0 {
		w.exec("write the version parts", versionPartSQL, w.kg, v.No, pq.Array(entities), pq.Array(types), pq.ByteaArray(sums), pq.Array(rows))
	}
}

// Restore implements graph.Catalog: the KG goes back to change number toSeq, the generation goes up by one and
// the restore is kept as an event.
func (s *Store) Restore(ctx context.Context, id graph.KGID, toSeq int64, head graph.Head, now time.Time) (graph.KG, error) {
	sh, err := s.rt.locate(id)
	if err != nil {
		return graph.KG{}, err
	}
	var out graph.KG
	err = sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		cur, err := loadHead(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if toSeq < 0 || toSeq > cur.head.ReadableThrough {
			return kernel.Errorf(graph.CodeNotReadable, "cannot restore to %d", toSeq)
		}
		if err := truncate(ctx, tx, id, toSeq); err != nil {
			return err
		}
		if err := dropLater(ctx, tx, id, toSeq); err != nil {
			return err
		}
		gen := cur.generation + 1
		for _, st := range []struct {
			what, query string
			args        []any
		}{
			{"raise the generation", `UPDATE kg SET generation = $2 WHERE kg_id = $1`, []any{gen}},
			{"keep the restore", `INSERT INTO generation_event (kg_id, generation, restored_to_seq, at) VALUES ($1, $2, $3, $4)`, []any{gen, toSeq, pgTime(now)}},
		} {
			if _, err := tx.ExecContext(ctx, st.query, append([]any{id.String()}, st.args...)...); err != nil {
				return dbErr(err, st.what)
			}
		}
		if err := saveHead(ctx, tx, id, head); err != nil {
			return err
		}
		row := tx.QueryRowContext(ctx, `SELECT `+kgColumns+` FROM kg WHERE kg_id = $1`, id.String())
		out, err = scanKG(row)
		return dbErr(err, "read the KG")
	})
	return out, err
}

// dropLater removes the ordering events and the versions that came after the restored change number.
func dropLater(ctx context.Context, tx *sql.Tx, kg graph.KGID, seq int64) error {
	for _, st := range []struct{ what, query string }{
		{"drop the later ordering events", `DELETE FROM ingest_order WHERE kg_id = $1 AND change_seq > $2`},
		{"drop the later version parts", `DELETE FROM kg_version_hash_part p USING kg_version v
			WHERE p.kg_id = $1 AND v.kg_id = p.kg_id AND v.version_no = p.version_no AND v.sealed_seq > $2`},
		{"drop the later versions", `DELETE FROM kg_version WHERE kg_id = $1 AND sealed_seq > $2`},
	} {
		if _, err := tx.ExecContext(ctx, st.query, kg.String(), seq); err != nil {
			return dbErr(err, st.what)
		}
	}
	return nil
}
