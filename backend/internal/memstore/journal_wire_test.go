package memstore

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
)

func snapBytes(t *testing.T, parts ...snapPart) []byte {
	t.Helper()
	var b bytes.Buffer
	enc := gob.NewEncoder(&b)
	for _, p := range parts {
		if err := enc.Encode(p); err != nil {
			t.Fatal(err)
		}
	}
	return b.Bytes()
}

// snapWithPart is a snapshot of one good KG with the part inside it.
func snapWithPart(t *testing.T, p snapPart) []byte {
	t.Helper()
	return snapBytes(t,
		snapPart{Kind: partHeader, Header: &snapHeader{Format: snapFormat, KGs: 1}},
		snapPart{Kind: partKG, KG: &snapKG{KG: wKG{ID: "kg-1", Name: "k", Role: graph.RoleTest}, Head: wHead{KG: "kg-1"}}},
		p, snapPart{Kind: partEndKG}, snapPart{Kind: partEnd})
}

func record(t *testing.T, kind byte, v any) []byte {
	t.Helper()
	b, err := encodeRecord(kind, v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func createRecord(t *testing.T, id string) []byte {
	return record(t, kindCreateKG, wCreate{KG: wKG{ID: id, Name: "k", Role: graph.RoleTest}})
}

func abandonRecord(t *testing.T, id string) []byte {
	return record(t, kindAbandon, wAbandon{KG: id, Head: wHead{KG: id}})
}

func restoreRecord(t *testing.T, id string) []byte {
	return record(t, kindRestore, wRestore{KG: id, Head: wHead{KG: id}, Now: time.Time{}})
}

// sealRecord is the seal of a KG that is named by id: with a good id the KG is not there.
func sealRecord(t *testing.T, r *jrig, id string) []byte {
	return record(t, kindSeal, wSeal{KG: id, Head: wHead{KG: id}})
}

// Values come back as they went in: the six kinds, a nil inside a list, empty lists and maps, large numbers,
// and text that is not valid UTF-8; and what is not one of the six is refused.
// [REQ-271]
func TestValuesRoundTripInTheirOwnForm(t *testing.T) {
	for _, v := range []any{nil, true, false, "", "héllo \xff", int64(0), int64(-1 << 62), int64(1<<63 - 1),
		[]any{}, []any{nil, "a", int64(2), true, []any{nil}}, map[string]any{}, map[string]any{"k": nil, "z": []any{map[string]any{"a": int64(1)}}}} {
		b, err := putValue(nil, v)
		if err != nil {
			t.Fatalf("%#v: %v", v, err)
		}
		got, err := readValue(b)
		if err != nil {
			t.Fatalf("%#v: %v", v, err)
		}
		if a, b := formatValue(v), formatValue(got); a != b {
			t.Fatalf("value %s came back as %s", a, b)
		}
	}
	for _, bad := range []any{1.5, int(3), []string{"a"}, map[string]any{"k": 2.5}, []any{3.5}} {
		if _, err := putValue(nil, bad); err == nil {
			t.Fatalf("%#v was written", bad)
		}
	}
	for name, b := range map[string][]byte{"empty": {}, "unknown tag": {99}, "cut string": {tagString, 5, 'a'},
		"cut int": {tagInt}, "cut list": {tagList, 2, tagNil}, "cut map": {tagMap, 1, 1, 'k'}, "trailing": {tagNil, 0},
		"huge count": {tagList, 0xff, 0xff, 0xff, 0xff, 0x0f}, "cut count": {tagList}} {
		if _, err := readValue(b); err == nil {
			t.Fatalf("%s was read", name)
		}
	}
}

func formatValue(v any) string {
	b, _ := putValue(nil, v)
	return string(b)
}

var errCut = errors.New("the writer stopped")

// cutWriter takes n bytes and then fails.
type cutWriter struct{ left int }

func (w *cutWriter) Write(p []byte) (int, error) {
	if len(p) > w.left {
		n := w.left
		w.left = 0
		return n, errCut
	}
	w.left -= len(p)
	return len(p), nil
}

// A snapshot whose writer fails, wherever it fails, is an error.
// [REQ-272]
func TestASnapshotWhoseWriterFailsIsAnError(t *testing.T) {
	r := newJRig(t, Options{}, 3)
	r.play()
	var all bytes.Buffer
	if err := r.s.writeSnapshot(&all); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < all.Len(); n += 7 {
		if err := r.s.writeSnapshot(&cutWriter{left: n}); err == nil {
			t.Fatalf("a writer that fails after %d of %d bytes did not fail the snapshot", n, all.Len())
		}
	}
}

func floatAttr() map[int]graph.Attr { return map[int]graph.Attr{1: {Value: 1.5}} }

// A chunk with a value of a type the journal cannot write is refused whichever row holds it, and a record that holds
// a value or an id that is wrong is refused when it is read.
// [REQ-273]
func TestAChunkThatCannotBeWrittenOrReadIsRefused(t *testing.T) {
	bad := graph.ChunkWrite{}
	for _, w := range []graph.ChunkWrite{
		{Objects: []graph.ObjectChange{{Old: &graph.ObjectRow{Attrs: floatAttr()}}}},
		{Objects: []graph.ObjectChange{{New: &graph.ObjectRow{Attrs: floatAttr()}}}},
		{Links: []graph.LinkChange{{Old: &graph.LinkRow{Attrs: floatAttr()}}}},
		{Links: []graph.LinkChange{{New: &graph.LinkRow{Attrs: floatAttr()}}}},
		{Volatile: []graph.VolatileRow{{Value: 1.5}}},
	} {
		if _, err := toWireChunk(w); err == nil {
			t.Fatalf("a chunk with a float was written: %+v", w)
		}
	}
	if _, err := toWireChunk(bad); err != nil {
		t.Fatal(err)
	}
	junk := []byte{99}
	for name, w := range map[string]wChunk{
		"object old value": {Objects: []wObjectChange{{Old: &wObject{ID: "obj_1", Attrs: map[int]wAttr{1: {Value: junk}}}}}},
		"object new value": {Objects: []wObjectChange{{New: &wObject{ID: "obj_1", Attrs: map[int]wAttr{1: {Value: junk}}}}}},
		"object old id":    {Objects: []wObjectChange{{Old: &wObject{ID: "bad id!"}}}},
		"object new id":    {Objects: []wObjectChange{{New: &wObject{ID: "bad id!"}}}},
		"link old value":   {Links: []wLinkChange{{Old: &wLink{ID: "lnk_1", Attrs: map[int]wAttr{1: {Value: junk}}}}}},
		"link new value":   {Links: []wLinkChange{{New: &wLink{ID: "lnk_1", Attrs: map[int]wAttr{1: {Value: junk}}}}}},
		"link old id":      {Links: []wLinkChange{{Old: &wLink{ID: "bad id!"}}}},
		"link new id":      {Links: []wLinkChange{{New: &wLink{ID: "bad id!"}}}},
		"volatile value":   {Volatile: []wVolatile{{Object: "obj_1", Value: junk}}},
		"the KG":           {KG: "bad id!"},
		"an event":         {Events: []wEvent{{LinkMap: []wLinkMap{{Old: "bad id!"}}}}},
		"an identifier":    {Identifiers: []wIdentChange{{Record: wIdentRec{Object: "bad id!"}}}},
		"a light change":   {Light: []wLight{{Object: "bad id!"}}},
		"the head":         {Head: wHead{KG: "bad id!"}},
	} {
		if _, err := fromWireChunk(w); err == nil {
			t.Fatalf("a chunk with a bad %s was read", name)
		}
	}
	if _, err := encodeRecord(kindChunk, make(chan int)); err == nil {
		t.Fatalf("a record that gob cannot encode was made")
	}
}

// Identity events with their link mappings survive the wire.
// [REQ-271]
func TestIdentityEventsSurviveTheWire(t *testing.T) {
	obj, _ := kernel.ParseID[graph.ObjectKind]("obj_a")
	into, _ := kernel.ParseID[graph.ObjectKind]("obj_b")
	l1, _ := kernel.ParseID[graph.LinkKind]("lnk_1")
	l2, _ := kernel.ParseID[graph.LinkKind]("lnk_2")
	ev := graph.IdentityEvent{Seq: 3, Kind: graph.EventMerge, Retired: obj, Into: into, LinkMap: []graph.LinkMapping{{Old: l1, New: l2}}}
	wire, err := toWireChunk(graph.ChunkWrite{Events: []graph.IdentityEvent{ev}})
	if err != nil {
		t.Fatal(err)
	}
	back, err := fromWireChunk(wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Events) != 1 || fmt.Sprintf("%+v", back.Events[0]) != fmt.Sprintf("%+v", ev) {
		t.Fatalf("event %+v came back as %+v", ev, back.Events)
	}
}
