package ingest

import (
	"fmt"
	"unicode/utf8"
)

// The grammar every id of a definition follows (ADR-0075): a lower-case letter, then lower-case letters, digits, "_" and
// "-", at most 64 characters in all. It holds for the id of a source, of a dataset, of a mapping, and of a refinement,
// a feed and a link definition inside a mapping. An id with a dot is not one, which keeps the dot free to join a
// source id and a dataset id later.
const (
	// IDPattern is the grammar as a regular expression.
	IDPattern = "[a-z][a-z0-9_-]{0,63}"
	// IDMaxLength is the most characters an id has.
	IDMaxLength = 64
	// SchemaVersion is the version of the two documents this build reads. A document with no "version" is this one.
	SchemaVersion = 1
)

// idClipAt is how many characters of a wrong id an error shows.
const idClipAt = 40

// ValidID tells whether an id follows the grammar. The empty id does not.
func ValidID(id string) bool { return IDProblem(id) == "" }

// IDProblem says in a few words why an id does not follow the grammar, or is "" when it does. The words end a sentence
// that starts with the id.
func IDProblem(id string) string {
	if id == "" {
		return "is empty"
	}
	if n := utf8.RuneCountInString(id); n > IDMaxLength {
		return fmt.Sprintf("has %d characters, and at most %d are allowed", n, IDMaxLength)
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && (r >= '0' && r <= '9' || r == '_' || r == '-'):
		case i == 0:
			return fmt.Sprintf("starts with %q, and the first character has to be a lower-case letter a-z", r)
		default:
			return fmt.Sprintf("has %q, which is not allowed", r)
		}
	}
	return ""
}

// checkID records an error with the code ec when a given id is not empty and does not follow the grammar. The empty id is
// the business of the code that says it is missing. what names the id in the sentence ("source", "dataset", ...); params
// are the values of the place, to which the id and the grammar are added.
func (c *collector) checkID(ec *errCode, pointer, what, id string, params Params) {
	problem := IDProblem(id)
	if id == "" || problem == "" {
		return
	}
	if params == nil {
		params = Params{}
	}
	params["id"], params["pattern"], params["max_length"] = clip(id, idClipAt), IDPattern, IDMaxLength
	c.add(ec, pointer, params, "%s id %q %s; an id is a lower-case letter, then lower-case letters, digits, \"_\" or \"-\", at most %d characters in all (%s)",
		what, clip(id, idClipAt), problem, IDMaxLength, IDPattern)
}

// checkVersion refuses a document of a version this build does not read. A document with no version is version 1.
func (c *collector) checkVersion(version *int, what string) {
	if version == nil || *version == SchemaVersion {
		return
	}
	c.add(ecDefVersion, ptr("version"), Params{"version": *version, "supported": []int{SchemaVersion}},
		"the %s has version %d, which this build does not know; it reads version %d only (set \"version\" to %d, or leave it out)", what, *version, SchemaVersion, SchemaVersion)
}
