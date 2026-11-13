package graph

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const scanPage = 1000

// RecomputeHash computes the graph hash of a snapshot from its rows alone,
// recomputing every content hash. It is the check of the hash that the
// applier keeps by differences, and the way to a head after a restore.
func RecomputeHash(ctx context.Context, snap Snapshot, reg *model.Registry, keys KeyProvider, kg KGID) (HashState, error) {
	var state HashState
	hs := hasher{reg: reg, keys: keys, kg: kg}
	var after ObjectID
	for {
		rows, err := snap.ScanObjects(ctx, after, scanPage)
		if err != nil {
			return state, err
		}
		for _, r := range rows {
			if err := addRecomputed(&state, hs, rowRef{r.ID.String(), entityObject, r.TypeNo}, r.ContentHash, func() (kernel.Hash, error) { return hs.objectContent(r) }); err != nil {
				return state, err
			}
			after = r.ID
		}
		if len(rows) < scanPage {
			break
		}
	}
	var afterLink LinkID
	for {
		rows, err := snap.ScanLinks(ctx, afterLink, scanPage)
		if err != nil {
			return state, err
		}
		for _, r := range rows {
			if err := addRecomputed(&state, hs, rowRef{r.ID.String(), entityLink, r.TypeNo}, r.ContentHash, func() (kernel.Hash, error) { return hs.linkContent(r) }); err != nil {
				return state, err
			}
			afterLink = r.ID
		}
		if len(rows) < scanPage {
			break
		}
	}
	return state, nil
}

// rowRef names a row for the recomputation.
type rowRef struct {
	id, entity string
	typeNo     int
}

func addRecomputed(state *HashState, hs hasher, ref rowRef, stored kernel.Hash, content func() (kernel.Hash, error)) error {
	got, err := content()
	if err != nil {
		return err
	}
	if got != stored {
		return kernel.Errorf(CodeHashMismatch, "%s %s: stored content hash %s, recomputed %s", ref.entity, ref.id, stored, got)
	}
	typeID := ""
	if ref.entity == entityObject {
		if t, ok := hs.reg.ObjectTypeByNo(ref.typeNo); ok {
			typeID = t.ID.String()
		}
	} else if t, ok := hs.reg.LinkTypeByNo(ref.typeNo); ok {
		typeID = t.ID.String()
	}
	sum, err := rowSum(ref.entity, ref.id, got)
	if err != nil {
		return err
	}
	state.Add(PartKey{Entity: ref.entity, TypeID: typeID}, sum)
	return nil
}
