package ingest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The fifth step's decisions, as pure functions (ADR-0123): who keeps an identifier, and which links take the places a binding holds to
// one. Every helper starts with iso.

func isoReg(t *testing.T) *model.Registry {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// isoTypes are the numbers of the types the cases use.
type isoTypes struct{ server, rack, host, vm, mounted, runsOn int }

func isoTypesOf(t *testing.T, reg *model.Registry) isoTypes {
	t.Helper()
	no := func(name string) int {
		ot, ok := reg.ObjectTypeByName(name)
		if !ok {
			t.Fatalf("no type %s", name)
		}
		return ot.TypeNo
	}
	link := func(name string) int {
		lt, ok := reg.LinkTypeByName(name)
		if !ok {
			t.Fatalf("no link type %s", name)
		}
		return lt.LinkTypeNo
	}
	return isoTypes{server: no("PhysicalServer"), rack: no("Rack"), host: no("HypervisorHost"), vm: no("VirtualMachine"), mounted: link("MOUNTED_IN"), runsOn: link("RUNS_ON")}
}

// [REQ-847]
func TestIsoTheObjectThatKeepsAnIdentifierIsTheOneTheKGHasItWithElseTheSmallestID(t *testing.T) {
	cases := []struct {
		name   string
		in     []IdentClaimant
		winner int64
		byKG   bool
		losers []int64
	}{
		{"two of the run", []IdentClaimant{{ObjNo: 2, Ref: "b", Staged: true}, {ObjNo: 1, Ref: "c", Staged: true}}, 2, false, []int64{1}},
		{"the KG keeps it with an object the run does not touch", []IdentClaimant{{ObjNo: 9, Ref: "z", Holds: true}, {ObjNo: 1, Ref: "a", Staged: true}}, 9, true, []int64{1}},
		{"the KG keeps it with an object of the run", []IdentClaimant{{ObjNo: 9, Ref: "z", Staged: true, Holds: true}, {ObjNo: 1, Ref: "a", Staged: true}, {ObjNo: 3, Ref: "b", Staged: true}}, 9, true, []int64{1, 3}},
		{"two hold it (a KG that broke the rule)", []IdentClaimant{{ObjNo: 9, Ref: "z", Holds: true}, {ObjNo: 8, Ref: "y", Holds: true, Staged: true}, {ObjNo: 1, Ref: "a", Staged: true}}, 8, true, []int64{1}},
		{"the ids and not the numbers", []IdentClaimant{{ObjNo: 1, Ref: "m", Staged: true}, {ObjNo: 2, Ref: "l", Staged: true}}, 2, false, []int64{1}},
	}
	for _, c := range cases {
		for i := 0; i < 2*len(c.in); i++ {
			// every turn of the list, forwards and backwards
			in := append(append([]IdentClaimant(nil), c.in[i%len(c.in):]...), c.in[:i%len(c.in)]...)
			if i >= len(c.in) {
				slices.Reverse(in)
			}
			w, byKG, losers := ElectIdent(IdentKey{Scheme: "s", Value: "v", Claimants: in})
			if w != c.winner || byKG != c.byKG || fmt.Sprint(losers) != fmt.Sprint(c.losers) {
				t.Errorf("%s (order %d): %d %v %v, want %d %v %v", c.name, i, w, byKG, losers, c.winner, c.byKG, c.losers)
			}
		}
	}
	if w, _, l := ElectIdent(IdentKey{}); w != 0 || l != nil {
		t.Errorf("no claimant: %d %v", w, l)
	}
}

// [REQ-847]
func TestIsoTheLinesThatLoseCarryTheNumberAndTheHashTheClaimsNameThemBy(t *testing.T) {
	m := NewIsolationMaker(isoReg(t))
	page := IsolatePage{Phase: IsolatePhaseIdents, Schemes: map[string]int{"is.serial": 4}, Keys: []IdentKey{
		{Scheme: "is.serial", Value: "v1", Claimants: []IdentClaimant{{ObjNo: 1, Ref: "a", Staged: true}, {ObjNo: 2, Ref: "b", Staged: true}}},
		{Scheme: "is.serial", Value: "v2", Claimants: []IdentClaimant{{ObjNo: 3, Ref: "c", Staged: true, Holds: true}}},
	}}
	out, err := m.MakeIsolated(page)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hashText("v1")
	if out.Keys != 2 || len(out.Idents) != 1 || out.Idents[0].ObjNo != 2 || out.Idents[0].SchemeNo != 4 || out.Idents[0].Hash != want || out.Idents[0].ByKG {
		t.Fatalf("%+v", out)
	}
	page.Schemes = map[string]int{}
	if _, err := m.MakeIsolated(page); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Errorf("a scheme that has no number: %v", err)
	}
	// the phase of the ends has nothing for Go
	if out, err := m.MakeIsolated(IsolatePage{Phase: IsolatePhaseEnds, Unit: 3, Last: true}); err != nil || out.Unit != 3 || !out.Last || out.Seats != nil {
		t.Errorf("%+v %v", out, err)
	}
}

// isoLink is a link of the cases: n is its number and its id is "l" and n (so the ids are in the order of the numbers).
func isoLink(ty isoTypes, n, src, dst int64, srcType, dstType, typ int) SeatLink {
	return SeatLink{LinkNo: n, Ref: fmt.Sprintf("l%02d", n), TypeNo: typ, Src: src, Dst: dst, SrcType: srcType, DstType: dstType, SrcName: fmt.Sprint("o", src), DstName: fmt.Sprint("o", dst)}
}

func isoLostText(lost []SeatLoss) string {
	var out []string
	for _, l := range lost {
		out = append(out, l.Ref)
	}
	return strings.Join(out, " ")
}

// [REQ-849]
func TestIsoALinkTakesItsPlacesWhenAllAreFreeInTheOrderOfThePage(t *testing.T) {
	reg := isoReg(t)
	ty := isoTypesOf(t, reg)
	m := NewIsolationMaker(reg)
	mount := func(n, src, dst int64) SeatLink { return isoLink(ty, n, src, dst, ty.server, ty.rack, ty.mounted) }
	host := func(n, src, dst int64) SeatLink { return isoLink(ty, n, src, dst, ty.host, ty.server, ty.runsOn) }
	kept := func(l SeatLink) SeatLink {
		l.Kept = true
		return l
	}
	cases := []struct {
		name  string
		page  IsolatePage
		lost  string
		seats int
	}{
		// N:1: a server has one rack; a rack has many servers
		{"one start", IsolatePage{Links: []SeatLink{mount(1, 10, 20), mount(2, 10, 21), mount(3, 11, 20)}}, "l02", 2},
		{"a place an earlier page took", IsolatePage{Links: []SeatLink{mount(2, 10, 21)}}, "", 1},
		// 1:1: a host runs on one server and a server has one host; a link that loses takes no place, so the one after it is free
		{"one to one", IsolatePage{Links: []SeatLink{host(1, 30, 40), host(2, 30, 41), host(3, 31, 41), host(4, 32, 40)}}, "l02 l04", 4},
		// a link the KG keeps holds its place
		{"a link the KG keeps", IsolatePage{Links: []SeatLink{mount(1, 10, 20)}, Fixed: []SeatLink{mount(9, 10, 22)}}, "l01", 0},
		// a link the KG has that the run staged again keeps its place against a new link of a smaller id
		{"a link the KG has and the run stages", IsolatePage{Links: []SeatLink{mount(1, 10, 20), kept(mount(2, 10, 21))}, Fixed: []SeatLink{kept(mount(2, 10, 21))}}, "l01", 1},
		// two of them at one place (a KG that broke the rule): the smaller id keeps it
		{"two links the KG has", IsolatePage{Links: []SeatLink{kept(mount(1, 10, 20)), kept(mount(2, 10, 21))}, Fixed: []SeatLink{kept(mount(1, 10, 20)), kept(mount(2, 10, 21))}}, "l02", 1},
		// a link of a type the binding does not hold to one, and one of no binding, take nothing and lose nothing
		{"no place", IsolatePage{Links: []SeatLink{isoLink(ty, 1, 30, 40, ty.vm, ty.vm, ty.runsOn), isoLink(ty, 2, 30, 41, ty.vm, ty.vm, ty.runsOn), isoLink(ty, 3, 10, 20, ty.rack, ty.rack, ty.mounted)}}, "", 0},
	}
	for _, c := range cases {
		c.page.Phase = IsolatePhaseSeats
		if c.name == "a place an earlier page took" {
			b := m.bindOf(ty.mounted, ty.server, ty.rack)
			c.page.Taken = []Seat{{Side: SeatOut, EndNo: 10, BindID: b.id, LinkNo: 1}}
			c.lost = "l02"
			c.seats = 0
		}
		out, err := m.MakeIsolated(c.page)
		if err != nil {
			t.Fatal(err)
		}
		if got := isoLostText(out.Lost); got != c.lost || len(out.Seats) != c.seats {
			t.Errorf("%s: lost %q with %d places, want %q with %d", c.name, got, len(out.Seats), c.lost, c.seats)
		}
	}
	out, _ := m.MakeIsolated(IsolatePage{Phase: IsolatePhaseSeats, Links: []SeatLink{mount(1, 10, 20), mount(2, 10, 21)}})
	if len(out.Lost) != 1 || !strings.HasPrefix(out.Lost[0].Reason, "the link MOUNTED_IN from o10 to o21 would break the cardinality N:1, so it was not made") {
		t.Errorf("the reason of the old flow: %+v", out.Lost)
	}
}

// [REQ-852]
func TestIsoThePlacesHangOnTheOrderOfTheLinksAloneAndNotOnThePages(t *testing.T) {
	reg := isoReg(t)
	ty := isoTypesOf(t, reg)
	m := NewIsolationMaker(reg)
	// a chain of one to one links: every link shares a place with the next
	var links []SeatLink
	for i := int64(1); i <= 9; i++ {
		src, dst := 100+(i+1)/2, 200+i/2
		links = append(links, isoLink(ty, i, src, dst, ty.host, ty.server, ty.runsOn))
	}
	whole, _ := m.MakeIsolated(IsolatePage{Phase: IsolatePhaseSeats, Links: links})
	for size := 1; size <= 4; size++ {
		var taken []Seat
		var lost []SeatLoss
		for at := 0; at < len(links); at += size {
			out, _ := m.MakeIsolated(IsolatePage{Phase: IsolatePhaseSeats, Links: links[at:min(at+size, len(links))], Taken: taken})
			taken, lost = append(taken, out.Seats...), append(lost, out.Lost...)
		}
		if isoLostText(lost) != isoLostText(whole.Lost) || len(taken) != len(whole.Seats) {
			t.Errorf("pages of %d: lost %q, as a whole %q", size, isoLostText(lost), isoLostText(whole.Lost))
		}
	}
	if isoLostText(whole.Lost) != "l02 l04 l06 l08" {
		t.Errorf("the chain lost %q", isoLostText(whole.Lost))
	}
}

// isoFake is a port that keeps the place in memory and says what was called, to see how RunIsolate drives it.
type isoFake struct {
	calls []string
	place IsolatePlace
	pages int
	fail  string
}

func (f *isoFake) isoErr(call string) error {
	f.calls = append(f.calls, call)
	if f.fail == call {
		return kernel.Errorf(kernel.CodeInternal, "%s fails", call)
	}
	return nil
}

func (f *isoFake) BeginIsolate(context.Context, RunLease, IsolateStart) error {
	f.place.Begun = true
	return f.isoErr("begin")
}

func (f *isoFake) IsolatePlace(context.Context, RunLease) (IsolatePlace, error) {
	return f.place, f.isoErr("place")
}

func (f *isoFake) IsolatePage(context.Context, RunLease) (IsolatePage, error) {
	return IsolatePage{Phase: IsolatePhaseEnds, Unit: f.place.Unit, Last: true}, f.isoErr("page")
}

func (f *isoFake) PutIsolated(_ context.Context, _ RunLease, p IsolatedPage) error {
	f.place.Unit++
	f.place.Finished = f.place.Unit >= f.pages
	return f.isoErr("put")
}

func (f *isoFake) IsolateReport(context.Context, RunLease) (IsolateCounts, error) {
	return IsolateCounts{Keys: f.place.Unit}, f.isoErr("report")
}

// [REQ-846]
func TestIsoRunIsolateBeginsOnceDoesThePagesAndStopsWhereItFails(t *testing.T) {
	m := NewIsolationMaker(isoReg(t))
	f := &isoFake{place: IsolatePlace{Stage: RunComputing}, pages: 2}
	counts, err := RunIsolate(context.Background(), f, RunLease{No: 1}, IsolateStart{}, m)
	if err != nil || counts.Keys != 2 || strings.Join(f.calls, " ") != "place begin place page put place page put place report" {
		t.Fatalf("%+v %v %v", counts, err, f.calls)
	}
	// a run that has begun is not begun again
	f.calls, f.place.Finished, f.pages = nil, false, 3
	if _, err := RunIsolate(context.Background(), f, RunLease{No: 1}, IsolateStart{}, m); err != nil || f.calls[1] == "begin" {
		t.Fatalf("%v %v", err, f.calls)
	}
	for _, call := range []string{"begin", "place", "page", "put", "report"} {
		g := &isoFake{place: IsolatePlace{Stage: RunComputing}, pages: 1, fail: call}
		if _, err := RunIsolate(context.Background(), g, RunLease{No: 1}, IsolateStart{}, m); kernel.CodeOf(err) != kernel.CodeInternal {
			t.Errorf("%s fails: %v", call, err)
		}
	}
	if _, err := RunIsolate(context.Background(), &isoFake{place: IsolatePlace{Stage: RunApplying}}, RunLease{No: 1}, IsolateStart{}, m); kernel.CodeOf(err) != CodeRunStage {
		t.Errorf("a run that is not computing: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunIsolate(ctx, &isoFake{place: IsolatePlace{Stage: RunComputing}, pages: 1}, RunLease{No: 1}, IsolateStart{}, m); !errors.Is(err, context.Canceled) {
		t.Errorf("a context that is done: %v", err)
	}
	for _, bad := range []IsolateStart{{Lines: -1}, {Cards: []ExpandCard{{LinkTypeNo: 1}, {LinkTypeNo: 1}}}} {
		if _, err := RunIsolate(context.Background(), &isoFake{}, RunLease{No: 1}, bad, m); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

// [REQ-846]
func TestIsoTheMakerIsOfTheModelOfThePlansAndTheCardsAreTheModels(t *testing.T) {
	if _, err := NewIsolationMakerOf(nil); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("no plan: %v", err)
	}
	if IsolateCardsOf(nil) != nil {
		t.Error("cards of no plan")
	}
	reg := isoReg(t)
	plan := &Plan{reg: reg}
	m, err := NewIsolationMakerOf(map[int64]*Plan{2: plan, 1: plan})
	if err != nil || m.reg != reg {
		t.Fatalf("%v", err)
	}
	if fmt.Sprint(IsolateCardsOf(map[int64]*Plan{1: plan})) != fmt.Sprint(ExpandCards(reg)) {
		t.Error("the cards are not the model's")
	}
	other, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	if other.Hash() == reg.Hash() {
		// the same model loaded twice is one model
		if _, err := NewIsolationMakerOf(map[int64]*Plan{1: plan, 2: {reg: other}}); err != nil {
			t.Errorf("one model loaded twice: %v", err)
		}
	}
}
