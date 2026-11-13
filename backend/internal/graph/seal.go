package graph

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// SealResult says what a seal did. Created is false when the graph hash had
// not changed since the last version: no new version is made and Version is
// the last one (empty when there is none yet).
type SealResult struct {
	Version Version
	Created bool
}

// Seal seals the KG as a version. It needs applied_seq equal to
// readable_through (no chunked apply open). The graph hash that the head has
// kept is the hash of the version; with VerifyOnSeal it is checked against a
// full recomputation first.
func (a *Applier) Seal(ctx context.Context, kg KGID, lease Lease, kind SealKind) (SealResult, error) {
	if !kind.Valid() {
		return SealResult{}, invalid("unknown seal kind %q", kind)
	}
	kgRow, err := a.store.GetKG(ctx, kg.String())
	if err != nil {
		return SealResult{}, err
	}
	head, err := a.store.Head(ctx, kg)
	if err != nil {
		return SealResult{}, err
	}
	if head.Open() || head.AppliedSeq != head.ReadableThrough {
		return SealResult{}, kernel.Errorf(CodeSealNotReady, "applied_seq %d, readable_through %d", head.AppliedSeq, head.ReadableThrough)
	}
	versions, err := a.store.Versions(ctx, kg)
	if err != nil {
		return SealResult{}, err
	}
	var last Version
	if len(versions) > 0 {
		last = versions[len(versions)-1]
	}
	if head.Hash.Sum == last.GraphHash && head.Hash.Rows == last.Rows {
		return SealResult{Version: last}, nil
	}
	if a.opts.VerifyOnSeal {
		if err := a.Verify(ctx, kg); err != nil {
			return SealResult{}, err
		}
	}
	return a.writeSeal(ctx, kgRow, lease, head, last, kind)
}

func (a *Applier) writeSeal(ctx context.Context, kg KG, lease Lease, head Head, last Version, kind SealKind) (SealResult, error) {
	engine := head.AppliedSemanticVersion
	if lease.Semantic > engine {
		engine = lease.Semantic
	}
	id, err := NewVersionID(last.ID, kg.DefinitionSetHash, head.InputDigest, engine)
	if err != nil {
		return SealResult{}, err
	}
	now := a.clock.Now()
	v := Version{KG: kg.ID, No: last.No + 1, ID: id, SealedSeq: head.ReadableThrough, Generation: kg.Generation, Prev: last.ID,
		DefinitionSetHash: kg.DefinitionSetHash, EngineVersion: engine, HashRuleVersion: HashRuleVersion, InputDigest: head.InputDigest,
		GraphHash: head.Hash.Sum, Rows: head.Hash.Rows, Parts: head.Hash.Clone().Parts, SealedAt: now, Kind: kind}
	ingest := head.IngestSeq + 1
	nh := head.Clone()
	nh.IngestSeq, nh.InputDigest = ingest, kernel.Hash{}
	if engine > nh.AppliedSemanticVersion {
		nh.AppliedSemanticVersion = engine
	}
	order := OrderEvent{IngestSeq: ingest, Kind: OrderSeal, Ref: id.String(), ChangeSeq: head.AppliedSeq}
	err = a.store.Seal(ctx, SealWrite{KG: kg.ID, Generation: kg.Generation, ExpectedSeq: head.AppliedSeq, Lease: lease, Now: now,
		Version: v, Order: order, Head: nh})
	if err != nil {
		return SealResult{}, err
	}
	return SealResult{Version: v, Created: true}, nil
}

// Verify recomputes the graph hash of the applied state from the rows and
// compares it with the hash the head kept (kg-storage.md 7.4). A row whose
// stored content hash is not what its content gives is a mismatch too.
func (a *Applier) Verify(ctx context.Context, kg KGID) error {
	head, err := a.store.Head(ctx, kg)
	if err != nil {
		return err
	}
	snap, err := a.store.Snapshot(ctx, ReadPoint{KG: kg, Kind: ReadApplied})
	if err != nil {
		return err
	}
	got, err := RecomputeHash(ctx, snap, a.reg, a.keys, kg)
	if err != nil {
		return err
	}
	if !got.Equal(head.Hash) {
		return kernel.Errorf(CodeHashMismatch, "kept %s over %d rows, recomputed %s over %d rows", head.Hash.Sum, head.Hash.Rows, got.Sum, got.Rows)
	}
	return nil
}
