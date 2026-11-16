package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// locateUnknown says where in a definition a field is that the decoder refused and which fields are allowed there,
// so a misspelt or misplaced field is fixed without a manual. It walks the document against the Go type the
// definition decodes into and finds the first object that has the field and does not accept it. The path is written
// as a person reads it (datasets[0].sheet; "" for the top level).
func locateUnknown(data []byte, field string, into any) (path string, allowed []string, ok bool) {
	var doc any
	if json.Unmarshal(data, &doc) != nil {
		return "", nil, false
	}
	return findUnknown(doc, reflect.TypeOf(into), field, "")
}

// unknownFieldSentence is the sentence that says where the field is and which fields are allowed there.
func unknownFieldSentence(field, path string, allowed []string) string {
	if path == "" {
		path = "the top level"
	}
	return fmt.Sprintf("%q is not a field of %s; the fields there are: %s", field, path, strings.Join(allowed, ", "))
}

// pathSteps splits the path of locateUnknown into the steps of a JSON pointer.
func pathSteps(path string) []any {
	var steps []any
	for _, part := range strings.Split(path, ".") {
		name, rest, _ := strings.Cut(part, "[")
		if name != "" {
			steps = append(steps, name)
		}
		for rest != "" {
			idx, more, _ := strings.Cut(rest, "]")
			if n, err := strconv.Atoi(idx); err == nil {
				steps = append(steps, n)
			}
			rest = strings.TrimPrefix(more, "[")
		}
	}
	return steps
}

// lineOf is the line of the document an offset is on, from 1.
func lineOf(data []byte, offset int64) int {
	return 1 + bytes.Count(data[:min(max(int(offset), 0), len(data))], []byte("\n"))
}

// jsonFields are the names a struct type reads, in the order of its declaration.
func jsonFields(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

func structType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func findUnknown(v any, t reflect.Type, field, path string) (string, []string, bool) {
	t = structType(t)
	switch x := v.(type) {
	case []any:
		if t.Kind() != reflect.Slice {
			return "", nil, false
		}
		for i, e := range x {
			if p, a, ok := findUnknown(e, t.Elem(), field, fmt.Sprintf("%s[%d]", path, i)); ok {
				return p, a, true
			}
		}
	case map[string]any:
		if t.Kind() != reflect.Struct {
			return "", nil, false
		}
		return findInObject(x, t, field, path)
	}
	return "", nil, false
}

func findInObject(obj map[string]any, t reflect.Type, field, path string) (string, []string, bool) {
	allowed := jsonFields(t)
	if _, has := obj[field]; has && !containsString(allowed, field) {
		return path, allowed, true
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ft, ok := fieldType(t, k)
		if !ok {
			continue
		}
		sub := k
		if path != "" {
			sub = path + "." + k
		}
		if p, a, found := findUnknown(obj[k], ft, field, sub); found {
			return p, a, true
		}
	}
	return "", nil, false
}

func fieldType(t reflect.Type, name string) (reflect.Type, bool) {
	for i := 0; i < t.NumField(); i++ {
		if n, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); n == name {
			return t.Field(i).Type, true
		}
	}
	return nil, false
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// wrongKindMessage says in plain words that a value of a definition is of the wrong kind: which field, what kind of
// value it takes and what kind it was given, and the line the value ends on.
func wrongKindMessage(data []byte, e *json.UnmarshalTypeError) string {
	field := e.Field
	if field == "" {
		field = "the document"
	}
	field = indexedPath(field)
	line := lineOf(data, e.Offset)
	return fmt.Sprintf("the field %q must be %s, but the value given is %s (line %d)", field, kindWanted(e.Type), kindGiven(e.Value), line)
}

// kindWanted is what a value of a Go type looks like in JSON, as a person writes it.
func kindWanted(t reflect.Type) string {
	if t == nil {
		return "another kind of value"
	}
	switch t.Kind() {
	case reflect.Bool:
		return "true or false (no quotes)"
	case reflect.String:
		return "text in quotes"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "a whole number (no quotes)"
	case reflect.Float32, reflect.Float64:
		return "a number (no quotes)"
	case reflect.Slice, reflect.Array:
		return "a list in [ ]"
	case reflect.Struct, reflect.Map, reflect.Pointer:
		if t.Kind() == reflect.Pointer {
			return kindWanted(t.Elem())
		}
		return "an object in { }"
	}
	return "another kind of value"
}

// kindGiven names the kind of the JSON value the decoder met.
func kindGiven(v string) string {
	switch {
	case v == "string":
		return "text"
	case v == "bool":
		return "true or false"
	case v == "array":
		return "a list"
	case v == "object":
		return "an object"
	case strings.HasPrefix(v, "number"):
		return "a number"
	}
	return v
}

var indexSegment = regexp.MustCompile(`\.(\d+)(\.|$)`)

// indexedPath writes the places of a list in a path the way a person reads them: datasets.0.name is datasets[0].name
// (the first of the list is 0).
func indexedPath(p string) string {
	for indexSegment.MatchString(p) {
		p = indexSegment.ReplaceAllString(p, "[$1]$2")
	}
	return p
}
