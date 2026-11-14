package query

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Include names of get.
const (
	IncludeAttributes  = "attributes"
	IncludeIdentifiers = "identifiers"
	IncludeLinkCounts  = "link_counts"
	IncludeEvidence    = "evidence"
	IncludeProvenance  = "provenance"
)

// GetInput is the input of get: an object id or an identifier. Include
// defaults to the attributes.
type GetInput struct {
	Common
	ObjectID   string
	Identifier *IdentifierFilter
	Include    []string
}

// ObjectRef names an object in an answer.
type ObjectRef struct {
	ObjectID    string `json:"object_id"`
	ObjectType  string `json:"object_type"`
	DisplayName string `json:"display_name"`
}

// AttributeValue is one attribute of an object with its evidence. A value the
// caller may not read is masked: the attribute is there, the value is not
// (access-control.md 7-3). A value older than max_staleness is not used: it is
// stale and carries no value.
type AttributeValue struct {
	Name      string `json:"name"`
	Value     any    `json:"value"`
	Unit      string `json:"unit,omitempty"`
	Grade     string `json:"grade"`
	Basis     string `json:"basis"`
	SeenAt    string `json:"seen_at,omitempty"`
	SourceRef string `json:"source_ref,omitempty"`
	Stale     bool   `json:"stale"`
	Volatile  bool   `json:"volatile,omitempty"`
	Masked    bool   `json:"masked,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// IdentifierItem is one identifier an object holds.
type IdentifierItem struct {
	Scheme string `json:"scheme"`
	Value  string `json:"value"`
	Scope  string `json:"scope,omitempty"`
}

// LinkCountGroup counts the links of one type and direction, without those whose other end is hidden.
type LinkCountGroup struct {
	LinkType  string `json:"link_type"`
	Direction string `json:"direction"`
	Count     int    `json:"count"`
}

// LinkCounts is the number of links of an object; the links themselves are read with neighbors.
type LinkCounts struct {
	Out    int              `json:"out"`
	In     int              `json:"in"`
	Groups []LinkCountGroup `json:"groups"`
}

// WorstFreshness is the oldest attribute the answer holds.
type WorstFreshness struct {
	AttributeName    string `json:"attribute"`
	SourceRef        string `json:"source_ref,omitempty"`
	OldestAgeSeconds int64  `json:"oldest_age_seconds"`
}

// ObjectEvidence summarises the grades and the worst freshness of the attributes.
type ObjectEvidence struct {
	MinGrade       *model.Grade    `json:"min_grade"`
	Counts         GradeCounts     `json:"counts"`
	WorstFreshness *WorstFreshness `json:"worst_freshness"`
}

// Valid is the span of change numbers during which the object had this content.
type Valid struct {
	FromChangeSeq int64  `json:"from_change_seq"`
	ToChangeSeq   *int64 `json:"to_change_seq"`
}

// GetData is the body of the answer of get.
type GetData struct {
	Object         ObjectRef        `json:"object"`
	Attributes     []AttributeValue `json:"attributes,omitempty"`
	Identifiers    []IdentifierItem `json:"identifiers,omitempty"`
	LinkCounts     *LinkCounts      `json:"link_counts,omitempty"`
	Evidence       *ObjectEvidence  `json:"evidence,omitempty"`
	Valid          Valid            `json:"valid"`
	ProvenanceRefs []string         `json:"provenance_refs,omitempty"`
}

// Get answers get: one object, with what the include list asks for.
func (e *Engine) Get(ctx context.Context, in GetInput, f ReadFilter) (Answer[GetData], error) {
	var zero Answer[GetData]
	inc, err := parseInclude(in.Include)
	if err != nil {
		return zero, err
	}
	r, err := e.open(ctx, in.Common, "get", f)
	if err != nil {
		return zero, err
	}
	id, err := e.resolveObject(ctx, r, in)
	if err != nil {
		return zero, err
	}
	v, err := e.object(ctx, r, id)
	if err != nil {
		return zero, err
	}
	if !v.ok || !v.dec.Visible {
		return zero, kernel.Errorf(CodeObjectNotFound, "no object %s", id)
	}
	data := e.objectHead(v)
	if inc[IncludeProvenance] {
		data.ProvenanceRefs = append([]string{}, v.row.SourceRefs...)
	}
	if err := e.sections(ctx, r, inc, v, &data); err != nil {
		return zero, err
	}
	env, err := e.envelope(ctx, r, ResultComplete{Total: 1, Returned: 1})
	if err != nil {
		return zero, err
	}
	return Answer[GetData]{Meta: env, Data: data}, nil
}

func (e *Engine) objectHead(v visible) GetData {
	data := GetData{
		Object: ObjectRef{ObjectID: v.row.ID.String(), ObjectType: e.types.byNo[v.row.TypeNo].typ.Name, DisplayName: v.row.Name},
		Valid:  Valid{FromChangeSeq: v.row.SeqFrom},
	}
	if v.row.SeqTo != 0 {
		to := v.row.SeqTo
		data.Valid.ToChangeSeq = &to
	}
	return data
}

// sections fills the parts of the body that the include list names.
func (e *Engine) sections(ctx context.Context, r *read, inc map[string]bool, v visible, data *GetData) error {
	if inc[IncludeAttributes] || inc[IncludeEvidence] {
		attrs, err := e.attributes(ctx, r, e.types.byNo[v.row.TypeNo], v)
		if err != nil {
			return err
		}
		if inc[IncludeAttributes] {
			data.Attributes = attrs
		}
		if inc[IncludeEvidence] {
			data.Evidence = e.objectEvidence(attrs)
		}
	}
	var err error
	if inc[IncludeIdentifiers] {
		if data.Identifiers, err = e.identifiers(ctx, r, v.row.ID); err != nil {
			return err
		}
	}
	if inc[IncludeLinkCounts] {
		if data.LinkCounts, err = e.linkCounts(ctx, r, v.row.ID); err != nil {
			return err
		}
	}
	return nil
}

func parseInclude(names []string) (map[string]bool, error) {
	inc := map[string]bool{}
	for _, n := range names {
		switch n {
		case IncludeAttributes, IncludeIdentifiers, IncludeLinkCounts, IncludeEvidence, IncludeProvenance:
			inc[n] = true
		default:
			return nil, kernel.Errorf(CodeIncludeUnknown, "cannot include %q", n)
		}
	}
	if len(inc) == 0 {
		inc[IncludeAttributes] = true
	}
	return inc, nil
}

func (e *Engine) resolveObject(ctx context.Context, r *read, in GetInput) (graph.ObjectID, error) {
	switch {
	case in.ObjectID != "" && in.Identifier != nil:
		return graph.ObjectID{}, inputf("object_id", "give object_id or identifier, not both")
	case in.ObjectID != "":
		id, err := kernel.ParseID[graph.ObjectKind](in.ObjectID)
		if err != nil {
			return id, kernel.Errorf(CodeObjectNotFound, "no object %q", in.ObjectID)
		}
		return id, nil
	case in.Identifier != nil:
		scheme, ok := e.reg.IdentifierSchemeByName(in.Identifier.Scheme)
		if !ok {
			return graph.ObjectID{}, inputf("identifier.scheme", "no identifier scheme %q; the schemes are: %s", in.Identifier.Scheme, strings.Join(e.reg.IdentifierSchemeNames(), ", "))
		}
		recs, err := r.snap.Identifiers(ctx, scheme.ID.String(), tidyIdentifier(scheme, in.Identifier.Value))
		if err != nil {
			return graph.ObjectID{}, err
		}
		switch len(recs) {
		case 0:
			return graph.ObjectID{}, kernel.Errorf(CodeObjectNotFound, "no object holds that identifier")
		case 1:
			return recs[0].Object, nil
		}
		return graph.ObjectID{}, inputf("identifier", "%d objects hold that identifier in different scopes; give object_id", len(recs))
	}
	return graph.ObjectID{}, inputf("object_id", "give object_id or identifier")
}

func (e *Engine) attributes(ctx context.Context, r *read, info *typeInfo, v visible) ([]AttributeValue, error) {
	nos := make([]int, 0, len(v.row.Attrs))
	for no := range v.row.Attrs {
		nos = append(nos, no)
	}
	sort.Ints(nos)
	var out []AttributeValue
	for _, no := range nos {
		a := v.row.Attrs[no]
		if !r.designed(a.Basis) {
			continue
		}
		out = append(out, e.attribute(r, info.byAttr[no], v.dec, stored{a.Value, a.Grade, a.Basis, a.SourceTime, a.SourceRef, false}))
	}
	if r.info.Volatile != graph.VolatileLatest {
		return out, nil
	}
	var vols []model.Property
	for _, p := range info.byAttr {
		if p.EffectiveHistory() == model.HistoryVolatile {
			vols = append(vols, p)
		}
	}
	sort.Slice(vols, func(i, j int) bool { return vols[i].AttrNo < vols[j].AttrNo })
	for _, p := range vols {
		row, ok, err := r.snap.Volatile(ctx, v.row.ID, p.AttrNo)
		if err != nil {
			return nil, err
		}
		if !ok || !r.designed(row.Basis) {
			continue
		}
		out = append(out, e.attribute(r, p, v.dec, stored{row.Value, row.Grade, row.Basis, row.SeenAt, row.Ref, true}))
	}
	return out, nil
}

// stored is one value as the rows keep it.
type stored struct {
	value    any
	grade    model.Grade
	basis    model.Basis
	seen     string
	ref      string
	volatile bool
}

// attribute builds one attribute of an answer: masked above the cap, stale and
// valueless when older than max_staleness. Only values actually given count as used.
func (e *Engine) attribute(r *read, p model.Property, dec Decision, v stored) AttributeValue {
	av := AttributeValue{Name: p.Name, Unit: p.Unit, Grade: string(v.grade), Basis: string(v.basis), SeenAt: v.seen, SourceRef: v.ref, Volatile: v.volatile}
	if SensitivityRank(p.EffectiveSensitivity()) > SensitivityRank(dec.Cap) {
		av.Masked, av.Reason = true, "sensitivity_above_limit"
		return av
	}
	if r.c.MaxStaleness > 0 {
		if t, err := time.Parse(time.RFC3339, v.seen); err == nil && e.clock.Now().Sub(t) > r.c.MaxStaleness {
			av.Stale = true
			return av
		}
	}
	av.Value = v.value
	r.use.add(v.grade, v.basis, v.seen, v.ref)
	return av
}

func (e *Engine) objectEvidence(attrs []AttributeValue) *ObjectEvidence {
	u := newUsage(e.clock.Now())
	var worst *WorstFreshness
	for _, a := range attrs {
		if a.Masked || a.Stale {
			continue
		}
		u.add(model.Grade(a.Grade), model.Basis(a.Basis), a.SeenAt, a.SourceRef)
		t, err := time.Parse(time.RFC3339, a.SeenAt)
		if err != nil {
			continue
		}
		age := int64(e.clock.Now().Sub(t) / time.Second)
		if worst == nil || age > worst.OldestAgeSeconds {
			worst = &WorstFreshness{AttributeName: a.Name, SourceRef: a.SourceRef, OldestAgeSeconds: age}
		}
	}
	ev := u.evidence()
	return &ObjectEvidence{MinGrade: ev.MinGrade, Counts: ev.Counts, WorstFreshness: worst}
}

func (e *Engine) identifiers(ctx context.Context, r *read, id graph.ObjectID) ([]IdentifierItem, error) {
	lister, ok := r.snap.(graph.IdentifierLister)
	if !ok {
		return nil, kernel.Errorf(kernel.CodeUnsupported, "this store cannot list the identifiers of an object")
	}
	recs, err := lister.IdentifiersOf(ctx, id)
	if err != nil {
		return nil, err
	}
	out := []IdentifierItem{}
	for _, rec := range recs {
		name := rec.Scheme
		for _, s := range e.reg.IdentifierSchemes() {
			if s.ID.String() == rec.Scheme {
				name = s.Name
			}
		}
		out = append(out, IdentifierItem{Scheme: name, Value: rec.Value, Scope: rec.Scope})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scheme != out[j].Scheme {
			return out[i].Scheme < out[j].Scheme
		}
		return out[i].Value < out[j].Value
	})
	return out, nil
}

// linkCounts counts the links of an object by type and direction. A link whose
// other end is hidden is not counted; the hidden end is noted as reached.
func (e *Engine) linkCounts(ctx context.Context, r *read, id graph.ObjectID) (*LinkCounts, error) {
	lc := &LinkCounts{Groups: []LinkCountGroup{}}
	groups := map[[2]string]int{}
	rows, err := r.snap.LinksOf(ctx, id, graph.Both)
	if err != nil {
		return nil, err
	}
	for _, l := range rows {
		if !r.designed(l.Basis) {
			continue
		}
		dir, peer := "out", l.Dst
		if l.Src != id {
			dir, peer = "in", l.Src
		}
		pv, err := e.object(ctx, r, peer)
		if err != nil {
			return nil, err
		}
		if !pv.ok {
			continue
		}
		if !pv.dec.Visible {
			r.hid.reached[peer.String()] = true
			continue
		}
		groups[[2]string{e.links[l.TypeNo].Name, dir}]++
		if dir == "out" {
			lc.Out++
		} else {
			lc.In++
		}
	}
	for k, n := range groups {
		lc.Groups = append(lc.Groups, LinkCountGroup{LinkType: k[0], Direction: k[1], Count: n})
	}
	sort.Slice(lc.Groups, func(i, j int) bool {
		if lc.Groups[i].LinkType != lc.Groups[j].LinkType {
			return lc.Groups[i].LinkType < lc.Groups[j].LinkType
		}
		return lc.Groups[i].Direction < lc.Groups[j].Direction
	})
	return lc, nil
}
