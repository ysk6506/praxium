package ingest

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The third step of the new ingestion flow (ADR-0086, ADR-0108): which object each record of the run is. The records are the ones
// the comparison (step 2) made a reason for and the ones whose references are still not solved. The step reads the keys the records
// give (from the rows received, or made again by Go from the cells a raw record keeps), finds them in the identification index,
// groups the records that share a key and gives every group an object. This file is the port a state machine drives it by, and the
// part of the work that is Go's; nothing here knows a database.

// The limits of the step (starting values, ingestion-at-scale-identity.md 2.2 and 2.3).
const (
	// DefaultIdentifyRounds is how many rounds a run goes through before the records whose parents are still not found are left.
	DefaultIdentifyRounds = 16
	// DefaultIdentifyTurns is how many turns the search for the groups of one round takes before the groups that are not known to be
	// whole are left unresolved. A turn follows a chain of keys by jumps, so the turns fall, but they depend on the diameter of the group; what the turns do not end Go ends (SettleMax).
	DefaultIdentifyTurns = 40
	// DefaultSettlePairs is the most pairs of a record and a feed that Go holds in memory to end the search for the groups that the turns
	// did not end. A pair is a number and its keys (about 200 bytes of memory with its share of the tables of the union), so 200,000
	// pairs are about 40 MB and the work takes less than a second (ADR-0108). Above it the groups are left, as before.
	DefaultSettlePairs = 200_000
	// DefaultIdentifyPages is how many pages of the rows received a unit copies the keys of.
	DefaultIdentifyPages = 8
	// MaxIdentifyFeeds is how many feeds a mapping may have for the step: a record and a feed are one number (record * 1024 + feed).
	MaxIdentifyFeeds = 1024
)

// IdentifyKey is the place of a key of a feed in two orders: the order the feed tries its keys in (Ord) and the order the model gives
// the keys of the type, whose first key names a new object (ModelOrd).
type IdentifyKey struct {
	Ord      int
	ModelOrd int
}

// IdentifyFeed is what the identification needs to know of a feed that the rows received do not say. The number of a feed is its
// place in the mapping of its source, so the table of the feeds is by source and feed: a record is read by the feeds of its own source.
type IdentifyFeed struct {
	// SrcNo is the number of the source whose mapping the feed is of.
	SrcNo int64
	Feed  int
	// TypeNo is the number of the object type of the feed.
	TypeNo int
	// Creates says that a record of the feed may make an object. A feed that does not only finds the objects there are.
	Creates bool
	// Retires says that the feed may retire the objects it makes: the references its records give to the objects are retirable, and an
	// object that no open record supports any more and that has such a reference is a candidate for retirement (Expander).
	Retires bool
	// NameCol is the place of the cell the name of the object is read from among the cells of a record, or -1 when it is not known
	// (the feed gives no name, or reads a dataset a refinement makes).
	NameCol int
	Keys    []IdentifyKey
}

// IdentifyStart is what begins the identification: the feeds, the limits and the sizes of a unit (0 is the default). The feeds are the
// ones of every source whose plan is given, the source of the run first of them: a record that was left unresolved by another run is
// identified by the feeds of its own source, and a source whose plan is not given is not touched.
type IdentifyStart struct {
	Feeds []IdentifyFeed
	// Schemes are the numbers the rows received name the identifier schemes by, and the names the identifiers are kept by.
	Schemes    map[string]int
	BucketRows int
	Pages      int
	Rounds     int
	Turns      int
	// SettleMax is the most pairs Go ends the search for the groups of a round for (0 is DefaultSettlePairs).
	SettleMax int
	// SettlePut is how many pairs a write of the labels Go found takes (0 is the default, 5,000): a test makes it small, so that the
	// labels of a chain take several writes (ADR-0116).
	SettlePut int
}

// Validate checks what the step cannot begin without.
func (s IdentifyStart) Validate() error {
	type feedAt struct {
		src  int64
		feed int
	}
	seen := map[feedAt]bool{}
	for _, f := range s.Feeds {
		switch {
		case f.Feed < 0 || f.Feed >= MaxIdentifyFeeds:
			return kernel.Errorf(kernel.CodeInvalidValue, "the identification takes feeds numbered 0 to %d, not %d", MaxIdentifyFeeds-1, f.Feed)
		case seen[feedAt{f.SrcNo, f.Feed}]:
			return kernel.Errorf(kernel.CodeInvalidValue, "the feed %d of the source %d is named twice", f.Feed, f.SrcNo)
		case s.BucketRows < 0 || s.Pages < 0 || s.Rounds < 0 || s.Turns < 0 || s.SettleMax < 0 || s.SettlePut < 0:
			return kernel.Errorf(kernel.CodeInvalidValue, "the sizes and the limits of the identification are not negative")
		}
		seen[feedAt{f.SrcNo, f.Feed}] = true
	}
	return nil
}

// IdentifyNeed says what the unit the run is at waits for from Go.
type IdentifyNeed string

// The things a unit may wait for: nothing (the database does the unit), the keys of records that are made again from their cells, or
// the ids of the objects a round makes under a parent.
const (
	NeedNothing IdentifyNeed = ""
	NeedRebuild IdentifyNeed = "rebuild"
	NeedAsk     IdentifyNeed = "ask"
	// NeedSettle is the end of a search for the groups that its turns did not end: Go reads the pairs of the groups that are not whole
	// and tells which of them are one group.
	NeedSettle IdentifyNeed = "settle"
)

// IdentifyPlace is where the identification of a run stands.
type IdentifyPlace struct {
	Stage    RunStage
	Begun    bool
	Finished bool
	// Phase is the work (seed, keys, rebuild, rounds, claims, finish), Round and Turn say where in the rounds it is.
	Phase string
	Round int
	Turn  int
	Need  IdentifyNeed
}

// Done says whether every unit is done.
func (p IdentifyPlace) Done() bool { return p.Begun && p.Finished }

// RebuildRow is a record whose keys Go makes again from its cells: they are in the order of the columns of the dataset.
type RebuildRow struct {
	Rid     int64
	SrcNo   int64
	Dataset string
	Key     string
	Fields  []string
}

// RebuildPage is the records a unit waits for the keys of. A page that has no row is a unit that has nothing to do.
type RebuildPage struct {
	Unit int
	Rows []RebuildRow
}

// RebuiltRow is the keys, the ends of the links and the claims Go made of one record.
type RebuiltRow struct {
	Rid     int64
	Keys    []StageKey
	Parents []StageParent
	Ends    []StageEnd
	Claims  []StageClaim
}

// AskRow is a new object whose key has a context: its parent is found, and Go makes its id and says whether it has a name.
type AskRow struct {
	Round     int
	Label     int64
	Rid       int64
	SrcNo     int64
	Dataset   string
	Key       string
	Fields    []string
	Feed      int
	Ord       int
	ParentRef string
}

// Answer is what Go says of a new object under a parent: its id, or that it has no name and is not made.
type Answer struct {
	Round   int
	Label   int64
	Ref     string
	Missing bool
}

// SettlePair is a pair of a record and a feed that is in a group the turns did not end, with the keys it has (each as one text).
type SettlePair struct {
	Pid  int64
	Keys []string
}

// SettlePage is a page of the pairs of the groups that are not whole, the groups with their labels in a range of the labels: Next is
// where to go on from, Done says there is no more, and Written is the last pair whose label a run that was stopped wrote already.
type SettlePage struct {
	Pairs   []SettlePair
	Next    int64
	Done    bool
	Written int64
}

// SettledPair is the label a group of pairs got: the smallest pair of the group.
type SettledPair struct {
	Pid   int64
	Label int64
}

// SettleEnd says how a page of settled pairs ends the work: Last is the last page, and Skipped says that Go wrote none because the
// pairs were more than it holds (the groups are left, as when no end is looked for).
type SettleEnd struct {
	Last    bool
	Skipped bool
}

// IdentifyCounts is what the identification did, in the numbers a report shows.
type IdentifyCounts struct {
	Seeded     int `json:"seeded"`
	Rebuilt    int `json:"rebuilt"`
	Skipped    int `json:"skipped"`
	Rounds     int `json:"rounds"`
	Bound      int `json:"bound"`
	NewObjects int `json:"new_objects"`
	NewKeys    int `json:"new_keys"`
	Dropped    int `json:"dropped"`
	// Left is how many records of the references that are not solved were not looked at, because no plan was given for their source.
	Left int `json:"left"`
	// Unsettled is how many pairs of a record and a feed a round did not bind, because the search for their groups was stopped by its
	// limit before it was done: they are left as references that are not solved.
	Unsettled  int `json:"unsettled"`
	Unresolved int `json:"unresolved"`
	Lost       int `json:"lost_claims"`
}

// Identifier is the port of the identification. Every write names the attempt of the lease, and a write whose attempt is not the
// run's any more is refused with CodeRunLeaseLost and changes nothing, as for RunLedger. A unit and the place that moves past it
// are one transaction, so a unit is done once or not at all and a run that is taken over goes on from the next unit.
//
// BeginIdentify starts the step of a run whose comparison is done; it empties nothing. IdentifyStep does the unit the place says (a
// place that waits for Go is refused with CodeRunStage). RebuildPage gives the records of a unit that waits for rebuilt keys, and
// PutRebuilt takes them and moves on; AskPage and PutAnswers do the same for the objects under a parent.
type Identifier interface {
	BeginIdentify(ctx context.Context, lease RunLease, start IdentifyStart) error
	IdentifyPlace(ctx context.Context, no int64) (IdentifyPlace, error)
	IdentifyStep(ctx context.Context, lease RunLease) error
	RebuildPage(ctx context.Context, lease RunLease) (RebuildPage, error)
	PutRebuilt(ctx context.Context, lease RunLease, unit int, rows []RebuiltRow) error
	SettleRead(ctx context.Context, lease RunLease, after int64) (SettlePage, error)
	PutSettled(ctx context.Context, lease RunLease, rows []SettledPair, end SettleEnd) error
	AskPage(ctx context.Context, lease RunLease) ([]AskRow, error)
	PutAnswers(ctx context.Context, lease RunLease, answers []Answer) error
	IdentifyReport(ctx context.Context, no int64) (IdentifyCounts, error)
}

// KeyMaker is what Go does for the identification.
type KeyMaker interface {
	// Rebuild makes the keys of the records of one dataset of one source from their cells.
	Rebuild(srcNo int64, dataset string, rows []RebuildRow) ([]RebuiltRow, error)
	// Answer makes the id of a new object under a parent and says whether it has a name.
	Answer(row AskRow) (Answer, error)
}

// RunIdentify does the whole identification of a run, or what is left of it: it begins it when it has not begun, and does the units
// from where the run is, giving the rows to the key maker where a unit waits for it. The context is looked at between units.
func RunIdentify(ctx context.Context, d Identifier, lease RunLease, start IdentifyStart, keys KeyMaker) (IdentifyCounts, error) {
	if err := start.Validate(); err != nil {
		return IdentifyCounts{}, err
	}
	place, err := d.IdentifyPlace(ctx, lease.No)
	if err != nil {
		return IdentifyCounts{}, err
	}
	if place.Stage != RunComputing {
		return IdentifyCounts{}, kernel.Errorf(CodeRunStage, "the run %d is %s: the identification is the work of a run that is computing", lease.No, place.Stage)
	}
	if !place.Begun {
		if err := d.BeginIdentify(ctx, lease, start); err != nil {
			return IdentifyCounts{}, err
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return IdentifyCounts{}, err
		}
		if place, err = d.IdentifyPlace(ctx, lease.No); err != nil {
			return IdentifyCounts{}, err
		}
		if place.Done() {
			return d.IdentifyReport(ctx, lease.No)
		}
		if err := doUnit(ctx, d, lease, place.Need, keys, start); err != nil {
			return IdentifyCounts{}, err
		}
	}
}

func doUnit(ctx context.Context, d Identifier, lease RunLease, need IdentifyNeed, keys KeyMaker, start IdentifyStart) error {
	switch need {
	case NeedRebuild:
		page, err := d.RebuildPage(ctx, lease)
		if err != nil {
			return err
		}
		rows, err := rebuildByDataset(keys, page.Rows)
		if err != nil {
			return err
		}
		return d.PutRebuilt(ctx, lease, page.Unit, rows)
	case NeedSettle:
		return settleGroupsBy(ctx, d, lease, start.SettleMax, start.SettlePut)
	case NeedAsk:
		asks, err := d.AskPage(ctx, lease)
		if err != nil {
			return err
		}
		answers := make([]Answer, 0, len(asks))
		for _, a := range asks {
			ans, err := keys.Answer(a)
			if err != nil {
				return err
			}
			answers = append(answers, ans)
		}
		return d.PutAnswers(ctx, lease, answers)
	}
	return d.IdentifyStep(ctx, lease)
}

// rebuildByDataset gives the rows to the key maker by source and dataset, and gives back what it made in the order of the rows.
func rebuildByDataset(keys KeyMaker, rows []RebuildRow) ([]RebuiltRow, error) {
	type group struct {
		src     int64
		dataset string
	}
	var order []group
	by := map[group][]RebuildRow{}
	for _, r := range rows {
		g := group{r.SrcNo, r.Dataset}
		if _, ok := by[g]; !ok {
			order = append(order, g)
		}
		by[g] = append(by[g], r)
	}
	var out []RebuiltRow
	for _, g := range order {
		made, err := keys.Rebuild(g.src, g.dataset, by[g])
		if err != nil {
			return nil, err
		}
		out = append(out, made...)
	}
	return out, nil
}
