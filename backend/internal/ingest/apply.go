package ingest

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// batch builds the change batch of the run: every object and link the source says should be there, and the retirements
// of what it no longer says. The applier finds what really changed, so an unchanged object costs no change.
// It also brings the objects and links of the state up to what the batch will be.
func (r Runner) batch(ctx context.Context, d *derivation, st *State, kg graph.KGID, objects []desiredObject) (graph.Batch, error) {
	b := graph.Batch{ApplyID: fmt.Sprintf("ingest-%s-%06d", d.p.Source.ID, st.Run), Kind: graph.BatchSync, SourceID: d.p.Source.ID}
	for _, o := range objects {
		b.Objects = append(b.Objects, o.upsert)
		b.Volatile = append(b.Volatile, o.volatile...)
		b.Identifiers = append(b.Identifiers, o.idents...)
		b.Identifiers = append(b.Identifiers, staleIdentifiers(st, o)...)
		d.rep.VolatileValues += len(o.volatile)
	}
	d.rep.Objects = len(objects)
	retired := retiredObjects(st, d)
	snap, err := r.Store.Snapshot(ctx, graph.ReadPoint{KG: kg, Kind: graph.ReadLatest})
	if err != nil {
		return b, err
	}
	links := map[string]bool{}
	for id := range st.Links {
		if _, keep := d.links[id]; !keep {
			links[id] = true
		}
	}
	for _, id := range retired {
		rows, err := snap.LinksOf(ctx, id, graph.Both)
		if err != nil {
			return b, err
		}
		for _, l := range rows {
			links[l.ID.String()] = true
		}
		b.RetireObjects = append(b.RetireObjects, id)
		b.Identifiers = append(b.Identifiers, closeIdentifiers(st.Objects[id.String()], id)...)
	}
	return r.finishBatch(b, d, st, links, objects, retired)
}

func (r Runner) finishBatch(b graph.Batch, d *derivation, st *State, links map[string]bool, objects []desiredObject, retired []graph.ObjectID) (graph.Batch, error) {
	ids := make([]string, 0, len(d.links))
	for id := range d.links {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	st.Links = map[string]bool{}
	for _, id := range ids {
		b.Links = append(b.Links, d.links[id].up)
		st.Links[id] = true
		delete(links, id)
	}
	for id := range links {
		lid, err := kernel.ParseID[graph.LinkKind](id)
		if err != nil {
			return b, err
		}
		b.RetireLinks = append(b.RetireLinks, lid)
	}
	sort.Slice(b.RetireLinks, func(i, j int) bool { return b.RetireLinks[i].String() < b.RetireLinks[j].String() })
	d.rep.Links, d.rep.RetiredObjects, d.rep.RetiredLinks = len(b.Links), len(retired), len(b.RetireLinks)
	for _, id := range retired {
		delete(st.Objects, id.String())
	}
	for _, o := range objects {
		info := st.Objects[o.acc.id.String()]
		info.Type = o.acc.typ.ID.String()
		info.Retirable = info.Retirable || o.acc.retires
		info.Idents = identList(o.acc)
		st.Objects[o.acc.id.String()] = info
	}
	return b, nil
}

func identList(acc *objAcc) []string {
	out := make([]string, 0, len(acc.idents))
	for s, v := range acc.idents {
		out = append(out, s+keySep+v)
	}
	sort.Strings(out)
	return out
}

// retiredObjects lists the objects the pipeline made, that a feed able to retire has watched, and that no feed shows
// any more: the rows that showed them were out of use for long enough (rawrec.go) or are gone for another reason.
func retiredObjects(st *State, d *derivation) []graph.ObjectID {
	var out []graph.ObjectID
	for id, info := range st.Objects {
		if info.Retirable && d.objects[id] == nil {
			if oid, err := parseObject(id); err == nil {
				out = append(out, oid)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// staleIdentifiers closes the identifiers an object had and no longer has.
func staleIdentifiers(st *State, o desiredObject) []graph.IdentifierWrite {
	info, ok := st.Objects[o.acc.id.String()]
	if !ok {
		return nil
	}
	now := map[string]bool{}
	for _, e := range identList(o.acc) {
		now[e] = true
	}
	var out []graph.IdentifierWrite
	for _, e := range info.Idents {
		if !now[e] {
			out = append(out, closeOne(o.acc.id, e))
		}
	}
	return out
}

func closeIdentifiers(info ObjectInfo, id graph.ObjectID) []graph.IdentifierWrite {
	out := make([]graph.IdentifierWrite, 0, len(info.Idents))
	for _, e := range info.Idents {
		out = append(out, closeOne(id, e))
	}
	return out
}

func closeOne(id graph.ObjectID, entry string) graph.IdentifierWrite {
	scheme, value, _ := strings.Cut(entry, keySep)
	sid, _ := kernel.ParseID[model.IdentifierSchemeKind](scheme)
	return graph.IdentifierWrite{Object: id, Scheme: sid, Value: value, Close: true}
}

// apply takes the lease, applies the batch and seals. A run that changed nothing leaves the KG as it was, and
// no new version comes of it.
func (r Runner) apply(ctx context.Context, kg graph.KGID, b graph.Batch, rep *Report) error {
	keys := r.Keys
	if keys == nil {
		keys = graph.NewTestKeyProvider("praxium-ingest")
	}
	a, err := graph.NewApplier(r.Store, r.Reg, keys, r.Clock, r.IDs, graph.Options{Semantic: 1, LeaseTTL: leaseTTL, ChunkRows: r.ChunkRows})
	if err != nil {
		return err
	}
	lease, err := a.Acquire(ctx, kg)
	if err != nil {
		return err
	}
	defer func() { _ = a.Release(ctx, lease) }()
	res, err := a.Apply(ctx, kg, lease, b)
	if err != nil {
		return kernel.Wrap(kernel.CodeOf(err), err, "the batch of source "+b.SourceID+" was not applied")
	}
	rep.Changed, rep.FromSeq, rep.ToSeq, rep.IngestSeq = res.Changed(), res.FromSeq, res.ToSeq, res.IngestSeq
	sr, err := a.Seal(ctx, kg, lease, graph.SealSync)
	if err != nil {
		return err
	}
	rep.NewVersion = sr.Created
	if !sr.Version.ID.IsZero() {
		rep.VersionID, rep.GraphHash = sr.Version.ID.String(), sr.Version.GraphHash.String()
	}
	return nil
}
