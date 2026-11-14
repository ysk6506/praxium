package memstore

import (
	"bytes"
	"encoding/gob"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The records of the journal and the parts of a snapshot are gob streams of the wire types below. A wire type is the
// type of the port with the one thing gob cannot carry, the value of an attribute, replaced by bytes (codec_value.go).
// A record is: the format version of the records (1 byte), the kind (1 byte) and the gob of the wire type.
const recordVersion byte = 1

// The kinds of record: the five writes that change what is stored. A lease is not stored (it is the state of a
// running process), and a read stores nothing.
const (
	kindCreateKG byte = iota + 1
	kindChunk
	kindAbandon
	kindSeal
	kindRestore
)

type wAttr struct {
	Value      []byte
	Grade      string
	Basis      string
	SourceTime string
	SourceRef  string
}

type wObject struct {
	ID          string
	TypeNo      int
	Name        string
	MinGrade    string
	Attrs       map[int]wAttr
	SourceRefs  []string
	ContentHash kernel.Hash
	SeqFrom     int64
	SeqTo       int64
}

type wLink struct {
	ID            string
	TypeNo        int
	Src           string
	Dst           string
	Discriminator string
	Grade         string
	Basis         string
	Attrs         map[int]wAttr
	SourceRefs    []string
	ContentHash   kernel.Hash
	SeqFrom       int64
	SeqTo         int64
}

type wVolatile struct {
	Object string
	AttrNo int
	Value  []byte
	Grade  string
	Basis  string
	SeenAt string
	Ref    string
}

type wObjectChange struct{ Old, New *wObject }
type wLinkChange struct{ Old, New *wLink }

// wChunk is a chunk write without what only the live call needs (the lease, the time, the expected head).
type wChunk struct {
	KG          string
	Batch       *graph.ChangeBatch
	Objects     []wObjectChange
	Links       []wLinkChange
	Volatile    []wVolatile
	Light       []wLight
	Identifiers []wIdentChange
	Events      []wEvent
	Entries     []graph.ChangeEntry
	Order       *graph.OrderEvent
	Head        wHead
	Serve       bool
}

type wCreate struct{ KG wKG }

type wAbandon struct {
	KG   string
	Head wHead
}

type wSeal struct {
	KG      string
	Version wVersion
	Order   graph.OrderEvent
	Head    wHead
}

type wRestore struct {
	KG    string
	ToSeq int64
	Head  wHead
	Now   time.Time
}

func toWireAttrs(in map[int]graph.Attr) (map[int]wAttr, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[int]wAttr, len(in))
	for no, a := range in {
		v, err := putValue(nil, a.Value)
		if err != nil {
			return nil, err
		}
		out[no] = wAttr{Value: v, Grade: string(a.Grade), Basis: string(a.Basis), SourceTime: a.SourceTime, SourceRef: a.SourceRef}
	}
	return out, nil
}

func fromWireAttrs(in map[int]wAttr) (map[int]graph.Attr, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[int]graph.Attr, len(in))
	for no, a := range in {
		v, err := readValue(a.Value)
		if err != nil {
			return nil, err
		}
		ga := graph.Attr{Value: v, SourceTime: a.SourceTime, SourceRef: a.SourceRef}
		set(&ga.Grade, a.Grade)
		set(&ga.Basis, a.Basis)
		out[no] = ga
	}
	return out, nil
}

func toWireObject(r *graph.ObjectRow) (*wObject, error) {
	if r == nil {
		return nil, nil
	}
	attrs, err := toWireAttrs(r.Attrs)
	return &wObject{r.ID.String(), r.TypeNo, r.Name, string(r.MinGrade), attrs, r.SourceRefs, r.ContentHash, r.SeqFrom, r.SeqTo}, err
}

func (d *ids) objectRow(w *wObject) (*graph.ObjectRow, error) {
	if w == nil {
		return nil, nil
	}
	attrs, err := fromWireAttrs(w.Attrs)
	if err != nil {
		return nil, err
	}
	row := &graph.ObjectRow{ID: d.object(w.ID), TypeNo: w.TypeNo, Name: w.Name, Attrs: attrs,
		SourceRefs: w.SourceRefs, ContentHash: w.ContentHash, SeqFrom: w.SeqFrom, SeqTo: w.SeqTo}
	set(&row.MinGrade, w.MinGrade)
	return row, d.err
}

func toWireLink(r *graph.LinkRow) (*wLink, error) {
	if r == nil {
		return nil, nil
	}
	attrs, err := toWireAttrs(r.Attrs)
	return &wLink{r.ID.String(), r.TypeNo, r.Src.String(), r.Dst.String(), r.Discriminator, string(r.Grade), string(r.Basis), attrs, r.SourceRefs,
		r.ContentHash, r.SeqFrom, r.SeqTo}, err
}

func (d *ids) linkRow(w *wLink) (*graph.LinkRow, error) {
	if w == nil {
		return nil, nil
	}
	attrs, err := fromWireAttrs(w.Attrs)
	if err != nil {
		return nil, err
	}
	row := &graph.LinkRow{ID: d.link(w.ID), TypeNo: w.TypeNo, Src: d.object(w.Src), Dst: d.object(w.Dst), Discriminator: w.Discriminator,
		Attrs: attrs, SourceRefs: w.SourceRefs, ContentHash: w.ContentHash, SeqFrom: w.SeqFrom, SeqTo: w.SeqTo}
	set(&row.Grade, w.Grade)
	set(&row.Basis, w.Basis)
	return row, d.err
}

func toWireVolatile(v graph.VolatileRow) (wVolatile, error) {
	b, err := putValue(nil, v.Value)
	return wVolatile{v.Object.String(), v.AttrNo, b, string(v.Grade), string(v.Basis), v.SeenAt, v.Ref}, err
}

func (d *ids) volatileRow(w wVolatile) (graph.VolatileRow, error) {
	v, err := readValue(w.Value)
	if err != nil {
		return graph.VolatileRow{}, err
	}
	row := graph.VolatileRow{Object: d.object(w.Object), AttrNo: w.AttrNo, Value: v, SeenAt: w.SeenAt, Ref: w.Ref}
	set(&row.Grade, w.Grade)
	set(&row.Basis, w.Basis)
	return row, d.err
}

func toWireChunk(w graph.ChunkWrite) (wChunk, error) {
	out := wChunk{KG: w.KG.String(), Batch: w.Batch, Entries: w.Entries, Order: w.Order, Head: wireHead(w.Head), Serve: w.Serve}
	for _, l := range w.Light {
		out.Light = append(out.Light, wireLight(l))
	}
	for _, c := range w.Identifiers {
		out.Identifiers = append(out.Identifiers, wIdentChange{wireIdent(c.Record), c.Closes})
	}
	for _, e := range w.Events {
		out.Events = append(out.Events, wireEvent(e))
	}
	for _, c := range w.Objects {
		o, err := toWireObject(c.Old)
		if err != nil {
			return out, err
		}
		n, err := toWireObject(c.New)
		if err != nil {
			return out, err
		}
		out.Objects = append(out.Objects, wObjectChange{o, n})
	}
	for _, c := range w.Links {
		o, err := toWireLink(c.Old)
		if err != nil {
			return out, err
		}
		n, err := toWireLink(c.New)
		if err != nil {
			return out, err
		}
		out.Links = append(out.Links, wLinkChange{o, n})
	}
	for _, v := range w.Volatile {
		wv, err := toWireVolatile(v)
		if err != nil {
			return out, err
		}
		out.Volatile = append(out.Volatile, wv)
	}
	return out, nil
}

func fromWireChunk(w wChunk) (graph.ChunkWrite, error) {
	d := &ids{}
	out := graph.ChunkWrite{KG: d.kg(w.KG), Batch: w.Batch, Entries: w.Entries, Order: w.Order, Head: d.head(w.Head), Serve: w.Serve}
	for _, l := range w.Light {
		out.Light = append(out.Light, d.light(l))
	}
	for _, c := range w.Identifiers {
		out.Identifiers = append(out.Identifiers, graph.IdentifierChange{Record: d.ident(c.Record), Closes: c.Closes})
	}
	for _, e := range w.Events {
		out.Events = append(out.Events, d.event(e))
	}
	for _, c := range w.Objects {
		o, err := d.objectRow(c.Old)
		if err != nil {
			return out, err
		}
		n, err := d.objectRow(c.New)
		if err != nil {
			return out, err
		}
		out.Objects = append(out.Objects, graph.ObjectChange{Old: o, New: n})
	}
	for _, c := range w.Links {
		o, err := d.linkRow(c.Old)
		if err != nil {
			return out, err
		}
		n, err := d.linkRow(c.New)
		if err != nil {
			return out, err
		}
		out.Links = append(out.Links, graph.LinkChange{Old: o, New: n})
	}
	for _, v := range w.Volatile {
		rv, err := d.volatileRow(v)
		if err != nil {
			return out, err
		}
		out.Volatile = append(out.Volatile, rv)
	}
	return out, d.err
}

// encodeRecord makes the bytes of one record.
func encodeRecord(kind byte, v any) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{recordVersion, kind})
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot encode a journal record")
	}
	return buf.Bytes(), nil
}

// decodeRecord splits a record into its kind and decodes the wire value into v.
func decodeRecord(b []byte, v any) error {
	if err := gob.NewDecoder(bytes.NewReader(b[2:])).Decode(v); err != nil {
		return kernel.Wrap(kernel.CodeInvalidValue, err, "cannot decode a journal record")
	}
	return nil
}

// set stores a wire string into a string-based field. The wire types keep
// grades and bases as plain strings so that this unit does not import the
// model; the encoding is the same as the named string types.
func set[T ~string](dst *T, s string) { *dst = T(s) }
