package ingest

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// ObjectComputer is the ObjectMaker of the plans of the sources of a KG, by the number of the source: it reads the cells of a record
// by the plan of the record's own source (the same functions the receiving and the old flow use for the same things: the refinements,
// cellValue, the claims), so a record is read the way it was when its keys were made. It is not safe for use by several goroutines.
type ObjectComputer struct {
	plans   map[int64]*Plan
	hasher  *graph.ContentHasher
	schemes map[string]int
	seen    string
	preps   map[prepKey]*Preparer
}

var _ ObjectMaker = (*ObjectComputer)(nil)

// NewObjectComputer makes the computer of the objects: the plans of the sources, the hasher of the KG, and the start of the step (the
// time seen and the numbers of the schemes).
func NewObjectComputer(plans map[int64]*Plan, hasher *graph.ContentHasher, start RecomputeStart) *ObjectComputer {
	return &ObjectComputer{plans: plans, hasher: hasher, schemes: start.Schemes, seen: start.Seen, preps: map[prepKey]*Preparer{}}
}

func (c *ObjectComputer) preparer(src int64, dataset string) (*Preparer, bool) {
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

// valueLine is one value that a record gives an attribute of an object, with what orders it among the others.
type valueLine struct {
	rank      int
	openedSeq int64
	srcNo     int64
	feed      int
	datasetNo int64
	key       string
	column    string
	attr      *attrPlan
	feedID    string
	value     any
	text      string
	grade     model.Grade
	basis     model.Basis
	ref       string
	dataset   string
	line      *RecomputeLine
}

// valueBefore is the order in which the values of one attribute of an object are chosen from (the first one wins): the lowest rank, then
// the record that was opened last (the larger opened_seq), then the source, the feed and the dataset by their numbers, then the natural
// key by its bytes, then the column. Two values are the same only when they are of the same record, feed and column, so the order is
// total and the value chosen does not depend on the order the lines were read in (ADR-0115 decision 5).
func valueBefore(a, b *valueLine) bool {
	switch {
	case a.rank != b.rank:
		return a.rank < b.rank
	case a.openedSeq != b.openedSeq:
		return a.openedSeq > b.openedSeq
	case a.srcNo != b.srcNo:
		return a.srcNo < b.srcNo
	case a.feed != b.feed:
		return a.feed < b.feed
	case a.datasetNo != b.datasetNo:
		return a.datasetNo < b.datasetNo
	case a.key != b.key:
		return strings.Compare(a.key, b.key) < 0
	}
	return a.column < b.column
}

// identLine is a value an object has for an identifier scheme, with what orders it among the others (the first one is the object's).
type identLine struct {
	value     string
	openedSeq int64
	srcNo     int64
	feed      int
	key       string
}

func identBefore(a, b identLine) bool {
	switch {
	case a.openedSeq != b.openedSeq:
		return a.openedSeq > b.openedSeq
	case a.key != b.key:
		return strings.Compare(a.key, b.key) < 0
	case a.srcNo != b.srcNo:
		return a.srcNo < b.srcNo
	case a.feed != b.feed:
		return a.feed < b.feed
	}
	return a.value < b.value
}

// Make makes the rows of the objects of a page.
func (c *ObjectComputer) Make(page RecomputePage) (RecomputedPage, error) {
	out := RecomputedPage{Unit: page.Unit, Through: page.Through, Last: page.Last, TooMany: len(page.TooMany)}
	seen := map[RecomputedRecord]bool{}
	for i := range page.Objects {
		o := &page.Objects[i]
		out.Lines += len(o.Lines)
		if len(o.Lines) == 0 {
			out.Empty = append(out.Empty, o.ObjNo)
			continue
		}
		made, probs, ok, err := c.object(o)
		if err != nil {
			return RecomputedPage{}, err
		}
		if !ok {
			out.Unreadable = append(out.Unreadable, o.ObjNo)
			continue
		}
		out.Objects = append(out.Objects, made)
		out.Problems = append(out.Problems, probs...)
		for j := range o.Lines {
			ln := &o.Lines[j]
			if r := (RecomputedRecord{SrcNo: ln.SrcNo, DatasetNo: ln.DatasetNo, Key: ln.Key}); ln.Rid == 0 && !seen[r] {
				seen[r] = true
				out.Records = append(out.Records, r)
			}
		}
	}
	return out, nil
}

// read is a line read by its plan: the values it gives, the claims it makes and why.
type lineRead struct {
	ln     *RecomputeLine
	pf     preparedFeed
	pr     *Preparer
	row    Row
	held   bool
	values []*valueLine
	left   []StageProblem
}

func (c *ObjectComputer) readLine(ln *RecomputeLine) (lineRead, bool) {
	pr, ok := c.preparer(ln.SrcNo, ln.Dataset)
	if !ok || len(ln.Fields) != len(pr.dataset.Columns) {
		return lineRead{}, false
	}
	pf, ok := pr.feedByIndex(ln.Feed)
	if !ok {
		return lineRead{}, false
	}
	row, err := pr.rowOf(ln.Key, ln.Fields)
	if err != nil {
		return lineRead{}, false
	}
	views, _ := pr.refine(row)
	v := views[pr.ids[pf.fp.feed.Dataset]]
	return lineRead{ln: ln, pf: pf, pr: pr, row: v.row, held: v.held}, true
}

// valuesOf converts the cells the feed of a line maps. A cell that is wrong gives no value and, for a feed that only attaches values
// (the receiving reports the cells of the feeds that make objects) and a record that was received now, a problem of the record.
func (c *ObjectComputer) valuesOf(r *lineRead, probs *[]RecomputedProblem) {
	fp, ln := r.pf.fp, r.ln
	plan := c.plans[ln.SrcNo]
	ref := plan.Source.ID + "/" + ln.Dataset + "/" + strings.ReplaceAll(ln.Key, keySep, "|")
	received := ln.Why == 0 || ln.Why == 3 || ln.Why == 4 // changed, new, returned (pgschema.CalcWhy*)
	for i := range fp.attrs {
		a := &fp.attrs[i]
		v, has, reason := cellValue(a.valueSpec, r.row.Fields[a.column])
		if reason != "" && !r.held && !fp.feed.CreatesObjects && received {
			*probs = append(*probs, RecomputedProblem{SrcNo: ln.SrcNo, DatasetNo: ln.DatasetNo, Key: ln.Key, Feed: ln.Feed, Field: a.column,
				Kind: ProblemInvalidValue, Reason: reason + leftOutSuffix})
		}
		if !has {
			continue
		}
		r.values = append(r.values, &valueLine{rank: a.rank, openedSeq: ln.OpenedSeq, srcNo: ln.SrcNo, feed: ln.Feed, datasetNo: ln.DatasetNo, key: ln.Key,
			column: a.column, attr: a, feedID: fp.feed.ID, value: v, text: r.row.Fields[a.column], grade: fp.grade, basis: fp.basis, ref: ref, dataset: ln.Dataset, line: ln})
	}
}

// claimsOf gives the identifier values a line claims that the object has (the claims that are solved, by scheme and hash).
func (c *ObjectComputer) claimsOf(r *lineRead, into map[string][]identLine) {
	if len(r.ln.Won) == 0 {
		return
	}
	won := map[RecomputeClaim]bool{}
	for _, w := range r.ln.Won {
		won[w] = true
	}
	formed, _ := r.pr.formAll(r.pf, r.row)
	var sp StagePage
	_ = r.pr.claims(&sp, 0, r.pf, formed, rowView{row: r.row, held: r.held}) // a claim's hash is of a text: it cannot fail
	for _, cl := range sp.Claims {
		if won[RecomputeClaim{Scheme: c.schemes[cl.Scheme], Hash: cl.ValueHash}] {
			into[cl.Scheme] = append(into[cl.Scheme], identLine{value: cl.Value, openedSeq: r.ln.OpenedSeq, srcNo: r.ln.SrcNo, feed: r.ln.Feed, key: r.ln.Key})
		}
	}
}

// object makes one object. ok is false when a line of it cannot be read, or its name is not known: the object is then left as it is.
func (c *ObjectComputer) object(o *RecomputeObject) (RecomputedObject, []RecomputedProblem, bool, error) {
	in, ok := c.readLines(o)
	if !ok {
		return RecomputedObject{}, nil, false, nil
	}
	firstKey, ok := c.firstKeyOf(o, in.reads)
	if !ok {
		return RecomputedObject{}, nil, false, nil
	}
	ch, err := c.choose(in.byAttr)
	if err != nil {
		return RecomputedObject{}, nil, false, err
	}
	if ch.name == "" {
		ch.name = nameFallback(o, firstKey)
	}
	if ch.name == "" {
		return RecomputedObject{}, nil, false, nil
	}
	probs := append(in.probs, conflictsOf(o, ch, in.byAttr)...)
	made, err := c.rows(o, ch.name, ch.attrs, in.refs, firstKey)
	if err != nil {
		return RecomputedObject{}, nil, false, err
	}
	made.Volatile = ch.volatile
	made.Idents = identsOf(in.claims)
	return made, probs, true, nil
}

// objectLines is what the lines of an object give: the values by attribute, the references of the sources, the claims, the problems of
// the cells and the lines as they were read.
type objectLines struct {
	byAttr map[int][]*valueLine
	refs   map[string]bool
	claims map[string][]identLine
	probs  []RecomputedProblem
	reads  []lineRead
}

// readLines reads every line of an object; ok is false when one cannot be read.
func (c *ObjectComputer) readLines(o *RecomputeObject) (*objectLines, bool) {
	in := &objectLines{byAttr: map[int][]*valueLine{}, refs: map[string]bool{}, claims: map[string][]identLine{}}
	for i := range o.Lines {
		r, ok := c.readLine(&o.Lines[i])
		if !ok {
			return nil, false
		}
		c.valuesOf(&r, &in.probs)
		c.claimsOf(&r, in.claims)
		for _, v := range r.values {
			in.byAttr[v.attr.prop.AttrNo] = append(in.byAttr[v.attr.prop.AttrNo], v)
			in.refs[v.ref] = true
		}
		in.refs[c.plans[r.ln.SrcNo].Source.ID+"/"+r.ln.Dataset+"/"+strings.ReplaceAll(r.ln.Key, keySep, "|")] = true
		in.reads = append(in.reads, r)
	}
	return in, true
}

// chosen is the value that wins each attribute, as the document, the volatile entries and the name they give. Its numbers are the
// attributes in order.
type chosen struct {
	nos      []int
	winners  map[int]*valueLine
	attrs    map[int]graph.Attr
	volatile []RecomputedVolatile
	name     string
}

// choose sorts the values of each attribute by the order and takes the first.
func (c *ObjectComputer) choose(byAttr map[int][]*valueLine) (*chosen, error) {
	ch := &chosen{winners: map[int]*valueLine{}, attrs: map[int]graph.Attr{}}
	for no := range byAttr {
		ch.nos = append(ch.nos, no)
	}
	sort.Ints(ch.nos)
	for _, no := range ch.nos {
		vs := byAttr[no]
		sort.Slice(vs, func(i, j int) bool { return valueBefore(vs[i], vs[j]) })
		win := vs[0]
		ch.winners[no] = win
		if win.attr.volatile {
			entry, err := graph.EncodeVolatileEntry(graph.VolatileWrite{Value: win.value, Grade: win.grade, Basis: win.basis, SeenAt: c.seen, Ref: win.ref})
			if err != nil {
				return nil, err
			}
			ch.volatile = append(ch.volatile, RecomputedVolatile{AttrNo: no, Entry: entry})
		} else {
			ch.attrs[no] = graph.Attr{Value: win.value, Grade: win.grade, Basis: win.basis, SourceTime: c.seen, SourceRef: win.ref}
		}
		if s, isText := win.value.(string); isText && s != "" && win.attr.prop.Name == "name" {
			ch.name = s
		}
	}
	return ch, nil
}

// conflictsOf is the problems of the values that lost to a value of another text.
func conflictsOf(o *RecomputeObject, ch *chosen, byAttr map[int][]*valueLine) []RecomputedProblem {
	var probs []RecomputedProblem
	for _, no := range ch.nos {
		win := ch.winners[no]
		for _, l := range byAttr[no][1:] {
			if l.text != win.text {
				probs = append(probs, RecomputedProblem{SrcNo: l.srcNo, DatasetNo: l.datasetNo, Key: l.key, Feed: l.feed, Field: l.column, Kind: ProblemSourceConflict,
					Reason: fmt.Sprintf("attribute %s of the object %s (%s): %q from %s lost to %q from %s (dataset %s, key %s)", l.attr.prop.Name, ch.name, o.Ref,
						l.text, l.feedID, win.text, win.feedID, win.dataset, strings.ReplaceAll(win.key, keySep, "|"))})
			}
		}
	}
	return probs
}

// identsOf is the identifier of each scheme that the lines won: the first by the order of the identifiers.
func identsOf(claims map[string][]identLine) []RecomputedIdent {
	schemes := make([]string, 0, len(claims))
	for s := range claims {
		schemes = append(schemes, s)
	}
	sort.Strings(schemes)
	var out []RecomputedIdent
	for _, s := range schemes {
		ls := claims[s]
		sort.Slice(ls, func(i, j int) bool { return identBefore(ls[i], ls[j]) })
		out = append(out, RecomputedIdent{Scheme: s, Value: ls[0].value})
	}
	return out
}

// rows makes the row of the object: the document, the references of its sources, the lowest grade and the hashes (graph, the meaning
// the row by row path has for them).
func (c *ObjectComputer) rows(o *RecomputeObject, name string, attrs map[int]graph.Attr, refs map[string]bool, firstKey []graph.KeyPart) (RecomputedObject, error) {
	doc, err := graph.EncodeAttrs(attrs)
	if err != nil {
		return RecomputedObject{}, err
	}
	list := make([]string, 0, len(refs))
	for r := range refs {
		list = append(list, r)
	}
	sort.Strings(list)
	srcRefs, err := graph.EncodeRefs(list)
	if err != nil {
		return RecomputedObject{}, err
	}
	content, err := c.hasher.Object(o.TypeNo, name, attrs)
	if err != nil {
		return RecomputedObject{}, err
	}
	sum, err := graph.RowSum("object", o.Ref, content)
	if err != nil {
		return RecomputedObject{}, err
	}
	out := RecomputedObject{ObjNo: o.ObjNo, Ref: o.Ref, TypeNo: o.TypeNo, Name: name, MinGrade: graph.MinGrade(attrs), Doc: doc, SourceRefs: srcRefs,
		ContentHash: content, RowHash: kernel.Hash(sum), IsNew: o.IsNew}
	if o.IsNew {
		key, err := graph.EncodeKey(firstKey)
		if err != nil {
			return RecomputedObject{}, err
		}
		out.FirstKey, _ = key.(string)
	}
	return out, nil
}

// nameFallback is the name an object has when no value of the attribute name is chosen: the first value of the key it was first seen
// with that is not the context, or, for an object that has no such key, the name the KG has for it.
func nameFallback(o *RecomputeObject, key []graph.KeyPart) string {
	for _, p := range key {
		if p.Kind != graph.PartContext && p.Value != "" {
			return p.Value
		}
	}
	if o.IsNew {
		return ""
	}
	return o.Name
}

// firstKeyOf is the key the object was first seen with: the stored one for an object the KG has, and for one this run makes the key of
// the record and the feed that made it whose id is the object's (a key that has a context gives its parent's id to the id).
func (c *ObjectComputer) firstKeyOf(o *RecomputeObject, reads []lineRead) ([]graph.KeyPart, bool) {
	if !o.IsNew {
		if len(bytes.TrimSpace(o.FirstKey)) == 0 {
			return nil, true // an object that was made before the first key was kept
		}
		key, err := graph.DecodeKey(bytes.TrimSpace(o.FirstKey))
		return key, err == nil
	}
	for i := range reads {
		r := &reads[i]
		if r.ln.Rid != o.OriginRid || r.ln.Feed != o.OriginFeed {
			continue
		}
		formed, _ := r.pr.formAll(r.pf, r.row)
		for _, f := range formed {
			pk := &r.pf.keys[f.ord]
			parts := keyParts(pk.kp, f.own)
			if pk.kp.ctx != nil {
				parent, err := kernel.ParseID[graph.ObjectKind](o.ParentRef)
				if err != nil {
					continue // the object was not made by this key: no question about a parent was asked for it
				}
				parts[0].Objects = []graph.ObjectID{parent}
			}
			if id, err := graph.NewObjectID(r.pf.fp.typ.ID, parts); err == nil && id.String() == o.Ref {
				return parts, true
			}
		}
	}
	return nil, false
}
