package ingest

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Path is a place inside a JSON value: the steps from the top, each the name of a key of an object or, when the value is
// an array, a position in it. The one grammar holds for the column paths of every adapter, for records_path of an HTTP
// dataset and for the paths into a response that the pagination reads (sources-and-adapters.md 3.2):
//
//	a.b                 the key b under the key a
//	a.0                 the first of the array under a
//	labels["app.name"]  the key app.name, a name with a dot in it, under labels (\" and \\ are the escapes)
//
// A step that is [*] or [?...] is a notation that is written down and not yet read; ParsePath refuses it with an error
// that says so.
type Path []string

// PathError says why a text is no path. Unsupported is true for the two notations the grammar keeps for later.
type PathError struct {
	// Reason ends a sentence that starts with the path: "has an empty step".
	Reason string
	// Notation is the notation that is not read yet ([*] or [?...]), when Unsupported is set.
	Notation    string
	Unsupported bool
}

func (e *PathError) Error() string { return "path " + e.Reason }

// ParsePath reads the text of a path.
func ParsePath(s string) (Path, error) {
	if s == "" {
		return nil, &PathError{Reason: "is empty"}
	}
	var steps Path
	for i := 0; i < len(s); {
		step, n, err := nextStep(s[i:])
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
		i += n
		if i < len(s) && s[i] == '.' {
			i++
			if i == len(s) || s[i] == '[' {
				return nil, &PathError{Reason: "has an empty step"}
			}
		}
	}
	return steps, nil
}

// nextStep reads one step from the front of the text: a bare name up to the next dot or bracket, or a bracketed name.
func nextStep(s string) (step string, n int, err error) {
	if s[0] == '[' {
		return bracketStep(s)
	}
	j := strings.IndexAny(s, ".[")
	if j < 0 {
		j = len(s)
	}
	name := s[:j]
	switch {
	case name == "":
		return "", 0, &PathError{Reason: "has an empty step"}
	case strings.ContainsAny(name, `]"`):
		return "", 0, &PathError{Reason: `has a ] or a " outside a bracketed name; write a name with a dot as ["a.b"]`}
	}
	return name, j, nil
}

// bracketStep reads ["name"] from the front of the text.
func bracketStep(s string) (string, int, error) {
	switch {
	case strings.HasPrefix(s, "[*]"):
		return "", 0, &PathError{Reason: "uses [*], which is not read yet", Notation: "[*]", Unsupported: true}
	case strings.HasPrefix(s, "[?"):
		return "", 0, &PathError{Reason: "uses [?name=value], which is not read yet", Notation: "[?name=value]", Unsupported: true}
	case !strings.HasPrefix(s, `["`):
		return "", 0, &PathError{Reason: `has a [ that does not begin a ["name"]; write the place in an array as a number after a dot (a.0)`}
	}
	return quotedName(s)
}

// quotedName reads "name"] from after the [ at the front of the text, with \" and \\ as the escapes.
func quotedName(s string) (string, int, error) {
	var name strings.Builder
	for i := 2; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
			if i >= len(s) || (s[i] != '"' && s[i] != '\\') {
				return "", 0, &PathError{Reason: `has a \ in a bracketed name that is not \" or \\`}
			}
			name.WriteByte(s[i])
		case '"':
			if i+1 >= len(s) || s[i+1] != ']' {
				return "", 0, &PathError{Reason: `has a bracketed name whose closing " is not followed by ]`}
			}
			if name.Len() == 0 {
				return "", 0, &PathError{Reason: "has an empty bracketed name"}
			}
			return name.String(), i + 2, nil
		default:
			name.WriteByte(s[i])
		}
	}
	return "", 0, &PathError{Reason: `has a bracketed name that is not closed by "]`}
}

// segmentsOf reads the path of a column of a file of JSON lines. A path with no bracket is the dotted path it has always
// been. A path with a bracket is read by the grammar; one the grammar refuses stays the dotted path it was, so that a
// file definition that ran before runs the same.
func segmentsOf(p string) []string {
	if !strings.Contains(p, "[") {
		return strings.Split(p, ".")
	}
	if steps, err := ParsePath(p); err == nil {
		return steps
	}
	return strings.Split(p, ".")
}

// Lookup follows the path into a value that was decoded from JSON (maps, slices, strings, json.Number, bool, nil).
func (p Path) Lookup(v any) (any, bool) {
	for _, seg := range p {
		switch x := v.(type) {
		case map[string]any:
			next, ok := x[seg]
			if !ok {
				return nil, false
			}
			v = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// Pick follows the path to a scalar and gives its text. A value that is not a scalar, or is null or the empty text, is
// no value: the second result is false.
func (p Path) Pick(v any) (string, bool) {
	at, ok := p.Lookup(v)
	if !ok {
		return "", false
	}
	switch x := at.(type) {
	case string:
		return x, x != ""
	case json.Number:
		return x.String(), true
	case bool:
		return strconv.FormatBool(x), true
	}
	return "", false
}

// Array follows the path to an array. The empty path is the value itself.
func (p Path) Array(v any) ([]any, bool) {
	at, ok := p.Lookup(v)
	if !ok {
		return nil, false
	}
	list, isList := at.([]any)
	return list, isList
}

// pick follows a path of object names and array positions. A value that is not a scalar, or is null, is no value:
// the second result is false.
func pick(v any, path []string) (string, bool) { return Path(path).Pick(v) }
