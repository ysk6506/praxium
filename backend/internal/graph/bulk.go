package graph

import (
	"context"
)

// BulkReader is an optional capability of a Snapshot: several objects, links or the links at several objects in
// one read, where the port reads one at a time. The applier asks for what a chunk or a batch touches in a few
// reads instead of one read per row, when the snapshot has it. An id that is not live in the state is not in the
// answer; the links at an object are in link id order, as LinksOf gives them. A store that does not have the
// capability (the reference stores) is read one at a time and the applier does the same work.
type BulkReader interface {
	ObjectsByID(ctx context.Context, ids []ObjectID) (map[ObjectID]ObjectRow, error)
	LinksByID(ctx context.Context, ids []LinkID) (map[LinkID]LinkRow, error)
	LinksOfMany(ctx context.Context, ids []ObjectID, dir Direction) (map[ObjectID][]LinkRow, error)
}

// bulkSnap is a Snapshot that answers Object, Link and LinksOf from what a bulk read fetched and asks the
// snapshot for the rest. A row that a bulk read did not find is remembered as not there: the snapshot is one
// state, so the answer does not change.
type bulkSnap struct {
	Snapshot
	bulk  BulkReader
	objs  map[ObjectID]*ObjectRow
	links map[LinkID]*LinkRow
	of    map[ofKey][]LinkRow
}

type ofKey struct {
	id  ObjectID
	dir Direction
}

// withBulk returns the snapshot with the cache of the bulk reads when it can bulk read, and the snapshot itself
// when it cannot.
func withBulk(s Snapshot) Snapshot {
	if _, ok := s.(*bulkSnap); ok {
		return s
	}
	b, ok := s.(BulkReader)
	if !ok {
		return s
	}
	return &bulkSnap{Snapshot: s, bulk: b, objs: map[ObjectID]*ObjectRow{}, links: map[LinkID]*LinkRow{}, of: map[ofKey][]LinkRow{}}
}

// prefetchObjects reads the objects that are not known yet in one read. It does nothing for a snapshot that
// cannot bulk read.
func prefetchObjects(ctx context.Context, s Snapshot, ids []ObjectID) error {
	b, ok := s.(*bulkSnap)
	if !ok {
		return nil
	}
	var want []ObjectID
	for _, id := range ids {
		if _, known := b.objs[id]; !known {
			b.objs[id] = nil
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return nil
	}
	got, err := b.bulk.ObjectsByID(ctx, want)
	if err != nil {
		return err
	}
	for id, row := range got {
		row := row
		b.objs[id] = &row
	}
	return nil
}

// prefetchLinks is prefetchObjects for links.
func prefetchLinks(ctx context.Context, s Snapshot, ids []LinkID) error {
	b, ok := s.(*bulkSnap)
	if !ok {
		return nil
	}
	var want []LinkID
	for _, id := range ids {
		if _, known := b.links[id]; !known {
			b.links[id] = nil
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return nil
	}
	got, err := b.bulk.LinksByID(ctx, want)
	if err != nil {
		return err
	}
	for id, row := range got {
		row := row
		b.links[id] = &row
	}
	return nil
}

// prefetchLinksOf reads the links at the objects, for one direction, in one read.
func prefetchLinksOf(ctx context.Context, s Snapshot, ids []ObjectID, dir Direction) error {
	b, ok := s.(*bulkSnap)
	if !ok {
		return nil
	}
	var want []ObjectID
	for _, id := range ids {
		if _, known := b.of[ofKey{id, dir}]; !known {
			b.of[ofKey{id, dir}] = nil
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return nil
	}
	got, err := b.bulk.LinksOfMany(ctx, want, dir)
	if err != nil {
		return err
	}
	for id, rows := range got {
		b.of[ofKey{id, dir}] = rows
	}
	return nil
}

func (b *bulkSnap) Object(ctx context.Context, id ObjectID) (ObjectRow, bool, error) {
	if row, known := b.objs[id]; known {
		if row == nil {
			return ObjectRow{}, false, nil
		}
		return row.Clone(), true, nil
	}
	return b.Snapshot.Object(ctx, id)
}

func (b *bulkSnap) Link(ctx context.Context, id LinkID) (LinkRow, bool, error) {
	if row, known := b.links[id]; known {
		if row == nil {
			return LinkRow{}, false, nil
		}
		return row.Clone(), true, nil
	}
	return b.Snapshot.Link(ctx, id)
}

func (b *bulkSnap) LinksOf(ctx context.Context, id ObjectID, dir Direction) ([]LinkRow, error) {
	if rows, known := b.of[ofKey{id, dir}]; known {
		out := make([]LinkRow, len(rows))
		for i, r := range rows {
			out[i] = r.Clone()
		}
		return out, nil
	}
	return b.Snapshot.LinksOf(ctx, id, dir)
}
