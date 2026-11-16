package mcpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The body of a request is read by this parser and not by encoding/json alone. The standard decoder matches the keys of
// an object to a struct without regard to case and takes the last of two equal keys, so it would read {"Method": ...}
// as "method" and let a second "name" override the first (kg-mcp.md 10). Here a key is the text it is, a key that
// repeats in the same object is refused, and so is a key with a NUL in it. The result holds map[string]any, []any,
// string, json.Number, bool and nil.

// The kinds of refusal of a body, which the transport answers with different codes.
var (
	errSyntax    = errors.New("the body is not JSON")
	errDuplicate = errors.New("an object has the same key twice")
	errNulKey    = errors.New("a key holds a NUL character")
	errTooDeep   = errors.New("the JSON is nested too deeply")
)

// maxDepth is how deep arrays and objects may nest.
const maxDepth = 32

// parseStrict reads one JSON value and nothing after it.
func parseStrict(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, errSyntax
	}
	p := &jsonParser{data: data}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos != len(p.data) {
		return nil, errSyntax
	}
	return v, nil
}

type jsonParser struct {
	data []byte
	pos  int
}

func (p *jsonParser) skipSpace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) value(depth int) (any, error) {
	if p.pos >= len(p.data) {
		return nil, errSyntax
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
	case c == '"':
		return p.str()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return p.literal()
}

func (p *jsonParser) literal() (any, error) {
	for text, v := range map[string]any{"true": true, "false": false, "null": nil} {
		if strings.HasPrefix(string(p.data[p.pos:min(len(p.data), p.pos+len(text))]), text) {
			p.pos += len(text)
			return v, nil
		}
	}
	return nil, errSyntax
}

func (p *jsonParser) object(depth int) (any, error) {
	if depth > maxDepth {
		return nil, errTooDeep
	}
	p.pos++
	out := map[string]any{}
	p.skipSpace()
	if p.peek() == '}' {
		p.pos++
		return out, nil
	}
	for {
		p.skipSpace()
		if p.peek() != '"' {
			return nil, errSyntax
		}
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		name := key.(string)
		if strings.ContainsRune(name, 0) {
			return nil, errNulKey
		}
		if _, dup := out[name]; dup {
			return nil, errDuplicate
		}
		p.skipSpace()
		if p.peek() != ':' {
			return nil, errSyntax
		}
		p.pos++
		p.skipSpace()
		if out[name], err = p.value(depth); err != nil {
			return nil, err
		}
		p.skipSpace()
		switch p.peek() {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return out, nil
		default:
			return nil, errSyntax
		}
	}
}

func (p *jsonParser) array(depth int) (any, error) {
	if depth > maxDepth {
		return nil, errTooDeep
	}
	p.pos++
	out := []any{}
	p.skipSpace()
	if p.peek() == ']' {
		p.pos++
		return out, nil
	}
	for {
		p.skipSpace()
		v, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skipSpace()
		switch p.peek() {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return out, nil
		default:
			return nil, errSyntax
		}
	}
}

// peek is the byte at the position, or 0 at the end (a NUL byte outside a string is a syntax error anyway).
func (p *jsonParser) peek() byte {
	if p.pos >= len(p.data) {
		return 0
	}
	return p.data[p.pos]
}

func (p *jsonParser) number() (any, error) {
	start := p.pos
	if p.peek() == '-' {
		p.pos++
	}
	switch c := p.peek(); {
	case c == '0':
		p.pos++
	case c >= '1' && c <= '9':
		p.digits()
	default:
		return nil, errSyntax
	}
	if p.peek() == '.' {
		p.pos++
		if !p.digits() {
			return nil, errSyntax
		}
	}
	if c := p.peek(); c == 'e' || c == 'E' {
		p.pos++
		if c := p.peek(); c == '+' || c == '-' {
			p.pos++
		}
		if !p.digits() {
			return nil, errSyntax
		}
	}
	return json.Number(p.data[start:p.pos]), nil
}

// digits skips a run of digits and says whether there was one.
func (p *jsonParser) digits() bool {
	start := p.pos
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.pos++
	}
	return p.pos > start
}

func (p *jsonParser) str() (any, error) {
	p.pos++
	var b strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return b.String(), nil
		case c < 0x20:
			return nil, errSyntax
		case c == '\\':
			if err := p.escape(&b); err != nil {
				return nil, err
			}
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
	return nil, errSyntax
}

// simpleEscapes are the escapes of one character.
var simpleEscapes = map[byte]byte{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}

// escape reads the escape sequence at the position (a backslash) into b.
func (p *jsonParser) escape(b *strings.Builder) error {
	p.pos++
	if p.pos >= len(p.data) {
		return errSyntax
	}
	c := p.data[p.pos]
	p.pos++
	if simple, ok := simpleEscapes[c]; ok {
		b.WriteByte(simple)
		return nil
	}
	if c != 'u' {
		return errSyntax
	}
	r, ok := p.unicodeEscape()
	if !ok {
		return errSyntax
	}
	b.WriteRune(r)
	return nil
}

// unicodeEscape reads the four digits after \u, and for a high surrogate the \u escape of the low one that must follow.
// A surrogate alone, or a pair that is not one, is not text.
func (p *jsonParser) unicodeEscape() (rune, bool) {
	r, ok := p.hex4()
	if !ok || !utf16.IsSurrogate(r) {
		return r, ok
	}
	if r >= 0xDC00 || p.peek() != '\\' || p.pos+1 >= len(p.data) || p.data[p.pos+1] != 'u' {
		return 0, false
	}
	p.pos += 2
	low, ok := p.hex4()
	if !ok {
		return 0, false
	}
	pair := utf16.DecodeRune(r, low)
	return pair, pair != utf8.RuneError
}

// hex4 reads four hexadecimal digits.
func (p *jsonParser) hex4() (rune, bool) {
	if p.pos+4 > len(p.data) {
		return 0, false
	}
	var r rune
	for _, c := range p.data[p.pos : p.pos+4] {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	p.pos += 4
	return r, true
}
