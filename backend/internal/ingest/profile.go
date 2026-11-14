package ingest

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The profile of a file is the first of the mapping helpers of the design (ingestion-extensions.md 4): what a person
// needs to know about a file before writing a mapping for it. The names of its fields are the design's: the
// columns with their value type, the share of empty values, the number of distinct values, the frequent values and
// the shape of the values, and sample rows.

// The value types a profile tells apart.
const (
	TypeEmpty    = "empty"
	TypeInteger  = "integer"
	TypeNumber   = "number"
	TypeBoolean  = "boolean"
	TypeDate     = "date"
	TypeDateTime = "datetime"
	TypeTime     = "time"
	TypeText     = "text"
	TypeMixed    = "mixed"
)

// ValueCount is a value and how many times it is in the column.
type ValueCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// ColumnProfile describes one column of a table.
type ColumnProfile struct {
	// Name is the header of the column; Column is its letters on a sheet.
	Name   string `json:"name"`
	Column string `json:"column,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
	// ValueType is the type of the values: integer, number, boolean, date, datetime, time, text, mixed (more than
	// one of them) or empty. A text of digits with a leading zero is text.
	ValueType     string       `json:"value_type"`
	EmptyRatio    float64      `json:"empty_ratio"`
	DistinctCount int          `json:"distinct_count"`
	TopValues     []ValueCount `json:"top_values,omitempty"`
	// Shape is the most common form of the values: A for an upper case letter, a for a lower case one, 9 for a digit,
	// + after a character that is repeated (SN-A100 is A+-A9+).
	Shape string `json:"shape,omitempty"`
	// ErrorCells and UncalculatedCells count the cells of a sheet that show an error and the formulas with no stored
	// value; they are no values.
	ErrorCells        int `json:"error_cells,omitempty"`
	UncalculatedCells int `json:"uncalculated_cells,omitempty"`
}

// TableProfile describes the rows of one table: a CSV file, a JSON Lines file or a sheet.
type TableProfile struct {
	Name string `json:"name"`
	// HeaderRow and HeaderRows are where the header is on a sheet, as given or as guessed.
	HeaderRow   int             `json:"header_row,omitempty"`
	HeaderRows  int             `json:"header_rows,omitempty"`
	HeaderGuess bool            `json:"header_guessed,omitempty"`
	Rows        int             `json:"rows"`
	Unreadable  int             `json:"unreadable_rows,omitempty"`
	Columns     []ColumnProfile `json:"columns"`
	SampleRows  [][]string      `json:"sample_rows"`
}

// SheetInfo describes one sheet of a workbook: its size, whether it is hidden, and how much of it is hidden.
type SheetInfo struct {
	Index         int    `json:"index"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	UsedRange     string `json:"used_range,omitempty"`
	Rows          int    `json:"rows"`
	Columns       int    `json:"columns"`
	HiddenRows    int    `json:"hidden_rows,omitempty"`
	HiddenColumns int    `json:"hidden_columns,omitempty"`
	MergedRanges  int    `json:"merged_ranges,omitempty"`
	HeaderRow     int    `json:"header_row,omitempty"`
}

// FileProfile is what a file looks like: its format, for a workbook its sheets, and a table for each thing read.
type FileProfile struct {
	Format string         `json:"format"`
	Bytes  int            `json:"bytes"`
	Sheets []SheetInfo    `json:"sheets,omitempty"`
	Tables []TableProfile `json:"tables"`
}

// maxTop is how many frequent values a column lists; maxShape and maxValue cut the length of what is shown.
const (
	maxTop      = 5
	maxShape    = 32
	maxValue    = 40
	maxDistinct = 50_000
)

// colAcc gathers the values of one column.
type colAcc struct {
	name, letter string
	hidden       bool
	rows, filled int
	counts       map[string]int
	kinds        map[string]int
	shapes       map[string]int
	errs, noval  int
}

func newAcc(name, letter string, hidden bool) *colAcc {
	return &colAcc{name: name, letter: letter, hidden: hidden, counts: map[string]int{}, kinds: map[string]int{}, shapes: map[string]int{}}
}

// add counts one cell. kind is the type of the value; an empty text is no value.
func (a *colAcc) add(kind, text string) {
	a.rows++
	if strings.TrimSpace(text) == "" {
		return
	}
	a.filled++
	if len(a.counts) < maxDistinct || a.counts[text] > 0 {
		a.counts[text]++
	}
	a.kinds[kind]++
	a.shapes[shapeOf(text)]++
}

func (a *colAcc) addIssue(kind string) {
	a.rows++
	if kind == CellError {
		a.errs++
	} else {
		a.noval++
	}
}

func (a *colAcc) finish() ColumnProfile {
	p := ColumnProfile{Name: a.name, Column: a.letter, Hidden: a.hidden, ErrorCells: a.errs, UncalculatedCells: a.noval, DistinctCount: len(a.counts)}
	p.ValueType = typeOfKinds(a.kinds)
	if a.rows > 0 {
		p.EmptyRatio = float64(int((float64(a.rows-a.filled)/float64(a.rows))*10000+0.5)) / 10000
	}
	p.TopValues = topOf(a.counts, maxTop, maxValue)
	if top := topOf(a.shapes, 1, maxShape); len(top) > 0 {
		p.Shape = top[0].Value
	}
	return p
}

// typeOfKinds is the one type of a column: its only type; dates and dates with a time are one type (a date with a
// time when one value has a time), and so are integers and numbers (a number when one value is no integer). More than
// one type is mixed.
func typeOfKinds(kinds map[string]int) string {
	var types []string
	if kinds[TypeDate]+kinds[TypeDateTime] > 0 {
		types = append(types, choose(kinds[TypeDateTime] > 0, TypeDateTime, TypeDate))
	}
	if kinds[TypeInteger]+kinds[TypeNumber] > 0 {
		types = append(types, choose(kinds[TypeNumber] > 0, TypeNumber, TypeInteger))
	}
	for _, k := range []string{TypeBoolean, TypeTime, TypeText} {
		if kinds[k] > 0 {
			types = append(types, k)
		}
	}
	switch len(types) {
	case 0:
		return TypeEmpty
	case 1:
		return types[0]
	}
	return TypeMixed
}

func choose(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

// topOf lists the most frequent keys: by count, then by text, so the same file always gives the same list.
func topOf(counts map[string]int, n, cut int) []ValueCount {
	out := make([]ValueCount, 0, len(counts))
	for v, c := range counts {
		out = append(out, ValueCount{Value: v, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	if len(out) > n {
		out = out[:n]
	}
	for i := range out {
		out[i].Value = clip(out[i].Value, cut)
	}
	return out
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// shapeOf writes the form of a value: the class of each character, a run of one class as the class and a +.
func shapeOf(s string) string {
	var b strings.Builder
	var last rune
	run := 0
	flush := func() {
		if run > 1 {
			b.WriteRune('+')
		}
	}
	for _, r := range s {
		c := classOf(r)
		if c == last {
			run++
			continue
		}
		flush()
		b.WriteRune(c)
		last, run = c, 1
	}
	flush()
	return b.String()
}

func classOf(r rune) rune {
	switch {
	case unicode.IsDigit(r):
		return '9'
	case unicode.IsUpper(r):
		return 'A'
	case unicode.IsLower(r):
		return 'a'
	case unicode.IsLetter(r):
		return 'L'
	}
	return r
}

// textKind tells the type of a value that came as text (a CSV cell, a JSON value). Digits with a leading zero are an
// identifier, not a number.
func textKind(s string) string {
	t := strings.TrimSpace(s)
	switch {
	case t == "true" || t == "false":
		return TypeBoolean
	case isLeadingZero(t):
		return TypeText
	}
	if isInteger(t) {
		return TypeInteger
	}
	if _, err := strconv.ParseFloat(t, 64); err == nil && !strings.ContainsAny(t, "nNiI") {
		return TypeNumber
	}
	if _, err := time.Parse("2006-01-02", t); err == nil {
		return TypeDate
	}
	if _, err := time.Parse(time.RFC3339, t); err == nil {
		return TypeDateTime
	}
	return TypeText
}

func isInteger(t string) bool { return digitsOnly(strings.TrimPrefix(t, "-")) }

func isLeadingZero(t string) bool {
	t = strings.TrimPrefix(t, "-")
	return len(t) > 1 && t[0] == '0' && digitsOnly(t)
}

func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// cellType is the type of a cell of a sheet.
func cellType(c Cell) string {
	switch c.Kind {
	case CellNumber:
		if isInteger(c.Text) {
			return TypeInteger
		}
		return TypeNumber
	case CellBool:
		return TypeBoolean
	case CellDate:
		switch {
		case strings.Contains(c.Text, "T"):
			return TypeDateTime
		case strings.Contains(c.Text, ":"):
			return TypeTime
		}
		return TypeDate
	}
	return TypeText
}
