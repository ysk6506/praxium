package graph

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// chunkBuilder diffs the candidates of one chunk against the applied state and
// keeps the new hash state. Rows get their change number in assign.
type chunkBuilder struct {
	a    *Applier
	kg   KGID
	snap Snapshot
	hs   hasher
	hash HashState

	objects  []ObjectChange
	links    []LinkChange
	entries  []ChangeEntry
	volatile []VolatileRow
	light    []LightChange
	idents   []IdentifierChange
	events   []IdentityEvent
}

func newChunkBuilder(a *Applier, kg KGID, snap Snapshot, hash HashState) *chunkBuilder {
	return &chunkBuilder{a: a, kg: kg, snap: snap, hs: hasher{reg: a.reg, keys: a.keys, kg: kg}, hash: hash.Clone()}
}

func (c *chunkBuilder) empty() bool {
	return len(c.objects)+len(c.links)+len(c.volatile)+len(c.idents)+len(c.events) == 0
}

func (c *chunkBuilder) build(ctx context.Context, p *plan, items []item, last bool) error {
	objects, links := map[string]plannedObject{}, map[string]plannedLink{}
	for _, o := range p.objects {
		objects[o.id.String()] = o
	}
	for _, l := range p.links {
		links[l.id.String()] = l
	}
	for _, it := range items {
		var err error
		switch {
		case it.entity == entityObject && it.retire:
			err = c.retireObject(ctx, p.retObjs[it.index])
		case it.entity == entityObject:
			err = c.upsertObject(ctx, objects[it.id])
		case it.retire:
			err = c.retireLink(ctx, p.retLinks[it.index])
		default:
			err = c.upsertLink(ctx, links[it.id])
		}
		if err != nil {
			return err
		}
	}
	if !last {
		return nil
	}
	if err := c.writeVolatile(ctx, p.volatile); err != nil {
		return err
	}
	if err := c.writeIdentifiers(ctx, p.idents); err != nil {
		return err
	}
	c.events = append(c.events, p.events...)
	return nil
}

func (c *chunkBuilder) objectKey(typeNo int) (PartKey, error) {
	t, ok := c.a.reg.ObjectTypeByNo(typeNo)
	if !ok {
		return PartKey{}, kernel.Errorf(kernel.CodeInternal, "stored row has unknown type number %d", typeNo)
	}
	return PartKey{Entity: entityObject, TypeID: t.ID.String()}, nil
}

func (c *chunkBuilder) linkKey(typeNo int) (PartKey, error) {
	t, ok := c.a.reg.LinkTypeByNo(typeNo)
	if !ok {
		return PartKey{}, kernel.Errorf(kernel.CodeInternal, "stored row has unknown link type number %d", typeNo)
	}
	return PartKey{Entity: entityLink, TypeID: t.ID.String()}, nil
}

func (c *chunkBuilder) upsertObject(ctx context.Context, po plannedObject) error {
	row := ObjectRow{ID: po.id, TypeNo: po.typ.TypeNo, Name: po.upsert.Name, Attrs: cloneAttrs(po.upsert.Attrs),
		SourceRefs: append([]string(nil), po.upsert.SourceRefs...), MinGrade: minGrade(po.upsert.Attrs)}
	content, err := c.hs.objectContent(row)
	if err != nil {
		return err
	}
	row.ContentHash = content
	old, found, err := c.snap.Object(ctx, po.id)
	if err != nil {
		return err
	}
	key := PartKey{Entity: entityObject, TypeID: po.typ.ID.String()}
	entry := ChangeEntry{Entity: entityObject, ID: po.id.String(), Change: ChangeAdded, NewHash: content}
	change := ObjectChange{New: &row}
	if found {
		if old.ContentHash == content {
			return nil
		}
		entry.Change, entry.OldHash = ChangeChanged, old.ContentHash
		change.Old = &old
		if err := c.count(key, entityObject, old.ID.String(), old.ContentHash, true); err != nil {
			return err
		}
	}
	if err := c.count(key, entityObject, row.ID.String(), content, false); err != nil {
		return err
	}
	c.objects, c.entries = append(c.objects, change), append(c.entries, entry)
	return nil
}

func (c *chunkBuilder) retireObject(ctx context.Context, id ObjectID) error {
	old, found, err := c.snap.Object(ctx, id)
	if err != nil || !found {
		return err
	}
	key, err := c.objectKey(old.TypeNo)
	if err != nil {
		return err
	}
	if err := c.count(key, entityObject, id.String(), old.ContentHash, true); err != nil {
		return err
	}
	c.objects = append(c.objects, ObjectChange{Old: &old})
	c.entries = append(c.entries, ChangeEntry{Entity: entityObject, ID: id.String(), Change: ChangeRetired, OldHash: old.ContentHash})
	return nil
}

func (c *chunkBuilder) upsertLink(ctx context.Context, pl plannedLink) error {
	u := pl.upsert
	row := LinkRow{ID: pl.id, TypeNo: pl.typ.LinkTypeNo, Src: u.Src, Dst: u.Dst, Discriminator: u.Discriminator,
		Grade: u.Grade, Basis: u.Basis, Attrs: cloneAttrs(u.Attrs), SourceRefs: append([]string(nil), u.SourceRefs...)}
	content, err := c.hs.linkContent(row)
	if err != nil {
		return err
	}
	row.ContentHash = content
	old, found, err := c.snap.Link(ctx, pl.id)
	if err != nil {
		return err
	}
	key := PartKey{Entity: entityLink, TypeID: pl.typ.ID.String()}
	entry := ChangeEntry{Entity: entityLink, ID: pl.id.String(), Change: ChangeAdded, NewHash: content}
	change := LinkChange{New: &row}
	if found {
		if old.ContentHash == content {
			return nil
		}
		entry.Change, entry.OldHash = ChangeChanged, old.ContentHash
		change.Old = &old
		if err := c.count(key, entityLink, old.ID.String(), old.ContentHash, true); err != nil {
			return err
		}
	}
	if err := c.count(key, entityLink, row.ID.String(), content, false); err != nil {
		return err
	}
	c.links, c.entries = append(c.links, change), append(c.entries, entry)
	return nil
}

func (c *chunkBuilder) retireLink(ctx context.Context, id LinkID) error {
	old, found, err := c.snap.Link(ctx, id)
	if err != nil || !found {
		return err
	}
	key, err := c.linkKey(old.TypeNo)
	if err != nil {
		return err
	}
	if err := c.count(key, entityLink, id.String(), old.ContentHash, true); err != nil {
		return err
	}
	c.links = append(c.links, LinkChange{Old: &old})
	c.entries = append(c.entries, ChangeEntry{Entity: entityLink, ID: id.String(), Change: ChangeRetired, OldHash: old.ContentHash})
	return nil
}

// count moves one row in or out of the running hash.
func (c *chunkBuilder) count(key PartKey, entity, id string, content kernel.Hash, remove bool) error {
	sum, err := rowSum(entity, id, content)
	if err != nil {
		return err
	}
	if remove {
		c.hash.Remove(key, sum)
	} else {
		c.hash.Add(key, sum)
	}
	return nil
}

func minGrade(attrs map[int]Attr) model.Grade {
	grades := make([]model.Grade, 0, len(attrs))
	for _, a := range attrs {
		grades = append(grades, a.Grade)
	}
	return model.Lowest(grades...)
}

func (c *chunkBuilder) writeVolatile(ctx context.Context, writes []VolatileWrite) error {
	for _, w := range writes {
		cur, found, err := c.snap.Volatile(ctx, w.Object, w.AttrNo)
		if err != nil {
			return err
		}
		mark, err := ValueMark(w.Value)
		if err != nil {
			return err
		}
		if found {
			curMark, err := ValueMark(cur.Value)
			if err != nil {
				return err
			}
			if curMark == mark && cur.Grade == w.Grade && cur.Basis == w.Basis {
				continue
			}
		}
		c.volatile = append(c.volatile, VolatileRow{Object: w.Object, AttrNo: w.AttrNo, Value: cloneValue(w.Value),
			Grade: w.Grade, Basis: w.Basis, SeenAt: w.SeenAt, Ref: w.Ref})
		c.light = append(c.light, LightChange{Object: w.Object, AttrNo: w.AttrNo, ValueMark: mark})
	}
	return nil
}

func (c *chunkBuilder) writeIdentifiers(ctx context.Context, writes []IdentifierWrite) error {
	for _, w := range writes {
		recs, err := c.snap.Identifiers(ctx, w.Scheme.String(), w.Value)
		if err != nil {
			return err
		}
		held := false
		for _, r := range recs {
			if r.Object == w.Object && r.Scope == w.Scope {
				held = true
			}
		}
		if held == w.Close {
			rec := IdentifierRecord{Object: w.Object, Scheme: w.Scheme.String(), Value: w.Value, Scope: w.Scope}
			c.idents = append(c.idents, IdentifierChange{Record: rec, Closes: w.Close})
		}
	}
	return nil
}

// assign gives every new row, entry and line the change number of the chunk.
func (c *chunkBuilder) assign(seq int64) {
	for i := range c.objects {
		if c.objects[i].New != nil {
			c.objects[i].New.SeqFrom = seq
		}
	}
	for i := range c.links {
		if c.links[i].New != nil {
			c.links[i].New.SeqFrom = seq
		}
	}
	for i := range c.entries {
		c.entries[i].Seq = seq
	}
	for i := range c.light {
		c.light[i].Seq = seq
	}
	for i := range c.idents {
		c.idents[i].Record.SeqFrom = seq
	}
	for i := range c.events {
		c.events[i].Seq = seq
	}
}
