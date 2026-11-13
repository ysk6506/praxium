package graph

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Restore takes a KG back to change number toSeq, as after a point-in-time
// restore of the database: later rows go, the generation goes up and readers
// holding the old generation get CodeGenerationChanged. The head is rebuilt
// from the state at toSeq: its hash is recomputed from the rows and the input
// chain is taken from the last ordering event up to that point.
func (a *Applier) Restore(ctx context.Context, kg KGID, toSeq int64) (KG, error) {
	head, err := a.store.Head(ctx, kg)
	if err != nil {
		return KG{}, err
	}
	if toSeq < 0 || toSeq > head.ReadableThrough {
		return KG{}, kernel.Errorf(CodeNotReadable, "cannot restore to %d: readable_through is %d", toSeq, head.ReadableThrough)
	}
	snap, err := a.store.Snapshot(ctx, ReadPoint{KG: kg, Kind: ReadSeq, Seq: toSeq})
	if err != nil {
		return KG{}, err
	}
	state, err := RecomputeHash(ctx, snap, a.reg, a.keys, kg)
	if err != nil {
		return KG{}, err
	}
	events, err := a.store.OrderEvents(ctx, kg, 0, 1<<30)
	if err != nil {
		return KG{}, err
	}
	nh := Head{KG: kg, AppliedSeq: toSeq, ReadableThrough: toSeq, Hash: state, AppliedSemanticVersion: head.AppliedSemanticVersion}
	for _, e := range events {
		if e.ChangeSeq <= toSeq {
			nh.IngestSeq, nh.InputDigest = e.IngestSeq, e.Digest
		}
	}
	return a.store.Restore(ctx, kg, toSeq, nh, a.clock.Now())
}
