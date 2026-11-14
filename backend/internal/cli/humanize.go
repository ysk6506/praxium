package cli

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// The table form of a nested value. A table cell cannot hold an object, but a person still wants to read it: a pair of
// key and value for an object of plain values, one line for each identity key and each property of a type, and the
// state of each health probe. The JSON form (--json) is never touched by this.

// mapText writes an object of plain values as key=value pairs in name order; an object with a nested value is not
// written out and gives "{...}".
func mapText(m map[string]any) string {
	if len(m) == 0 {
		return "-"
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		switch m[k].(type) {
		case map[string]any, []any:
			return "{...}"
		}
		parts = append(parts, k+"="+scalar(m[k]))
	}
	return strings.Join(parts, " ")
}

// fieldLines is the lines one field of an object is written with: one line for most, several for a list of identity
// keys or of properties.
func fieldLines(name string, v any) []string {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return []string{scalar(v)}
	}
	var lines []string
	for i, it := range items {
		m, isMap := it.(map[string]any)
		if !isMap {
			return []string{scalar(v)}
		}
		switch name {
		case "identity_keys":
			lines = append(lines, fmt.Sprintf("%d. %s: %s", i+1, scalar(m["strength"]), keyText(m)))
		case "properties":
			lines = append(lines, propertyText(m))
		default:
			return []string{scalar(v)}
		}
	}
	return lines
}

// shortRef is the last part of an id: pr.rack.asset_tag is asset_tag.
func shortRef(v any) string {
	s, _ := v.(string)
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

// keyText writes an identity key: the identifier scheme it is, or its components joined with a plus.
func keyText(key map[string]any) string {
	comps, _ := key["components"].([]any)
	if len(comps) == 0 {
		return "scheme " + shortRef(key["scheme"])
	}
	parts := make([]string, 0, len(comps))
	for _, c := range comps {
		m, _ := c.(map[string]any)
		switch kind, _ := m["kind"].(string); kind {
		case "property":
			parts = append(parts, "property "+shortRef(m["ref"]))
		case "set":
			parts = append(parts, "the set of the objects a "+shortRef(m["ref"])+" link reaches")
		default:
			parts = append(parts, "link "+shortRef(m["ref"]))
		}
	}
	return strings.Join(parts, " + ")
}

// propertyText writes a property: its name, value type, whether it is required and its unit.
func propertyText(p map[string]any) string {
	out := fmt.Sprintf("%s (%s", scalar(p["name"]), shortRef(p["value_type"]))
	if req, _ := p["required"].(bool); req {
		out += ", required"
	}
	if unit, _ := p["unit"].(string); unit != "" {
		out += ", unit " + unit
	}
	return out + ")"
}

// healthText is the state of one probe: ok for 200, and what the server said otherwise.
func healthText(status int) string {
	if status == http.StatusOK {
		return "ok (200)"
	}
	return fmt.Sprintf("not ok (%d)", status)
}
