package worldgen

import (
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// val is the text of one property value. quoted says whether it is a JSON
// string (the text is the unquoted content) or a literal (number, boolean,
// list, map) that is written as it is.
type val struct {
	s      string
	quoted bool
}

type propVal struct {
	idx int // index into the properties of the type
	v   val
}

// ref points at an object: its id, its type, and whether the object can be
// delivered by any source (an identity key exists).
type ref struct {
	id string
	t  int
	ni bool // not ingestible
}

type ident struct{ scheme, value string }

// Object is one object of the ground truth. An observer must copy what it
// keeps: the generator reuses the buffers behind Props, Unknown, Assumed and IDs.
type Object struct {
	ID      string
	T       *typeSpec
	Props   []propVal
	Unknown []int
	Assumed []int
	IDs     []ident
	Basis   model.Basis
	Grade   model.Grade
	// NI is empty for an ingestible object, else why no source can deliver it.
	NI string
}

// Link is one link of the ground truth.
type Link struct {
	ID    string
	T     *linkSpec
	From  ref
	To    ref
	Props []propVal
	Basis model.Basis
	Grade model.Grade
	NI    string
}

// clone copies an object so it can be kept.
func (o *Object) clone() *Object {
	c := *o
	c.Props = append([]propVal(nil), o.Props...)
	c.Unknown = append([]int(nil), o.Unknown...)
	c.Assumed = append([]int(nil), o.Assumed...)
	c.IDs = append([]ident(nil), o.IDs...)
	return &c
}

func (l *Link) clone() *Link {
	c := *l
	c.Props = append([]propVal(nil), l.Props...)
	return &c
}

// get returns the text of a property by name.
func (o *Object) get(name string) (string, bool) {
	idx, ok := o.T.byName[name]
	if !ok {
		return "", false
	}
	for _, p := range o.Props {
		if p.idx == idx {
			return p.v.s, true
		}
	}
	return "", false
}

// str returns a property or "".
func (o *Object) str(name string) string {
	v, _ := o.get(name)
	return v
}

func (l *Link) get(name string) (string, bool) {
	idx, ok := l.T.byName[name]
	if !ok {
		return "", false
	}
	for _, p := range l.Props {
		if p.idx == idx {
			return p.v.s, true
		}
	}
	return "", false
}

// Scope marks the structure of the estate for observers that render per unit.
type Scope uint8

// The scopes, from the outside in.
const (
	ScopeSite Scope = iota
	ScopeHall
	ScopeRack
	ScopeCluster
	ScopeServices
)

// Observer receives every object and link in the order they are made. A
// link may name an object that is made later (a dependency into a site that
// comes after), so a consumer that checks endpoints reads the whole world first.
type Observer interface {
	Scope(kind Scope, site, index int, begin bool) error
	Object(o *Object) error
	Link(l *Link) error
}

// appendJSONString writes s as a JSON string.
func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		b = append(b, s[start:i]...)
		switch c {
		case '"', '\\':
			b = append(b, '\\', c)
		case '\n':
			b = append(b, '\\', 'n')
		case '\t':
			b = append(b, '\\', 't')
		default:
			b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		}
		start = i + 1
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}

func (v val) appendJSON(b []byte) []byte {
	if v.quoted {
		return appendJSONString(b, v.s)
	}
	return append(b, v.s...)
}

func strVal(s string) val   { return val{s: s, quoted: true} }
func intVal(n int) val      { return val{s: strconv.Itoa(n)} }
func boolVal(v bool) val    { return val{s: strconv.FormatBool(v)} }
func litVal(s string) val   { return val{s: s} }
func decVal(tenths int) val { return val{s: fmtTenths(tenths)} }

// fmtTenths writes a number given in tenths with exactly one decimal.
func fmtTenths(t int) string {
	neg := t < 0
	if neg {
		t = -t
	}
	s := strconv.Itoa(t/10) + "." + strconv.Itoa(t%10)
	if neg {
		return "-" + s
	}
	return s
}
