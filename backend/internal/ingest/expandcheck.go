package ingest

import (
	"slices"
	"strings"
)

// The response format of a dataset of an HTTP source, and the records_path that opens an array inside each record (ADR-0109).
const (
	ResponseJSON      = "json"
	ResponseJSONLines = "json_lines"
)

// What a record that has no array at the place the records_path names is (MissingArray). Both are for a path with [*].
const (
	// MissingSeenOnly is the default: the record has no elements in what was read, and the dataset is read as what the source
	// showed, because the source may leave an array out where it has elements it did not show.
	MissingSeenOnly = "seen_only"
	// MissingEmpty says that the source leaves out an empty array: the record has no elements, and the dataset is as complete as it was.
	MissingEmpty = "empty"
)

// The most elements one record may have, and the range a definition may set it to. A record is at most 2 MiB, so the number
// is what a page of rows may be made of when one record is a page of its own, and it is measured as a limit (E8).
const (
	DefaultMaxElements = 100_000
	MaxElementsCeiling = 1_000_000
)

// Records reads the records_path of the dataset.
func (d Dataset) Records() (RecordsPath, error) { return ParseRecordsPath(d.RecordsPath) }

// OpensArray says whether the records_path of the dataset has [*]. A path that cannot be read has none (it is reported elsewhere).
func (d Dataset) OpensArray() bool {
	rp, err := d.Records()
	return err == nil && rp.Expand
}

// ElementLimit is the most elements one record of the dataset may have.
func (d Dataset) ElementLimit() int {
	if d.MaxElements > 0 {
		return d.MaxElements
	}
	return DefaultMaxElements
}

// JSONLines says whether the response of the dataset is read as JSON values one after the other.
func (d Dataset) JSONLines() bool { return d.ResponseFormat == ResponseJSONLines }

// MissingIsEmpty says that a record that has no array has no elements and the dataset is complete all the same.
func (d Dataset) MissingIsEmpty() bool { return d.MissingArray == MissingEmpty }

// readsRecordsPath says whether a dataset of a file reads a records_path: one of JSON lines does, in the flow of the adapters.
func (d Dataset) readsRecordsPath(c *collector, adapter string) bool {
	return adapter == AdapterFile && c.adapterFlow && d.Parser == ParserJSONLines
}

// checkRecordsPath checks the records_path of the dataset and what goes with it.
func (d Dataset) checkRecordsPath(c *collector, source, at string) {
	rp, err := d.Records()
	if err != nil {
		c.pathProblem(source, d.ID, at+ptr("records_path"), "records_path", d.RecordsPath, err)
		return
	}
	d.checkExpandFields(c, source, at, rp)
}

// checkExpandFields checks missing_array and max_elements, which belong to a path with [*], and the natural key, which may
// not read a place in an array or the outer record alone.
func (d Dataset) checkExpandFields(c *collector, source, at string, rp RecordsPath) {
	bad := func(field, reason string) {
		c.add(ecAdExpand, at+ptr(field), Params{"source": source, "dataset": d.ID, "field": field, "reason": reason},
			"source %s, dataset %s: %s %s", source, d.ID, field, reason)
	}
	if !rp.Expand {
		if d.MissingArray != "" {
			bad("missing_array", "is for a records_path that has [*] (result[*].interfaces)")
		}
		if d.MaxElements != 0 {
			bad("max_elements", "is for a records_path that has [*] (result[*].interfaces)")
		}
		return
	}
	if d.MissingArray != "" && d.MissingArray != MissingSeenOnly && d.MissingArray != MissingEmpty {
		bad("missing_array", "is seen_only or empty")
	}
	if d.MaxElements != 0 {
		c.checkRange(ecAdExpand, Params{"source": source, "dataset": d.ID}, "max_elements", at+ptr("max_elements"), float64(d.MaxElements), numBounds{1, MaxElementsCeiling})
	}
	d.checkExpandKey(c, source, at)
}

// checkExpandKey checks the natural key of a dataset whose records are elements of an array: a column of the key does not read
// a place in an array (a number: the order of an array may change, and the key would then name another element), and some
// column of the key reads the element itself and not the outer record only (two elements of different records would be one).
func (d Dataset) checkExpandKey(c *collector, source, at string) {
	ownField := false
	for i, name := range d.NaturalKey {
		for _, col := range d.Columns {
			if col.Name != name {
				continue
			}
			colOwn := len(col.Paths) > 0 // a column that reads the outer record first never reads the element (the paths are tried in order)
			for _, text := range col.Paths {
				cp, err := ParseColumnPath(text, true)
				if err != nil {
					continue
				}
				colOwn = colOwn && !cp.Up
				if slices.ContainsFunc(cp.Path, isNumber) {
					c.add(ecAdExpand, at+ptr("natural_key", i), Params{"source": source, "dataset": d.ID, "field": "natural_key", "reason": "names a column that reads a place in an array (a number)"},
						"source %s, dataset %s: natural_key column %q reads a place in an array (a number); the order of an array can change, so the key would name another element: use a field of the element", source, d.ID, clip(name, idClipAt))
				}
			}
			ownField = ownField || colOwn
		}
	}
	if len(d.NaturalKey) > 0 && !ownField {
		c.add(ecAdExpand, at+ptr("natural_key"), Params{"source": source, "dataset": d.ID, "field": "natural_key", "reason": "reads the outer record only"},
			"source %s, dataset %s: natural_key reads only fields of the outer record (^.), so every element of a record would have the same key; add a field of the element", source, d.ID)
	}
}

// isNumber tells whether a step of a path is a number: a place in an array.
func isNumber(step string) bool {
	return step != "" && strings.Trim(step, "0123456789") == ""
}

// checkResponseFormat checks the response format of a dataset of an HTTP source, and that the pages it goes by are none.
func (d Dataset) checkResponseFormat(c *collector, source, at string) {
	switch d.ResponseFormat {
	case "", ResponseJSON:
		return
	case ResponseJSONLines:
		if d.Pagination != nil && d.Pagination.Kind != PageNone {
			c.add(ecAdFormatPage, at+ptr("pagination", "kind"), Params{"source": source, "dataset": d.ID, "kind": d.Pagination.Kind},
				"source %s, dataset %s: the response_format json_lines has no pages (the pagination is none): an export that is streamed is read in one response", source, d.ID)
		}
	default:
		c.add(ecAdFormat, at+ptr("response_format"), Params{"source": source, "dataset": d.ID, "response_format": clip(d.ResponseFormat, idClipAt), "supported": []string{ResponseJSON, ResponseJSONLines}},
			"source %s, dataset %s: the response_format %q is neither json nor json_lines", source, d.ID, clip(d.ResponseFormat, idClipAt))
	}
}
