package ingest

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// unknownFieldHint says where in a definition a field is that the decoder refused and which fields are allowed
// there, so a misspelt or misplaced field is fixed without a manual. It walks the document against the Go type the
// definition decodes into and reports the first object that has the field and does not accept it. It returns "" when
// the place cannot be found.
func unknownFieldHint(data []byte, field string, into any) string {
	var doc any
	if json.Unmarshal(data, &doc) != nil {
		return ""
	}
	path, allowed, ok := findUnknown(doc, reflect.TypeOf(into), field, "")
	if !ok {
		return ""
	}
	if path == "" {
		path = "the top level"
	}
	return fmt.Sprintf("%q is not a field of %s; the fields there are: %s", field, path, strings.Join(allowed, ", "))
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
