package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ysk6506/praxium/backend/internal/filesource"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/xlsx"
)

const inspectUsage = `usage: praxium ingest inspect -input <file> [-format csv|jsonl|xlsx] [-sheet <name|number>] [-header-row N] [-header-rows N] [-rows N] [-o table|json]
  Look at a file before writing a mapping for it (docs/design-docs/ingestion-extensions.md 4): CSV, JSON Lines or an
  Excel workbook. For a workbook: its sheets with their size and whether they are hidden, the header row it guesses (or
  the one -header-row and -header-rows give), and for each sheet the columns with their type, the share of empty
  values, the number of distinct values, the frequent values and the shape of the values, and the first rows.
  The file is only read. -o json gives the same as JSON.
`

// inspectFlags is what the command line of one inspection says.
type inspectFlags struct {
	input, format, sheet, output string
	headerRow, headerRows, rows  int
}

func parseInspect(args []string) (inspectFlags, error) {
	f := inspectFlags{output: "table"}
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return f, kernel.Errorf(kernel.CodeInvalidValue, "%s wants a value", args[i])
		}
		if err := f.set(args[i], args[i+1]); err != nil {
			return f, err
		}
	}
	if f.input == "" {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "ingest inspect needs -input")
	}
	return f, nil
}

func (f *inspectFlags) set(flag, v string) error {
	number := func(into *int, min int) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < min {
			return kernel.Errorf(kernel.CodeInvalidValue, "%s wants a number of %d or more, got %q", flag, min, v)
		}
		*into = n
		return nil
	}
	switch flag {
	case "-input":
		f.input = v
	case "-format":
		if v != ingest.FormatCSV && v != ingest.FormatJSONLines && v != ingest.FormatXLSX {
			return kernel.Errorf(kernel.CodeInvalidValue, "-format wants csv, jsonl or xlsx, got %q", v)
		}
		f.format = v
	case "-sheet":
		f.sheet = v
	case "-o":
		if v != "table" && v != "json" {
			return kernel.Errorf(kernel.CodeInvalidValue, "-o wants table or json, got %q", v)
		}
		f.output = v
	case "-header-row":
		return number(&f.headerRow, 1)
	case "-header-rows":
		return number(&f.headerRows, 1)
	case "-rows":
		return number(&f.rows, 1)
	default:
		return kernel.Errorf(kernel.CodeInvalidValue, "unexpected argument %q", flag)
	}
	return nil
}

// runInspect is `praxium ingest inspect ...`.
func runInspect(args []string, stdout, stderr io.Writer, w wiring) int {
	f, err := parseInspect(args)
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n%s", err, inspectUsage)
		return exitUsage
	}
	if err := inspectFile(f, stdout, w); err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n", err)
		return exitFailure
	}
	return exitOK
}

func inspectFile(f inspectFlags, stdout io.Writer, w wiring) error {
	in, err := openTruth(w, f.input)
	if err != nil {
		return kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read "+f.input)
	}
	defer in.Close()
	data, err := io.ReadAll(io.LimitReader(in, filesource.DefaultMaxBytes+1))
	if err != nil {
		return kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read "+f.input)
	}
	if len(data) > filesource.DefaultMaxBytes {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s is larger than the limit of the product (%d bytes)", f.input, filesource.DefaultMaxBytes)
	}
	format := f.format
	if format == "" {
		format = detectFormat(f.input, data)
	}
	prof, err := ingest.Inspect(data, ingest.InspectOptions{Format: format, Sheet: f.sheet, HeaderRow: f.headerRow, HeaderRows: f.headerRows, SampleRows: f.rows}, xlsx.Reader{})
	if err != nil {
		return err
	}
	if f.output == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(prof)
	}
	printProfile(stdout, f.input, prof)
	return nil
}

// detectFormat tells the format of a file: an archive or the old binary container is a workbook whatever it is
// named (so the error says what is wrong with it), then the name, then the first character.
func detectFormat(name string, data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("PK")) || bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return ingest.FormatXLSX
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx", ".xlsm", ".xls":
		return ingest.FormatXLSX
	case ".jsonl", ".ndjson":
		return ingest.FormatJSONLines
	case ".csv":
		return ingest.FormatCSV
	}
	if t := bytes.TrimSpace(data); len(t) > 0 && t[0] == '{' {
		return ingest.FormatJSONLines
	}
	return ingest.FormatCSV
}

// printProfile writes the profile as tables for a person to read.
func printProfile(out io.Writer, name string, p ingest.FileProfile) {
	fmt.Fprintf(out, "file: %s\nformat: %s\nsize: %d bytes\n", name, p.Format, p.Bytes)
	if len(p.Sheets) > 0 {
		fmt.Fprintf(out, "\nsheets (%d):\n", len(p.Sheets))
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  #\tNAME\tKIND\tSTATE\tUSED RANGE\tROWS\tCOLUMNS\tHIDDEN ROWS\tHIDDEN COLUMNS\tMERGED\tHEADER ROW")
		for _, s := range p.Sheets {
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n", s.Index, s.Name, s.Kind, s.State, dash(s.UsedRange), s.Rows, s.Columns,
				s.HiddenRows, s.HiddenColumns, s.MergedRanges, dash(numberText(s.HeaderRow)))
		}
		tw.Flush()
	}
	for _, t := range p.Tables {
		printTable(out, t)
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func numberText(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func printTable(out io.Writer, t ingest.TableProfile) {
	head := fmt.Sprintf("\ntable %q: %d rows", t.Name, t.Rows)
	if t.HeaderRow > 0 {
		head += fmt.Sprintf(", header in row %d", t.HeaderRow)
		if t.HeaderRows > 1 {
			head += fmt.Sprintf(" (%d rows)", t.HeaderRows)
		}
		if t.HeaderGuess {
			head += " (guessed; -header-row sets it)"
		}
	}
	if t.Unreadable > 0 {
		head += fmt.Sprintf(", %d rows that cannot be read", t.Unreadable)
	}
	fmt.Fprintln(out, head+":")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  COLUMN\tNAME\tTYPE\tEMPTY\tDISTINCT\tSHAPE\tFREQUENT VALUES")
	for _, c := range t.Columns {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%d\t%s\t%s\n", dash(c.Column+hiddenMark(c)), c.Name, columnType(c), percent(c.EmptyRatio), c.DistinctCount, dash(c.Shape), frequent(c))
	}
	tw.Flush()
	if len(t.SampleRows) == 0 {
		return
	}
	fmt.Fprintf(out, "first rows:\n")
	tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	names := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		names[i] = c.Name
	}
	fmt.Fprintln(tw, "  "+strings.Join(names, "\t"))
	for _, r := range t.SampleRows {
		fmt.Fprintln(tw, "  "+strings.Join(r, "\t"))
	}
	tw.Flush()
}

func hiddenMark(c ingest.ColumnProfile) string {
	if c.Hidden && c.Column != "" {
		return " (hidden)"
	}
	return ""
}

// columnType is the value type with the cells that gave no value.
func columnType(c ingest.ColumnProfile) string {
	out := c.ValueType
	if c.ErrorCells > 0 {
		out += fmt.Sprintf(" (+%d error cells)", c.ErrorCells)
	}
	if c.UncalculatedCells > 0 {
		out += fmt.Sprintf(" (+%d never calculated)", c.UncalculatedCells)
	}
	return out
}

func percent(r float64) string { return strconv.FormatFloat(r*100, 'f', -1, 64) + "%" }

func frequent(c ingest.ColumnProfile) string {
	var parts []string
	for i, v := range c.TopValues {
		if i == 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", v.Value, v.Count))
	}
	return dash(strings.Join(parts, ", "))
}
