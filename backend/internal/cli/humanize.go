package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		case "properties", "effective_properties":
			lines = append(lines, propertyText(m))
		case "bindings":
			lines = append(lines, bindingText(m))
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
			parts = append(parts, "property "+refName(key, m["ref"]))
		case "set":
			parts = append(parts, "the set of the objects a "+shortRef(m["ref"])+" link reaches")
		default:
			parts = append(parts, "link "+shortRef(m["ref"]))
		}
	}
	text := strings.Join(parts, " + ")
	for _, c := range comps {
		if m, _ := c.(map[string]any); m["kind"] != "property" {
			return text + " (not usable in file ingestion)"
		}
	}
	return text
}

// refName is the name of a property a key names: the one the view looked up, or the end of its id.
func refName(key map[string]any, ref any) string {
	if names, ok := key["_names"].(map[string]string); ok {
		if n := names[scalar(ref)]; n != "" {
			return n
		}
	}
	return shortRef(ref)
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
	if vals, _ := p["enum_values"].([]any); len(vals) > 0 {
		out += ", values " + scalar(vals)
	}
	return out + ")"
}

// printTypeView writes an object type for a person who writes a mapping: the properties the type has in the end (its
// own, its ancestors' and its traits'), and the identity keys by the names of the properties, with the keys that
// have a link in them marked as not usable in file ingestion.
func printTypeView(w io.Writer, env *Envelope) error {
	var obj map[string]any
	if err := json.Unmarshal(env.Data, &obj); err != nil {
		return transportError{err: err}
	}
	names := map[string]string{}
	eff, _ := obj["effective_properties"].([]any)
	for _, p := range eff {
		if m, ok := p.(map[string]any); ok {
			names[scalar(m["id"])] = scalar(m["name"])
		}
	}
	if len(eff) > 0 {
		obj["properties"] = eff
		delete(obj, "effective_properties")
	}
	keys, _ := obj["identity_keys"].([]any)
	for _, k := range keys {
		if m, ok := k.(map[string]any); ok {
			m["_names"] = names
		}
	}
	return printFields(w, obj)
}

// healthText is the state of one probe: ok for 200, and what the server said otherwise.
func healthText(status int) string {
	if status == http.StatusOK {
		return "ok (200)"
	}
	return fmt.Sprintf("not ok (%d)", status)
}

// bindingText writes a binding of a link type as one line: the type that may be at the start, the type that may be at
// the end and how many links of the type an object may have, so that a person who writes a mapping sees which object
// types the link joins. A binding with a constraint on a parameter says so, and so does one with a default.
func bindingText(b map[string]any) string {
	names, _ := b["_names"].(map[string]string)
	out := endText(b["from"], names) + " -> " + endText(b["to"], names) + "  cardinality " + scalar(b["cardinality"])
	if text := paramsText(b["param_constraints"]); text != "" {
		out += "  only with " + text
	}
	if text := paramsText(b["param_defaults"]); text != "" {
		out += "  by default " + text
	}
	return out
}

// paramsText writes the parameters of a map as name=value, in the order of the names; it is empty when there are none.
func paramsText(v any) string {
	m, _ := v.(map[string]any)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + scalar(m[k])
	}
	return strings.Join(parts, " ")
}

// endText is one end of a binding: the name of the object type, "any object with the trait T", or "any object".
func endText(v any, names map[string]string) string {
	e, _ := v.(map[string]any)
	ref := scalar(e["ref"])
	name := names[ref]
	if name == "" {
		name = shortRef(ref)
	}
	switch kind, _ := e["kind"].(string); kind {
	case "type":
		return name
	case "trait":
		return "any object with the trait " + name
	}
	return "any object"
}

// printLinkTypeView writes a link type with its bindings as lines that name the object types at both ends. The names
// come from the object types and traits of the model; when they cannot be read the end of the id is shown instead.
func printLinkTypeView(ctx context.Context, c *Client, w io.Writer, env *Envelope) error {
	var obj map[string]any
	if err := json.Unmarshal(env.Data, &obj); err != nil {
		return transportError{err: err}
	}
	names := map[string]string{}
	for _, path := range []string{kindPaths["object"], kindPaths["trait"]} {
		for _, row := range readAllRows(ctx, c, path) {
			names[scalar(row["id"])] = scalar(row["name"])
		}
	}
	bindings, _ := obj["bindings"].([]any)
	for _, b := range bindings {
		if m, ok := b.(map[string]any); ok {
			m["_names"] = names
		}
	}
	return printFields(w, obj)
}

// readAllRows reads every page of a list; a list that cannot be read gives what was read before the failure.
func readAllRows(ctx context.Context, c *Client, path string) []map[string]any {
	var rows []map[string]any
	query := url.Values{}
	for {
		env, err := c.Get(ctx, path, query)
		if err != nil {
			return rows
		}
		var page []map[string]any
		if json.Unmarshal(env.Data, &page) != nil {
			return rows
		}
		rows = append(rows, page...)
		next := nextCursor(env.Meta)
		if next == "" {
			return rows
		}
		query.Set("cursor", next)
	}
}
