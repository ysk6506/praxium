package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"regexp"
	"sort"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// The second step of the equality test: a change batch the test builds by hand (an attribute changes, a link goes and
// another is added, an object is retired with its links, an identifier is replaced) goes into the rows path as a batch
// and into the staged apply as the difference of the same rows. There is no calculation of a difference here: the
// batch says what changes, and the chunks the rows path wrote for it are what is staged.

// sapFacts are the rows of the first load the batch is built from.
type sapFacts struct {
	reg     *model.Registry
	objects []graph.ObjectChange
	links   []graph.LinkRow
	idents  []graph.IdentifierRecord
}

func sapFactsOf(reg *model.Registry, chunks []graph.ChunkWrite) sapFacts {
	f := sapFacts{reg: reg}
	for _, w := range chunks {
		for _, o := range w.Objects {
			if o.New != nil && o.Old == nil {
				f.objects = append(f.objects, o)
			}
		}
		for _, l := range w.Links {
			if l.New != nil && l.Old == nil {
				f.links = append(f.links, *l.New)
			}
		}
		for _, i := range w.Identifiers {
			if !i.Closes {
				f.idents = append(f.idents, i.Record)
			}
		}
	}
	return f
}

func (f sapFacts) sapTypeID(no int) model.ObjectTypeID {
	for _, t := range f.reg.ObjectTypes() {
		if t.TypeNo == no {
			return t.ID
		}
	}
	return model.ObjectTypeID{}
}

func (f sapFacts) sapLinkTypeID(no int) model.LinkTypeID {
	for _, t := range f.reg.LinkTypes() {
		if t.LinkTypeNo == no {
			return t.ID
		}
	}
	return model.LinkTypeID{}
}

// sapChanged is the upsert of an object whose first plain text attribute (by attribute number) and name are changed.
// The attributes are visited in the order of their numbers, not of the map, so the same world always changes the same
// attribute; and only an attribute whose value type is text and takes the changed value is picked (a time is text too).
func (f sapFacts) sapChanged(t *testing.T) (graph.ObjectUpsert, graph.ObjectID) {
	t.Helper()
	for _, o := range f.objects {
		nos := make([]int, 0, len(o.New.Attrs))
		for no := range o.New.Attrs {
			nos = append(nos, no)
		}
		sort.Ints(nos)
		for _, no := range nos {
			a := o.New.Attrs[no]
			changed, ok := f.sapChangedText(no, a.Value)
			if !ok {
				continue
			}
			attrs := maps.Clone(o.New.Attrs)
			a.Value = changed
			attrs[no] = a
			return graph.ObjectUpsert{Type: f.sapTypeID(o.New.TypeNo), Key: o.Key, Name: o.New.Name + " sap", Attrs: attrs, SourceRefs: o.New.SourceRefs}, o.New.ID
		}
	}
	t.Fatal("no object with a plain text attribute")
	return graph.ObjectUpsert{}, graph.ObjectID{}
}

// sapChangedText gives the changed value of a text attribute, if the attribute's value type is text and takes it.
func (f sapFacts) sapChangedText(no int, value any) (string, bool) {
	s, ok := value.(string)
	if !ok || s == "" {
		return "", false
	}
	p, ok := f.reg.PropertyByNo(no)
	if !ok {
		return "", false
	}
	vt, ok := f.reg.ValueType(p.Property.ValueType)
	if !ok || vt.Base != model.BaseString {
		return "", false
	}
	changed := s + "-sap"
	if _, err := model.NormalizeValue(p.Property.ValueType, changed); err != nil {
		return "", false
	}
	return changed, true
}

// sapRetired picks an object that ends a link and gives it with all its links, whoever made them.
func (f sapFacts) sapRetired() (graph.ObjectID, []graph.LinkID) {
	last := f.links[len(f.links)-1]
	var gone []graph.LinkID
	for _, l := range f.links {
		if l.Src == last.Src || l.Dst == last.Src {
			gone = append(gone, l.ID)
		}
	}
	return last.Src, gone
}

// sapBatch builds the change batch from the facts: the attribute change, a link that goes, a link that is added
// (a second link of a type between two objects, told apart by a discriminator), the retired object with its links and
// the identifier that is replaced.
func sapBatch(t *testing.T, f sapFacts) graph.Batch {
	t.Helper()
	up, changedID := f.sapChanged(t)
	b := graph.Batch{ApplyID: "sap-step-2", Kind: graph.BatchSync, SourceID: "sap", Objects: []graph.ObjectUpsert{up}}
	retire, links := f.sapRetired()
	b.RetireObjects, b.RetireLinks = []graph.ObjectID{retire}, links
	for _, l := range f.links {
		if l.Src != retire && l.Dst != retire && l.Src != changedID && l.Dst != changedID {
			b.RetireLinks = append(b.RetireLinks, l.ID)
			b.Links = append(b.Links, graph.LinkUpsert{Type: f.sapLinkTypeID(l.TypeNo), Src: l.Src, Dst: l.Dst, Discriminator: "sap-added",
				Grade: l.Grade, Basis: l.Basis, SourceRefs: l.SourceRefs})
			break
		}
	}
	for _, i := range f.idents {
		if id, ok := f.sapReplaced(i, retire); ok {
			b.Identifiers = []graph.IdentifierWrite{{Object: i.Object, Scheme: id.scheme, Value: i.Value, Scope: i.Scope, Close: true},
				{Object: i.Object, Scheme: id.scheme, Value: id.value, Scope: i.Scope}}
			break
		}
	}
	return b
}

type sapIdent struct {
	scheme model.IdentifierSchemeID
	value  string
}

// sapReplaced finds a value that the scheme of an identifier takes in place of its value.
func (f sapFacts) sapReplaced(i graph.IdentifierRecord, not graph.ObjectID) (sapIdent, bool) {
	if i.Object == not {
		return sapIdent{}, false
	}
	sid, err := kernel.ParseID[model.IdentifierSchemeKind](i.Scheme)
	if err != nil {
		return sapIdent{}, false
	}
	sch, ok := f.reg.IdentifierScheme(sid)
	if !ok {
		return sapIdent{}, false
	}
	for _, tail := range []string{"7", "x", "0"} {
		if v, err := model.NormalizeValue(sch.Normalizer, i.Value+tail); err == nil && v != "" && v != i.Value {
			return sapIdent{sid, v}, true
		}
	}
	return sapIdent{}, false
}

// sapStepTwo applies the batch to the rows path and, from the chunks that wrote, to the staged apply.
func sapStepTwo(t *testing.T, p *sapPair, reg *model.Registry) (graph.Batch, graph.ObjectID) {
	t.Helper()
	ctx := context.Background()
	facts := sapFactsOf(reg, p.first)
	b := sapBatch(t, facts)
	_, changed := facts.sapChanged(t)
	var err error
	p.rows.lease, err = p.rows.app.Acquire(ctx, p.rows.kg.ID)
	noErr(t, "lease", err)
	_, err = p.rows.app.Apply(ctx, p.rows.kg.ID, p.rows.lease, b)
	noErr(t, "rows path, second step", err)
	p.sapStageChunks(t, p.rec.sapTake())
	if got := sapRun(t, p.staged, "sap-2"); got.State != graph.StagedDone {
		t.Fatalf("the second apply ended in %q", got.State)
	}
	return b, changed
}

// sapSameObject compares the answers about one object, at the newest change number and at the end of the first load.
func sapSameObject(t *testing.T, a, b sapSide, id graph.ObjectID, label string) int {
	t.Helper()
	ctx := context.Background()
	for _, pair := range [][2]*int64{{nil, nil}, {&a.first, &b.first}} {
		ga := query.GetInput{Common: query.Common{KG: graph.MainAlias, ChangeSeq: pair[0]}, ObjectID: id.String(),
			Include: []string{query.IncludeAttributes, query.IncludeIdentifiers, query.IncludeLinkCounts}}
		gb := ga
		gb.Common = query.Common{KG: graph.MainAlias, ChangeSeq: pair[1]}
		sapAnswer(t, label+" get "+id.String(), func(e *query.Engine, in query.GetInput) (string, error) {
			r, err := e.Get(ctx, in, query.AllowAll())
			if err != nil {
				return "", err
			}
			return sapJSON(r.Meta, r.Data), nil
		}, a.engine, b.engine, ga, gb)
	}
	return 2
}

// sapPoints are the change numbers a read is compared at: the newest and the one the first load ended at.
type sapSide struct {
	engine *query.Engine
	first  int64
}

func sapEngine(t *testing.T, k *sapKG, store graph.Store) sapSide {
	t.Helper()
	e, err := query.New(store, k.reg, k.clock)
	noErr(t, "engine", err)
	h, err := store.Head(context.Background(), k.kg.ID)
	noErr(t, "head", err)
	return sapSide{engine: e, first: h.ReadableThrough}
}

// sapSameReads compares what the query engine answers on the two sides (find by type, neighbors, one object with its
// attributes, identifiers and link counts) at the newest change number and at the end of the first load.
func sapSameReads(t *testing.T, a, b sapSide, label string) int {
	t.Helper()
	ctx := context.Background()
	compared := 0
	for _, pair := range [][2]*int64{{nil, nil}, {&a.first, &b.first}} {
		ca, cb := query.Common{KG: graph.MainAlias, ChangeSeq: pair[0]}, query.Common{KG: graph.MainAlias, ChangeSeq: pair[1]}
		first, err := a.engine.Find(ctx, query.FindInput{Common: ca}, query.AllowAll())
		noErr(t, label+" find", err)
		if len(first.Data.CountsByType) < 3 {
			t.Fatalf("%s: the world has %d types", label, len(first.Data.CountsByType))
		}
		someone := ""
		for _, c := range first.Data.CountsByType[:3] {
			in := query.FindInput{ObjectType: c.ObjectType, Common: ca}
			in.Limit, in.Shape = 15, query.ShapeItems
			ia, ib := in, in
			ib.Common = cb
			ib.Limit, ib.Shape = 15, query.ShapeItems
			sapAnswer(t, label+" find "+c.ObjectType, func(e *query.Engine, in query.FindInput) (string, error) {
				r, err := e.Find(ctx, in, query.AllowAll())
				if err != nil {
					return "", err
				}
				if someone == "" && len(r.Data.Items) > 0 {
					someone = r.Data.Items[0].ObjectID
				}
				return sapJSON(r.Meta, r.Data), nil
			}, a.engine, b.engine, ia, ib)
			compared++
		}
		compared += sapAroundOne(t, a, b, ca, cb, someone, label)
	}
	return compared
}

// sapAnswer asks both engines and fails when the answers (or the codes of their refusals) differ.
func sapAnswer[In any](t *testing.T, what string, ask func(*query.Engine, In) (string, error), ea, eb *query.Engine, ia, ib In) string {
	t.Helper()
	x, errA := ask(ea, ia)
	y, errB := ask(eb, ib)
	if (errA == nil) != (errB == nil) || kernel.CodeOf(errA) != kernel.CodeOf(errB) {
		t.Fatalf("%s: rows path answered with %v, staged with %v", what, errA, errB)
	}
	if x != y {
		at := 0
		for at < len(x) && at < len(y) && x[at] == y[at] {
			at++
		}
		from := max(at-150, 0)
		t.Fatalf("%s differs at %d:\nrows   ...%.400s\nstaged ...%.400s", what, at, x[from:], y[from:])
	}
	return x
}

func sapJSON(meta query.Envelope, data any) string {
	raw, err := json.Marshal(struct {
		Evidence query.Evidence       `json:"evidence"`
		Complete query.ResultComplete `json:"complete"`
		Data     any                  `json:"data"`
	}{meta.Evidence, meta.ResultComplete, data})
	if err != nil {
		return err.Error()
	}
	return sapSeqText.ReplaceAllString(string(raw), `_change_seq":"#"`)
}

// sapSeqText finds the change numbers in an answer. The two paths cut a world into chunks in their own ways, so the
// change numbers differ; what the answers say is the same.
var sapSeqText = regexp.MustCompile(`_change_seq":\d+`)

// sapAroundOne compares the neighbors of one object (three directions) and the object itself.
func sapAroundOne(t *testing.T, a, b sapSide, ca, cb query.Common, someone, label string) int {
	t.Helper()
	ctx := context.Background()
	n := 0
	for _, dir := range []string{"out", "in", "both"} {
		ia := query.NeighborsInput{Common: ca, ObjectID: someone, Direction: dir, MaxSteps: 2}
		ib := ia
		ib.Common = cb
		sapAnswer(t, label+" neighbors "+dir, func(e *query.Engine, in query.NeighborsInput) (string, error) {
			r, err := e.Neighbors(ctx, in, query.AllowAll())
			if err != nil {
				return "", err
			}
			return sapJSON(r.Meta, r.Data), nil
		}, a.engine, b.engine, ia, ib)
		n++
	}
	ga := query.GetInput{Common: ca, ObjectID: someone, Include: []string{query.IncludeAttributes, query.IncludeIdentifiers, query.IncludeLinkCounts}}
	gb := ga
	gb.Common = cb
	sapAnswer(t, label+" get", func(e *query.Engine, in query.GetInput) (string, error) {
		r, err := e.Get(ctx, in, query.AllowAll())
		if err != nil {
			return "", err
		}
		return sapJSON(r.Meta, r.Data), nil
	}, a.engine, b.engine, ga, gb)
	return n + 1
}

// sapSameState fails when the head hashes of the two sides differ or the staged side's rows do not give its hash.
func sapSameState(t *testing.T, p *sapPair, label string) {
	t.Helper()
	ctx := context.Background()
	if p.rows != nil {
		ha, err := p.rows.st.Head(ctx, p.rows.kg.ID)
		noErr(t, label+" head, rows path", err)
		hb, err := p.staged.st.Head(ctx, p.staged.kg.ID)
		noErr(t, label+" head, staged", err)
		if !ha.Hash.Equal(hb.Hash) {
			t.Fatalf("%s: the state hashes differ: rows path %+v, staged %+v", label, ha.Hash.Rows, hb.Hash.Rows)
		}
	}
	noErr(t, label+" verify, staged", p.staged.app.Verify(ctx, p.staged.kg.ID))
}

// The same world loaded by the rows path and by the staged apply has the same state hash and the same answers to the
// reads; a second step of changes made by hand, applied to both the same way, keeps them the same.
//
// [REQ-720]
func TestSapStagedApplyAndRowsPathGiveTheSameStateAndReads(t *testing.T) {
	reg, err := model.LoadStandard()
	noErr(t, "model", err)
	p := sapLoadBoth(t, reg, sapTruth(t, reg, 9, 500), 200)
	if got := sapRun(t, p.staged, "sap-1"); got.State != graph.StagedDone || got.ChunkCount < 3 {
		t.Fatalf("the first apply: %+v", got)
	}
	steps, wrote := sapVolatile(t, p.staged, "sap-1", 400)
	if steps < 2 || wrote == 0 {
		t.Fatalf("the volatile values went in %d intervals, %d values", steps, wrote)
	}
	_, err = p.staged.app.Seal(context.Background(), p.staged.kg.ID, p.staged.lease, graph.SealSync)
	noErr(t, "seal, staged", err)
	sapSameState(t, p, "first load")
	a, b := sapEngine(t, p.rows, p.rows.st), sapEngine(t, p.staged, p.staged.st)
	n := sapSameReads(t, a, b, "first load")
	batch, changed := sapStepTwo(t, p, reg)
	sapSameState(t, p, "second step")
	n += sapSameReads(t, a, b, "second step")
	n += sapSameObject(t, a, b, changed, "the changed object")
	n += sapSameObject(t, a, b, batch.RetireObjects[0], "the retired object")
	sapStepTwoDidSomething(t, p.staged, batch)
	if n < 32 {
		t.Fatalf("only %d answers were compared", n)
	}
}

// sapStepTwoDidSomething checks that the second apply of the staged side did what the batch said: the changed object
// has a history row, the retired object and its links are closed, an identifier is closed, a link was added.
func sapStepTwoDidSomething(t *testing.T, k *sapKG, b graph.Batch) {
	t.Helper()
	ctx := context.Background()
	a, err := k.st.access(ctx, k.kg.ID)
	noErr(t, "access", err)
	count := func(q string, args ...any) int {
		var n int
		noErr(t, q, a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error { return tx.QueryRowContext(ctx, q, args...).Scan(&n) }))
		return n
	}
	if n := count(`SELECT count(*) FROM ` + a.t(pgschema.TableObject) + ` WHERE retired_seq IS NOT NULL`); n != 1 {
		t.Fatalf("%d objects are retired in the registry, wanted 1", n)
	}
	if n := count(`SELECT count(*) FROM ` + a.t(pgschema.TableObjectHist)); n < 2 {
		t.Fatalf("the object history has %d rows, wanted at least 2", n)
	}
	if n := count(`SELECT count(*) FROM ` + a.t(pgschema.TableLinkHist)); n < len(b.RetireLinks) {
		t.Fatalf("the link history has %d rows, wanted at least %d", n, len(b.RetireLinks))
	}
	if n := count(`SELECT count(*) FROM ` + a.t(pgschema.TableObjectIdentifier) + ` WHERE seq_to IS NOT NULL`); n < 1 && len(b.Identifiers) > 0 {
		t.Fatalf("no identifier was closed")
	}
	if len(b.Links) == 0 || len(b.Identifiers) == 0 {
		t.Fatalf("the batch has %d links and %d identifier writes, wanted both", len(b.Links), len(b.Identifiers))
	}
}
