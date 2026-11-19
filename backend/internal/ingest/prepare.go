package ingest

import (
	"slices"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Preparer turns the pages of one dataset into prepared rows (stage.go). It is the row-by-row work of the receiving
// step, with no database in it: the refinements and the natural key, the hash of the raw row, the identity keys with
// the parents they need, the keys of the ends of the links, the identifiers a row claims and the problems of the row,
// all by the functions the current flow uses for the same things. What a key means for an object, and which object it
// names, is not decided here.
//
// A Preparer is made for one dataset of a plan and used for the pages of one read, in order: it remembers only that the
// hint about a row with no key was given once. It is not safe for use by several goroutines.
type Preparer struct {
	plan    *Plan
	dataset Dataset
	refs    []Refinement
	ids     map[string]int
	feeds   []preparedFeed
	links   []preparedLink
	hinted  bool
	buf     []rowView
}

// rowView is a row as a feed or a link end sees it: after the refinements it reads, and whether a rule filtered it out
// on the way (then the row is held, kept for an object that is waited for).
type rowView struct {
	row  Row
	held bool
}

type preparedFeed struct {
	fp   *feedPlan
	keys []preparedKey
}

// preparedKey is an identity key with what forming it takes: the plan of its own parts as a feed of the type of the
// object, and for a context key the chain of the parents, nearest first.
type preparedKey struct {
	kp      *keyPlan
	own     *feedPlan
	parents []preparedParent
}

type preparedParent struct {
	link model.LinkType
	typ  model.ObjectType
	plan *keyPlan
	own  *feedPlan
}

type preparedLink struct {
	def int
	lp  *linkPlan
}

// NewPreparer makes the Preparer of one dataset of a plan.
func NewPreparer(p *Plan, dataset string) (*Preparer, error) {
	i := slices.IndexFunc(p.Source.Datasets, func(d Dataset) bool { return d.ID == dataset })
	if i < 0 {
		return nil, kernel.Errorf(kernel.CodeNotFound, "the source has no dataset %s", dataset)
	}
	pr := &Preparer{plan: p, dataset: p.Source.Datasets[i], ids: map[string]int{dataset: 0}}
	for _, r := range p.refs {
		if p.roots[r.ID] == dataset {
			pr.ids[r.ID] = len(pr.refs) + 1
			pr.refs = append(pr.refs, r)
		}
	}
	pr.buf = make([]rowView, len(pr.refs)+1)
	for _, fp := range p.feeds {
		if p.roots[fp.feed.Dataset] == dataset {
			pr.feeds = append(pr.feeds, newPreparedFeed(fp))
		}
	}
	for i, lp := range p.links {
		if p.roots[lp.from.feed.Dataset] == dataset || p.roots[lp.to.feed.Dataset] == dataset {
			pr.links = append(pr.links, preparedLink{def: i, lp: lp})
		}
	}
	return pr, nil
}

func newPreparedFeed(fp *feedPlan) preparedFeed {
	pf := preparedFeed{fp: fp}
	for i := range fp.keys {
		kp := &fp.keys[i]
		pk := preparedKey{kp: kp, own: ownFeed(fp.typ, kp)}
		for c := kp.ctx; c != nil; c = c.key.ctx {
			pk.parents = append(pk.parents, preparedParent{link: c.link, typ: c.parent, plan: &c.key, own: ownFeed(c.parent, &c.key)})
		}
		pf.keys = append(pf.keys, pk)
	}
	return pf
}

// Prepare turns one page of rows into prepared rows. The rows of the page are numbered by their place in it.
func (pr *Preparer) Prepare(page Page) (StagePage, error) {
	sp := StagePage{Dataset: pr.dataset.ID, Page: page.No, Token: page.Token, Last: page.Last, End: page.End, Skipped: page.Skipped, Unreadable: len(page.Unreadable)}
	pr.pageProblems(&sp, page)
	for no, row := range page.Rows {
		if err := pr.prepareRow(&sp, no, row); err != nil {
			return StagePage{}, err
		}
	}
	return sp, nil
}

func (pr *Preparer) prepareRow(sp *StagePage, no int, row Row) error {
	hash, err := rowHash(row)
	if err != nil {
		return err
	}
	views, by := pr.refine(row)
	sp.Records = append(sp.Records, StageRecord{No: no, Key: row.Key, Line: row.Line, Where: row.Where, Filtered: by != "", FilteredBy: by, RawHash: hash, Fields: pr.cells(row)})
	for _, pf := range pr.feeds {
		if err := pr.feedRow(sp, no, pf, views[pr.ids[pf.fp.feed.Dataset]]); err != nil {
			return err
		}
	}
	return pr.linkEnds(sp, no, views)
}

// cells is the cells of the declared columns of a row, in the order of the columns.
func (pr *Preparer) cells(row Row) []string {
	out := make([]string, len(pr.dataset.Columns))
	for i, c := range pr.dataset.Columns {
		out[i] = row.Fields[c.Name]
	}
	return out
}

// refine runs the refinements of the dataset over the row, one after another, the way refineRows runs them over many:
// a row a filter drops is held, and the rewrites after it still reach it. It gives the view of the row for the dataset
// and for every refinement, and the first operation that dropped the row (empty when none did).
func (pr *Preparer) refine(row Row) (views []rowView, filteredBy string) {
	views = pr.buf
	views[0] = rowView{row: row}
	for i, r := range pr.refs {
		in := views[pr.ids[r.Input]]
		var kept, heldIn []Row
		if in.held {
			heldIn = []Row{in.row}
		} else {
			kept = []Row{in.row}
		}
		kept, held, dropped := refineRows(r, kept, heldIn)
		if len(kept) == 1 {
			views[i+1] = rowView{row: kept[0]}
			continue
		}
		views[i+1] = rowView{row: held[0], held: true}
		if filteredBy == "" {
			filteredBy = dropped[row.Key]
		}
	}
	return views, filteredBy
}

// pageProblems gives the problems of the page that are about no record: the header, the lines that could not be read, and
// a dataset that was not read to its end.
func (pr *Preparer) pageProblems(sp *StagePage, page Page) {
	if sh := page.Shape; sh != nil {
		pr.shapeProblems(sp, *sh)
	}
	for _, u := range page.Unreadable {
		reason := unreadableReason(u)
		if u.AllEmpty && !pr.hinted {
			pr.hinted = true
			reason += noKeyHint
		}
		sp.Problems = append(sp.Problems, StageProblem{No: -1, Line: u.Line, Where: u.Where, Kind: ProblemUnreadable, Reason: reason})
	}
	if page.Last && !page.End.Complete {
		sp.Problems = append(sp.Problems, StageProblem{No: -1, Kind: ProblemIncomplete, Reason: page.End.Reason})
	}
}

// shapeProblems reports a header that lacks a declared column (the dataset is not used) or has columns no declared column
// reads, with the same words as the current flow.
func (pr *Preparer) shapeProblems(sp *StagePage, sh Shape) {
	if len(sh.Missing) > 0 {
		reason := shapeReason(Table{Missing: sh.Missing, Header: sh.Header, Hint: sh.Hint})
		sp.Problems = append(sp.Problems, StageProblem{No: -1, Kind: ProblemShapeChanged, Reason: reason})
		return
	}
	var rep Report
	reportExtra(&rep, pr.dataset, sh.Extra)
	for _, p := range rep.Problems {
		sp.Problems = append(sp.Problems, StageProblem{No: -1, Kind: p.Kind, Reason: p.Reason})
	}
}

func (pr *Preparer) rowProblem(sp *StagePage, no int, fp *feedPlan, row Row, p StageProblem) {
	p.No, p.Key, p.Line, p.Where, p.Feed = no, strings.ReplaceAll(row.Key, keySep, "|"), row.Line, row.Where, fp.feed.ID
	sp.Problems = append(sp.Problems, p)
}
