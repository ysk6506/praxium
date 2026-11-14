package cli

import (
	"fmt"
	"sort"
	"strings"
)

// Cause is what the server's answer says was wrong, in one phrase: which field and why, or what was not found.
// The server fills params.message and params.field for a value it refused, and params.resource and params.id for
// something it did not find (the notes of the error shape in rest-api.md). An answer without them, such as one of
// an older server, gives "" and the code alone is shown.
func (p *ProblemError) Cause() string {
	if p.Code == "not_found.resource" {
		return notFoundCause(p.Params)
	}
	var parts []string
	if msg := message(p.Params); msg != "" {
		if field, _ := p.Params["field"].(string); field != "" {
			msg = fmt.Sprintf("field %q%s: %s", field, flagNote(field), msg)
		}
		parts = append(parts, msg)
	}
	for _, e := range p.Errors {
		parts = append(parts, fieldCause(e))
	}
	if len(parts) == 0 {
		parts = append(parts, plainParams(p.Params, "message", "field", "detail")...)
	}
	return strings.Join(parts, "; ")
}

// message is the plain sentence of an answer: params.message, or the older params.detail without its code.
func message(params map[string]any) string {
	if m, _ := params["message"].(string); m != "" {
		return m
	}
	d, _ := params["detail"].(string)
	if head, rest, ok := strings.Cut(d, ": "); ok && !strings.ContainsAny(head, " \"") {
		return rest
	}
	return d
}

func notFoundCause(params map[string]any) string {
	resource, _ := params["resource"].(string)
	id, _ := params["id"].(string)
	switch {
	case resource == "" && id == "":
		return ""
	case id == "":
		return "no such " + spaced(resource)
	case resource == "object":
		return fmt.Sprintf("no object %q (it does not exist, or this token cannot see it)", id)
	case resource == "route":
		return fmt.Sprintf("the server has no route %q (is --endpoint a Praxium server?)", id)
	}
	return fmt.Sprintf("no %s %q", spaced(resource), id)
}

func spaced(resource string) string {
	if resource == "kg" {
		return "KG"
	}
	return strings.ReplaceAll(resource, "_", " ")
}

// flagOf names the flag of the command line that fills a field of the request, for the fields that have one.
var flagOf = map[string]string{
	"object_type": "--type", "trait": "--trait", "link_types": "--link-type", "meanings": "--meaning", "peer_types": "--peer-type",
	"direction": "--direction", "view": "--view", "identifier.scheme": "--identifier", "identifier": "--identifier", "attributes": "--attr",
	"max_steps": "--steps", "kg_version_id": "--version",
}

func fieldCause(e FieldProblem) string {
	out := fmt.Sprintf("field %q (%s", e.Pointer, e.Code)
	if extra := plainParams(e.Params); len(extra) > 0 {
		out += ", " + strings.Join(extra, ", ")
	}
	return out + ")"
}

// plainParams writes the scalar params as key=value, in name order, leaving out the named keys.
func plainParams(params map[string]any, skip ...string) []string {
	var out []string
	for k, v := range params {
		if containsWord(skip, k) {
			continue
		}
		switch x := v.(type) {
		case string, float64, bool:
			out = append(out, fmt.Sprintf("%s=%v", k, x))
		}
	}
	sort.Strings(out)
	return out
}

func containsWord(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// flagNote is " (--type)" for a field that a flag of the command line fills, and nothing for the others.
func flagNote(field string) string {
	if f, ok := flagOf[field]; ok {
		return " (" + f + ")"
	}
	return ""
}
