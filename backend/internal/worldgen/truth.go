package worldgen

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// Names of the formats this package writes. Each carries FormatVersion.
const (
	TruthFormat = "praxium-worldgen-truth"
	TruthFile   = "truth.jsonl"
)

// Counts are the numbers of records of a world per type and link type.
type Counts struct {
	Objects      [nTypes]int64
	Links        [nLinks]int64
	ObjectsNI    int64
	LinksNI      int64
	TotalObjects int64
	TotalLinks   int64
}

// Observer methods let Counts be used on its own.

// Scope ignores scopes.
func (c *Counts) Scope(Scope, int, int, bool) error { return nil }

// Object counts an object.
func (c *Counts) Object(o *Object) error {
	c.Objects[o.T.idx]++
	c.TotalObjects++
	if o.NI != "" {
		c.ObjectsNI++
	}
	return nil
}

// Link counts a link.
func (c *Counts) Link(l *Link) error {
	c.Links[l.T.idx]++
	c.TotalLinks++
	if l.NI != "" {
		c.LinksNI++
	}
	return nil
}

// TruthInfo describes a finished truth file.
type TruthInfo struct {
	File    string `json:"file"`
	Format  string `json:"format"`
	Version int    `json:"version"`
	Records int64  `json:"records"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

// TruthWriter writes the ground truth as JSON Lines (docs/design-docs/worldgen.md):
// a header line, then one line per object or link in the order they are made.
type TruthWriter struct {
	Counts
	bw      *bufio.Writer
	h       hash.Hash
	buf     []byte
	bytes   int64
	records int64
	err     error
}

// NewTruthWriter writes to w; the content hash is computed on the way.
func NewTruthWriter(w io.Writer) *TruthWriter {
	t := &TruthWriter{h: sha256.New()}
	t.bw = bufio.NewWriterSize(io.MultiWriter(w, t.h), 1<<16)
	t.buf = make([]byte, 0, 4096)
	t.buf = append(t.buf, `{"kind":"header","format":"`+TruthFormat+`","version":1}`+"\n"...)
	t.flush()
	t.records = 0
	return t
}

func (t *TruthWriter) flush() {
	if t.err == nil {
		n, err := t.bw.Write(t.buf)
		t.bytes += int64(n)
		t.err = err
	}
	t.records++
	t.buf = t.buf[:0]
}

// Close flushes and returns the description of the file.
func (t *TruthWriter) Close() (TruthInfo, error) {
	if t.err == nil {
		t.err = t.bw.Flush()
	}
	return TruthInfo{File: TruthFile, Format: TruthFormat, Version: FormatVersion, Records: t.records,
		Bytes: t.bytes, SHA256: hex.EncodeToString(t.h.Sum(nil))}, t.err
}

// Object writes an object.
func (t *TruthWriter) Object(o *Object) error {
	_ = t.Counts.Object(o)
	b := append(t.buf, `{"kind":"object","id":`...)
	b = appendJSONString(b, o.ID)
	b = append(b, `,"type":"`...)
	b = append(b, o.T.id...)
	b = append(b, `","props":`...)
	b = appendProps(b, o.T.props, o.Props)
	b = appendNames(b, `,"unknown":`, o.T.props, o.Unknown)
	b = appendNames(b, `,"assumed":`, o.T.props, o.Assumed)
	if len(o.IDs) > 0 {
		b = append(b, `,"identifiers":{`...)
		for i, x := range o.IDs {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, x.scheme)
			b = append(b, ':')
			b = appendJSONString(b, x.value)
		}
		b = append(b, '}')
	}
	b = appendTail(b, string(o.Grade), string(o.Basis), o.NI)
	t.buf = append(b, '\n')
	t.flush()
	return t.err
}

// Link writes a link.
func (t *TruthWriter) Link(l *Link) error {
	_ = t.Counts.Link(l)
	b := append(t.buf, `{"kind":"link","id":`...)
	b = appendJSONString(b, l.ID)
	b = append(b, `,"type":"`...)
	b = append(b, l.T.id...)
	b = append(b, `","from":`...)
	b = appendJSONString(b, l.From.id)
	b = append(b, `,"to":`...)
	b = appendJSONString(b, l.To.id)
	b = append(b, `,"props":`...)
	b = appendProps(b, l.T.props, l.Props)
	b = appendTail(b, string(l.Grade), string(l.Basis), l.NI)
	t.buf = append(b, '\n')
	t.flush()
	return t.err
}

// Scope ignores scopes.
func (t *TruthWriter) Scope(Scope, int, int, bool) error { return t.err }

func appendProps(b []byte, specs []propSpec, props []propVal) []byte {
	b = append(b, '{')
	for i, p := range props {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, specs[p.idx].key...)
		b = p.v.appendJSON(b)
	}
	return append(b, '}')
}

func appendNames(b []byte, key string, specs []propSpec, idx []int) []byte {
	if len(idx) == 0 {
		return b
	}
	b = append(b, key...)
	b = append(b, '[')
	for i, x := range idx {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendJSONString(b, specs[x].name)
	}
	return append(b, ']')
}

func appendTail(b []byte, grade, basis, ni string) []byte {
	b = append(b, `,"grade":"`...)
	b = append(b, grade...)
	b = append(b, `","basis":"`...)
	b = append(b, basis...)
	b = append(b, '"')
	if ni != "" {
		b = append(b, `,"not_ingestible":"`...)
		b = append(b, ni...)
		b = append(b, '"')
	}
	return append(b, '}')
}
