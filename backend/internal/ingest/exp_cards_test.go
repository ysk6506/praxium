package ingest

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-891] [REQ-956]
func TestExpTheEndsThatALinkTypeHoldsToOneAreTheOnesItsOneToOneOneToManyAndManyToOneBindingsSay(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	cards := map[int]ExpandCard{}
	for _, c := range ExpandCards(reg) {
		cards[c.LinkTypeNo] = c
	}
	seen := map[model.Cardinality]int{}
	for _, lt := range reg.LinkTypes() {
		has := map[model.Cardinality]bool{}
		for _, b := range lt.Bindings {
			has[b.Cardinality] = true
		}
		c, given := cards[lt.LinkTypeNo]
		for card, ok := range has {
			if ok {
				seen[card]++
			}
		}
		switch {
		case has[model.CardOneToOne]:
			if !given || !c.FromOne || !c.ToOne {
				t.Errorf("the link type %s has a 1:1 binding and holds both ends to one: %+v", lt.Name, c)
			}
		case has[model.CardManyToOne] && has[model.CardOneToMany]:
			if !given || !c.FromOne || !c.ToOne {
				t.Errorf("the link type %s has a 1:N and an N:1 binding and holds both ends to one: %+v", lt.Name, c)
			}
		case has[model.CardManyToOne]:
			if !given || !c.FromOne || c.ToOne {
				t.Errorf("the link type %s has an N:1 binding and holds the start to one: %+v", lt.Name, c)
			}
		case has[model.CardOneToMany]:
			if !given || c.FromOne || !c.ToOne {
				t.Errorf("the link type %s has a 1:N binding and holds the end to one: %+v", lt.Name, c)
			}
		default:
			if given {
				t.Errorf("the link type %s has neither end held to one, and is given: %+v", lt.Name, c)
			}
		}
	}
	// the standard model has a link type of these kinds, so that these branches above are looked at (it has none of 1:N: a model that has
	// one is made below)
	for _, card := range []model.Cardinality{model.CardOneToOne, model.CardManyToOne, model.CardManyToMany} {
		if seen[card] == 0 {
			t.Errorf("no link type of the standard model has a binding of %s: the test does not look at it", card)
		}
	}
	defs := reg.Definitions()
	changed := 0
	for i, lt := range defs.LinkTypes {
		if c, given := cards[lt.LinkTypeNo]; !given || (!c.FromOne && !c.ToOne) {
			for j := range defs.LinkTypes[i].Bindings {
				defs.LinkTypes[i].Bindings[j].Cardinality = model.CardOneToMany
			}
			changed = lt.LinkTypeNo
			break
		}
	}
	if changed == 0 {
		t.Fatal("no link type of the standard model holds no end to one: the test does not look at 1:N")
	}
	one, err := model.NewRegistry(defs)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ExpandCards(one) {
		if c.LinkTypeNo == changed && (c.FromOne || !c.ToOne) {
			t.Errorf("the link type %d has only 1:N bindings and holds the end to one: %+v", changed, c)
		} else if c.LinkTypeNo == changed {
			changed = 0
		}
	}
	if changed != 0 {
		t.Errorf("the link type %d has only 1:N bindings and is not given", changed)
	}
}

// [REQ-890] [REQ-956]
func TestExpTheFeedsOfTheExpansionAreTheOnesTheIdentificationIsGivenAndAFeedGivenTwiceOrNoFeedForTheExpansionOfEverythingIsRefused(t *testing.T) {
	r := loaded(t)
	feeds := ExpandFeedsOf(map[int64]*Plan{7: r.plan})
	ident := IdentifyFeedsOf(map[int64]*Plan{7: r.plan})
	if len(feeds) == 0 || len(feeds) != len(ident) {
		t.Fatalf("%d feeds for the expansion and %d for the identification", len(feeds), len(ident))
	}
	for i, f := range feeds {
		if g := ident[i]; f.SrcNo != 7 || f.SrcNo != g.SrcNo || f.Feed != g.Feed || f.Creates != g.Creates || f.Retires != g.Retires {
			t.Errorf("the feed %+v of the expansion is not the feed %+v of the identification", f, g)
		}
	}
	if err := (ExpandStart{Full: true, Feeds: feeds}).Validate(); err != nil {
		t.Errorf("the expansion of everything with the feeds: %v", err)
	}
	if err := (ExpandStart{Full: true}).Validate(); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("the expansion of everything with no feeds takes no record of any source: %v", err)
	}
	if err := (ExpandStart{Feeds: append(feeds, feeds[0])}).Validate(); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a feed given twice: %v", err)
	}
	other := ExpandFeed{SrcNo: 8, Feed: feeds[0].Feed}
	if err := (ExpandStart{Feeds: append(feeds, other)}).Validate(); err != nil {
		t.Errorf("the same number of a feed of another source: %v", err)
	}
}
