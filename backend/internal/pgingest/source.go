package pgingest

import (
	"context"
	"database/sql"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// Putting a source to a KG and taking it off. The partitions of a source (the raw records and the four tables it is received
// into) are made here and not by a run, so that a run does no DDL (ADR-0095 decision 6). Each of the two is one transaction.

// SourceRef is a source put to a KG: the number it has in the database (the key of its partitions) and the number of each of
// its datasets by the id of the dataset.
type SourceRef struct {
	KGNo     int64
	SrcNo    int64
	Datasets map[string]int64
}

// chunkRows is how many rows of an unpartitioned table one transaction deletes when a source is taken off.
const chunkRows = 10_000

// AttachSource puts a source to a KG: its row of the state of sources, a row for each dataset it names and its partitions.
// The call can be made again: what is there is left as it is, and a dataset that was not named before is added. A source that
// is put to the KG is as new until its first run, so both of its marks ("receive everything", "compute everything") are set.
func (s *Store) AttachSource(ctx context.Context, kg graph.KGID, source string, datasets []string) (SourceRef, error) {
	var ref SourceRef
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		var err error
		ref, err = attachTx(ctx, tx, kg, source, datasets)
		return err
	})
	return ref, err
}

// attachTx is the work of AttachSource in a transaction of the caller.
func attachTx(ctx context.Context, tx *sql.Tx, kg graph.KGID, source string, datasets []string) (SourceRef, error) {
	if source == "" || len(datasets) == 0 {
		return SourceRef{}, kernel.Errorf(kernel.CodeInvalidValue, "a source put to a KG has an id and at least one dataset")
	}
	kgNo, err := kgNoOf(ctx, tx, kg)
	if err != nil {
		return SourceRef{}, err
	}
	// two calls for the same source take turns: the second finds what the first made.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "praxium.attach:"+kg.String()+":"+source); err != nil {
		return SourceRef{}, dbErr(err, "take the lock of the source")
	}
	ref := SourceRef{KGNo: kgNo, Datasets: map[string]int64{}}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+pgschema.TableSourceState+" ("+pgschema.ColKGID+", "+pgschema.ColSourceID+") VALUES ($1, $2) ON CONFLICT ("+
		pgschema.ColKGID+", "+pgschema.ColSourceID+") DO NOTHING", kg.String(), source); err != nil {
		return SourceRef{}, dbErr(err, "put the source to the KG")
	}
	err = tx.QueryRowContext(ctx, "SELECT "+pgschema.ColSrcNo+" FROM "+pgschema.TableSourceState+" WHERE "+pgschema.ColKGID+" = $1 AND "+pgschema.ColSourceID+" = $2",
		kg.String(), source).Scan(&ref.SrcNo)
	if err != nil {
		return SourceRef{}, dbErr(err, "read the number of the source")
	}
	if err := attachDatasets(ctx, tx, &ref, datasets); err != nil {
		return SourceRef{}, err
	}
	return ref, ensurePartitions(ctx, tx, ref)
}

// attachDatasets gives the datasets that the source does not have yet a row each (the numbers come from the sequence, once
// for the whole database) and reads the number of every dataset the source has.
func attachDatasets(ctx context.Context, tx *sql.Tx, ref *SourceRef, datasets []string) error {
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+pgschema.TableSourceDataset+" ("+pgschema.ColSrcNo+", "+pgschema.ColDatasetID+") SELECT $1, d FROM unnest($2::text[]) AS d"+
		" ON CONFLICT ("+pgschema.ColSrcNo+", "+pgschema.ColDatasetID+") DO NOTHING", ref.SrcNo, textArray(datasets)); err != nil {
		return dbErr(err, "add the datasets of the source")
	}
	return attachRead(ctx, tx, ref)
}

// attachRead reads the number of every dataset the source has.
func attachRead(ctx context.Context, tx *sql.Tx, ref *SourceRef) error {
	rows, err := tx.QueryContext(ctx, "SELECT "+pgschema.ColDatasetID+", "+pgschema.ColDatasetNo+" FROM "+pgschema.TableSourceDataset+" WHERE "+pgschema.ColSrcNo+" = $1", ref.SrcNo)
	if err != nil {
		return dbErr(err, "read the datasets of the source")
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var no int64
		if err := rows.Scan(&id, &no); err != nil {
			return dbErr(err, "read the datasets of the source")
		}
		ref.Datasets[id] = no
	}
	return dbErr(rows.Err(), "read the datasets of the source")
}

// ensurePartitions makes the partitions of the source that are not there and attaches the ones that are not attached. A
// partition is made as a table of its own and attached, which leaves the other sources free to read and write meanwhile.
func ensurePartitions(ctx context.Context, tx *sql.Tx, ref SourceRef) error {
	steps, err := pgschema.SourcePartitionSteps(ref.KGNo, ref.SrcNo)
	if err != nil {
		return err
	}
	have, err := partitionsOf(ctx, tx, ref)
	if err != nil {
		return err
	}
	for _, st := range steps {
		attached, exists := have[st.Name]
		if (st.What == pgschema.WhatTable && exists) || (st.What == pgschema.WhatPartition && attached) {
			continue
		}
		if _, err := tx.ExecContext(ctx, st.SQL); err != nil {
			return dbErr(err, "run the step that makes the "+st.What+" "+st.Name)
		}
	}
	return nil
}

// partitionsOf finds which tables of the partitions of the source exist, and whether each is attached.
func partitionsOf(ctx context.Context, tx *sql.Tx, ref SourceRef) (map[string]bool, error) {
	names := make([]string, 0, len(pgschema.SourceTables()))
	for _, t := range pgschema.SourceTables() {
		names = append(names, pgschema.SourcePartition(t, ref.SrcNo))
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.relname, i.inhrelid IS NOT NULL FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_inherits i ON i.inhrelid = c.oid WHERE n.nspname = $1 AND c.relname = ANY($2::text[])`, pgschema.SchemaName(ref.KGNo), textArray(names))
	if err != nil {
		return nil, dbErr(err, "look for the partitions of the source")
	}
	have := map[string]bool{} // name -> attached
	for rows.Next() {
		var name string
		var attached bool
		if err := rows.Scan(&name, &attached); err != nil {
			_ = rows.Close()
			return nil, dbErr(err, "look for the partitions of the source")
		}
		have[name] = attached
	}
	if err := rows.Close(); err != nil {
		return nil, dbErr(err, "look for the partitions of the source")
	}
	return have, nil
}

// DetachSource takes a source off a KG: the rows the runs of the source left in the tables that are not partitioned are
// deleted a chunk to a transaction, and then, in one transaction, its partitions are dropped and its row of the state of
// sources goes with its datasets and its runs. A source that has a run that is not over is CodeRunOpen: end the run first. A
// source that is not put to the KG is not found.
func (s *Store) DetachSource(ctx context.Context, kg graph.KGID, source string) error {
	var kgNo, srcNo int64
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		var err error
		if kgNo, err = kgNoOf(ctx, tx, kg); err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, "SELECT "+pgschema.ColSrcNo+" FROM "+pgschema.TableSourceState+" WHERE "+pgschema.ColKGID+" = $1 AND "+pgschema.ColSourceID+" = $2",
			kg.String(), source).Scan(&srcNo)
		if isNoRows(err) {
			return kernel.Errorf(kernel.CodeNotFound, "the source %q is not put to the KG %q", source, kg)
		}
		if err != nil {
			return dbErr(err, "find the source")
		}
		return noOpenRun(ctx, tx, srcNo)
	})
	if err != nil {
		return err
	}
	for _, table := range []string{pgschema.TableRecordRef, pgschema.TableRecordWait, pgschema.TableRecordProblem} {
		if err := s.deleteInChunks(ctx, kgNo, srcNo, table); err != nil {
			return err
		}
	}
	return s.tx(ctx, roleApplier, func(tx *sql.Tx) error { return dropTx(ctx, tx, kgNo, srcNo) })
}

// deleteInChunks deletes the rows of a source in a table of the KG, chunkRows to a transaction.
func (s *Store) deleteInChunks(ctx context.Context, kgNo, srcNo int64, table string) error {
	t := pgschema.TableRef(kgNo, table)
	stmt := "DELETE FROM " + t + " WHERE ctid = ANY (ARRAY(SELECT ctid FROM " + t + " WHERE " + pgschema.ColSrcNo + " = $1 LIMIT $2))"
	for {
		var n int64
		err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx, stmt, srcNo, chunkRows)
			if err != nil {
				return dbErr(err, "delete the rows of the source in "+table)
			}
			n, _ = res.RowsAffected()
			return nil
		})
		if err != nil || n < chunkRows {
			return err
		}
	}
}

// dropTx drops the partitions of a source and its state, in a transaction of the caller. A source with a run that is not over
// is refused.
func dropTx(ctx context.Context, tx *sql.Tx, kgNo, srcNo int64) error {
	if err := noOpenRun(ctx, tx, srcNo); err != nil {
		return err
	}
	steps, err := pgschema.DropSourcePartitionSteps(kgNo, srcNo)
	if err != nil {
		return err
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.SQL); err != nil {
			return dbErr(err, "drop the "+st.Name)
		}
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM "+pgschema.TableSourceState+" WHERE "+pgschema.ColSrcNo+" = $1", srcNo)
	return dbErr(err, "delete the state of the source")
}

// noOpenRun refuses a source that has a run that is not over.
func noOpenRun(ctx context.Context, tx *sql.Tx, srcNo int64) error {
	var open bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM "+pgschema.TableSyncRun+" WHERE "+pgschema.ColSrcNo+" = $1 AND "+notOver()+")", srcNo).Scan(&open)
	switch {
	case err != nil:
		return dbErr(err, "look for a run of the source")
	case open:
		return kernel.Errorf(ingest.CodeRunOpen, "the source has a run that is not over: end it before the source is taken off")
	}
	return nil
}

// textArray is the text of a PostgreSQL array of text for a parameter that is cast ($n::text[]): every element in double
// quotes, with the backslash and the double quote escaped.
func textArray(items []string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		appendArrayText(&b, it)
	}
	b.WriteByte('}')
	return b.String()
}

func appendArrayText(b *strings.Builder, it string) {
	b.WriteByte('"')
	for j := 0; j < len(it); j++ {
		if c := it[j]; c == '\\' || c == '"' {
			b.WriteByte('\\')
		}
		b.WriteByte(it[j])
	}
	b.WriteByte('"')
}
