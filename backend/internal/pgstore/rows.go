package pgstore

import (
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The scans of the rows that every part of the storage reads the same way.

func scanIdentifier(sc scanner) (graph.IdentifierRecord, error) {
	var (
		r      graph.IdentifierRecord
		object string
		to     *int64
	)
	if err := sc.Scan(&object, &r.Scheme, &r.Value, &r.Scope, &r.SeqFrom, &to); err != nil {
		return r, err
	}
	var err error
	if r.Object, err = parseObject(object); err != nil {
		return r, err
	}
	if to != nil {
		r.SeqTo = *to
	}
	return r, nil
}

func scanEvent(sc scanner) (graph.IdentityEvent, error) {
	var (
		e                   graph.IdentityEvent
		kind, retired, into string
		links               []byte
	)
	if err := sc.Scan(&e.Seq, &kind, &retired, &into, &links); err != nil {
		return e, err
	}
	var err error
	e.Kind = graph.IdentityEventKind(kind)
	if e.Retired, err = parseObject(retired); err != nil {
		return e, err
	}
	if e.Into, err = parseObject(into); err != nil {
		return e, err
	}
	e.LinkMap, err = linkMapOf(links)
	return e, err
}

func scanBatch(sc scanner) (graph.ChangeBatch, error) {
	var b graph.ChangeBatch
	var kind string
	err := sc.Scan(&b.Seq, &kind, &b.SourceID, &b.ApplyID, &b.ChunkNo, &b.ChunkCount, &b.IngestSeq, &b.Attempt, &b.AppliedAt, &b.Objects, &b.Links)
	b.Kind, b.AppliedAt = graph.BatchKind(kind), b.AppliedAt.UTC()
	return b, err
}

func scanEntry(sc scanner) (graph.ChangeEntry, error) {
	var e graph.ChangeEntry
	var change string
	var oldHash, newHash []byte
	if err := sc.Scan(&e.Seq, &e.Entity, &e.ID, &change, &oldHash, &newHash); err != nil {
		return e, err
	}
	var err error
	e.Change = graph.ChangeKind(change)
	if e.OldHash, err = hashOf(oldHash); err != nil {
		return e, err
	}
	e.NewHash, err = hashOf(newHash)
	return e, err
}

// afterText is the id to scan after; an empty text is below every id.
func afterText[K any](id kernel.ID[K]) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}

const versionColumns = `version_no, version_id, sealed_seq, generation, prev_version_id, definition_set_hash, engine_version,
	hash_rule_version, input_digest, graph_hash, row_count, sealed_at, seal_kind`

func scanVersion(kg graph.KGID) func(scanner) (graph.Version, error) {
	return func(sc scanner) (graph.Version, error) {
		v := graph.Version{KG: kg}
		var id, prev, kind string
		var definitions, digest, sum []byte
		if err := sc.Scan(&v.No, &id, &v.SealedSeq, &v.Generation, &prev, &definitions, &v.EngineVersion, &v.HashRuleVersion, &digest, &sum, &v.Rows, &v.SealedAt, &kind); err != nil {
			return v, err
		}
		var err error
		if v.ID, err = parseID[graph.VersionKind](id); err != nil {
			return v, err
		}
		if prev != "" {
			if v.Prev, err = parseID[graph.VersionKind](prev); err != nil {
				return v, err
			}
		}
		if v.DefinitionSetHash, err = hashOf(definitions); err != nil {
			return v, err
		}
		if v.InputDigest, err = hashOf(digest); err != nil {
			return v, err
		}
		if v.GraphHash, err = sumFrom(sum); err != nil {
			return v, err
		}
		v.SealedAt, v.Kind = v.SealedAt.UTC(), graph.SealKind(kind)
		v.Parts = map[graph.PartKey]graph.Part{}
		return v, nil
	}
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
