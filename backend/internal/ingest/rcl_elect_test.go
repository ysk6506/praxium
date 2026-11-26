package ingest

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

// The choice of the record a link is made by when several records make it (ADR-0122). Every helper of the tests starts with rcl.

func rclWho(rid int64, src int64, def int, held bool, dataset int64, key string) LinkWho {
	return LinkWho{Rid: rid, Order: LinkOrder{SrcNo: src, Def: def, Held: held, DatasetNo: dataset, Key: key}}
}

func rclCand(ref string, who LinkWho) LinkCandidate {
	return LinkCandidate{Row: LinkRow{Ref: ref, Def: who.Order.Def}, Who: who}
}

// [REQ-869]
func TestRclTheOrderOfTheRecordsOfALinkGoesBySourceDefinitionHeldDatasetAndKey(t *testing.T) {
	base := LinkOrder{SrcNo: 1, Def: 1, Held: false, DatasetNo: 2, Key: "b"}
	for _, c := range []struct {
		name  string
		first LinkOrder
		later LinkOrder
	}{
		{"source", LinkOrder{SrcNo: 0, Def: 9, Held: true, DatasetNo: 9, Key: "z"}, base},
		{"definition", LinkOrder{SrcNo: 1, Def: 0, Held: true, DatasetNo: 9, Key: "z"}, base},
		{"held", base, LinkOrder{SrcNo: 1, Def: 1, Held: true, DatasetNo: 0, Key: "a"}},
		{"dataset", LinkOrder{SrcNo: 1, Def: 1, DatasetNo: 1, Key: "z"}, base},
		{"key", LinkOrder{SrcNo: 1, Def: 1, DatasetNo: 2, Key: "a"}, base},
	} {
		if !c.first.Before(c.later) || c.later.Before(c.first) {
			t.Errorf("%s: the one that goes first is not before the one that goes later", c.name)
		}
	}
	if base.Before(base) {
		t.Error("an order is before itself")
	}
	// the bytes of the key, not its words: "B" is before "a"
	if !(LinkOrder{Key: "B"}).Before(LinkOrder{Key: "a"}) {
		t.Error("the key goes by its bytes")
	}
}

// [REQ-869]
func TestRclTheRecordThatComesLastMakesTheLinkAndTheOthersLoseItWhateverTheOrderTheyComeIn(t *testing.T) {
	ws := []LinkWho{
		rclWho(5, 1, 0, false, 1, "k1"), rclWho(6, 1, 0, false, 1, "k3"), rclWho(7, 1, 0, true, 1, "k9"),
		rclWho(8, 1, 0, false, 2, "k0"), rclWho(9, 0, 3, false, 5, "zz"),
	}
	want := ""
	rng := rand.New(rand.NewSource(7))
	for round := 0; round < 20; round++ {
		rng.Shuffle(len(ws), func(i, j int) { ws[i], ws[j] = ws[j], ws[i] })
		var cands []LinkCandidate
		for _, w := range ws {
			cands = append(cands, rclCand("l1", w))
		}
		got := ElectLinks(nil, cands)
		if len(got.Wins) != 1 || got.Wins[0].Who.Rid != 7 || got.Wins[0].Replaces {
			t.Fatalf("the winner is %+v, and the record that a rule holds comes last", got.Wins)
		}
		var lost []int
		for _, l := range got.Lost {
			lost = append(lost, int(l.Who.Rid))
		}
		sort.Ints(lost)
		if text := fmt.Sprint(lost); want == "" {
			want = text
		} else if text != want {
			t.Fatalf("the records that lose are %s and were %s", text, want)
		}
		if !got.Used[LinkUse{Rid: 7, Def: 0}] || len(got.Used) != 1 {
			t.Errorf("the ends used are %v", got.Used)
		}
	}
	if want != "[5 6 8 9]" {
		t.Errorf("the records that lose are %s", want)
	}
}

// [REQ-869]
func TestRclARecordOfALaterPageTakesTheLinkOfTheHolderAndOneBeforeItLosesAtOnce(t *testing.T) {
	holder := rclWho(3, 1, 0, false, 1, "k5")
	holders := map[string]LinkHolder{"l1": {No: 11, Who: holder}}
	// a later record takes it: the holder is displaced, and loses
	got := ElectLinks(holders, []LinkCandidate{rclCand("l1", rclWho(4, 1, 0, false, 1, "k7"))})
	if len(got.Wins) != 1 || !got.Wins[0].Replaces || got.Wins[0].Who.Rid != 4 || len(got.Displaced) != 1 || got.Displaced[0].Who.Rid != 3 {
		t.Errorf("a later record does not take the link: %+v", got)
	}
	if len(got.Lost) != 1 || got.Lost[0].Who.Rid != 3 {
		t.Errorf("the holder does not lose: %+v", got.Lost)
	}
	// an earlier record loses, and the holder stays
	got = ElectLinks(holders, []LinkCandidate{rclCand("l1", rclWho(2, 1, 0, false, 1, "k1")), rclCand("l2", rclWho(2, 1, 1, false, 1, "k1"))})
	if len(got.Wins) != 1 || got.Wins[0].Row.Ref != "l2" || len(got.Lost) != 1 || got.Lost[0].Ref != "l1" || len(got.Displaced) != 0 {
		t.Errorf("an earlier record takes the link: %+v", got)
	}
}

// [REQ-869]
func TestRclTheChoiceIsTheSameWhateverPagesTheCandidatesComeIn(t *testing.T) {
	var all []LinkCandidate
	for i := 0; i < 9; i++ {
		all = append(all, rclCand("l1", rclWho(int64(i), 1, 0, i == 4, int64(i%3), fmt.Sprintf("k%d", i))))
	}
	whole := ElectLinks(nil, all)
	for size := 1; size < len(all); size++ {
		holders := map[string]LinkHolder{}
		lost := map[int64]bool{}
		for from := 0; from < len(all); from += size {
			to := min(from+size, len(all))
			got := ElectLinks(holders, all[from:to])
			for _, l := range got.Lost {
				lost[l.Who.Rid] = true
			}
			for _, w := range got.Wins {
				holders[w.Row.Ref] = LinkHolder{No: 1, Who: w.Who}
			}
		}
		if holders["l1"].Who.Rid != whole.Wins[0].Who.Rid {
			t.Errorf("pages of %d: the winner is %d and not %d", size, holders["l1"].Who.Rid, whole.Wins[0].Who.Rid)
		}
		if len(lost) != len(all)-1 || lost[holders["l1"].Who.Rid] {
			t.Errorf("pages of %d: the records that lose are %v", size, lost)
		}
	}
}
