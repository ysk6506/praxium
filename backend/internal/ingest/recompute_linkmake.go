package ingest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// LinkComputer is the LinkMaker of the plans of the sources of a KG, by the number of the source: it reads the cells of a record by the plan
// of the record's own source (the same functions the receiving and the old flow use for the same things: the refinements, the keys of the
// ends, the values of the cells), so the end of a record is read the way it was when its key was made. It is not safe for use by several
// goroutines.
type LinkComputer struct {
	plans  map[int64]*Plan
	hasher *graph.ContentHasher
	preps  map[prepKey]*Preparer
}

var _ LinkMaker = (*LinkComputer)(nil)

// NewLinkComputer makes the computer of the links: the plans of the sources and the hasher of the KG.
func NewLinkComputer(plans map[int64]*Plan, hasher *graph.ContentHasher) *LinkComputer {
	return &LinkComputer{plans: plans, hasher: hasher, preps: map[prepKey]*Preparer{}}
}

func (c *LinkComputer) preparer(src int64, dataset string) (*Preparer, bool) {
	key := prepKey{src, dataset}
	if pr, ok := c.preps[key]; ok {
		return pr, pr != nil
	}
	var pr *Preparer
	if plan, ok := c.plans[src]; ok {
		pr, _ = NewPreparer(plan, dataset)
	}
	c.preps[key] = pr
	return pr, pr != nil
}

// MakeLinks makes what a page needs: only the phase from has ends to read. The other phases are the database's and the page is given back
// as it is.
func (c *LinkComputer) MakeLinks(page RecomputeLinksPage) (RecomputedLinksPage, error) {
	out := RecomputedLinksPage{Unit: page.Unit, Phase: page.Phase, Through: page.Through, Last: page.Last, Keys: page.Keys, Ends: len(page.Ends)}
	for i := range page.Ends {
		e := &page.Ends[i]
		res, cand, probs, err := c.end(e)
		if err != nil {
			return RecomputedLinksPage{}, err
		}
		if res == nil {
			out.Skipped++
			continue
		}
		out.Results = append(out.Results, *res)
		out.Problems = append(out.Problems, probs...)
		if cand != nil {
			out.Candidates = append(out.Candidates, *cand)
		}
	}
	return out, nil
}

// end reads the end of one record. The first value is nil when the end makes nothing and is no problem of the record's (it has no object
// that the database found, its cells are not the ones the dataset declares now, or they give no key for the end).
func (c *LinkComputer) end(e *RecomputeEnd) (*LinkEndResult, *LinkCandidate, []RecomputedProblem, error) {
	plan := c.plans[e.SrcNo]
	pr, ok := c.preparer(e.SrcNo, e.Dataset)
	if plan == nil || !ok || e.Def < 0 || e.Def >= len(plan.links) || len(e.Fields) != len(pr.dataset.Columns) {
		return nil, nil, nil, nil
	}
	lp := plan.links[e.Def]
	row, err := pr.rowOf(e.Key, e.Fields)
	if err != nil {
		return nil, nil, nil, nil
	}
	views, _ := pr.refine(row)
	v := views[pr.ids[lp.from.feed.Dataset]]
	if _, ok := lp.linkKey(v.row, lp.def.From.Columns); !ok {
		return nil, nil, nil, nil
	}
	res := &LinkEndResult{Rid: e.Rid, Def: e.Def, Hash: e.Hash, Held: v.held}
	problem := func(kind, reason string) []RecomputedProblem {
		if v.held {
			return nil // a record a rule filtered out makes its links, and reports nothing
		}
		return []RecomputedProblem{{SrcNo: e.SrcNo, DatasetNo: e.DatasetNo, Key: e.Key, Feed: e.Def, Kind: kind, Reason: reason}}
	}
	toDS := plan.roots[lp.to.feed.Dataset]
	switch e.Targets {
	case 0:
		return res, nil, problem(ProblemLinkTarget, lp.missingTarget(toDS, v.row)), nil
	case 1:
	default:
		return res, nil, problem(ProblemLinkAmbiguous, "rows of "+toDS+" with this key are different objects"), nil
	}
	cand, err := c.candidate(plan, lp, e, v)
	if err != nil {
		return nil, nil, nil, err
	}
	res.Ref = cand.Row.Ref
	return res, cand, nil, nil
}

// candidate makes the link the end of a record makes: the id, the attributes of the row, the grade (the lowest of the two feeds), the
// reference of the source and the hashes (graph, the meaning the row by row path has for them).
func (c *LinkComputer) candidate(plan *Plan, lp *linkPlan, e *RecomputeEnd, v rowView) (*LinkCandidate, error) {
	src, err := parseObject(e.FromRef)
	if err != nil {
		return nil, err
	}
	dst, err := parseObject(e.ToRef)
	if err != nil {
		return nil, err
	}
	id, err := graph.NewLinkID(lp.lt.ID, src, dst, "")
	if err != nil {
		return nil, err
	}
	ref := plan.Source.ID + "/" + plan.roots[lp.from.feed.Dataset] + "/" + strings.ReplaceAll(v.row.Key, keySep, "|")
	attrs, left := lp.attrValues(v.row, ref)
	content, err := c.hasher.Link(graph.LinkRow{ID: id, TypeNo: lp.lt.LinkTypeNo, Src: src, Dst: dst, Grade: lp.grade(), Basis: lp.basis, Attrs: attrs, SourceRefs: []string{ref}})
	if err != nil {
		return nil, err
	}
	sum, err := graph.RowSum("link", id.String(), content)
	if err != nil {
		return nil, err
	}
	doc, err := graph.EncodeAttrs(attrs)
	if err != nil {
		return nil, err
	}
	refs, err := graph.EncodeRefs([]string{ref})
	if err != nil {
		return nil, err
	}
	who := LinkWho{Rid: e.Rid, Order: LinkOrder{SrcNo: e.SrcNo, Def: e.Def, Held: v.held, DatasetNo: e.DatasetNo, Key: e.Key},
		Sig: attrsSignature(attrs), Shown: plan.attrsShown(attrs), LinkName: lp.lt.Name, DefID: lp.def.ID, Dataset: e.Dataset}
	if !v.held {
		who.Left = left
	}
	return &LinkCandidate{Who: who, Row: LinkRow{Ref: id.String(), TypeNo: lp.lt.LinkTypeNo, Def: e.Def, Src: e.FromObj, Dst: e.ToObj, Grade: string(lp.grade()),
		Basis: string(lp.basis), Doc: doc, SourceRefs: refs, ContentHash: content, RowHash: kernel.Hash(sum)}}, nil
}

// attrsSignature is what two sets of attributes of a link are the same by: the number, the value, the grade and the basis of each (the
// reference of the source is not, as in the old flow).
func attrsSignature(attrs map[int]graph.Attr) string {
	nos := sortedNos(attrs)
	var b strings.Builder
	for _, no := range nos {
		a := attrs[no]
		fmt.Fprintf(&b, "%d=%v|%s|%s;", no, a.Value, a.Grade, a.Basis)
	}
	return b.String()
}

func sortedNos(attrs map[int]graph.Attr) []int {
	nos := make([]int, 0, len(attrs))
	for no := range attrs {
		nos = append(nos, no)
	}
	sort.Ints(nos)
	return nos
}

// attrsShown lists the attributes of a link by name, with the grade: {position_u=5 fact}, or {no attributes}.
func (p *Plan) attrsShown(attrs map[int]graph.Attr) string {
	if len(attrs) == 0 {
		return "{no attributes}"
	}
	nos := sortedNos(attrs)
	parts := make([]string, len(nos))
	for i, no := range nos {
		name := fmt.Sprint(no)
		if ref, ok := p.reg.PropertyByNo(no); ok {
			name = ref.Property.Name
		}
		parts[i] = fmt.Sprintf("%s=%v %s", name, attrs[no].Value, attrs[no].Grade)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
