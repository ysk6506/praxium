package graph

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-246]
func TestChunkingCutsTheSameWorkAtAnyLimit(t *testing.T) {
	var want HashState
	for i, limit := range []int{1, 5, 7, 1000} {
		u := newUnit(t, nil, Options{ChunkRows: limit})
		res := u.apply(u.fx.World(2, 2, 3))
		wantChunks := (34 + limit - 1) / limit
		if res.Chunks != wantChunks || len(u.store.writes) != wantChunks {
			t.Errorf("limit %d: %d chunks written, want %d", limit, res.Chunks, wantChunks)
		}
		h := u.store.head
		if h.AppliedSeq != int64(wantChunks) || h.ReadableThrough != h.AppliedSeq || h.Open() || u.store.kg.State != StateServing {
			t.Errorf("limit %d: head %+v", limit, h)
		}
		for k, w := range u.store.writes {
			if (w.Order != nil) != (k == len(u.store.writes)-1) || (w.Head.ReadableThrough == w.Head.AppliedSeq) != (k == len(u.store.writes)-1) {
				t.Errorf("limit %d: chunk %d: the order event and readable_through belong to the last chunk only", limit, k)
			}
		}
		if err := u.a.Verify(u.ctx, u.kg.ID); err != nil {
			t.Errorf("limit %d: %v", limit, err)
		}
		if i == 0 {
			want = h.Hash
		} else if !h.Hash.Equal(want) {
			t.Errorf("limit %d: the graph hash differs from limit 1", limit)
		}
	}
}

// [REQ-233] [REQ-246]
func TestSameBatchesGiveTheSameIdsNumbersHashesAndVersions(t *testing.T) {
	run := func(limit int, seed uint64) (*unit, []VersionID) {
		u := newUnit(t, nil, Options{ChunkRows: limit})
		var versions []VersionID
		for i, b := range []Batch{u.fx.World(1, 2, 2), u.fx.World(2, 2, 2)} {
			u.apply(shuffled(b, seed+uint64(i)))
			versions = append(versions, u.seal().Version.ID)
		}
		u.apply(Batch{Volatile: []VolatileWrite{u.fx.Power(u.fx.ServerID("SN-000000"), "off", "2030-01-01T01:00:00Z")}})
		u.apply(Batch{RetireObjects: nil, Objects: []ObjectUpsert{u.fx.Server("SN-000000", "M9")}})
		return u, versions
	}
	one, v1 := run(4, 1)
	two, v2 := run(4, 99)
	three, v3 := run(1000, 5)
	if v1[0] == v1[1] || v1[0].IsZero() {
		t.Fatalf("versions %v", v1)
	}
	for _, other := range []struct {
		u *unit
		v []VersionID
	}{{two, v2}, {three, v3}} {
		if fmt.Sprint(other.v) != fmt.Sprint(v1) {
			t.Errorf("version ids differ: %v and %v", other.v, v1)
		}
		if !other.u.store.head.Hash.Equal(one.store.head.Hash) || other.u.store.head.InputDigest != one.store.head.InputDigest {
			t.Errorf("hashes differ")
		}
		if len(other.u.store.objects) != len(one.store.objects) {
			t.Errorf("row counts differ")
		}
		for id, row := range one.store.objects {
			got, ok := other.u.store.objects[id]
			if !ok || got.ContentHash != row.ContentHash || got.TypeNo != row.TypeNo {
				t.Fatalf("object %s differs", id)
			}
		}
	}
	// The same limit gives the same change numbers too.
	if two.store.head.AppliedSeq != one.store.head.AppliedSeq {
		t.Errorf("change numbers differ: %d and %d", two.store.head.AppliedSeq, one.store.head.AppliedSeq)
	}
	// Rows point at types by number; hashes by immutable id.
	for _, row := range one.store.objects {
		typ, ok := one.fx.Reg.ObjectTypeByNo(row.TypeNo)
		if !ok || typ.TypeNo != row.TypeNo {
			t.Fatalf("a row does not hold a type number")
		}
		for no := range row.Attrs {
			if _, ok := one.fx.Reg.PropertyByNo(no); !ok {
				t.Fatalf("a row attribute is not keyed by attribute number: %d", no)
			}
		}
	}
}

// [REQ-233] [REQ-246]
func TestRenamingAndRenumberingTypesLeavesHashesAsTheyWere(t *testing.T) {
	fx := mustFixture(t)
	defs := fx.Reg.Definitions()
	renamed := defs.Clone()
	for i := range renamed.ObjectTypes {
		renamed.ObjectTypes[i].Name += "Renamed"
		for j := range renamed.ObjectTypes[i].Properties {
			renamed.ObjectTypes[i].Properties[j].Name += "_renamed"
		}
	}
	for i := range renamed.LinkTypes {
		renamed.LinkTypes[i].Name += "_RENAMED"
	}
	renumbered := defs.Clone()
	for i := range renumbered.ObjectTypes {
		renumbered.ObjectTypes[i].TypeNo += 1000
	}
	for i := range renumbered.LinkTypes {
		renumbered.LinkTypes[i].LinkTypeNo += 1000
	}
	reg0, reg1, reg2 := fx.Reg, mustRegistry(t, renamed), mustRegistry(t, renumbered)
	if reg1.Hash() != reg0.Hash() {
		t.Fatalf("renaming changed the hash of the model: the test cannot tell what it means to")
	}
	base := newUnit(t, reg0, Options{})
	base.apply(fx.World(2, 2, 2))
	for name, reg := range map[string]*model.Registry{"renamed": reg1, "renumbered": reg2} {
		u := newUnit(t, reg, Options{})
		u.apply(fx.World(2, 2, 2))
		if !u.store.head.Hash.Equal(base.store.head.Hash) {
			t.Errorf("%s: the graph hash changed", name)
		}
		for id, row := range base.store.objects {
			if got := u.store.objects[id]; got.ContentHash != row.ContentHash {
				t.Fatalf("%s: object %s has another content hash", name, id)
			}
		}
		for id, row := range base.store.links {
			if got := u.store.links[id]; got.ContentHash != row.ContentHash {
				t.Fatalf("%s: link %s has another content hash", name, id)
			}
		}
		if name == "renumbered" {
			any := false
			for id, row := range u.store.objects {
				any = any || row.TypeNo != base.store.objects[id].TypeNo
			}
			if !any {
				t.Errorf("the rows did not follow the new numbers")
			}
		}
		if err := u.a.Verify(u.ctx, u.kg.ID); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func mustRegistry(t *testing.T, defs model.Definitions) *model.Registry {
	t.Helper()
	reg, err := model.NewRegistry(defs)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return reg
}

// [REQ-247]
func TestVolatileChangesLeaveTheHashAloneAndMakeLightLines(t *testing.T) {
	u := newUnit(t, nil, Options{})
	u.apply(u.fx.World(1, 1, 2))
	before := u.store.head
	server := u.fx.ServerID("SN-000000")
	res := u.apply(Batch{Volatile: []VolatileWrite{u.fx.Power(server, "off", "2030-01-01T02:00:00Z")}})
	last := u.store.writes[len(u.store.writes)-1]
	if !res.Changed() || len(last.Light) != 1 || last.Light[0].Seq != res.ToSeq || len(last.Objects)+len(last.Links)+len(last.Entries) != 0 {
		t.Fatalf("a volatile change is one light line and no row: %+v", last)
	}
	if !u.store.head.Hash.Equal(before.Hash) {
		t.Errorf("a volatile change moved the graph hash")
	}
	if got := u.store.volatile[volKey{server, AttrServerPower}]; got.Value != "off" || got.SeenAt != "2030-01-01T02:00:00Z" {
		t.Errorf("volatile row %+v", got)
	}
	if res := u.apply(Batch{Volatile: []VolatileWrite{u.fx.Power(server, "off", "2030-01-01T03:00:00Z")}}); res.Changed() {
		t.Errorf("the same value made a change")
	}
	grade := u.fx.Power(server, "off", "")
	grade.Grade = model.GradeAssumed
	if res := u.apply(Batch{Volatile: []VolatileWrite{grade}}); !res.Changed() {
		t.Errorf("a changed grade is a change of the value")
	}
}

func TestSealRulesOnTheLiveStore(t *testing.T) {
	u := newUnit(t, nil, Options{Semantic: 3})
	if u.seal().Created {
		t.Errorf("an empty KG made a version")
	}
	u.apply(u.fx.World(1, 1, 1))
	first := u.seal()
	if !first.Created || first.Version.EngineVersion != 3 || first.Version.HashRuleVersion != HashRuleVersion {
		t.Fatalf("first seal %+v", first)
	}
	if u.seal().Created {
		t.Errorf("a second seal without a change made a version")
	}
	u.apply(u.fx.World(1, 1, 1))
	if u.seal().Created {
		t.Errorf("an apply with the same content made a version")
	}
	u.apply(Batch{Objects: []ObjectUpsert{u.fx.Site("S00", "Else")}})
	second := u.seal()
	if !second.Created || second.Version.Prev != first.Version.ID || second.Version.InputDigest == first.Version.InputDigest {
		t.Errorf("second seal %+v", second)
	}
}

func TestSealRefusesWhileAnApplyIsOpen(t *testing.T) {
	u := newUnit(t, nil, Options{ChunkRows: 3})
	run, err := u.a.Begin(u.ctx, u.kg.ID, u.lease, u.fx.World(1, 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Next(u.ctx); err != nil {
		t.Fatal(err)
	}
	_, err = u.a.Seal(u.ctx, u.kg.ID, u.lease, SealSync)
	wantCode(t, "a seal in the middle of an apply", err, CodeSealNotReady)
	_, err = u.a.Begin(u.ctx, u.kg.ID, u.lease, u.fx.World(1, 1, 1))
	wantCode(t, "another batch in the middle of an apply", err, CodeApplyInProgress)
	if _, err := u.a.Seal(u.ctx, u.kg.ID, u.lease, "weekly"); err == nil {
		t.Errorf("an unknown seal kind was accepted")
	}
	// The live store keeps no history, so it cannot abandon: the applier passes its refusal on.
	wantCode(t, "abandon on a store that cannot", u.a.Abandon(u.ctx, u.kg.ID, u.lease), kernel.CodeUnsupported)
	for !run.Done() {
		if err := run.Next(u.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := run.Next(u.ctx); err != nil {
		t.Errorf("Next after the end must do nothing: %v", err)
	}
	if err := u.a.Abandon(u.ctx, u.kg.ID, u.lease); err != nil {
		t.Errorf("abandoning when nothing is open is not an error: %v", err)
	}
}

func TestVerifyFindsAHashThatDoesNotMatchTheRows(t *testing.T) {
	u := newUnit(t, nil, Options{VerifyOnSeal: true})
	u.apply(u.fx.World(1, 1, 1))
	if err := u.a.Verify(u.ctx, u.kg.ID); err != nil {
		t.Fatal(err)
	}
	site := u.fx.SiteID("S00")
	row := u.store.objects[site]
	row.Attrs[AttrSiteAddress] = fact("tampered")
	u.store.objects[site] = row
	wantCode(t, "a row that does not give its hash", u.a.Verify(u.ctx, u.kg.ID), CodeHashMismatch)
	row.Attrs[AttrSiteAddress] = fact("Fixture Road S00")
	u.store.objects[site] = row
	u.store.head.Hash.Sum = u.store.head.Hash.Sum.Add(Sum{31: 1})
	wantCode(t, "a head that drifted", u.a.Verify(u.ctx, u.kg.ID), CodeHashMismatch)
	u.store.head.Hash.Sum = u.store.head.Hash.Sum.Sub(Sum{31: 1})
	if _, err := u.a.Seal(u.ctx, u.kg.ID, u.lease, SealSync); err != nil {
		t.Errorf("a sound KG could not be sealed: %v", err)
	}
	u.store.head.Hash.Rows++
	u.store.head.Hash.Sum = u.store.head.Hash.Sum.Add(Sum{31: 1})
	_, err := u.a.Seal(u.ctx, u.kg.ID, u.lease, SealSync)
	wantCode(t, "a seal of a drifted head", err, CodeHashMismatch)
}

func TestOrderEventsAndTheInputChain(t *testing.T) {
	u := newUnit(t, nil, Options{})
	kinds := []OrderKind{OrderSourceChange, OrderClock, OrderHeldRelease, OrderHeldDiscard, OrderGuardConfirm, OrderRawExclude, OrderDefinitionSwitch}
	var chain []string
	for _, k := range kinds {
		u.apply(Batch{Event: k, EventRef: "ref-" + string(k), Objects: []ObjectUpsert{u.fx.Site("S"+string(k), "x")}})
		chain = append(chain, u.store.head.InputDigest.String())
	}
	seen := map[string]bool{}
	for _, c := range chain {
		if seen[c] {
			t.Errorf("two events left the same input digest")
		}
		seen[c] = true
	}
	events, _ := u.store.OrderEvents(u.ctx, u.kg.ID, 0, 100)
	for i, e := range events {
		if e.IngestSeq != int64(i+1) || e.Kind != kinds[i] || e.Ref != "ref-"+string(kinds[i]) || e.ChangeSeq != int64(i+1) {
			t.Errorf("event %d: %+v", i, e)
		}
	}
	// The same events in another order give another chain: the order is an input.
	a, b := newUnit(t, nil, Options{}), newUnit(t, nil, Options{})
	a.apply(Batch{Event: OrderClock, Objects: []ObjectUpsert{a.fx.Site("A", "x")}})
	a.apply(Batch{Event: OrderRawExclude, Objects: []ObjectUpsert{a.fx.Site("B", "x")}})
	b.apply(Batch{Event: OrderRawExclude, Objects: []ObjectUpsert{b.fx.Site("B", "x")}})
	b.apply(Batch{Event: OrderClock, Objects: []ObjectUpsert{b.fx.Site("A", "x")}})
	if a.store.head.InputDigest == b.store.head.InputDigest || !a.store.head.Hash.Equal(b.store.head.Hash) {
		t.Errorf("order must change the input digest and leave the graph hash alone")
	}
}

func TestApplierConstructionAndRefusals(t *testing.T) {
	fx := mustFixture(t)
	clock := kernel.NewManualClock(epoch)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(1))
	if _, err := NewApplier(nil, fx.Reg, NewTestKeyProvider("k"), clock, ids, Options{}); err == nil {
		t.Errorf("an applier without a store was built")
	}
	a, err := NewApplier(newLiveStore(), fx.Reg, NewTestKeyProvider("k"), clock, ids, Options{})
	if err != nil || a.opts.ChunkRows != DefaultChunkRows || a.opts.LeaseTTL != DefaultLeaseTTL {
		t.Errorf("defaults %+v (%v)", a.opts, err)
	}
	u := newUnit(t, nil, Options{})
	u.apply(u.fx.World(1, 1, 1))
	b := u.fx.World(1, 1, 1)
	b.ApplyID = "once"
	u.apply(b)
	if res := u.apply(b); !res.Duplicate {
		t.Errorf("a finished apply was applied again")
	}
	u.store.head.ApplyID, u.store.head.ChunkCount = "other", 9
	_, err = u.a.Begin(u.ctx, u.kg.ID, u.lease, u.fx.World(1, 1, 1))
	wantCode(t, "a batch while another apply is open", err, CodeApplyInProgress)
	b.ApplyID = "other"
	_, err = u.a.Begin(u.ctx, u.kg.ID, u.lease, b)
	wantCode(t, "a resume that cuts differently", err, CodeBatchInvalid)
	if _, err := u.a.Restore(u.ctx, u.kg.ID, 99); err == nil {
		t.Errorf("a restore past readable_through was accepted")
	}
}

func TestCodesAreListedAndExplained(t *testing.T) {
	codes := Codes()
	if len(codes) != 18 {
		t.Errorf("%d codes: the list is part of the contract and a change needs a decision", len(codes))
	}
	for i, c := range codes {
		if m, ok := CodeMeaning(c); !ok || m == "" {
			t.Errorf("code %s has no meaning", c)
		}
		if i > 0 && codes[i-1] >= c {
			t.Errorf("codes are not sorted at %s", c)
		}
	}
	if _, ok := CodeMeaning("nothing"); ok {
		t.Errorf("an unknown code has a meaning")
	}
}

func TestClosedListsOfRolesAndKinds(t *testing.T) {
	if !(RoleProduction.Valid() && RoleCandidate.Valid() && RoleTest.Valid() && RoleSystem.Valid()) || Role("x").Valid() {
		t.Errorf("roles are production, candidate, test and system")
	}
	if !(BatchSync.Valid() && BatchStream.Valid() && BatchMerge.Valid()) || BatchKind("x").Valid() {
		t.Errorf("batch kinds are sync, stream and merge")
	}
	if !(SealSync.Valid() && SealMerge.Valid() && SealPeriodic.Valid()) || SealKind("x").Valid() {
		t.Errorf("seal kinds are sync, merge and periodic")
	}
}

func TestRetirementsChangesAndIdentifiersOnTheLiveStore(t *testing.T) {
	u := newUnit(t, nil, Options{ChunkRows: 4})
	u.apply(u.fx.World(1, 1, 2))
	one, two, rack := u.fx.ServerID("SN-000000"), u.fx.ServerID("SN-000001"), u.fx.RackID(u.fx.SiteID("S00"), "R00")
	twoLink, err := NewLinkID(u.fx.linkID("MOUNTED_IN"), two, rack, "")
	if err != nil {
		t.Fatal(err)
	}
	moved := u.fx.MountedIn(one, rack, 9)
	u.apply(Batch{RetireObjects: []ObjectID{two, u.fx.ServerID("SN-404")}, RetireLinks: []LinkID{twoLink}, Links: []LinkUpsert{moved}})
	if _, there := u.store.objects[two]; there {
		t.Errorf("the retired server is still there")
	}
	fresh := newUnit(t, nil, Options{})
	fresh.apply(fresh.fx.World(1, 1, 1))
	if got, want := u.store.head.Hash.Rows, fresh.store.head.Hash.Rows; got != want {
		t.Errorf("%d rows, want %d", got, want)
	}
	if u.store.head.Hash.Equal(fresh.store.head.Hash) {
		t.Errorf("the changed mount position must show in the hash")
	}
	if got := u.store.links[moved.id(t, u)]; got.Attrs[AttrMountPosition].Value != int64(9) {
		t.Errorf("the link attribute did not change: %+v", got.Attrs)
	}
	u.apply(Batch{Links: []LinkUpsert{moved}, Objects: []ObjectUpsert{u.fx.Server("SN-000000", "M1")}})

	scheme, _ := u.fx.Reg.IdentifierSchemeByName("redfish_system_id")
	write := IdentifierWrite{Object: one, Scheme: scheme.ID, Value: "sys-1", Scope: "a"}
	u.apply(Batch{Identifiers: []IdentifierWrite{write}})
	recs, _ := (liveSnap{s: u.store}).Identifiers(u.ctx, scheme.ID.String(), "sys-1")
	if len(recs) != 1 {
		t.Fatalf("records %+v", recs)
	}
	if res := u.apply(Batch{Identifiers: []IdentifierWrite{write}}); res.Changed() {
		t.Errorf("an identifier already held made a change")
	}
	write.Close = true
	if res := u.apply(Batch{Identifiers: []IdentifierWrite{write}}); !res.Changed() {
		t.Errorf("closing a held identifier made no change")
	}
	write.Value = "sys-2"
	if res := u.apply(Batch{Identifiers: []IdentifierWrite{write}}); res.Changed() {
		t.Errorf("closing an identifier that is not held made a change")
	}
}

func (l LinkUpsert) id(t *testing.T, u *unit) LinkID {
	t.Helper()
	typ, _ := u.fx.Reg.LinkType(l.Type)
	id, err := NewLinkID(typ.ID, l.Src, l.Dst, l.Discriminator)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
