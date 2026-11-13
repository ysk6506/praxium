package devload

import (
	"context"
	"fmt"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// run is one load in progress.
type run struct {
	l     Loader
	w     *world
	a     *graph.Applier
	kg    graph.KGID
	lease graph.Lease
	rep   *Report
	attrs attrBuilder
}

// apply sends one batch under a name that depends only on its place in the load, so a replay repeats it.
func (r *run) apply(ctx context.Context, b graph.Batch) error {
	b.ApplyID = fmt.Sprintf("devload-%06d", r.rep.Batches)
	b.Kind = graph.BatchSync
	b.SourceID = sourceID
	res, err := r.a.Apply(ctx, r.kg, r.lease, b)
	if err != nil {
		return kernel.Wrap(kernel.CodeOf(err), err, fmt.Sprintf("batch %d of the load", r.rep.Batches))
	}
	r.rep.Batches++
	r.rep.Chunks += res.Chunks
	return nil
}

// objects applies the loadable objects in the order that puts every parent before the objects it identifies.
func (r *run) objects(ctx context.Context) error {
	for _, o := range r.w.objects {
		switch {
		case o.rec.NotIngestible != "":
			r.rep.ObjectsNotIngestible++
		case !o.ok:
			r.rep.ObjectsUnkeyed++
		}
	}
	order, step := r.w.order, r.l.size()
	for start := 0; start < len(order); start += step {
		var b graph.Batch
		for _, o := range order[start:min(start+step, len(order))] {
			if err := r.addObject(&b, o); err != nil {
				return err
			}
		}
		if err := r.apply(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) addObject(b *graph.Batch, o *object) error {
	attrs, vol, err := r.attrs.object(o)
	if err != nil {
		return err
	}
	b.Objects = append(b.Objects, graph.ObjectUpsert{Type: o.typ.typ.ID, Key: o.key, Name: nameOf(o), Attrs: attrs,
		SourceRefs: []string{sourceRef + "/" + o.rec.ID}})
	b.Volatile = append(b.Volatile, vol...)
	r.rep.Objects++
	r.rep.VolatileValues += int64(len(vol))
	return r.addIdentifiers(b, o)
}

func (r *run) addIdentifiers(b *graph.Batch, o *object) error {
	for scheme, text := range o.rec.Identifiers {
		sid, err := kernel.ParseID[model.IdentifierSchemeKind](scheme)
		if err != nil {
			return err
		}
		sch, ok := r.l.Reg.IdentifierScheme(sid)
		if !ok {
			return kernel.Errorf(kernel.CodeNotFound, "identifier scheme %s is not in the model", scheme)
		}
		value, err := model.NormalizeValue(sch.Normalizer, text)
		if err != nil || value == "" {
			r.rep.IdentifiersInvalid++
			continue
		}
		b.Identifiers = append(b.Identifiers, graph.IdentifierWrite{Object: o.id, Scheme: sid, Value: value})
		r.rep.Identifiers++
	}
	return nil
}

// links applies the links whose two ends are in the KG, in batches of bounded size. A link of the truth that
// no source can deliver, or one with an end that was not loaded, is counted and left out.
func (r *run) links(ctx context.Context) error {
	var b graph.Batch
	seen := map[[3]string]bool{}
	for _, l := range r.w.edges {
		switch {
		case l.rec.NotIngestible != "":
			r.rep.LinksNotIngestible++
			continue
		case !l.from.ok || !l.to.ok:
			r.rep.LinksDangling++
			continue
		}
		up, err := r.upsert(l, seen)
		if err != nil {
			return err
		}
		b.Links = append(b.Links, up)
		r.rep.Links++
		if len(b.Links) >= r.l.size() {
			if err := r.apply(ctx, b); err != nil {
				return err
			}
			b = graph.Batch{}
		}
	}
	if len(b.Links) == 0 {
		return nil
	}
	return r.apply(ctx, b)
}

// upsert builds the link. Two links of one type between the same objects are told apart by the truth id of the later one.
func (r *run) upsert(l *link, seen map[[3]string]bool) (graph.LinkUpsert, error) {
	attrs, err := r.attrs.link(l)
	if err != nil {
		return graph.LinkUpsert{}, err
	}
	up := graph.LinkUpsert{Type: l.typ.ID, Src: l.from.id, Dst: l.to.id, Grade: model.Grade(l.rec.Grade),
		Basis: model.Basis(l.rec.Basis), Attrs: attrs, SourceRefs: []string{sourceRef + "/" + l.rec.ID}}
	ends := [3]string{l.typ.ID.String(), l.from.id.String(), l.to.id.String()}
	if seen[ends] {
		up.Discriminator = l.rec.ID
	}
	seen[ends] = true
	return up, nil
}

// seal seals the loaded state as a version and fills the last fields of the report.
func (r *run) seal(ctx context.Context) (Report, error) {
	res, err := r.a.Seal(ctx, r.kg, r.lease, graph.SealSync)
	if err != nil {
		return *r.rep, err
	}
	head, err := r.l.Store.Head(ctx, r.kg)
	if err != nil {
		return *r.rep, err
	}
	r.rep.ChangeSeq = head.ReadableThrough
	r.rep.VersionID = res.Version.ID.String()
	r.rep.GraphHash = res.Version.GraphHash.String()
	return *r.rep, nil
}
