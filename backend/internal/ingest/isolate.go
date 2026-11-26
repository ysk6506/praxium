package ingest

import (
	"context"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The fifth step of the new ingestion flow, its first part (ADR-0123): before the difference reads what the run staged, the rows the KG
// cannot have are taken out of it, in the order ingestion-at-scale-identity.md 4 gives, each check once: ① an identifier that another
// object keeps (the line of the object that loses, not the object), ② a link whose end is neither an object of the KG nor one the run
// makes (and a link whose end retires, which goes with the object and is no problem), ③ a link that would give an object a second link
// of a binding that holds that end to one. What is taken out is written to calc_isolated, the staged row is taken away and the reference
// of the record that made it is not solved any more, so the next run reads the record again. The database finds what to look at; Go
// decides what wins (the rule of the identifiers and the order of the places are Go's, so a test holds them as pure functions) and
// makes what is not SQL (the hash of an identifier, the binding of a link). This file is the port a state machine drives it by.

// The phases of the step: the identifiers, the ends, and the cardinality in two passes (the links the KG has first, then the others).
const (
	IsolatePhaseIdents = "idents"
	IsolatePhaseEnds   = "ends"
	IsolatePhaseKept   = "kept"
	IsolatePhaseFresh  = "fresh"
	IsolatePhaseDone   = "done"
)

// DefaultIsolateLines is how many staged rows a page of the step reads.
const DefaultIsolateLines = DefaultRecomputeLines

// IsolateStart is what begins the step: the number of rows of a page (0 is the default) and the link types that hold an end to one (the
// ones whose links the check of the cardinality looks at: ExpandCards of the model).
type IsolateStart struct {
	Lines int
	Cards []ExpandCard
}

// Validate checks what the step cannot begin without.
func (s IsolateStart) Validate() error {
	if s.Lines < 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the size of a page of the fifth step is not negative")
	}
	seen := map[int]bool{}
	for _, c := range s.Cards {
		if seen[c.LinkTypeNo] {
			return kernel.Errorf(kernel.CodeInvalidValue, "the link type %d is given twice to the fifth step", c.LinkTypeNo)
		}
		seen[c.LinkTypeNo] = true
	}
	return nil
}

// IsolateCursor is where a phase stands: the last identifier (scheme, value, scope), the last link number, or the last link id.
type IsolateCursor struct {
	Scheme string `json:"scheme,omitempty"`
	Value  string `json:"value,omitempty"`
	Scope  string `json:"scope,omitempty"`
	No     int64  `json:"no,omitempty"`
	Ref    string `json:"ref,omitempty"`
}

// IsolatePlace is where the step stands.
type IsolatePlace struct {
	Stage    RunStage
	Begun    bool
	Finished bool
	Phase    string
	Unit     int
	Cursor   IsolateCursor
}

// Done says whether every page of every phase is done.
func (p IsolatePlace) Done() bool { return p.Begun && p.Finished }

// IsolateCounts is what the step did, in the numbers a report shows: Keys the identifiers that more than one object had, Idents the
// identifier lines taken out, Missing the links whose end is not there, Retiring the links that go with an object that retires, Seats the
// places of the cardinality the run's links took, Cardinality the links that lost a place, Refs the references of records that were
// changed or written (not solved any more), Problems the problems written.
type IsolateCounts struct {
	Keys        int `json:"keys"`
	Idents      int `json:"idents"`
	Missing     int `json:"missing"`
	Retiring    int `json:"retiring"`
	Seats       int `json:"seats"`
	Cardinality int `json:"cardinality"`
	Refs        int `json:"refs"`
	Problems    int `json:"problems"`
}

// IdentClaimant is an object that has an identifier: one whose staged identifiers have it (Staged), and one that has it in the KG and
// keeps it (Holds: the run does not compute the object, which keeps what it has, or its staged identifiers have it too).
type IdentClaimant struct {
	ObjNo  int64
	Ref    string
	Staged bool
	Holds  bool
}

// IdentKey is an identifier that more than one object has, with the objects.
type IdentKey struct {
	Scheme, Value, Scope string
	Claimants            []IdentClaimant
}

// SeatLink is a link at a place of the cardinality: its number, id, type and ends, with the type and the name of each end object.
type SeatLink struct {
	LinkNo           int64
	Ref              string
	TypeNo           int
	Src, Dst         int64
	SrcType, DstType int
	SrcName, DstName string
}

// Seat is a place that holds one link (the side of the link: SeatOut is its start, SeatIn its end; the object there; the binding) and the
// link that has it.
type Seat struct {
	Side   int16
	EndNo  int64
	BindID string
	LinkNo int64
}

// The sides of a place.
const (
	SeatOut int16 = 1
	SeatIn  int16 = 2
)

// IsolatePage is a page of a phase. In the phase idents it has the identifiers of the page that more than one object has; in the phases
// kept and fresh the links of the page in the order of their ids, the links the KG keeps at their ends that the run does not touch
// (Fixed: they keep their places), and the places at their ends that earlier pages gave (Taken). Schemes are the numbers of the schemes
// the references of the claims name them by.
type IsolatePage struct {
	Unit    int
	Phase   string
	Through IsolateCursor
	Last    bool
	Schemes map[string]int
	Keys    []IdentKey
	Links   []SeatLink
	Fixed   []SeatLink
	Taken   []Seat
}

// IdentLoss is an identifier line that lost: the object, the identifier, the number and the hash the references name it by, and why.
type IdentLoss struct {
	ObjNo                int64
	Scheme, Value, Scope string
	SchemeNo             int
	Hash                 KeyHash
	ByKG                 bool
}

// SeatLoss is a link that lost a place, and the reason as a person reads it.
type SeatLoss struct {
	LinkNo int64
	Ref    string
	Reason string
}

// IsolatedPage is what Go made of a page: the identifier lines that lost, the places the links of the page took, and the links that lost.
type IsolatedPage struct {
	Unit    int
	Phase   string
	Through IsolateCursor
	Last    bool
	Keys    int
	Idents  []IdentLoss
	Seats   []Seat
	Lost    []SeatLoss
}

// Isolator is the port of the step. Every call names the attempt of the lease, and a call whose attempt is not the run's any more is
// refused with CodeRunLeaseLost and changes nothing (reads too). A page and the place that moves past it are one transaction, so a page is
// done once or not at all, and a page that is put twice is refused with CodeRunStage. BeginIsolate starts the step of a run whose fourth
// step (the objects and the links) is done.
type Isolator interface {
	BeginIsolate(ctx context.Context, lease RunLease, start IsolateStart) error
	IsolatePlace(ctx context.Context, lease RunLease) (IsolatePlace, error)
	IsolatePage(ctx context.Context, lease RunLease) (IsolatePage, error)
	PutIsolated(ctx context.Context, lease RunLease, page IsolatedPage) error
	IsolateReport(ctx context.Context, lease RunLease) (IsolateCounts, error)
}

// IsolateMaker is what Go does for the step: it decides a page.
type IsolateMaker interface {
	MakeIsolated(page IsolatePage) (IsolatedPage, error)
}

// RunIsolate does the step, or what is left of it: it begins it when it has not begun, and does the pages from where the run is. The
// context is looked at between pages.
func RunIsolate(ctx context.Context, d Isolator, lease RunLease, start IsolateStart, maker IsolateMaker) (IsolateCounts, error) {
	if err := start.Validate(); err != nil {
		return IsolateCounts{}, err
	}
	place, err := d.IsolatePlace(ctx, lease)
	if err != nil {
		return IsolateCounts{}, err
	}
	if place.Stage != RunComputing {
		return IsolateCounts{}, kernel.Errorf(CodeRunStage, "the run %d is %s: the fifth step is the work of a run that is computing", lease.No, place.Stage)
	}
	if !place.Begun {
		if err := d.BeginIsolate(ctx, lease, start); err != nil {
			return IsolateCounts{}, err
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return IsolateCounts{}, err
		}
		place, err := d.IsolatePlace(ctx, lease)
		if err != nil {
			return IsolateCounts{}, err
		}
		if place.Done() {
			return d.IsolateReport(ctx, lease)
		}
		page, err := d.IsolatePage(ctx, lease)
		if err != nil {
			return IsolateCounts{}, err
		}
		made, err := maker.MakeIsolated(page)
		if err != nil {
			return IsolateCounts{}, err
		}
		if err := d.PutIsolated(ctx, lease, made); err != nil {
			return IsolateCounts{}, err
		}
	}
}

// ElectIdent decides who keeps an identifier that more than one object has (ingestion-at-scale-identity.md 2.4): the object that has it in
// the KG and keeps it, else the one of the smallest id among the ones whose staged identifiers have it. Should two objects hold it in the
// KG (a KG that broke the rule before), the one of the smaller id keeps it. The objects that lose are the staged ones that do not keep it,
// in the order of their ids; byKG says the one that keeps it holds it in the KG.
func ElectIdent(k IdentKey) (winner int64, byKG bool, losers []int64) {
	list := append([]IdentClaimant(nil), k.Claimants...)
	sort.Slice(list, func(i, j int) bool {
		if list[i].Holds != list[j].Holds {
			return list[i].Holds
		}
		if list[i].Ref != list[j].Ref {
			return list[i].Ref < list[j].Ref
		}
		return list[i].ObjNo < list[j].ObjNo
	})
	if len(list) == 0 {
		return 0, false, nil
	}
	winner, byKG = list[0].ObjNo, list[0].Holds
	for _, c := range list[1:] {
		if c.Staged && c.ObjNo != winner {
			losers = append(losers, c.ObjNo)
		}
	}
	return winner, byKG, losers
}
