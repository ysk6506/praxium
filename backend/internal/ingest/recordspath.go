package ingest

import (
	"strings"
)

// The records_path that opens an array inside each record (ADR-0109, E8). The grammar of a path (ParsePath) is left as it
// is, because it is the one grammar of the column paths of every adapter and of the old paths of the files of JSON lines.
// What is read here is the one place that grammar did not read yet: [*] in records_path, written once, and ^. at the front of
// the path of a column, which reads the record the array is in.
//
//	items                     the records are the array under items (or the one value at items in a line of JSON lines)
//	result[*].interfaces      each of the records of the array under result has an array under interfaces, and the records
//	                          are the elements of those arrays
//	^.hostid                  (the path of a column) the key hostid of the record the element is in
//	^["a.b"]                  the key a.b of that record
//
// A key that is named ^ is written ["^"]. A ^ that is not the first step of a path, and a ^ that comes twice, are errors.

// RecordsPath is a records_path that has been read. Without [*] Outer is the whole path and the records are what it leads to.
// With it, Outer leads to the records that have an array in them (the outer records) and Inner is the way from an outer record
// to its array; the records the dataset gives are the elements of those arrays.
type RecordsPath struct {
	Outer  Path
	Inner  Path
	Expand bool
}

// ArrayState says what an outer record has at the place of its array.
type ArrayState int

const (
	// ArrayPresent is an array (it may be empty).
	ArrayPresent ArrayState = iota
	// ArrayMissing is no value at the place, or null: the record may have had elements that were not shown.
	ArrayMissing
	// ArrayWrongKind is a value that is not an array (an object, a text, a number, a boolean).
	ArrayWrongKind
)

// rawStep is a step of a path as written: its name, whether it was written as a bare name (and not as ["name"]), or the [*].
type rawStep struct {
	name string
	bare bool
	star bool
}

// scanPath reads the steps of a path as ParsePath does, and reads [*] as a step of its own.
func scanPath(s string) ([]rawStep, error) {
	if s == "" {
		return nil, &PathError{Reason: "is empty"}
	}
	var out []rawStep
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "[*]") {
			out = append(out, rawStep{star: true})
			i += len("[*]")
			if i < len(s) && s[i] != '.' && s[i] != '[' {
				return nil, &PathError{Reason: "has text right after [*]; a . comes before the next name (a[*].b)"}
			}
		} else {
			step, n, err := nextStep(s[i:])
			if err != nil {
				return nil, err
			}
			out = append(out, rawStep{name: step, bare: s[i] != '['})
			i += n
		}
		if i < len(s) && s[i] == '.' {
			i++
			if i == len(s) || s[i] == '[' {
				return nil, &PathError{Reason: "has an empty step"}
			}
		}
	}
	return out, nil
}

// ParseRecordsPath reads the records_path of a dataset. The empty text is no path: the records are the value itself.
func ParseRecordsPath(s string) (RecordsPath, error) {
	if s == "" {
		return RecordsPath{}, nil
	}
	steps, err := scanPath(s)
	if err != nil {
		return RecordsPath{}, err
	}
	var rp RecordsPath
	for _, st := range steps {
		switch {
		case st.star && rp.Expand:
			return RecordsPath{}, &PathError{Reason: "uses [*] more than once; one array of records is opened inside the records, and no more"}
		case st.star:
			rp.Expand = true
		case rp.Expand:
			rp.Inner = append(rp.Inner, st.name)
		default:
			rp.Outer = append(rp.Outer, st.name)
		}
	}
	if rp.Expand && len(rp.Inner) == 0 {
		return RecordsPath{}, &PathError{Reason: "has nothing after [*]; name the array in each record (result[*].interfaces)"}
	}
	return rp, nil
}

// Elements gives the elements of the array an outer record has at Inner, and says what is there. It is only for a path that
// has [*].
func (p RecordsPath) Elements(outer any) ([]any, ArrayState) {
	at, ok := p.Inner.Lookup(outer)
	if !ok || at == nil {
		return nil, ArrayMissing
	}
	list, isList := at.([]any)
	if !isList {
		return nil, ArrayWrongKind
	}
	return list, ArrayPresent
}

// ColumnPath is the path of a column that has been read: the steps, and whether they begin in the record the element is in
// (Up) and not in the element.
type ColumnPath struct {
	Path Path
	Up   bool
}

// ParseColumnPath reads the path of a column. Where the records have an array opened in them (outer is true), a ^ as the first
// step is the outer record; elsewhere the text is read by ParsePath, as it always was.
func ParseColumnPath(s string, outer bool) (ColumnPath, error) {
	if !outer {
		p, err := ParsePath(s)
		return ColumnPath{Path: p}, err
	}
	steps, err := scanPath(s)
	if err != nil {
		return ColumnPath{}, err
	}
	var cp ColumnPath
	for i, st := range steps {
		switch {
		case st.star:
			_, err := ParsePath(s) // the notation the grammar keeps for later says so
			return ColumnPath{}, err
		case st.bare && st.name == "^" && i == 0:
			cp.Up = true
		case st.bare && st.name == "^":
			return ColumnPath{}, &PathError{Reason: `has a ^ that is not the first step; a key that is named ^ is written ["^"]`}
		default:
			cp.Path = append(cp.Path, st.name)
		}
	}
	if cp.Up && len(cp.Path) == 0 {
		return ColumnPath{}, &PathError{Reason: "is ^ alone; name a field of the outer record (^.name)"}
	}
	return cp, nil
}

// Pick follows the path to a scalar of the element, or of the outer record when the path begins with ^.
func (p ColumnPath) Pick(rec, outer any) (string, bool) {
	if p.Up {
		return p.Path.Pick(outer)
	}
	return p.Path.Pick(rec)
}
