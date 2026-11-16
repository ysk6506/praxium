package graph

import (
	"reflect"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// pvtLive is the live objects and links of the store of a unit, by entity and id, with their content hashes.
func pvtLive(u *unit) map[string]kernel.Hash {
	out := map[string]kernel.Hash{}
	for id, r := range u.store.objects {
		if r.Open() {
			out["object|"+id.String()] = r.ContentHash
		}
	}
	for id, r := range u.store.links {
		if r.Open() {
			out["link|"+id.String()] = r.ContentHash
		}
	}
	return out
}

func pvtBatch(u *unit) Batch {
	fx := u.fx
	rack := fx.RackID(fx.SiteID("S00"), "R00")
	s0, s1 := fx.ServerID("SN-000000"), fx.ServerID("SN-000001")
	return Batch{
		Objects:       []ObjectUpsert{fx.Server("SN-000000", "M9"), fx.Server("SN-000001", "M1"), fx.Server("SN-NEW", "M1"), fx.Site("S00", "Moved Road")},
		Links:         []LinkUpsert{fx.MountedIn(s0, rack, 1), fx.MountedIn(fx.ServerID("SN-NEW"), rack, 8)},
		RetireObjects: []ObjectID{fx.ServerID("SN-000002"), fx.ServerID("SN-GHOST")},
		RetireLinks:   []LinkID{fx.MountedInID(fx.ServerID("SN-000002"), rack), fx.MountedInID(s1, rack)},
	}
}

// [REQ-410]
func TestPreviewListsWhatTheApplyChanges(t *testing.T) {
	u := newUnit(t, nil, Options{ChunkRows: 2})
	u.apply(u.fx.World(1, 1, 3))
	b := pvtBatch(u)
	before := pvtLive(u)
	p, err := u.a.Preview(u.ctx, u.kg.ID, b)
	if err != nil {
		t.Fatal(err)
	}
	u.apply(b)
	after := pvtLive(u)
	got := map[string]ChangeItem{}
	for _, c := range p.Changes {
		got[c.Entity+"|"+c.ID] = c
	}
	seen := 0
	for key, h := range after {
		old, was := before[key]
		if was && old == h {
			continue
		}
		seen++
		c, ok := got[key]
		if !ok || c.NewHash != h || (was && (c.Change != ChangeChanged || c.OldHash != old)) || (!was && c.Change != ChangeAdded) {
			t.Errorf("%s: the apply made %v (was %v), the preview says %+v", key, h, was, c)
		}
	}
	for key, old := range before {
		if _, still := after[key]; !still {
			seen++
			if c, ok := got[key]; !ok || c.Change != ChangeRetired || c.OldHash != old || c.NewHash != (kernel.Hash{}) {
				t.Errorf("%s: retired by the apply, the preview says %+v", key, c)
			}
		}
	}
	if seen != len(p.Changes) || p.Summary.Changed != seen || p.Summary.Candidates != 10 || p.Summary.Unchanged != 10-seen {
		t.Errorf("the apply changed %d things, the preview lists %d, summary %+v", seen, len(p.Changes), p.Summary)
	}
	if p.Summary.ByChange[ChangeAdded] != 2 || p.Summary.ByChange[ChangeRetired] != 3 || p.Summary.ByEntity["link"] != 3 || len(p.Summary.ByType) != 3 {
		t.Errorf("summary %+v", p.Summary)
	}
	again, err := u.a.Preview(u.ctx, u.kg.ID, b)
	if err != nil || len(again.Changes) != 0 {
		t.Errorf("after the apply: %d changes, %v", len(again.Changes), err)
	}
}

// [REQ-410]
func TestPreviewWritesNothingAndRefusesLikeTheApply(t *testing.T) {
	u := newUnit(t, nil, Options{ChunkRows: 2})
	u.apply(u.fx.World(1, 1, 3))
	writes, head := len(u.store.writes), u.store.head.Clone()
	if _, err := u.a.Preview(u.ctx, u.kg.ID, pvtBatch(u)); err != nil {
		t.Fatal(err)
	}
	if len(u.store.writes) != writes || u.store.head.AppliedSeq != head.AppliedSeq || !u.store.head.Hash.Equal(head.Hash) ||
		u.store.head.IngestSeq != head.IngestSeq || u.store.lease == nil || u.store.lease.Attempt != u.lease.Attempt {
		t.Errorf("a preview wrote: %d writes, head %+v", len(u.store.writes), u.store.head)
	}
	rack := u.fx.RackID(u.fx.SiteID("S00"), "R00")
	bad := []Batch{
		{Links: []LinkUpsert{u.fx.LocatedIn(u.fx.ServerID("SN-000000"), rack)}},
		{Objects: []ObjectUpsert{u.fx.Site("S00", "a"), u.fx.Site("S00", "b")}},
		{RetireObjects: []ObjectID{u.fx.SiteID("S00")}},
	}
	for i, b := range bad {
		_, perr := u.a.Preview(u.ctx, u.kg.ID, b)
		_, aerr := u.a.Apply(u.ctx, u.kg.ID, u.lease, b)
		if perr == nil || aerr == nil || kernel.CodeOf(perr) != kernel.CodeOf(aerr) || perr.Error() != aerr.Error() {
			t.Errorf("batch %d: preview says %v, apply says %v", i, perr, aerr)
		}
	}
}

// [REQ-410]
func TestPreviewWaitsForAHalfDoneApply(t *testing.T) {
	u := newUnit(t, nil, Options{ChunkRows: 2})
	u.apply(u.fx.World(1, 1, 1))
	run, err := u.a.Begin(u.ctx, u.kg.ID, u.lease, u.fx.World(2, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Next(u.ctx); err != nil || run.Done() {
		t.Fatalf("the first chunk: %v, done %v", err, run.Done())
	}
	_, err = u.a.Preview(u.ctx, u.kg.ID, u.fx.World(2, 2, 2))
	wantCode(t, "preview of a half done apply", err, CodeApplyInProgress)
}

// [REQ-410]
func TestPreviewFingerprintFollowsTheListOnly(t *testing.T) {
	u := newUnit(t, nil, Options{})
	u.apply(u.fx.World(1, 1, 3))
	base := pvtBatch(u)
	p, err := u.a.Preview(u.ctx, u.kg.ID, base)
	if err != nil {
		t.Fatal(err)
	}
	fp := func(b Batch) kernel.Hash {
		q, err := u.a.Preview(u.ctx, u.kg.ID, b)
		if err != nil {
			t.Fatal(err)
		}
		return q.Fingerprint
	}
	if fp(base) != p.Fingerprint {
		t.Error("the same batch gave two fingerprints")
	}
	stamped := base
	stamped.ApplyID, stamped.EventRef = "other", "other"
	stamped.Objects = append([]ObjectUpsert(nil), base.Objects...)
	for i := range stamped.Objects {
		attrs := map[int]Attr{}
		for no, a := range base.Objects[i].Attrs {
			a.SourceTime, a.SourceRef = "2031-01-01T00:00:00Z", "elsewhere"
			attrs[no] = a
		}
		stamped.Objects[i].Attrs, stamped.Objects[i].SourceRefs = attrs, []string{"elsewhere"}
	}
	if fp(stamped) != p.Fingerprint {
		t.Error("times and references changed the fingerprint")
	}
	changed := base
	changed.Objects = append([]ObjectUpsert(nil), base.Objects...)
	changed.Objects[0] = u.fx.Server("SN-000000", "M8")
	if fp(changed) == p.Fingerprint {
		t.Error("another value kept the fingerprint")
	}
	if want, err := ChangeFingerprint(p.Changes); err != nil || want != p.Fingerprint {
		t.Errorf("the fingerprint of the list is %v, the preview says %v (%v)", want, p.Fingerprint, err)
	}
	if empty, err := ChangeFingerprint(nil); err != nil || empty == p.Fingerprint || !reflect.DeepEqual(summarize(0, nil).ByType, map[string]int{}) {
		t.Errorf("an empty list: %v %v", empty, err)
	}
}
