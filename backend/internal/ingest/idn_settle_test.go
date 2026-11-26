package ingest

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// idnSettleFake is the database at the end of a search for the groups: it gives its pairs in pages of a size and keeps what is written.
type idnSettleFake struct {
	idnFake
	pairs   []SettlePair
	size    int
	written int64
	puts    [][]SettledPair
	ends    []SettleEnd
	reads   int
	failPut bool
}

func (d *idnSettleFake) SettleRead(_ context.Context, _ RunLease, after int64) (SettlePage, error) {
	d.reads++
	page := SettlePage{Written: d.written}
	for _, p := range d.pairs {
		if p.Pid > after && len(page.Pairs) < d.size {
			page.Pairs = append(page.Pairs, p)
		}
	}
	if n := len(page.Pairs); n > 0 {
		page.Next = page.Pairs[n-1].Pid
	}
	page.Done = len(page.Pairs) < d.size
	return page, nil
}

func (d *idnSettleFake) PutSettled(_ context.Context, _ RunLease, rows []SettledPair, end SettleEnd) error {
	if d.failPut {
		return kernel.Errorf(kernel.CodeInternal, "the write failed")
	}
	d.puts, d.ends = append(d.puts, rows), append(d.ends, end)
	return nil
}

// idnPath is n pairs one after another that share a key with the next, with numbers in the order given.
func idnPath(n int, order []int) []SettlePair {
	out := make([]SettlePair, n)
	for i := 0; i < n; i++ {
		p := SettlePair{Pid: int64(order[i] + 1)}
		if i > 0 {
			p.Keys = append(p.Keys, "k"+strconv.Itoa(i-1))
		}
		if i < n-1 {
			p.Keys = append(p.Keys, "k"+strconv.Itoa(i))
		}
		out[i] = p
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pid < out[j].Pid })
	return out
}

// [REQ-882]
func TestIdnTheGroupsOfPairsThatShareKeysAreFoundWhateverTheNumbersAndEachGetsItsSmallestPair(t *testing.T) {
	order := idnPerm(1600, 7)
	rows := settleLabels(idnPath(1600, order))
	if len(rows) != 1600 {
		t.Fatal(len(rows))
	}
	for i, r := range rows {
		if r.Label != 1 || r.Pid != int64(i+1) {
			t.Fatalf("%+v", r)
		}
	}
	// two groups, and a pair alone
	pairs := []SettlePair{{Pid: 9, Keys: []string{"a"}}, {Pid: 4, Keys: []string{"a", "b"}}, {Pid: 7, Keys: []string{"b"}}, {Pid: 5, Keys: []string{"c"}}, {Pid: 6}}
	got := fmt.Sprint(settleLabels(pairs))
	if got != "[{4 4} {5 5} {6 6} {7 4} {9 4}]" {
		t.Errorf("%s", got)
	}
}

// [REQ-882]
func TestIdnSettlingReadsAllThePagesWritesTheLabelsInPagesAndTheLastPageEndsIt(t *testing.T) {
	d := &idnSettleFake{pairs: idnPath(12000, idnPerm(12000, 3)), size: 5000}
	if err := settleGroups(context.Background(), d, RunLease{No: 1}, 0); err != nil {
		t.Fatal(err)
	}
	if d.reads != 3 || len(d.puts) != 3 || len(d.puts[0]) != settlePut || !d.ends[2].Last || d.ends[0].Last || d.ends[2].Skipped {
		t.Fatalf("%d reads, %d writes, %+v", d.reads, len(d.puts), d.ends)
	}
	// a run that was stopped after the first page goes on from there
	d2 := &idnSettleFake{pairs: d.pairs, size: 5000, written: d.puts[0][len(d.puts[0])-1].Pid}
	if err := settleGroups(context.Background(), d2, RunLease{No: 1}, 0); err != nil {
		t.Fatal(err)
	}
	if n := len(d2.puts[0]); n == 0 || d2.puts[0][0].Pid != d.puts[0][len(d.puts[0])-1].Pid+1 {
		t.Errorf("a stopped run writes %d pairs again", n)
	}
	// every pair written was already
	d3 := &idnSettleFake{pairs: idnPath(10, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}), size: 100, written: 99}
	if err := settleGroups(context.Background(), d3, RunLease{No: 1}, 0); err != nil || len(d3.puts) != 1 || len(d3.puts[0]) != 0 || !d3.ends[0].Last {
		t.Errorf("%v %+v", err, d3.ends)
	}
}

// [REQ-882]
func TestIdnSettlingWritesNothingWhenThePairsAreMoreThanGoHoldsAndSaysSo(t *testing.T) {
	d := &idnSettleFake{pairs: idnPath(50, idnPerm(50, 1)), size: 20}
	if err := settleGroups(context.Background(), d, RunLease{No: 1}, 30); err != nil {
		t.Fatal(err)
	}
	if len(d.puts) != 1 || len(d.puts[0]) != 0 || !d.ends[0].Skipped || !d.ends[0].Last {
		t.Errorf("%+v", d.ends)
	}
	d.failPut = true
	if err := settleGroups(context.Background(), d, RunLease{No: 1}, 0); err == nil {
		t.Error("a write that fails is not told")
	}
}

// [REQ-882]
func TestIdnSettlingTheMostPairsGoHoldsTakesTheMemoryAndTheTimeTheDecisionSays(t *testing.T) {
	pairs := idnPath(DefaultSettlePairs, idnPerm(DefaultSettlePairs, 5))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	clock := kernel.SystemClock{}
	began := clock.Now()
	rows := settleLabels(pairs)
	took := clock.Now().Sub(began)
	runtime.ReadMemStats(&after)
	t.Logf("%d pairs: %v, %d MB allocated in all (the pairs themselves are %d MB)", len(rows), took, (after.TotalAlloc-before.TotalAlloc)>>20, before.HeapAlloc>>20)
	if rows[0].Label != 1 || rows[len(rows)-1].Label != 1 {
		t.Fatalf("%+v", rows[0])
	}
	if took > 5*time.Second || (after.TotalAlloc-before.TotalAlloc)>>20 > 200 {
		t.Errorf("%v, %d MB", took, (after.TotalAlloc-before.TotalAlloc)>>20)
	}
}

// idnPerm is a permutation of 0..n-1 that is the same every time (a step of a linear congruential generator for each swap).
func idnPerm(n int, seed uint64) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	x := seed*2862933555777941757 + 3037000493
	for i := n - 1; i > 0; i-- {
		x = x*6364136223846793005 + 1442695040888963407
		j := int((x >> 33) % uint64(i+1))
		out[i], out[j] = out[j], out[i]
	}
	return out
}
