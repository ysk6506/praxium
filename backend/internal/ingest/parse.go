package ingest

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
)

// keySep joins the values of a natural key of more than one column.
const keySep = "\x1f"

func itoa(n int) string { return strconv.Itoa(n) }

// Row is one record of a dataset: the cells by column name and the natural key that names it.
type Row struct {
	Key  string
	Line int
	// Where says where the row is when a line number does not: the range of the row on its sheet (Servers!A12:F12).
	Where  string
	Fields map[string]string
}

// with returns a copy of the row with one cell changed.
func (r Row) with(column, value string) Row {
	f := make(map[string]string, len(r.Fields))
	for k, v := range r.Fields {
		f[k] = v
	}
	f[column] = value
	r.Fields = f
	return r
}

// Unreadable is a line that could not become a row. It is counted and kept for a sample; it is never dropped
// without a trace (ingestion.md 3.4).
type Unreadable struct {
	Line int
	// Where is the cell that made the row unreadable, for a workbook (Servers!B12).
	Where  string
	Reason string
	// Column is the natural key column that gave no value, for a row that is unreadable for that reason.
	Column string
}

// Table is what parsing a capture gives: the rows read and the lines that could not be read.
type Table struct {
	Rows       []Row
	Unreadable []Unreadable
	// Missing lists the declared columns a CSV header does not have. A table with any is a changed shape and is
	// not used (ingestion.md 9.1).
	Missing []string
	// Extra lists the header names no column of the dataset reads.
	Extra []string
	// Reason says why a table is unusable when it is not for a missing column (a sheet the workbook does not have).
	Reason string
	// Header is the names of the header the parser read (blank names left out) and HeaderRow the row of a sheet
	// they were read from, so a message about missing columns can say what the file has. Hint is what the parser
	// noticed about why the header may not be the one meant (a CSV that uses another delimiter).
	Header    []string
	HeaderRow int
	Hint      string
	// Sheet, SheetState and the counts that follow are those of a workbook: which sheet was read, whether it is
	// hidden, how many hidden rows and hidden mapped columns were read, what was passed over and why, and the cells
	// that gave no value.
	Sheet          string
	SheetState     string
	HiddenRowsRead int
	HiddenColsRead int
	Skipped        map[string]int
	CellIssues     []CellIssue
}

// Received is every data line of the capture, read or not.
func (t Table) Received() int { return len(t.Rows) + len(t.Unreadable) }

// Parse turns the bytes of a capture into a table by the parser of the dataset. It is a function of the bytes and
// the dataset: the same file gives the same table.
func Parse(d Dataset, data []byte) Table {
	if d.Parser == ParserCSV {
		return parseCSV(d, data)
	}
	return parseJSONLines(d, data)
}

func parseCSV(d Dataset, data []byte) Table {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = d.comma()
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var t Table
	header, err := r.Read()
	if err != nil {
		if !errors.Is(err, io.EOF) {
			t.Unreadable = append(t.Unreadable, Unreadable{Line: 1, Reason: "unreadable_header"})
		}
		t.Missing = requiredColumns(d)
		return t
	}
	idx, missing := csvIndex(d, header)
	t.Missing = missing
	t.Extra = extraHeader(d, header)
	t.Header = nonBlank(header)
	if len(missing) > 0 {
		t.Hint = delimiterHint(header, r.Comma)
		return t
	}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return t
		}
		line, _ := r.FieldPos(0)
		if err != nil {
			t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Reason: "malformed_csv"})
			continue
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		if len(rec) != len(header) {
			t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Reason: "wrong_field_count"})
			continue
		}
		fields := make(map[string]string, len(d.Columns))
		for _, c := range d.Columns {
			if i, ok := idx[c.Name]; ok {
				fields[c.Name] = rec[i]
			}
		}
		t.addRow(d, line, fields)
	}
}

func requiredColumns(d Dataset) []string {
	var out []string
	for _, c := range d.Columns {
		if !c.Optional {
			out = append(out, c.Name)
		}
	}
	return out
}

// csvIndex finds, for every column, the position in the header of the first path that is there.
func csvIndex(d Dataset, header []string) (map[string]int, []string) {
	idx := map[string]int{}
	var missing []string
	for _, c := range d.Columns {
		found := false
		for _, p := range c.Paths {
			if i := slices.Index(header, p); i >= 0 {
				idx[c.Name], found = i, true
				break
			}
		}
		if !found && !c.Optional {
			missing = append(missing, c.Name)
		}
	}
	return idx, missing
}

func extraHeader(d Dataset, header []string) []string {
	var out []string
	for _, h := range header {
		known := false
		for _, c := range d.Columns {
			known = known || slices.Contains(c.Paths, h)
		}
		if !known {
			out = append(out, h)
		}
	}
	return out
}

// addRow stores a row when its natural key is complete; a row without one cannot be a raw record.
func (t *Table) addRow(d Dataset, line int, fields map[string]string) {
	parts := make([]string, len(d.NaturalKey))
	for i, k := range d.NaturalKey {
		parts[i] = strings.TrimSpace(fields[k])
		if parts[i] == "" {
			t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Reason: "missing_natural_key", Column: k})
			return
		}
	}
	t.Rows = append(t.Rows, Row{Key: strings.Join(parts, keySep), Line: line, Fields: fields})
}

func parseJSONLines(d Dataset, data []byte) Table {
	var t Table
	br := bufio.NewReader(bytes.NewReader(data))
	for line := 1; ; line++ {
		text, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(text)) > 0 {
			t.jsonLine(d, line, text)
		}
		if err != nil {
			return t
		}
	}
}

func (t *Table) jsonLine(d Dataset, line int, text []byte) {
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || dec.More() {
		t.Unreadable = append(t.Unreadable, Unreadable{Line: line, Reason: "invalid_json"})
		return
	}
	fields := make(map[string]string, len(d.Columns))
	for _, c := range d.Columns {
		for _, p := range c.Paths {
			if v, ok := pick(doc, strings.Split(p, ".")); ok {
				fields[c.Name] = v
				break
			}
		}
	}
	t.addRow(d, line, fields)
}

// pick follows a path of object names and array positions. A value that is not a scalar, or is null, is no value:
// the second result is false.
func pick(v any, path []string) (string, bool) {
	for _, seg := range path {
		switch x := v.(type) {
		case map[string]any:
			next, ok := x[seg]
			if !ok {
				return "", false
			}
			v = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return "", false
			}
			v = x[i]
		default:
			return "", false
		}
	}
	switch x := v.(type) {
	case string:
		return x, x != ""
	case json.Number:
		return x.String(), true
	case bool:
		return strconv.FormatBool(x), true
	}
	return "", false
}
