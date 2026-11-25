package ingest

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The expansion of the new ingestion flow (ADR-0086, ADR-0114): between the identification (step 3) and the new computation of the
// objects (step 4) the set of the run is widened to what the records of the run touch (ingestion-at-scale-identity.md 3, the five rules
// of the expansion: the objects the records were bound to, the end keys of the links, the ends held to one, the identifiers and the
// retirement). A record that changes may leave an object, take the place of a link another record made, share an identifier
// with another object or be the last one an object stands on, and the others have to be looked at again for the result to be the one
// a computation of everything would give (I2). The step is SQL only: this file is the port a state machine drives it by.

// DefaultExpandRows is how many records of the run (or objects, for the rules that go through the objects) a unit of the expansion looks at.
const DefaultExpandRows = 2000

// ExpandCard says which end of a link type holds at most one link of the type, as the bindings of the type say it: the links of a
// type that have the same end object compete for it, and the one with the smaller id is made (the other is a problem of the record).
// The binding of a link depends on the types of its ends, which the expansion does not know, so an end that any binding of the type
// holds to one is held to one here: the expansion may look at more than it has to, never at less.
type ExpandCard struct {
	LinkTypeNo int
	// FromOne says that an object has at most one link of the type that starts at it, ToOne that it has at most one that ends at it.
	FromOne bool
	ToOne   bool
}

// ExpandStart is what begins the expansion: the sizes of a unit (0 is the default), the link types that hold an end to one, and whether
// it is the expansion of everything.
type ExpandStart struct {
	// Full makes every open record of the sources of the KG a record of the run (the computation of everything, ingestion-at-scale.md
	// P4): the rules then go through all of them, so that what is expanded is everything the records say.
	Full  bool
	Rows  int
	Cards []ExpandCard
}

// Validate checks what the step cannot begin without.
func (s ExpandStart) Validate() error {
	if s.Rows < 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the size of a unit of the expansion is not negative")
	}
	seen := map[int]bool{}
	for _, c := range s.Cards {
		if seen[c.LinkTypeNo] {
			return kernel.Errorf(kernel.CodeInvalidValue, "the link type %d is given twice to the expansion", c.LinkTypeNo)
		}
		seen[c.LinkTypeNo] = true
	}
	return nil
}

// ExpandCards says, for every link type of the model that holds an end to one, which end it is (ExpandStart.Cards).
func ExpandCards(reg *model.Registry) []ExpandCard {
	var out []ExpandCard
	for _, lt := range reg.LinkTypes() {
		c := ExpandCard{LinkTypeNo: lt.LinkTypeNo}
		for _, b := range lt.Bindings {
			c.FromOne = c.FromOne || b.Cardinality == model.CardManyToOne || b.Cardinality == model.CardOneToOne
			c.ToOne = c.ToOne || b.Cardinality == model.CardOneToMany || b.Cardinality == model.CardOneToOne
		}
		if c.FromOne || c.ToOne {
			out = append(out, c)
		}
	}
	return out
}

// ExpandCards says what the expansion needs of the link types of the model of the plan.
func (p *Plan) ExpandCards() []ExpandCard { return ExpandCards(p.reg) }

// ExpandPlace is where the expansion of a run stands.
type ExpandPlace struct {
	Stage    RunStage
	Begun    bool
	Finished bool
	// Rule is the rule the next unit is of, Unit how many units are done.
	Rule string
	Unit int
}

// Done says whether every unit is done.
func (p ExpandPlace) Done() bool { return p.Begun && p.Finished }

// ExpandCounts is what the expansion added, in the numbers a report shows.
type ExpandCounts struct {
	// Everything is the records of the sources that the computation of everything made records of the run.
	Everything int `json:"everything"`
	// OldObjects is the objects that records of the run were bound to before, Retiring how many of them are candidates for retirement
	// (no open record stands under them and a reference to them is retirable).
	OldObjects int `json:"old_objects"`
	Retiring   int `json:"retiring"`
	// RetireRecords is the records that made the links of a candidate, KeyRecords the records that read a link end key that a record of
	// the run gave or had, LimitRecords the records that made a link that competes for an end held to one.
	RetireRecords int `json:"retire_records"`
	KeyRecords    int `json:"key_records"`
	LimitRecords  int `json:"limit_records"`
	// ClaimObjects is the objects that claim an identifier a record of the run claims.
	ClaimObjects int `json:"claim_objects"`
}

// Expander is the port of the expansion. Every call names the attempt of the lease, and a call whose attempt is not the run's any more
// is refused with CodeRunLeaseLost and changes nothing (reads too), as for RunLedger. A unit and the place that moves past it are one
// transaction, so a unit is done once or not at all.
//
// BeginExpand starts the step of a run whose identification is done; it empties nothing. ExpandStep does the unit the place says; a
// place that is done is refused with CodeRunStage.
type Expander interface {
	BeginExpand(ctx context.Context, lease RunLease, start ExpandStart) error
	ExpandPlace(ctx context.Context, lease RunLease) (ExpandPlace, error)
	ExpandStep(ctx context.Context, lease RunLease) error
	ExpandReport(ctx context.Context, lease RunLease) (ExpandCounts, error)
}

// RunExpand does the whole expansion of a run, or what is left of it: it begins it when it has not begun, and does the units from
// where the run is. The context is looked at between units.
func RunExpand(ctx context.Context, d Expander, lease RunLease, start ExpandStart) (ExpandCounts, error) {
	if err := start.Validate(); err != nil {
		return ExpandCounts{}, err
	}
	place, err := d.ExpandPlace(ctx, lease)
	if err != nil {
		return ExpandCounts{}, err
	}
	if place.Stage != RunComputing {
		return ExpandCounts{}, kernel.Errorf(CodeRunStage, "the run %d is %s: the expansion is the work of a run that is computing", lease.No, place.Stage)
	}
	if !place.Begun {
		if err := d.BeginExpand(ctx, lease, start); err != nil {
			return ExpandCounts{}, err
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return ExpandCounts{}, err
		}
		if place, err = d.ExpandPlace(ctx, lease); err != nil {
			return ExpandCounts{}, err
		}
		if place.Done() {
			return d.ExpandReport(ctx, lease)
		}
		if err := d.ExpandStep(ctx, lease); err != nil {
			return ExpandCounts{}, err
		}
	}
}
