package definitions

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// maxDepth is how deep a body may nest; deeper text is refused rather than walked.
const maxDepth = 64

// NormalizeBody returns the normal form of a JSON object: no white space, keys in byte order, the same number written
// one way (an integer without a fraction, other numbers as the shortest text of the 64-bit float). Two texts of the
// same content give the same bytes, so the same hash. A text that is not one JSON object, has a key twice, nests too
// deep or is not valid UTF-8 is refused.
func NormalizeBody(raw []byte) (json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a body is UTF-8 text")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := readValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a body is one JSON value, with nothing after it")
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a body is a JSON object")
	}
	var out bytes.Buffer
	if err := writeValue(&out, obj); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func readValue(dec *json.Decoder, depth int) (any, error) {
	if depth > maxDepth {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a body nests deeper than %d levels", maxDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a body is not valid JSON: %v", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if delim == '{' {
		return readObject(dec, depth)
	}
	return readList(dec, depth)
}

func readObject(dec *json.Decoder, depth int) (any, error) {
	obj := map[string]any{}
	for dec.More() {
		keyTok, err := dec.Token()
		key, isString := keyTok.(string)
		if err != nil || !isString {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "a body is not valid JSON: a key is not text")
		}
		if _, dup := obj[key]; dup {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "a body has the key %q twice", key)
		}
		if obj[key], err = readValue(dec, depth+1); err != nil {
			return nil, err
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a body is not valid JSON: %v", err)
	}
	return obj, nil
}

func readList(dec *json.Decoder, depth int) (any, error) {
	list := []any{}
	for dec.More() {
		v, err := readValue(dec, depth+1)
		if err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	if _, err := dec.Token(); err != nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a body is not valid JSON: %v", err)
	}
	return list, nil
}

func writeValue(out *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(t))
	case string:
		return writeString(out, t)
	case json.Number:
		text, err := normalNumber(string(t))
		out.WriteString(text)
		return err
	case []any:
		return writeList(out, t)
	case map[string]any:
		return writeObject(out, t)
	}
	return nil
}

func writeString(out *bytes.Buffer, s string) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "a text cannot be written")
	}
	out.Write(bytes.TrimRight(buf.Bytes(), "\n"))
	return nil
}

func writeList(out *bytes.Buffer, list []any) error {
	out.WriteByte('[')
	for i, v := range list {
		if i > 0 {
			out.WriteByte(',')
		}
		if err := writeValue(out, v); err != nil {
			return err
		}
	}
	out.WriteByte(']')
	return nil
}

func writeObject(out *bytes.Buffer, obj map[string]any) error {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			out.WriteByte(',')
		}
		if err := writeString(out, k); err != nil {
			return err
		}
		out.WriteByte(':')
		if err := writeValue(out, obj[k]); err != nil {
			return err
		}
	}
	out.WriteByte('}')
	return nil
}

// normalNumber writes a number one way. An integer text stays an integer (a minus zero is zero); anything else goes
// through the 64-bit float, which loses digits beyond its precision, and is written without a trailing ".0".
func normalNumber(text string) (string, error) {
	if !strings.ContainsAny(text, ".eE") {
		if text == "-0" {
			return "0", nil
		}
		return text, nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return "", kernel.Errorf(kernel.CodeInvalidValue, "the number %s is out of range", text)
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatFloat(f+0, 'f', -1, 64), nil
	}
	return strconv.FormatFloat(f, 'g', -1, 64), nil
}

// RevisionHash is the content hash of a revision: its kind, schema version and normalized body. The name of the item
// is not in it, so two items of one kind with the same body share a hash.
func RevisionHash(kind string, schemaVersion int, body json.RawMessage) (string, error) {
	h, err := kernel.ContentHash([]any{"praxium.defrev.v1", kind, schemaVersion, string(body)})
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

// SetHash is the content hash of a definition set: the baseline and the sorted (item ID, content hash) pairs. With no
// entries it is the baseline as it is, so a deployment with no definitions keeps the hash the baseline had before this
// store existed. The parent set and the numbers are not in it, so two sets of one content have one hash.
func SetHash(baseline string, entries []SetEntry) (string, error) {
	if len(entries) == 0 {
		return baseline, nil
	}
	pairs := make([][]string, len(entries))
	for i, e := range entries {
		pairs[i] = []string{e.ItemID, e.ContentHash}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	h, err := kernel.ContentHash([]any{"praxium.defset.v1", baseline, pairs})
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

// BranchChangesHash is the content hash of what a branch changes: the sorted (item ID, operation, content hash) triples.
// The approvals of a proposal are tied to it.
func BranchChangesHash(entries []BranchEntry) (string, error) {
	rows := make([][]string, len(entries))
	for i, e := range entries {
		rows[i] = []string{e.ItemID, string(e.Op), e.ContentHash}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	h, err := kernel.ContentHash([]any{"praxium.defbranch.v1", rows})
	if err != nil {
		return "", err
	}
	return h.String(), nil
}
