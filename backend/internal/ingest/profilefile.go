package ingest

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The formats of a file that Inspect reads.
const (
	FormatCSV       = "csv"
	FormatJSONLines = "jsonl"
	FormatXLSX      = "xlsx"
)

// InspectOptions say what to look at. A header row that is 0 is guessed on a sheet; Sheet is a name or a number from 1
// and empty means every sheet.
type InspectOptions struct {
	Format     string
	Sheet      string
	HeaderRow  int
	HeaderRows int
	SampleRows int
	Limits     Limits
}

// DefaultSampleRows is how many rows of a table a profile shows.
const DefaultSampleRows = 5

// Inspect makes the profile of a file: what is in it, before a mapping is written (ingestion-extensions.md 4). It
// reads and counts; it applies no mapping and writes nothing.
func Inspect(data []byte, o InspectOptions, books WorkbookReader) (FileProfile, error) {
	if o.SampleRows <= 0 {
		o.SampleRows = DefaultSampleRows
	}
	fp := FileProfile{Format: o.Format, Bytes: len(data)}
	switch o.Format {
	case FormatCSV:
		fp.Tables = []TableProfile{profileCSV(data, o)}
	case FormatJSONLines:
		fp.Tables = []TableProfile{profileJSONLines(data, o)}
	case FormatXLSX:
		if books == nil {
			return fp, kernel.Errorf(kernel.CodeUnsupported, "this build has no reader of workbook files")
		}
		wb, err := books.ReadWorkbook(data, o.Limits)
		if err != nil {
			return fp, err
		}
		return profileWorkbook(fp, wb, o)
	default:
		return fp, kernel.Errorf(kernel.CodeInvalidValue, "the format %q is none of csv, jsonl, xlsx", o.Format)
	}
	return fp, nil
}

// grid gathers the columns and the sample of one table while rows are added.
type grid struct {
	name       string
	accs       []*colAcc
	rows       int
	sample     [][]string
	sampleSize int
}

// pcell is one cell as a profile counts it: the type of its value and its text, or the issue that gave it no value
// (an error cell, a formula never calculated).
type pcell struct {
	kind, text, issue string
}

func (g *grid) addRow(cells []pcell) {
	g.rows++
	row := make([]string, len(g.accs))
	for i, a := range g.accs {
		var c pcell
		if i < len(cells) {
			c = cells[i]
		}
		if c.issue != "" {
			a.addIssue(c.issue)
			row[i] = issueText(c)
			continue
		}
		a.add(c.kind, c.text)
		row[i] = clip(c.text, maxValue)
	}
	if len(g.sample) < g.sampleSize {
		g.sample = append(g.sample, row)
	}
}

// issueText is what a sample shows for a cell that gave no value.
func issueText(c pcell) string {
	if c.issue == CellError {
		return c.text
	}
	return "(never calculated)"
}

func (g *grid) profile() TableProfile {
	t := TableProfile{Name: g.name, Rows: g.rows, SampleRows: g.sample, Columns: make([]ColumnProfile, len(g.accs))}
	if t.SampleRows == nil {
		t.SampleRows = [][]string{}
	}
	for i, a := range g.accs {
		t.Columns[i] = a.finish()
	}
	return t
}

func unnamed(name string, i int) string {
	if strings.TrimSpace(name) == "" {
		return "(column " + strconv.Itoa(i+1) + ")"
	}
	return name
}

// profileCSV reads a CSV file the way the csv parser of the pipeline does: the first row is the header.
func profileCSV(data []byte, o InspectOptions) TableProfile {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	g := &grid{name: "csv", sampleSize: o.SampleRows}
	header, err := r.Read()
	if err != nil {
		return g.profile()
	}
	for i, h := range header {
		g.accs = append(g.accs, newAcc(unnamed(h, i), "", false))
	}
	bad := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || len(rec) != len(header) {
			bad++
			continue
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		cells := make([]pcell, len(rec))
		for i, v := range rec {
			cells[i] = pcell{kind: textKind(v), text: v}
		}
		g.addRow(cells)
	}
	t := g.profile()
	t.Unreadable = bad
	return t
}

// profileJSONLines reads a JSON Lines file: the columns are the paths of the scalar values, in the order they were
// first seen (alphabetical within a line), with dots for objects and positions for lists.
func profileJSONLines(data []byte, o InspectOptions) TableProfile {
	recs, order, bad := readJSONLines(data)
	g := &grid{name: "jsonl", sampleSize: o.SampleRows}
	for _, k := range order {
		g.accs = append(g.accs, newAcc(k, "", false))
	}
	for _, r := range recs {
		cells := make([]pcell, len(order))
		for i, k := range order {
			cells[i] = pcell{kind: textKind(r[k]), text: r[k]}
		}
		g.addRow(cells)
	}
	t := g.profile()
	t.Unreadable = bad
	return t
}

// readJSONLines flattens every line, and gives the paths in the order they first appeared and the lines that are no
// JSON object.
func readJSONLines(data []byte) (recs []map[string]string, order []string, bad int) {
	seen := map[string]bool{}
	br := bufio.NewReader(bytes.NewReader(data))
	for {
		text, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(text)) > 0 {
			flat, ok := flattenLine(text)
			if !ok {
				bad++
			}
			keys := make([]string, 0, len(flat))
			for k := range flat {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if !seen[k] {
					seen[k] = true
					order = append(order, k)
				}
			}
			if ok {
				recs = append(recs, flat)
			}
		}
		if err != nil {
			return recs, order, bad
		}
	}
}

func flattenLine(text []byte) (map[string]string, bool) {
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || dec.More() {
		return nil, false
	}
	flat := map[string]string{}
	flatten("", doc, flat, 0)
	return flat, true
}

// flatten turns a JSON value into the text of its scalar values by path. A null is no value.
func flatten(path string, v any, out map[string]string, depth int) {
	if depth > 6 {
		return
	}
	join := func(seg string) string {
		if path == "" {
			return seg
		}
		return path + "." + seg
	}
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			flatten(join(k), e, out, depth+1)
		}
	case []any:
		for i, e := range x {
			if i >= 5 {
				break
			}
			flatten(join(strconv.Itoa(i)), e, out, depth+1)
		}
	case string:
		out[path] = x
	case json.Number:
		out[path] = x.String()
	case bool:
		out[path] = strconv.FormatBool(x)
	}
}
