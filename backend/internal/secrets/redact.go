package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Redacted is the text that stands for a value in every output.
const Redacted = "[REDACTED]"

// redacted gives the types that embed it the whole set of ways a value can end up in text: fmt (every verb), JSON,
// text encoders and log/slog. All of them write Redacted. A type holding a value embeds it, so the way out is
// closed at one place.
type redacted struct{}

// String returns Redacted.
func (redacted) String() string { return Redacted }

// GoString returns Redacted, for the %#v verb.
func (redacted) GoString() string { return Redacted }

// Format writes Redacted for every verb and flag.
func (redacted) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, Redacted) }

// MarshalJSON returns the JSON string Redacted.
func (redacted) MarshalJSON() ([]byte, error) { return []byte(`"` + Redacted + `"`), nil }

// MarshalText returns Redacted.
func (redacted) MarshalText() ([]byte, error) { return []byte(Redacted), nil }

// LogValue returns Redacted, so a log/slog handler never sees the value.
func (redacted) LogValue() slog.Value { return slog.StringValue(Redacted) }

// Value is one opened field of a secret. Printing it in any way gives Redacted; the text comes out through Reveal
// and nothing else. The bytes are held by a function value, which fmt shows as an address under every verb: fmt
// does not call methods on an unexported field and, for a wrong verb, prints what a pointer points to, so a Value
// in an unexported field of some struct would show its bytes if it held them in a struct or a slice.
type Value struct {
	redacted
	reveal func() string
}

// secretBytes is the bytes of one value, held by the closures of the Value and of the wipe function only.
type secretBytes struct{ b []byte }

func (h *secretBytes) value() Value {
	return Value{reveal: func() string { return string(h.b) }}
}

func (h *secretBytes) wipe() {
	clear(h.b)
	h.b = nil
}

// Reveal returns the value as text. This is the one way to get the value out: name it where you use it, and keep
// the text out of logs and error messages. After the Opened it came from is closed, it is empty.
func (v Value) Reveal() string {
	if v.reveal == nil {
		return ""
	}
	return v.reveal()
}

// Fields holds the field values of a secret on the way in (CreateInput, AddVersion). Printing it in any way gives
// Redacted too, so a request that is logged whole does not show its values, even through a wrong verb or an
// unexported field. It is made with NewFields and read by the
// service; a Fields has no way to give a value back. Like Value it holds the map in a function value.
type Fields struct {
	redacted
	read func() map[string]string
}

// NewFields makes Fields from a map of field names to values. The map is copied.
func NewFields(values map[string]string) Fields {
	m := make(map[string]string, len(values))
	for k, v := range values {
		m[k] = v
	}
	return Fields{read: func() map[string]string { return m }}
}

// UnmarshalJSON reads a JSON object of strings, for the request that carries the values in. A name that comes twice, or
// twice with only the case different, is refused, and no error repeats a character of the body.
func (f *Fields) UnmarshalJSON(data []byte) error {
	bad := kernel.Errorf(kernel.CodeInvalidValue, "the fields are not a JSON object of text values with names that differ")
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return bad
	}
	m, seen := map[string]string{}, map[string]bool{}
	for dec.More() {
		name, ok1 := nextText(dec)
		value, ok2 := nextText(dec)
		if !ok1 || !ok2 || seen[strings.ToLower(name)] {
			return bad
		}
		seen[strings.ToLower(name)] = true
		m[name] = value
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return bad
	}
	if _, err := dec.Token(); err != io.EOF {
		return bad
	}
	*f = NewFields(m)
	return nil
}

func nextText(dec *json.Decoder) (string, bool) {
	tok, err := dec.Token()
	text, ok := tok.(string)
	return text, err == nil && ok
}

// Names returns the field names, sorted.
func (f Fields) Names() []string {
	names := make([]string, 0, len(f.values()))
	for n := range f.values() {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (f Fields) values() map[string]string {
	if f.read == nil {
		return nil
	}
	return f.read()
}

// Opened is a secret opened for one use: the fields of the current version as Values. Close it when the use ends; it
// overwrites the bytes, and the values read as empty from then on. Printing it in any way gives Redacted.
type Opened struct {
	redacted
	d *openedData
}

type openedData struct {
	id      string
	kind    Kind
	version int
	addr    Address
	values  map[string]Value
	holders []*secretBytes // the bytes the values read, to overwrite them
}

// SecretID is the id of the secret.
func (o *Opened) SecretID() string { return o.d.id }

// Kind is the kind of the secret.
func (o *Opened) Kind() Kind { return o.d.kind }

// Version is the version that was opened.
func (o *Opened) Version() int { return o.d.version }

// Address is the address the open was allowed for, in its normal form. The code that connects uses this address and
// not the one it asked about, so that what was checked is what is connected to.
func (o *Opened) Address() Address { return o.d.addr }

// Value returns a field by name.
func (o *Opened) Value(field string) (Value, bool) {
	v, ok := o.d.values[field]
	return v, ok
}

// Close overwrites the bytes of every value. It may be called more than once, and on a zero Opened.
func (o *Opened) Close() {
	if o == nil || o.d == nil {
		return
	}
	for _, h := range o.d.holders {
		h.wipe()
	}
}

func wipe(b []byte) { clear(b) }
