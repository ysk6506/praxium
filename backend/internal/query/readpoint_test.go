package query

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// sealTheKG seals the KG as it is and returns the version.
func sealTheKG(r *rig) graph.Version {
	r.t.Helper()
	res, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync)
	if err != nil {
		r.t.Fatalf("seal: %v", err)
	}
	return res.Version
}

// [REQ-252]
func TestReadsAtTheCurrentChangeNumberGiveTheSameAnswerAsTheLatest(t *testing.T) {
	r := newRig(t, false)
	r.world()
	head := r.headSeq()
	latest := r.mustFind(FindInput{ObjectType: "Site"})
	if latest.Meta.AsOf.ChangeSeq != head || latest.Meta.VolatileAsOf != "current" || latest.Meta.AsOf.SealedVersion != nil {
		t.Fatalf("latest read: %+v volatile %q", latest.Meta.AsOf, latest.Meta.VolatileAsOf)
	}
	pinned := r.mustFind(FindInput{ObjectType: "Site", Common: r.common(func(c *Common) { c.ChangeSeq = &head })})
	if !reflect.DeepEqual(names(pinned.Data.Items), names(latest.Data.Items)) || pinned.Meta.AsOf.ChangeSeq != head {
		t.Fatalf("pinned read: %v at %d", names(pinned.Data.Items), pinned.Meta.AsOf.ChangeSeq)
	}
	if pinned.Meta.VolatileAsOf == "current" {
		t.Fatalf("a read at a change number does not carry the volatile values of now: %q", pinned.Meta.VolatileAsOf)
	}
}

// [REQ-252]
func TestAReadAtAChangeNumberThatIsNotThereIsRefused(t *testing.T) {
	r := newRig(t, false)
	r.world()
	head := r.headSeq()
	older, later := head-1, head+1
	tests := []struct {
		name string
		seq  *int64
		code kernel.Code
	}{
		{"older than the store keeps", &older, kernel.CodeUnsupported},
		{"not readable yet", &later, graph.CodeNotReadable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.find(FindInput{ObjectType: "Site", Common: r.common(func(c *Common) { c.ChangeSeq = tc.seq })}, AllowAll())
			if got := codeOf(err); got != tc.code {
				t.Fatalf("code %q (%v), want %q", got, err, tc.code)
			}
		})
	}
}

// [REQ-252]
func TestAReadAtASealedVersionNamesItAndAnAnswerAtItsNumberNamesItToo(t *testing.T) {
	r := newRig(t, false)
	r.world()
	v := sealTheKG(r)
	byVersion := r.mustFind(FindInput{ObjectType: "Site", Common: r.common(func(c *Common) { c.KGVersionID = v.ID.String() })})
	if byVersion.Meta.AsOf.SealedVersion == nil || *byVersion.Meta.AsOf.SealedVersion != v.ID.String() {
		t.Fatalf("by version: %+v", byVersion.Meta.AsOf)
	}
	latest := r.mustFind(FindInput{ObjectType: "Site"})
	if latest.Meta.AsOf.SealedVersion == nil || *latest.Meta.AsOf.SealedVersion != v.ID.String() {
		t.Fatalf("the latest state equals the sealed one, so it names the version: %+v", latest.Meta.AsOf)
	}
	r.apply(graph.Batch{Volatile: []graph.VolatileWrite{r.fx.Power(r.fx.ServerID("SN-000102"), "off", seenAt)}})
	if after := r.mustFind(FindInput{ObjectType: "Site"}); after.Meta.AsOf.SealedVersion != nil && after.Meta.AsOf.ChangeSeq != v.SealedSeq {
		t.Fatalf("a state after the seal must not name it: %+v", after.Meta.AsOf)
	}
}

// [REQ-252]
func TestWaitingForAChangeNumberReadsWhenItIsThereAndRefusesWhenItIsNot(t *testing.T) {
	r := newRig(t, false)
	r.world()
	head := r.headSeq()
	a := r.mustFind(FindInput{ObjectType: "Site", Common: r.common(func(c *Common) { c.MinSeq, c.WaitTimeout = head, time.Second })})
	if a.Meta.AsOf.ChangeSeq != head {
		t.Fatalf("read at %d, want %d", a.Meta.AsOf.ChangeSeq, head)
	}
	_, err := r.find(FindInput{ObjectType: "Site", Common: r.common(func(c *Common) { c.MinSeq, c.WaitTimeout = head+5, time.Millisecond })}, AllowAll())
	if got := codeOf(err); got != graph.CodeWaitTimeout {
		t.Fatalf("code %q (%v)", got, err)
	}
}

// [REQ-252]
func TestACursorOfAnotherGenerationOrWithABadVersionIsRefused(t *testing.T) {
	r := newRig(t, false)
	r.world()
	in := FindInput{ObjectType: "PhysicalServer", Common: r.common(func(c *Common) { c.Limit = 2 })}
	page := r.mustFind(in)
	if page.Meta.ResultComplete.NextCursor == nil {
		t.Fatal("the first page has no cursor")
	}
	var cur cursor
	raw, err := base64.RawURLEncoding.DecodeString(*page.Meta.ResultComplete.NextCursor)
	if err != nil || json.Unmarshal(raw, &cur) != nil {
		t.Fatalf("the cursor is not the one this unit writes: %v", err)
	}
	next := func(change func(*cursor)) (Answer[FindData], error) {
		c := cur
		change(&c)
		in.Cursor = c.encode()
		return r.find(in, AllowAll())
	}
	if _, err := next(func(c *cursor) { c.Gen++ }); codeOf(err) != graph.CodeGenerationChanged {
		t.Fatalf("another generation: %v", err)
	}
	if _, err := next(func(c *cursor) { c.Version = "!x" }); codeOf(err) != CodeCursorInvalid {
		t.Fatalf("a bad version: %v", err)
	}
	_, err = r.find(FindInput{ObjectType: "Site", Common: r.common(func(c *Common) { c.KGVersionID = "!x" })}, AllowAll())
	if codeOf(err) != CodeInput || FieldOf(err) != "kg_version_id" {
		t.Fatalf("a version that is not an id: %v", err)
	}
	v := sealTheKG(r)
	a, err := next(func(c *cursor) { c.Version = v.ID.String() })
	if err != nil || a.Meta.AsOf.SealedVersion == nil || *a.Meta.AsOf.SealedVersion != v.ID.String() {
		t.Fatalf("a page at a sealed version: %v %+v", err, a.Meta.AsOf)
	}
}
