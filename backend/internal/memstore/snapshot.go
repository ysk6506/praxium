package memstore

import (
	"encoding/gob"
	"io"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The snapshot is one gob stream of parts. Each part holds a slice of one kind of row of one KG, at most partRows
// rows, so a snapshot of millions of rows is written and read without a copy of the whole state.
const (
	snapFormat = 1
	partRows   = 2048

	partHeader   byte = 'H'
	partKG       byte = 'K'
	partObjects  byte = 'o'
	partLinks    byte = 'l'
	partVolatile byte = 'v'
	partLight    byte = 'L'
	partBatches  byte = 'b'
	partEntries  byte = 'e'
	partIdents   byte = 'i'
	partEvents   byte = 'E'
	partOrder    byte = 'O'
	partVersions byte = 'V'
	partGens     byte = 'G'
	partEndKG    byte = 'Z'
	partEnd      byte = 'X'
)

type snapHeader struct{ Format, KGs int }

type snapKG struct {
	KG      wKG
	Head    wHead
	Attempt int64
}

type snapPart struct {
	Kind     byte
	Header   *snapHeader
	KG       *snapKG
	Objects  []wObject
	Links    []wLink
	Volatile []wVolatile
	Light    []wLight
	Batches  []graph.ChangeBatch
	Entries  []graph.ChangeEntry
	Idents   []wIdentRec
	Events   []wEvent
	Order    []graph.OrderEvent
	Versions []wVersion
	Gens     []graph.GenerationEvent
}

func mapSlice[A, B any](in []A, f func(A) B) []B {
	out := make([]B, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

// emit sends items in parts of at most partRows.
func emit[T any](enc *gob.Encoder, kind byte, items []T, set func(*snapPart, []T)) error {
	for len(items) > 0 {
		n := min(partRows, len(items))
		p := snapPart{Kind: kind}
		set(&p, items[:n])
		if err := enc.Encode(p); err != nil {
			return err
		}
		items = items[n:]
	}
	return nil
}

// writeSnapshot writes the whole state; the caller holds a lock that keeps writers out.
func (s *Store) writeSnapshot(w io.Writer) error {
	enc := gob.NewEncoder(w)
	if err := enc.Encode(snapPart{Kind: partHeader, Header: &snapHeader{Format: snapFormat, KGs: len(s.created)}}); err != nil {
		return err
	}
	for _, id := range s.created {
		st := s.kgs[id]
		if err := enc.Encode(snapPart{Kind: partKG, KG: &snapKG{KG: wireKG(st.kg), Head: wireHead(st.head), Attempt: st.attempt}}); err != nil {
			return err
		}
		if err := st.writeRows(enc); err != nil {
			return err
		}
		if err := enc.Encode(snapPart{Kind: partEndKG}); err != nil {
			return err
		}
	}
	return enc.Encode(snapPart{Kind: partEnd})
}

func (st *kgState) writeRows(enc *gob.Encoder) error {
	if err := st.writeObjectsAndLinks(enc); err != nil {
		return err
	}
	vols := make([]wVolatile, 0, len(st.volatile))
	keys := make([]volKey, 0, len(st.volatile))
	for k := range st.volatile {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if a, b := keys[i].object.String(), keys[j].object.String(); a != b {
			return a < b
		}
		return keys[i].attr < keys[j].attr
	})
	for _, k := range keys {
		v, err := toWireVolatile(st.volatile[k])
		if err != nil {
			return err
		}
		vols = append(vols, v)
	}
	var entries []graph.ChangeEntry
	for _, b := range st.batches {
		entries = append(entries, st.entries[b.Seq]...)
	}
	light := mapSlice(st.light, wireLight)
	idents := mapSlice(st.idents, wireIdent)
	events := mapSlice(st.events, wireEvent)
	versions := mapSlice(st.versions, wireVersion)
	steps := []func() error{
		func() error {
			return emit(enc, partVolatile, vols, func(p *snapPart, x []wVolatile) { p.Volatile = x })
		},
		func() error {
			return emit(enc, partLight, light, func(p *snapPart, x []wLight) { p.Light = x })
		},
		func() error {
			return emit(enc, partBatches, st.batches, func(p *snapPart, x []graph.ChangeBatch) { p.Batches = x })
		},
		func() error {
			return emit(enc, partEntries, entries, func(p *snapPart, x []graph.ChangeEntry) { p.Entries = x })
		},
		func() error {
			return emit(enc, partIdents, idents, func(p *snapPart, x []wIdentRec) { p.Idents = x })
		},
		func() error {
			return emit(enc, partEvents, events, func(p *snapPart, x []wEvent) { p.Events = x })
		},
		func() error {
			return emit(enc, partOrder, st.order, func(p *snapPart, x []graph.OrderEvent) { p.Order = x })
		},
		func() error {
			return emit(enc, partVersions, versions, func(p *snapPart, x []wVersion) { p.Versions = x })
		},
		func() error {
			return emit(enc, partGens, st.gens, func(p *snapPart, x []graph.GenerationEvent) { p.Gens = x })
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// writeObjectsAndLinks writes the row chains in id order, each chain oldest row first.
func (st *kgState) writeObjectsAndLinks(enc *gob.Encoder) error {
	if err := st.writeObjects(enc); err != nil {
		return err
	}
	return st.writeLinks(enc)
}

func (st *kgState) writeObjects(enc *gob.Encoder) error {
	var objs []wObject
	for _, id := range st.objIDs {
		for i := range st.objects[id] {
			w, err := toWireObject(&st.objects[id][i])
			if err != nil {
				return err
			}
			objs = append(objs, *w)
			if len(objs) >= partRows {
				if err := enc.Encode(snapPart{Kind: partObjects, Objects: objs}); err != nil {
					return err
				}
				objs = objs[:0]
			}
		}
	}
	if len(objs) == 0 {
		return nil
	}
	return enc.Encode(snapPart{Kind: partObjects, Objects: objs})
}

func (st *kgState) writeLinks(enc *gob.Encoder) error {
	var links []wLink
	for _, id := range st.lnkIDs {
		for i := range st.links[id] {
			w, err := toWireLink(&st.links[id][i])
			if err != nil {
				return err
			}
			links = append(links, *w)
			if len(links) >= partRows {
				if err := enc.Encode(snapPart{Kind: partLinks, Links: links}); err != nil {
					return err
				}
				links = links[:0]
			}
		}
	}
	if len(links) == 0 {
		return nil
	}
	return enc.Encode(snapPart{Kind: partLinks, Links: links})
}

// snapLoader holds the KG that the parts being read belong to.
type snapLoader struct {
	s   *Store
	cur *kgState
}

// loadSnapshot reads a snapshot into the empty store.
func (s *Store) loadSnapshot(r io.Reader) error {
	dec := gob.NewDecoder(r)
	ld := &snapLoader{s: s}
	for first := true; ; first = false {
		var p snapPart
		if err := dec.Decode(&p); err != nil {
			return kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the snapshot")
		}
		done, err := ld.part(p, first)
		if err != nil || done {
			return err
		}
	}
}

// part handles one part and says whether the snapshot is at its end.
func (l *snapLoader) part(p snapPart, first bool) (bool, error) {
	switch {
	case first:
		return false, checkSnapHeader(p)
	case p.Kind == partEnd:
		return true, nil
	case p.Kind == partKG && p.KG != nil:
		return false, l.startKG(p.KG)
	case p.Kind == partEndKG && l.cur != nil:
		l.cur.reindexIdents()
		l.cur.dirty = true
		l.cur.rebuild()
		l.cur = nil
		return false, nil
	case l.cur != nil && p.Kind != partHeader:
		return false, l.cur.loadPart(p)
	}
	return false, kernel.Errorf(kernel.CodeInvalidValue, "the snapshot has a part %q where none belongs", p.Kind)
}

func checkSnapHeader(p snapPart) error {
	if p.Kind != partHeader || p.Header == nil {
		return kernel.Errorf(kernel.CodeInvalidValue, "the snapshot does not begin with its header")
	}
	if p.Header.Format != snapFormat {
		return kernel.Errorf(kernel.CodeUnsupported, "the snapshot has state format %d; this build reads %d only", p.Header.Format, snapFormat)
	}
	return nil
}

func (l *snapLoader) startKG(w *snapKG) error {
	d := &ids{}
	kg, head := d.kgRow(w.KG), d.head(w.Head)
	if d.err != nil {
		return d.err
	}
	l.cur = newState(kg)
	l.cur.head, l.cur.attempt = head, w.Attempt
	l.cur.gens = nil
	l.s.kgs[kg.ID] = l.cur
	l.s.created = append(l.s.created, kg.ID)
	return nil
}

func (st *kgState) loadPart(p snapPart) error {
	d := &ids{}
	for i := range p.Objects {
		row, err := d.objectRow(&p.Objects[i])
		if err != nil {
			return err
		}
		st.objects[row.ID] = append(st.objects[row.ID], *row)
	}
	for i := range p.Links {
		row, err := d.linkRow(&p.Links[i])
		if err != nil {
			return err
		}
		st.addLink(*row)
	}
	for _, w := range p.Volatile {
		v, err := d.volatileRow(w)
		if err != nil {
			return err
		}
		st.volatile[volKey{v.Object, v.AttrNo}] = v
	}
	for _, l := range p.Light {
		st.light = append(st.light, d.light(l))
	}
	st.batches = append(st.batches, p.Batches...)
	for _, e := range p.Entries {
		st.entries[e.Seq] = append(st.entries[e.Seq], e)
	}
	for _, r := range p.Idents {
		st.idents = append(st.idents, d.ident(r))
	}
	for _, e := range p.Events {
		st.events = append(st.events, d.event(e))
	}
	st.order = append(st.order, p.Order...)
	for _, v := range p.Versions {
		st.versions = append(st.versions, cloneVersion(d.versionRow(v)))
	}
	st.gens = append(st.gens, p.Gens...)
	return d.err
}
