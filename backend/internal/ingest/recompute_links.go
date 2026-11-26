package ingest

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The fourth step of the new ingestion flow, for the links (ADR-0122), after the objects (RunRecompute): the ends of the links the records
// of the run have are resolved against the To side (the objects the records that have the same end key stand for), and a link is made
// from the object of a record of the From side to the object found. The database reads and resolves (the ends of a page, the objects
// each key finds, the numbers); Go makes what is not SQL (ingestion-at-scale.md P3): the link id, the attributes, the grade, the
// references of the sources, the hashes, and the problems with their words. The database writes what Go made to stage_link, calc_ref and
// calc_problem. This file is the port a state machine drives it by.
//
// The step goes in phases, each of them in pages by a place that is kept: the records that the run only reads again are given the ends their
// references say (ends: the identification made the ends of the records it read, and these are read like them), the To ends of the run
// are written as references (to), the
// From ends are resolved and the links made (from), the records that made a link that another record made as well, and were not used,
// are said to have made it with other attributes (conflicts), and the cells that gave no value to a link that was made are said (cells).

// The phases of the step.
const (
	LinksPhaseEnds      = "ends"
	LinksPhaseTo        = "to"
	LinksPhaseFrom      = "from"
	LinksPhaseConflicts = "conflicts"
	LinksPhaseCells     = "cells"
	LinksPhaseDone      = "done"
)

// DefaultRecomputeLinkLines is how many ends a page of the links reads.
const DefaultRecomputeLinkLines = DefaultRecomputeLines

// RecomputeLinkDef says which feeds the ends of a link definition of the plan of a source are the objects of: the number of the
// definition (its place in the mapping) and the numbers of the feeds of its From and To side. The database finds the object of an end by
// the feed.
type RecomputeLinkDef struct {
	SrcNo    int64
	Def      int
	FromFeed int
	ToFeed   int
}

// RecomputeLinkDefsOf is the link definitions of the plans of the sources, by the number of the source and of the definition.
func RecomputeLinkDefsOf(plans map[int64]*Plan) []RecomputeLinkDef {
	srcs := make([]int64, 0, len(plans))
	for src := range plans {
		srcs = append(srcs, src)
	}
	sort.Slice(srcs, func(i, j int) bool { return srcs[i] < srcs[j] })
	var out []RecomputeLinkDef
	for _, src := range srcs {
		for def, lp := range plans[src].links {
			out = append(out, RecomputeLinkDef{SrcNo: src, Def: def, FromFeed: lp.from.index, ToFeed: lp.to.index})
		}
	}
	return out
}

// RecomputeLinksStart is what begins the step: the number of ends of a page (0 is the default) and the link definitions of the sources whose
// plan the run was given.
type RecomputeLinksStart struct {
	Lines int
	Defs  []RecomputeLinkDef
}

// DefsText is the definitions as one text in the order of the sources and the definitions, to compare two starts and to keep in a place.
func (s RecomputeLinksStart) DefsText() string {
	defs := append([]RecomputeLinkDef(nil), s.Defs...)
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].SrcNo != defs[j].SrcNo {
			return defs[i].SrcNo < defs[j].SrcNo
		}
		return defs[i].Def < defs[j].Def
	})
	var b strings.Builder
	for _, d := range defs {
		b.WriteString(strconv.FormatInt(d.SrcNo, 10) + "/" + strconv.Itoa(d.Def) + "=" + strconv.Itoa(d.FromFeed) + ">" + strconv.Itoa(d.ToFeed) + ";")
	}
	return b.String()
}

// Validate checks what the step cannot begin without.
func (s RecomputeLinksStart) Validate() error {
	if s.Lines < 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the size of a page of the computation of the links is not negative")
	}
	type at struct {
		src int64
		def int
	}
	seen := map[at]bool{}
	for _, d := range s.Defs {
		if seen[at{d.SrcNo, d.Def}] {
			return kernel.Errorf(kernel.CodeInvalidValue, "the link definition %d of the source %d is given twice to the computation of the links", d.Def, d.SrcNo)
		}
		seen[at{d.SrcNo, d.Def}] = true
	}
	return nil
}

// LinkCursor is where a phase stands: the last end done (by the link definition, the key and the record) in the phases that go through the
// ends, and the last link (its id) in the phases that go through the links.
type LinkCursor struct {
	Def  int    `json:"def"`
	Hash []byte `json:"hash,omitempty"`
	Rid  int64  `json:"rid"`
	Ref  string `json:"ref,omitempty"`
}

// RecomputeLinksPlace is where the step stands: whether it has begun and is finished, the phase and how many pages are done. Defs (as
// DefsText) is the one the step began with, which a server that takes the run over must be given again.
type RecomputeLinksPlace struct {
	Stage    RunStage
	Begun    bool
	Finished bool
	Phase    string
	Unit     int
	Cursor   LinkCursor
	Defs     string
}

// Done says whether every page of every phase is done.
func (p RecomputeLinksPlace) Done() bool { return p.Begun && p.Finished }

// RecomputeLinksCounts is what the step did, in the numbers a report shows. They are the rows the statements wrote (counted from what the
// statement gave back): Links the rows of stage_link, Refs the references (calc_ref) of the ends, Problems the rows of calc_problem, Keys
// the keys the To side was counted for (calc_tokey). Ends is the ends of the From side that were read and Skipped the ones that made no
// reference (a record that has no object, or whose cells are not the ones the dataset declares now, or whose end key is not one), Lost the
// records whose end made a link that another record made as well, and that was not used.
type RecomputeLinksCounts struct {
	Carried   int `json:"carried"`
	Ends      int `json:"ends"`
	Skipped   int `json:"skipped"`
	Keys      int `json:"keys"`
	Links     int `json:"links"`
	Refs      int `json:"refs"`
	Lost      int `json:"lost"`
	Missing   int `json:"missing"`
	Ambiguous int `json:"ambiguous"`
	Conflicts int `json:"conflicts"`
	Cells     int `json:"cells"`
	Problems  int `json:"problems"`
}

// RecomputeEnd is an end of the From side of a page, with the object of its record and what the To side has for its key: Targets is the
// number of objects found (0, 1, or 2 for more than one) and ToObj the one found when it is one. Fields are the cells of the record (nil
// when they were kept under another declaration than the dataset has now).
type RecomputeEnd struct {
	Rid       int64
	SrcNo     int64
	DatasetNo int64
	Dataset   string
	Key       string
	Fields    []string
	Def       int
	Hash      KeyHash
	Held      bool
	FromObj   int64
	FromRef   string
	Targets   int
	ToObj     int64
	ToRef     string
}

// LinkKeyCount is what the To side has for a key: how many objects (0, 1, or 2 for more than one), and one of them with its id.
type LinkKeyCount struct {
	SrcNo   int64
	Def     int
	Hash    KeyHash
	Targets int
	Obj     int64
	Ref     string
}

// RecomputeLinksPage is a page of a phase. In the phase from it has the ends and the counts of the keys that no page counted before; in the
// other phases it is the place the page stands for (Through is the last place of it, Last says that none is left after it).
type RecomputeLinksPage struct {
	Unit    int
	Phase   string
	Through LinkCursor
	Last    bool
	Ends    []RecomputeEnd
	Keys    []LinkKeyCount
}

// LinkOrder is what decides, of the records that make the same link, the one that is used: the one that comes last (ADR-0122). The source,
// the link definition, whether the end is of a record that a rule filtered out (such a record comes after the others), the dataset by its
// number and the natural key by its bytes. No two records have the same order.
type LinkOrder struct {
	SrcNo     int64
	Def       int
	Held      bool
	DatasetNo int64
	Key       string
}

// Before says whether the record of o comes before the record of p.
func (o LinkOrder) Before(p LinkOrder) bool {
	switch {
	case o.SrcNo != p.SrcNo:
		return o.SrcNo < p.SrcNo
	case o.Def != p.Def:
		return o.Def < p.Def
	case o.Held != p.Held:
		return !o.Held
	case o.DatasetNo != p.DatasetNo:
		return o.DatasetNo < p.DatasetNo
	}
	return strings.Compare(o.Key, p.Key) < 0
}

// LinkRow is the row of stage_link that Go made of the end of a record: everything but the number the link gets.
type LinkRow struct {
	Ref         string
	TypeNo      int
	Def         int
	Src, Dst    int64
	Grade       string
	Basis       string
	Doc         []byte
	SourceRefs  []byte
	ContentHash kernel.Hash
	RowHash     kernel.Hash
}

// LinkWho is a record that made a link, with what the words of a problem about it need: its place in the order, the cells of the link that
// gave no value, a text that says the attributes it gave and a text that compares them (Sig), and the names of the link type, of the
// definition and of the dataset.
type LinkWho struct {
	Rid      int64
	Order    LinkOrder
	Sig      string
	Shown    string
	LinkName string
	DefID    string
	Dataset  string
	Left     []string
}

// LinkCandidate is the link a record's end makes, as a candidate for the link: when several records make the same link (the same Ref)
// the one that comes last in the order is used.
type LinkCandidate struct {
	Row LinkRow
	Who LinkWho
}

// LinkEndResult says what became of an end of the From side: it made the link of Ref (when it is not empty), or it found none or more
// than one object. The reference of the record is written for it.
type LinkEndResult struct {
	Rid  int64
	Def  int
	Hash KeyHash
	Held bool
	Ref  string
}

// RecomputedLinksPage is what Go made of a page: the counts it passes on, the ends and what they made, the candidates and the problems of
// the ends that found no object or too many (a record that a rule filtered out has none).
type RecomputedLinksPage struct {
	Unit       int
	Phase      string
	Through    LinkCursor
	Last       bool
	Keys       []LinkKeyCount
	Results    []LinkEndResult
	Candidates []LinkCandidate
	Problems   []RecomputedProblem
	Skipped    int
	Ends       int
}

// LinkRecomputer is the port of the step of the links. Every call names the attempt of the lease, and a call whose attempt is not the
// run's any more is refused with CodeRunLeaseLost and changes nothing (reads too). A page and the place that moves past it are one
// transaction, so a page is done once or not at all, and a page that is put twice is refused with CodeRunStage.
//
// BeginRecomputeLinks starts the step of a run whose objects are computed. RecomputeLinksPage reads the page the place says (it changes
// nothing) and PutRecomputedLinks takes what Go made of it.
type LinkRecomputer interface {
	BeginRecomputeLinks(ctx context.Context, lease RunLease, start RecomputeLinksStart) error
	RecomputeLinksPlace(ctx context.Context, lease RunLease) (RecomputeLinksPlace, error)
	RecomputeLinksPage(ctx context.Context, lease RunLease) (RecomputeLinksPage, error)
	PutRecomputedLinks(ctx context.Context, lease RunLease, page RecomputedLinksPage) error
	RecomputeLinksReport(ctx context.Context, lease RunLease) (RecomputeLinksCounts, error)
}

// LinkMaker is what Go does for the step: it makes the links of a page.
type LinkMaker interface {
	MakeLinks(page RecomputeLinksPage) (RecomputedLinksPage, error)
}

// RunRecomputeLinks does the whole step, or what is left of it: it begins it when it has not begun, and does the pages from where the run
// is. The context is looked at between pages.
func RunRecomputeLinks(ctx context.Context, d LinkRecomputer, lease RunLease, start RecomputeLinksStart, maker LinkMaker) (RecomputeLinksCounts, error) {
	if err := start.Validate(); err != nil {
		return RecomputeLinksCounts{}, err
	}
	place, err := d.RecomputeLinksPlace(ctx, lease)
	if err != nil {
		return RecomputeLinksCounts{}, err
	}
	if place.Stage != RunComputing {
		return RecomputeLinksCounts{}, kernel.Errorf(CodeRunStage, "the run %d is %s: the computation of the links is the work of a run that is computing", lease.No, place.Stage)
	}
	if !place.Begun {
		if err := d.BeginRecomputeLinks(ctx, lease, start); err != nil {
			return RecomputeLinksCounts{}, err
		}
	} else if place.Defs != start.DefsText() {
		// a server that takes the run over is given the definitions the run began with, or the ends are read for other feeds
		return RecomputeLinksCounts{}, kernel.Errorf(kernel.CodeConflict, "the computation of the links of the run %d began with the definitions %q, not %q", lease.No, place.Defs, start.DefsText())
	}
	for {
		done, err := recomputeOneLinksPage(ctx, d, lease, maker)
		if err != nil {
			return RecomputeLinksCounts{}, err
		}
		if done {
			return d.RecomputeLinksReport(ctx, lease)
		}
	}
}

// recomputeOneLinksPage does the page the place says, and says when there is none left.
func recomputeOneLinksPage(ctx context.Context, d LinkRecomputer, lease RunLease, maker LinkMaker) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	place, err := d.RecomputeLinksPlace(ctx, lease)
	if err != nil {
		return false, err
	}
	if place.Done() {
		return true, nil
	}
	page, err := d.RecomputeLinksPage(ctx, lease)
	if err != nil {
		return false, err
	}
	made, err := maker.MakeLinks(page)
	if err != nil {
		return false, err
	}
	return false, d.PutRecomputedLinks(ctx, lease, made)
}
