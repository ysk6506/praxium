package ingest

import (
	"sort"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// contrib is one value a feed gives an attribute of an object.
type contrib struct {
	rank   int
	run    int
	feed   int
	attr   *attrPlan
	value  any
	text   string
	grade  model.Grade
	basis  model.Basis
	ref    string
	feedID string
}

// objAcc gathers everything the feeds say about one object in this run.
type objAcc struct {
	id       graph.ObjectID
	typ      model.ObjectType
	key      []graph.KeyPart
	contribs map[int][]contrib
	idents   map[string]string
	refs     map[string]bool
	creates  bool
	watched  bool
	retires  bool
	nameFall string
}

// fate is what became of a row: the row count classifies it at the end (pipeline.md 7).
type fate struct{ used, problem, filtered bool }

// derivation is the part of a run that turns the raw records into the objects and links that should be in the KG:
// refine, feed, identify and link. It reads the state and the plan and gives the desired objects; it writes the
// identity index of the state, which the caller saves only when the batch was applied.
type derivation struct {
	p       *Plan
	st      *State
	rep     *Report
	now     time.Time
	rows    map[string][]Row
	fates   map[string]map[string]*fate
	objects map[string]*objAcc
	// rowObj is the object of every row, by feed and natural key.
	rowObj map[string]map[string]string
	// identOwner is the object that holds an identifier of a scheme, to keep two objects from claiming one.
	identOwner map[string]string
	links      map[string]desiredLink
}

type desiredLink struct {
	up   graph.LinkUpsert
	from graph.ObjectID
}

func newDerivation(p *Plan, st *State, rep *Report, now time.Time) *derivation {
	return &derivation{p: p, st: st, rep: rep, now: now, rows: map[string][]Row{}, fates: map[string]map[string]*fate{},
		objects: map[string]*objAcc{}, rowObj: map[string]map[string]string{}, identOwner: map[string]string{}, links: map[string]desiredLink{}}
}

func (d *derivation) problem(kind, dataset, feed, key string, line int, reason string) {
	d.rep.problem(Problem{Kind: kind, Dataset: dataset, Feed: feed, Key: key, Line: line, Reason: reason})
}

// materialize reads the open raw records of every dataset as rows and runs the refinements over them.
func (d *derivation) materialize() {
	for _, ds := range d.p.Source.Datasets {
		state := d.st.dataset(ds.ID)
		keys := make([]string, 0, len(state.Open))
		for k := range state.Open {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rows := make([]Row, len(keys))
		d.fates[ds.ID] = map[string]*fate{}
		for i, k := range keys {
			rec := state.Open[k]
			rows[i] = Row{Key: k, Line: rec.Line, Fields: rec.Fields}
			d.fates[ds.ID][k] = &fate{}
		}
		d.rows[ds.ID] = rows
	}
	for _, r := range d.p.refs {
		kept, dropped := refineRows(r, d.rows[r.Input])
		d.rows[r.ID] = kept
		for k := range dropped {
			d.fates[d.p.roots[r.ID]][k].filtered = true
		}
	}
}

// feeds runs the feeds that create objects first, then those that only attach values, so that a value finds its
// object whatever the order of the mapping.
func (d *derivation) feeds() error {
	for _, creating := range []bool{true, false} {
		for _, fp := range d.p.feeds {
			if fp.feed.CreatesObjects != creating {
				continue
			}
			d.rowObj[fp.feed.ID] = map[string]string{}
			for _, row := range d.rows[fp.feed.Dataset] {
				if err := d.feedRow(fp, row); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (d *derivation) fate(fp *feedPlan, row Row) *fate {
	return d.fates[d.p.roots[fp.feed.Dataset]][row.Key]
}

// feedRow maps one row: its values, its identity and the object it belongs to. A row that cannot be mapped is
// set aside with the reason and nothing of it is used.
func (d *derivation) feedRow(fp *feedPlan, row Row) error {
	root, f := d.p.roots[fp.feed.Dataset], d.fate(fp, row)
	contribs, err := d.values(fp, row, root)
	if err != nil {
		f.problem = true
		d.problem(ProblemInvalidValue, root, fp.feed.ID, row.Key, row.Line, err.Error())
		return nil
	}
	formed, why := formKeys(fp, d.p.reg, row)
	if len(formed) == 0 {
		f.problem = true
		d.problem(ProblemMissingIdentity, root, fp.feed.ID, row.Key, row.Line, strings.Join(why, "; "))
		return nil
	}
	res, found, err := identify(fp, d.st, formed)
	if err != nil {
		return err
	}
	acc := d.objects[res.id.String()]
	if !found || (!fp.feed.CreatesObjects && acc == nil) {
		f.problem = true
		d.problem(ProblemOrphanValue, root, fp.feed.ID, row.Key, row.Line, "no object of type "+fp.typ.Name+" has this identity")
		return nil
	}
	if acc == nil {
		acc = d.newObject(fp, res, row, root)
	}
	for _, c := range res.conflicts {
		f.problem = true
		d.problem(ProblemIdentityConflict, root, fp.feed.ID, row.Key, row.Line, c)
	}
	d.attach(fp, acc, formed, contribs, row)
	d.rowObj[fp.feed.ID][row.Key] = res.id.String()
	f.used = true
	return nil
}

// newObject starts the account of an object that a creating feed has a row for. The key it was first seen with is the
// one kept in the state, which is not the key of this row when the row has a stronger one than the first row had.
func (d *derivation) newObject(fp *feedPlan, res resolution, row Row, root string) *objAcc {
	key := res.key
	if saved, ok := d.st.Keys[res.id.String()]; ok {
		key = saved
	} else {
		d.st.Keys[res.id.String()] = key
	}
	acc := &objAcc{id: res.id, typ: fp.typ, key: key, contribs: map[int][]contrib{}, idents: map[string]string{}, refs: map[string]bool{}}
	acc.nameFall = key[0].Value
	d.objects[res.id.String()] = acc
	return acc
}

// attach adds what a row says to the account of its object.
func (d *derivation) attach(fp *feedPlan, acc *objAcc, formed []formedKey, contribs []contrib, row Row) {
	root := d.p.roots[fp.feed.Dataset]
	acc.refs[d.p.Source.ID+"/"+root+"/"+strings.ReplaceAll(row.Key, keySep, "|")] = true
	acc.creates = acc.creates || fp.feed.CreatesObjects
	if fp.feed.RetiresObjects {
		acc.retires = true
	}
	for _, c := range contribs {
		c.ref = d.p.Source.ID + "/" + root + "/" + strings.ReplaceAll(row.Key, keySep, "|")
		acc.contribs[c.attr.prop.AttrNo] = append(acc.contribs[c.attr.prop.AttrNo], c)
	}
	for _, fk := range formed {
		if !fk.plan.scheme.IsZero() {
			d.claim(acc, fk.plan.scheme.String(), fk.parts[0].Value, fp, row, root)
		}
	}
	for _, ip := range fp.idents {
		cell := strings.TrimSpace(row.Fields[ip.column])
		if cell == "" {
			continue
		}
		if v, err := model.NormalizeValue(ip.vt, cell); err == nil && v != "" {
			d.claim(acc, ip.scheme.String(), v, fp, row, root)
		}
	}
}

// claim gives an object an identifier unless another object has it.
func (d *derivation) claim(acc *objAcc, scheme, value string, fp *feedPlan, row Row, root string) {
	k := scheme + keySep + value
	if owner, ok := d.identOwner[k]; ok && owner != acc.id.String() {
		d.problem(ProblemIdentityConflict, root, fp.feed.ID, row.Key, row.Line, "identifier "+scheme+" "+value+" already names another object")
		return
	}
	d.identOwner[k] = acc.id.String()
	acc.idents[scheme] = value
}

// values converts the cells a feed maps. An empty cell gives no value: the feed does not hold the attribute then,
// and another feed's value, or none, is what the object has (pipeline.md 3.2).
func (d *derivation) values(fp *feedPlan, row Row, root string) ([]contrib, error) {
	var out []contrib
	for i := range fp.attrs {
		a := &fp.attrs[i]
		cell := row.Fields[a.column]
		if strings.TrimSpace(cell) == "" {
			continue
		}
		v, err := convertCell(a.valueSpec, cell)
		if err != nil {
			return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "attribute "+a.prop.Name)
		}
		out = append(out, contrib{rank: a.rank, run: d.runOf(root, row.Key), feed: fp.index, attr: a, value: v, text: cell,
			grade: fp.grade, basis: fp.basis, feedID: fp.feed.ID})
	}
	return out, nil
}

func (d *derivation) runOf(root, key string) int {
	if rec := d.st.dataset(root).Open[key]; rec != nil {
		return rec.FromRun
	}
	return 0
}
