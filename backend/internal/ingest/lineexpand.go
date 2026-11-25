package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
)

// The lines of a file of JSON lines that has a records_path (ADR-0109): the record of a line is the value at records_path in
// the line (the line itself when the path is empty), and a path with [*] opens an array in that record, whose elements are the
// rows. A file with no records_path is read the way it always was (jsonLine); this reading is the one of the flow of the adapters.

// lineState is what the reading of the lines learns that the end of the dataset needs.
type lineState struct {
	// read is the bytes of the lines read so far, and cells the bytes of the cells of the rows made from them: RowsOver bounds the
	// second by the first (N1, the same bound as a response of HTTP has).
	read, cells int
	// missing is how many records had no array (or null) at the place of the records_path.
	missing int
	// outers is how many records had an array looked for and had a value there or none (not a value of another kind); when it is the
	// same as missing, no record had one.
	outers int
	// bad is how many of the tables the lines gave held a line that could not be read (not JSON, nothing at records_path, a value of
	// another kind than the definition reads, an element that is not an object or has no natural key): a dataset with any is not
	// complete (the same as a response of JSON lines, ADR-0109). recs counts what the lines gave (rows, lines that could not be read
	// and rows passed over by a rule) and rows the rows.
	bad, recs, rows int
}

// count notes what a table holds.
func (st *lineState) count(t Table) {
	st.bad += len(t.Unreadable)
	st.rows += len(t.Rows)
	st.recs += t.Received()
	for _, n := range t.Skipped {
		st.recs += n
	}
}

// elementLimitError says that a record had more elements than max_elements.
type elementLimitError struct{ line, limit int }

func (e *elementLimitError) Error() string {
	return "line " + strconv.Itoa(e.line) + " has more than " + strconv.Itoa(e.limit) + " elements (max_elements)"
}

// recordLines reads the lines of a file of JSON lines that has a records_path, one Table each: a line of records_path without
// [*] is one Table, and the elements of the array a line opens are one Table each, so that a page is cut inside a line.
func recordLines(br *bufio.Reader, ds Dataset, rp RecordsPath, st *lineState) (func() (Table, error), error) {
	rb, err := NewRowBuilder(ds)
	if err != nil {
		return nil, err
	}
	line := 0
	var stopped error
	var pending struct {
		outer any
		elems []any
		at    int
		line  int
	}
	next := func() (Table, error) {
		for {
			if pending.at < len(pending.elems) {
				j := pending.at
				pending.at++
				where := "line " + strconv.Itoa(pending.line) + ", element " + strconv.Itoa(j+1)
				return rb.Element(pending.outer, pending.elems[j], pending.line, where), nil
			}
			if stopped != nil {
				return Table{}, stopped
			}
			text, err := br.ReadBytes('\n')
			line++
			st.read += len(text)
			stopped = err
			if len(bytes.TrimSpace(text)) == 0 {
				continue
			}
			t, outer, elems, lerr := openLine(ds, rb, rp, line, text, st)
			if lerr != nil {
				return Table{}, lerr
			}
			if elems == nil {
				return t, nil
			}
			pending.outer, pending.elems, pending.at, pending.line = outer, elems, 0, line
		}
	}
	return func() (Table, error) {
		t, err := next()
		if err == nil {
			st.count(t)
			st.cells += CellBytes(t)
			if RowsOver(st.cells, st.read, DefaultPageBytes) {
				return Table{}, &rowBytesError{}
			}
		}
		return t, err
	}, nil
}

// openLine reads one line. It gives the Table of the line when the line has no array to open (a record, or a line that cannot be
// read), and else the outer record and its elements.
func openLine(ds Dataset, rb *RowBuilder, rp RecordsPath, line int, text []byte, st *lineState) (Table, any, []any, error) {
	where := "line " + strconv.Itoa(line)
	var t Table
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil || dec.More() {
		t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Where: where, Reason: "invalid_json"})
		return t, nil, nil, nil
	}
	rec := doc
	if len(rp.Outer) > 0 {
		var ok bool
		if rec, ok = rp.Outer.Lookup(doc); !ok || rec == nil {
			t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Where: where, Reason: "no_records_path"})
			return t, nil, nil, nil
		}
	}
	if !rp.Expand {
		return rb.Record(rec, line, where), nil, nil, nil
	}
	elems, state := rp.Elements(rec)
	if state != ArrayWrongKind {
		st.outers++
	}
	switch {
	case state == ArrayMissing:
		st.missing++
		return t, nil, nil, nil
	case state == ArrayWrongKind:
		t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Where: where, Reason: "array_wrong_kind"})
		return t, nil, nil, nil
	case len(elems) > ds.ElementLimit():
		return t, nil, nil, &elementLimitError{line: line, limit: ds.ElementLimit()}
	case len(elems) == 0:
		return t, nil, nil, nil
	}
	return t, rec, elems, nil
}
