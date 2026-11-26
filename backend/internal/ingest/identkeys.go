package ingest

import (
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// IdentifyFeedsOf says what the identification needs of the feeds of the plans of the sources of a KG, by the number of the source.
func IdentifyFeedsOf(plans map[int64]*Plan) []IdentifyFeed {
	var srcs []int64
	for src := range plans {
		srcs = append(srcs, src)
	}
	sort.Slice(srcs, func(i, j int) bool { return srcs[i] < srcs[j] })
	var out []IdentifyFeed
	for _, src := range srcs {
		out = append(out, plans[src].IdentifyFeeds(src)...)
	}
	return out
}

// IdentifyFeeds says what the identification needs of every feed of the plan of the source numbered srcNo, by the number the rows
// received give the feed (the place of the feed in the mapping).
func (p *Plan) IdentifyFeeds(srcNo int64) []IdentifyFeed {
	out := make([]IdentifyFeed, 0, len(p.feeds))
	for _, fp := range p.feeds {
		f := IdentifyFeed{SrcNo: srcNo, Feed: fp.index, TypeNo: fp.typ.TypeNo, Creates: fp.feed.CreatesObjects, Retires: fp.feed.CreatesObjects && fp.feed.RetiresObjects, NameCol: p.nameCell(fp)}
		for ord := range fp.keys {
			f.Keys = append(f.Keys, IdentifyKey{Ord: ord, ModelOrd: fp.keys[ord].modelOrd})
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Feed < out[j].Feed })
	return out
}

// nameCell is the place, among the cells of a record of the dataset of the feed, of the cell the attribute name is read from: -1 when
// the feed gives no name or reads a dataset that a refinement makes (its columns are not the cells of the record).
func (p *Plan) nameCell(fp *feedPlan) int {
	if fp.nameCol == "" {
		return -1
	}
	for _, ds := range p.Source.Datasets {
		if ds.ID != fp.feed.Dataset {
			continue
		}
		for i, c := range ds.Columns {
			if c.Name == fp.nameCol {
				return i
			}
		}
	}
	return -1
}

// PlanKeys is the KeyMaker that reads the cells of a record by the plan of its source: the same preparation the receiving does, so
// a key made again is the key the receiving would have made of the same cells.
type PlanKeys struct {
	plans map[int64]*Plan
	preps map[prepKey]*Preparer
}

type prepKey struct {
	src     int64
	dataset string
}

var _ KeyMaker = (*PlanKeys)(nil)

// NewPlanKeys makes the key maker of the plans of the sources of a KG, by the number of the source.
func NewPlanKeys(plans map[int64]*Plan) *PlanKeys {
	return &PlanKeys{plans: plans, preps: map[prepKey]*Preparer{}}
}

func (k *PlanKeys) preparer(src int64, dataset string) (*Preparer, error) {
	key := prepKey{src, dataset}
	if pr, ok := k.preps[key]; ok {
		return pr, nil
	}
	plan, ok := k.plans[src]
	if !ok {
		return nil, kernel.Errorf(kernel.CodeNotFound, "the plan of the source %d is not given, so its records cannot be read again", src)
	}
	pr, err := NewPreparer(plan, dataset)
	if err != nil {
		return nil, err
	}
	k.preps[key] = pr
	return pr, nil
}

// rowOf is the row a record's cells make, by the columns the dataset declares.
func (pr *Preparer) rowOf(key string, cells []string) (Row, error) {
	if len(cells) != len(pr.dataset.Columns) {
		return Row{}, kernel.Errorf(kernel.CodeInternal, "the record %q keeps %d cells and dataset %s declares %d columns", key, len(cells), pr.dataset.ID, len(pr.dataset.Columns))
	}
	row := Row{Key: key, Fields: make(map[string]string, len(cells))}
	for i, c := range pr.dataset.Columns {
		row.Fields[c.Name] = cells[i]
	}
	return row, nil
}

// Rebuild makes the keys, the ends and the claims of the records from their cells.
func (k *PlanKeys) Rebuild(src int64, dataset string, rows []RebuildRow) ([]RebuiltRow, error) {
	pr, err := k.preparer(src, dataset)
	if err != nil {
		return nil, err
	}
	page := Page{Dataset: dataset, Rows: make([]Row, len(rows))}
	for i, r := range rows {
		if page.Rows[i], err = pr.rowOf(r.Key, r.Fields); err != nil {
			return nil, err
		}
	}
	sp, err := pr.Prepare(page)
	if err != nil {
		return nil, err
	}
	out := make([]RebuiltRow, len(rows))
	for i, r := range rows {
		out[i].Rid = r.Rid
	}
	for _, key := range sp.Keys {
		out[key.No].Keys = append(out[key.No].Keys, key)
	}
	for _, p := range sp.Parents {
		out[p.No].Parents = append(out[p.No].Parents, p)
	}
	for _, e := range sp.Ends {
		out[e.No].Ends = append(out[e.No].Ends, e)
	}
	for _, c := range sp.Claims {
		out[c.No].Claims = append(out[c.No].Claims, c)
	}
	return out, nil
}

// Answer makes the id of a new object that has a parent: the key of its feed, the parent's id in the place of the context, and the
// type. It says too whether the object has a name, by the rule the current flow has for it: the value of the attribute name when the
// row gives one, else the first value of the key that is not the context.
func (k *PlanKeys) Answer(a AskRow) (Answer, error) {
	out := Answer{Round: a.Round, Label: a.Label}
	pr, err := k.preparer(a.SrcNo, a.Dataset)
	if err != nil {
		return out, err
	}
	row, err := pr.rowOf(a.Key, a.Fields)
	if err != nil {
		return out, err
	}
	views, _ := pr.refine(row)
	pf, ok := pr.feedByIndex(a.Feed)
	if !ok || a.Ord < 0 || a.Ord >= len(pf.keys) {
		return out, kernel.Errorf(kernel.CodeInternal, "the feed %d has no key %d", a.Feed, a.Ord)
	}
	pk := &pf.keys[a.Ord]
	f, _, formed := pr.formOne(pk, views[pr.ids[pf.fp.feed.Dataset]].row)
	if !formed {
		out.Missing = true
		return out, nil
	}
	parts := keyParts(pk.kp, f.own)
	if pk.kp.ctx != nil {
		parent, err := kernel.ParseID[graph.ObjectKind](a.ParentRef)
		if err != nil {
			return out, err
		}
		parts[0].Objects = []graph.ObjectID{parent}
	}
	if objectName(pf.fp, views[pr.ids[pf.fp.feed.Dataset]].row, f.own) == "" {
		out.Missing = true
		return out, nil
	}
	id, err := graph.NewObjectID(pf.fp.typ.ID, parts)
	if err != nil {
		return out, err
	}
	out.Ref = id.String()
	return out, nil
}

func (pr *Preparer) feedByIndex(index int) (preparedFeed, bool) {
	for _, pf := range pr.feeds {
		if pf.fp.index == index {
			return pf, true
		}
	}
	return preparedFeed{}, false
}

// objectName is the name an object of a row gets (the rule of derivation, merge.go): the attribute name when the row gives a value
// for it, else the first value of the key that is not the context. It is empty when the row gives neither, and then no object is made.
func objectName(fp *feedPlan, row Row, own []graph.KeyPart) string {
	for i := range fp.attrs {
		a := &fp.attrs[i]
		if a.prop.Name != "name" {
			continue
		}
		if v, has, reason := cellValue(a.valueSpec, row.Fields[a.column]); has && reason == "" {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	if len(own) > 0 {
		return own[0].Value
	}
	return ""
}
