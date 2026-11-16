package graph

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ChangeItem is one thing a batch would change: an object or a link that would be
// created, modified or retired. OldHash is zero for a creation and NewHash is zero
// for a retirement.
type ChangeItem struct {
	// Entity is "object" or "link".
	Entity string
	ID     string
	// TypeID is the id of the object type or link type.
	TypeID  string
	Change  ChangeKind
	OldHash kernel.Hash
	NewHash kernel.Hash
}

// PreviewSummary counts what a preview found. It always counts everything, whatever
// the caller does with the list.
type PreviewSummary struct {
	// Candidates is the number of objects and links the batch names (upserts and
	// retirements); Changed of them would change and Unchanged would stay as they are
	// (this includes the retirement of something that is not live).
	Candidates int
	Changed    int
	Unchanged  int
	// ByEntity, ByType (the type id; object and link type ids do not overlap) and
	// ByChange count the changed items.
	ByEntity map[string]int
	ByType   map[string]int
	ByChange map[ChangeKind]int
}

// ChangePreview is what applying a batch would change, computed without writing.
// Changes are in the order the applier works in: objects before links, by id.
type ChangePreview struct {
	// BaseSeq is the change number of the state the batch was compared with.
	BaseSeq int64
	Changes []ChangeItem
	Summary PreviewSummary
	// Fingerprint identifies the list: see ChangeFingerprint.
	Fingerprint kernel.Hash
}

// Preview says what applying b to the KG would change, without applying it. It takes
// no lease and writes nothing: the head, the lease, the change numbers and the
// versions stay as they are. It makes the checks Apply makes, so a batch Apply would
// refuse is refused with the same error; and it refuses (CodeApplyInProgress) while an
// apply is half done, since the state is not settled then.
//
// The list is not limited: the batch is already in memory, so the list is as large as
// the batch at most, and the fingerprint needs all of it. A caller that shows the
// list may cut it; the summary counts everything. Volatile values, identifiers and
// identity events are not in the list (they are not versioned rows).
func (a *Applier) Preview(ctx context.Context, kg KGID, b Batch) (ChangePreview, error) {
	kgRow, err := a.store.GetKG(ctx, kg.String())
	if err != nil {
		return ChangePreview{}, err
	}
	p, err := buildPlan(a.v, b)
	if err != nil {
		return ChangePreview{}, err
	}
	head, err := a.store.Head(ctx, kgRow.ID)
	if err != nil {
		return ChangePreview{}, err
	}
	if head.Open() {
		return ChangePreview{}, kernel.Errorf(CodeApplyInProgress, "apply %s is half done: finish or abandon it before a preview", head.ApplyID)
	}
	snap, err := a.checkFresh(ctx, kgRow.ID, p)
	if err != nil {
		return ChangePreview{}, err
	}
	cb := newChunkBuilder(a, kgRow.ID, snap, head.Hash)
	if err := cb.build(ctx, p, p.items, false); err != nil {
		return ChangePreview{}, err
	}
	changes, err := a.changeItems(cb)
	if err != nil {
		return ChangePreview{}, err
	}
	out := ChangePreview{BaseSeq: snap.Info().Seq, Changes: changes, Summary: summarize(len(p.items), changes)}
	out.Fingerprint, err = ChangeFingerprint(changes)
	return out, err
}

// changeItems turns what the chunk builder found into the list. The builder keeps the
// entries, the object changes and the link changes each in the order of the items, so
// they are walked together to give every entry its type.
func (a *Applier) changeItems(cb *chunkBuilder) ([]ChangeItem, error) {
	items := make([]ChangeItem, 0, len(cb.entries))
	oi, li := 0, 0
	for _, e := range cb.entries {
		it := ChangeItem{Entity: e.Entity, ID: e.ID, Change: e.Change, OldHash: e.OldHash, NewHash: e.NewHash}
		if e.Entity == entityObject {
			it.TypeID = a.objTypeIDs[typeNoOf(cb.objects[oi].Old, cb.objects[oi].New)]
			oi++
		} else {
			it.TypeID = a.linkTypeIDs[linkTypeNoOf(cb.links[li].Old, cb.links[li].New)]
			li++
		}
		items = append(items, it)
	}
	if oi != len(cb.objects) || li != len(cb.links) {
		return nil, kernel.Errorf(kernel.CodeInternal, "the preview found %d entries for %d object and %d link changes", len(cb.entries), len(cb.objects), len(cb.links))
	}
	return items, nil
}

func typeNoOf(old, cur *ObjectRow) int {
	if cur != nil {
		return cur.TypeNo
	}
	return old.TypeNo
}

func linkTypeNoOf(old, cur *LinkRow) int {
	if cur != nil {
		return cur.TypeNo
	}
	return old.TypeNo
}

func summarize(candidates int, changes []ChangeItem) PreviewSummary {
	s := PreviewSummary{Candidates: candidates, Changed: len(changes), Unchanged: candidates - len(changes),
		ByEntity: map[string]int{}, ByType: map[string]int{}, ByChange: map[ChangeKind]int{}}
	for _, c := range changes {
		s.ByEntity[c.Entity]++
		s.ByType[c.TypeID]++
		s.ByChange[c.Change]++
	}
	return s
}

// ChangeFingerprint is the hash of a list of changes, made of (entity, id, change, old
// hash, new hash) of each item in the order given and nothing else. The type is left
// out (the id and the hashes already say it) and so is everything that differs from
// one run to the next: times, source references and the apply id are not part of a
// content hash. The same batch against the same state always gives the same
// fingerprint, so a later step can tell whether the change a person was shown is the
// change about to be applied.
func ChangeFingerprint(changes []ChangeItem) (kernel.Hash, error) {
	rows := make([]any, 0, len(changes))
	for _, c := range changes {
		rows = append(rows, []any{c.Entity, c.ID, string(c.Change), c.OldHash.String(), c.NewHash.String()})
	}
	return kernel.ContentHash([]any{"praxium.preview.v1", rows})
}
