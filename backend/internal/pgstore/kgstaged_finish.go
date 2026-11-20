package pgstore

import (
	"crypto/sha256"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// digestAndBatch makes the digest of the chunk (the statement of pgschema, over the lines of the chunk in their stored
// order) and writes the record of the chunk. The digest is read after the rows are written, and the lines it reads are
// those of the difference, which no statement of the chunk changes.
func (c *stagedChunk) digestAndBatch() {
	if c.err != nil {
		return
	}
	var d []byte
	if err := c.tx.QueryRowContext(c.ctx, pgschema.ChunkDigestSQL(c.a.rec.KGNo), c.from.NextChunk).Scan(&d); err != nil {
		c.fail(dbErr(err, "make the digest of the chunk"))
		return
	}
	if len(d) != sha256.Size {
		c.fail(kernel.Errorf(kernel.CodeInternal, "the digest of chunk %d is %d bytes", c.from.NextChunk, len(d)))
		return
	}
	copy(c.digest[:], d)
	kind := c.ap.Kind
	if kind == "" {
		kind = graph.BatchSync
	}
	c.exec("write the change batch", c.a.sub(kgBatchSQL, pgschema.TableChangeBatch), c.seq, string(kind), c.ap.SourceID, c.ap.ApplyID, c.from.NextChunk,
		c.from.ChunkCount, c.cur.head.IngestSeq+1, c.ap.Lease.Attempt, pgTime(c.ap.Now), c.total(pgschema.DiffKindObject), c.total(pgschema.DiffKindLink))
}

// finish moves the head. A chunk that is not the last leaves the apply open at the next chunk, with the hash state
// from before the apply kept for an abandon. The last chunk closes it: the ordering event is written, readable_through
// moves, the input digest takes the chain of the chunks, and a KG that was building is serving.
func (c *stagedChunk) finish() (graph.StagedProgress, graph.Head, error) {
	last := c.from.Last()
	chain := kernel.Hash(pgschema.ChainDigest(c.from.Chain, c.digest))
	nh := c.cur.head.Clone()
	nh.Hash, nh.AppliedSeq = c.hash, c.seq
	out := graph.StagedProgress{State: graph.StagedOpen, NextChunk: c.from.NextChunk + 1, ChunkCount: c.from.ChunkCount, AppliedSeq: c.seq, Chain: chain}
	if !last {
		if !c.cur.head.Open() {
			start := c.cur.head.Hash.Clone()
			nh.Rollback = &start
		}
		nh.ApplyID, nh.NextChunkNo, nh.ChunkCount = c.ap.ApplyID, c.from.NextChunk+1, c.from.ChunkCount
	} else {
		if err := c.close(&nh, chain); err != nil {
			return graph.StagedProgress{}, graph.Head{}, err
		}
		out.State = graph.StagedDone
	}
	if err := saveKGHead(c.ctx, c.tx, c.a, nh); err != nil {
		return graph.StagedProgress{}, graph.Head{}, err
	}
	if last && c.cur.state == graph.StateBuilding {
		c.exec("serve the KG", c.a.sub(`UPDATE @ SET state = 'serving' WHERE id = 1`, pgschema.TableHead))
	}
	return out, nh, c.err
}

// close is the end of the apply: the ordering event with the chain of the input, and the head that readers may see.
func (c *stagedChunk) close(nh *graph.Head, chain kernel.Hash) error {
	event := c.ap.Event
	if event == "" {
		event = graph.OrderSourceChange
	}
	if !event.Valid() {
		return kernel.Errorf(kernel.CodeInvalidValue, "%q is not an ordering event", event)
	}
	ingest := c.cur.head.IngestSeq + 1
	input, err := kernel.ContentHash([]any{"praxium.input.v1", c.cur.head.InputDigest.String(), ingest, string(event), c.ap.EventRef, chain.String()})
	if err != nil {
		return err
	}
	c.exec("write the ordering event", c.a.sub(`INSERT INTO @ (ingest_seq, event_kind, ref, digest, change_seq) VALUES ($1, $2, $3, $4, $5)`, pgschema.TableIngestOrder),
		ingest, string(event), c.ap.EventRef, hashBytes(input), c.seq)
	nh.ReadableThrough, nh.IngestSeq, nh.InputDigest = nh.AppliedSeq, ingest, input
	nh.ApplyID, nh.NextChunkNo, nh.ChunkCount, nh.Rollback, nh.LastApplyID = "", 0, 0, nil, c.ap.ApplyID
	if c.ap.Lease.Semantic > nh.AppliedSemanticVersion {
		nh.AppliedSemanticVersion = c.ap.Lease.Semantic
	}
	return c.err
}
