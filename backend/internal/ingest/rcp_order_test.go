package ingest

import (
	"context"
	"sort"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The tests of the order the computation of the objects chooses by (ADR-0115 decision 5). Every helper and type they add starts with rcp.

// rcpLines is every combination of the fields the order looks at, two values of each: where two lines tie on a field, the next field
// decides, and two lines that are the same in all of them are the same line.
func rcpLines() []*valueLine {
	var out []*valueLine
	for mask := 0; mask < 1<<8; mask++ {
		pick := func(bit int) int { return (mask >> bit) & 1 }
		keys := []string{"a", "b"}
		cols := []string{"x", "y"}
		out = append(out, &valueLine{rank: pick(0), openedSeq: int64(pick(1)), srcNo: int64(pick(2)), feed: pick(3), datasetNo: int64(pick(4)),
			key: keys[pick(5)], column: cols[pick(6)], text: map[int]string{0: "p", 1: "q"}[pick(7)]})
	}
	return out
}

func rcpSame(a, b *valueLine) bool {
	return a.rank == b.rank && a.openedSeq == b.openedSeq && a.srcNo == b.srcNo && a.feed == b.feed && a.datasetNo == b.datasetNo && a.key == b.key && a.column == b.column
}

// [REQ-930]
func TestRcpTheOrderOfTheValuesIsAStrictTotalOrder(t *testing.T) {
	lines := rcpLines()
	for _, a := range lines {
		if valueBefore(a, a) {
			t.Fatalf("a line is before itself: %+v", a)
		}
	}
	for i, a := range lines {
		for _, b := range lines[i+1:] {
			ab, ba := valueBefore(a, b), valueBefore(b, a)
			switch {
			case rcpSame(a, b) && (ab || ba):
				t.Fatalf("the same line is ordered: %+v %+v", a, b)
			case !rcpSame(a, b) && ab == ba:
				t.Fatalf("two lines that differ are not ordered (total): %+v %+v", a, b)
			}
		}
	}
	// transitive, on a sample that takes every line against every other one two steps apart
	for i := 0; i < len(lines); i += 3 {
		for j := 0; j < len(lines); j += 5 {
			for k := 0; k < len(lines); k += 7 {
				if a, b, c := lines[i], lines[j], lines[k]; valueBefore(a, b) && valueBefore(b, c) && !valueBefore(a, c) {
					t.Fatalf("the order is not transitive: %+v %+v %+v", a, b, c)
				}
			}
		}
	}
}

// [REQ-930]
func TestRcpTheValueChosenDoesNotDependOnTheOrderTheLinesAreRead(t *testing.T) {
	base := rcpLines()[:0:0]
	for _, mask := range []int{0, 1, 2, 3, 5, 12, 17, 64, 96, 128, 160} {
		base = append(base, rcpLines()[mask])
	}
	first := rcpWinner(base)
	rcpPermute(len(base), func(p []int) {
		var got []*valueLine
		for _, i := range p {
			got = append(got, base[i])
		}
		if w := rcpWinner(got); !rcpSame(w, first) {
			t.Fatalf("the winner is %+v for the order %v and %+v for the first order", w, p, first)
		}
	})
}

func rcpWinner(lines []*valueLine) *valueLine {
	cp := append([]*valueLine(nil), lines...)
	sort.Slice(cp, func(i, j int) bool { return valueBefore(cp[i], cp[j]) })
	return cp[0]
}

// rcpPermute calls f with some orders of n items: the ones in a fixed walk through all of them for a small n.
func rcpPermute(n int, f func([]int)) {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	var walk func(k int)
	count := 0
	walk = func(k int) {
		if count > 5000 {
			return
		}
		if k == n {
			count++
			f(p)
			return
		}
		for i := k; i < n; i++ {
			p[k], p[i] = p[i], p[k]
			walk(k + 1)
			p[k], p[i] = p[i], p[k]
		}
	}
	walk(0)
}

// [REQ-931]
func TestRcpTheIdentifierOfAnObjectIsTheFirstOfAStrictTotalOrder(t *testing.T) {
	var lines []identLine
	for mask := 0; mask < 1<<5; mask++ {
		lines = append(lines, identLine{openedSeq: int64(mask & 1), key: []string{"a", "b"}[(mask>>1)&1], srcNo: int64((mask >> 2) & 1), feed: (mask >> 3) & 1, value: []string{"v", "w"}[(mask>>4)&1]})
	}
	for i, a := range lines {
		if identBefore(a, a) {
			t.Fatalf("a line is before itself: %+v", a)
		}
		for _, b := range lines[i+1:] {
			if identBefore(a, b) == identBefore(b, a) {
				t.Fatalf("two lines that differ are not ordered: %+v %+v", a, b)
			}
		}
	}
}

// [REQ-930]
func TestRcpAnObjectsLinesThatAreTheLaterRecordWinTheLowerRankFirst(t *testing.T) {
	a := &valueLine{rank: 1, openedSeq: 9}
	b := &valueLine{rank: 0, openedSeq: 1}
	c := &valueLine{rank: 0, openedSeq: 2}
	if !valueBefore(b, a) || !valueBefore(c, b) {
		t.Fatal("the lower rank is first and, at a rank, the record that was opened last")
	}
}

type rcpPlace struct {
	Recomputer
	place RecomputePlace
	calls []string
}

func (f *rcpPlace) RecomputePlace(context.Context, RunLease) (RecomputePlace, error) {
	return f.place, nil
}

// [REQ-937]
func TestRcpTheStepIsRefusedForARunThatIsNotComputing(t *testing.T) {
	f := &rcpPlace{place: RecomputePlace{Stage: RunReceiving}}
	_, err := RunRecompute(context.Background(), f, RunLease{No: 1}, RecomputeStart{Seen: "2030-01-01T00:00:00Z"}, nil)
	if kernel.CodeOf(err) != CodeRunStage {
		t.Fatalf("the error is %v", err)
	}
}

// [REQ-937]
func TestRcpAStartNeedsATimeInUTCAndSizesThatAreNotNegative(t *testing.T) {
	for _, s := range []RecomputeStart{{Seen: ""}, {Seen: "2030-01-01T00:00:00+09:00"}, {Seen: "2030-01-01T00:00:00Z", Lines: -1}, {Seen: "2030-01-01T00:00:00Z", ObjectMax: -1}} {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v is accepted", s)
		}
	}
	if err := (RecomputeStart{Seen: "2030-01-01T00:00:00Z"}).Validate(); err != nil {
		t.Error(err)
	}
}
