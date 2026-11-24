package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The material of the tests of the sheet scanner (ADR-0103): sheets and datasets that are written by hand and sheets that
// a seeded generator makes, and the digest of a table. The expected answers in testdata were made by the code before the
// scanner (commit d1df3c0) from the same material; a tool that makes them is not kept, since a tool kept would make them
// again from the new code and the comparison would mean nothing.
//
// The cells of a test sheet are written as text: "x" is text, "#5" a number, "@2026-03-14" a date, "!#N/A" an error
// cell, "?" a formula with no stored value, "" no cell.

type xsCase struct {
	name string
	ds   Dataset
	wb   *Workbook
	// read is the columns the dataset reads, for the variant that leaves the other cells of the data rows without text.
	read map[int]bool
}

type xsRowSpec struct {
	n      int
	cells  []string
	hidden bool
}

func xsCellOf(col int, s string) (Cell, bool) {
	c := Cell{Col: col}
	switch {
	case s == "":
		return c, false
	case strings.HasPrefix(s, "#"):
		c.Kind, c.Text = CellNumber, s[1:]
	case strings.HasPrefix(s, "@"):
		c.Kind, c.Text = CellDate, s[1:]
	case strings.HasPrefix(s, "!"):
		c.Kind, c.Text = CellError, s[1:]
	case s == "?":
		c.Kind, c.Formula, c.NoValue = CellText, true, true
	default:
		c.Kind, c.Text = CellText, s
	}
	return c, true
}

// xsSheet makes a sheet from rows in the order of their numbers; a row without any cell is kept as a row.
func xsSheet(name string, rows []xsRowSpec, merges []Merge, hiddenCols []ColSpan) Sheet {
	sh := Sheet{Name: name, Index: 1, Kind: SheetKindWorksheet, State: SheetVisible, Merges: merges, HiddenCols: hiddenCols}
	for _, spec := range rows {
		r := SheetRow{Number: spec.n, Hidden: spec.hidden}
		for i, s := range spec.cells {
			if c, ok := xsCellOf(i+1, s); ok {
				r.Cells = append(r.Cells, c)
			}
		}
		if spec.hidden {
			sh.HiddenRows++
		}
		sh.Rows = append(sh.Rows, r)
	}
	return sh
}

func xsBook(sheets ...Sheet) *Workbook {
	wb := &Workbook{Sheets: sheets}
	for i := range wb.Sheets {
		wb.Sheets[i].Index = i + 1
	}
	return wb
}

func xsDataset(opts *SheetOptions, key []string, cols ...Column) Dataset {
	d := Dataset{ID: "ds", Parser: ParserXLSX, NaturalKey: key, Columns: cols, Sheet: opts}
	d, err := d.normalized("src")
	if err != nil {
		panic(err)
	}
	return d
}

func xsColumn(name string, optional bool, paths ...string) Column {
	return Column{Name: name, Paths: paths, Optional: optional}
}

// xsDigest is the SHA-256 of the table as JSON.
func xsDigest(t Table) string {
	b, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func xsRows(specs ...xsRowSpec) []xsRowSpec { return specs }

func xsRow(n int, cells ...string) xsRowSpec { return xsRowSpec{n: n, cells: cells} }

func xsHiddenRow(n int, cells ...string) xsRowSpec {
	return xsRowSpec{n: n, cells: cells, hidden: true}
}

// xsTableRows is the standard small table of the hand-written cases: a key, a name and a zone under a header in row 1.
func xsTableRows(first int, extra ...xsRowSpec) []xsRowSpec {
	out := []xsRowSpec{xsRow(first, "Asset", "Name", "Zone")}
	return append(out, extra...)
}

func xsStd() []Column {
	return []Column{xsColumn("asset", false, "Asset"), xsColumn("name", false, "Name"), xsColumn("zone", true, "Zone")}
}

// xsHandCases are the cases written by hand, one for each thing the scanner decides.
func xsHandCases() []xsCase {
	std := xsStd()
	one := func(name string, d Dataset, sh ...Sheet) xsCase { return xsCase{name: name, ds: d, wb: xsBook(sh...)} }
	key := []string{"asset"}
	cases := []xsCase{
		one("plain", xsDataset(nil, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#10001", "web-01", "A"), xsRow(3, "#10002", "web-02", "B")), nil, nil)),
		one("title above the header", xsDataset(&SheetOptions{HeaderRow: 3}, key, std...), xsSheet("S", xsRows(
			xsRow(1, "Server list"), xsRow(3, "Asset", "Name", "Zone"), xsRow(4, "#1", "a", "A"), xsRow(6, "#2", "b", "B")), nil, nil)),
		one("blank rows in the middle and white space", xsDataset(nil, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(5, "#2", "b", "B"), xsRow(6, " ", "\t"), xsRow(7, "?", "?"), xsRow(8), xsRow(9, "#3", "c", "C")), nil, nil)),
		one("error and formula cells", xsDataset(nil, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "!#N/A"), xsRow(3, "#2", "?", "B"), xsRow(4, "!#REF!", "c", "C"), xsRow(5, "?", "d", "D")), nil, nil)),
		one("hidden rows are read", xsDataset(nil, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsHiddenRow(3, "#2", "b", "B")), nil, nil)),
		one("hidden rows are skipped", xsDataset(&SheetOptions{SkipHiddenRows: true}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsHiddenRow(3, "#2", "b", "B"), xsHiddenRow(4)), nil, nil)),
		one("hidden columns that are read", xsDataset(nil, key, std...), xsSheet("S", xsTableRows(1, xsRow(2, "#1", "a", "A")), nil,
			[]ColSpan{{Min: 2, Max: 3}, {Min: 9, Max: 9}})),
		one("a range of the data", xsDataset(&SheetOptions{Range: "A2:C4"}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(3, "#2", "b", "B"), xsRow(4, "#3", "c", "C"), xsRow(5, "#4", "d", "D"), xsRow(7, "#5", "e", "E")), nil, nil)),
		one("a range that starts at the second column", xsDataset(&SheetOptions{Range: "B2:D5"}, []string{"name"},
			xsColumn("name", false, "Name"), xsColumn("zone", true, "Zone"), xsColumn("rack", true, "Rack")),
			xsSheet("S", xsRows(xsRow(1, "Asset", "Name", "Zone", "Rack"), xsRow(2, "#1", "a", "A", "R1"), xsRow(4, "#2", "b", "B", "R2"), xsRow(6, "#3", "c", "C", "R3")), nil, nil)),
		one("the data ends after blank rows", xsDataset(&SheetOptions{StopAfterBlankRows: 2}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(5, "#2", "b", "B"), xsRow(7, "#3", "c", "C"), xsRow(8, "#4", "d", "D"), xsRow(9, " ", "\t"), xsRow(10, "", "", "", "x")), nil, nil)),
		one("the data ends and what follows is only white space", xsDataset(&SheetOptions{StopAfterBlankRows: 1}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(4, "#2", "b", "B"), xsRow(5, " "), xsRow(6, "?", "?", "?")), nil, nil)),
		one("merged values are first only", xsDataset(nil, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(3, "#2", "b"), xsRow(4, "#3", "c")), []Merge{{Row1: 2, Col1: 3, Row2: 4, Col2: 3}}, nil)),
		one("merged values are filled", xsDataset(&SheetOptions{MergedCells: MergeFill}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(3, "#2", "b"), xsRow(4, "#3", "c")), []Merge{{Row1: 2, Col1: 3, Row2: 4, Col2: 3}}, nil)),
		one("a merge whose first cell has no value", xsDataset(&SheetOptions{MergedCells: MergeFill}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a"), xsRow(3, "#2", "b", "B"), xsRow(4, "#3", "c")), []Merge{{Row1: 2, Col1: 3, Row2: 4, Col2: 3}, {Row1: 3, Col1: 3, Row2: 4, Col2: 3}}, nil)),
		one("a merge whose first cell is an error or has no stored value", xsDataset(&SheetOptions{MergedCells: MergeFill}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "!#N/A"), xsRow(3, "#2", "b"), xsRow(5, "#3", "c", "?"), xsRow(6, "#4", "d")), []Merge{{Row1: 2, Col1: 3, Row2: 3, Col2: 3}, {Row1: 5, Col1: 3, Row2: 6, Col2: 3}}, nil)),
		one("overlapping merges: the first in order decides", xsDataset(&SheetOptions{MergedCells: MergeFill}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(3, "#2", "b"), xsRow(4, "#3", "c", "Z")), []Merge{{Row1: 2, Col1: 2, Row2: 3, Col2: 3}, {Row1: 2, Col1: 3, Row2: 3, Col2: 3}, {Row1: 4, Col1: 3, Row2: 5, Col2: 4}}, nil)),
		one("a merge in the key column", xsDataset(&SheetOptions{MergedCells: MergeFill}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(3, "", "b", "B"), xsRow(4, "", "c", "C")), []Merge{{Row1: 2, Col1: 1, Row2: 4, Col2: 1}}, nil)),
		one("a header in two rows with a merged group", xsDataset(&SheetOptions{HeaderRows: 2}, []string{"id"},
			xsColumn("id", false, "ID"), xsColumn("cpu", false, "Spec CPU"), xsColumn("ram", false, "Spec RAM")),
			xsSheet("S", xsRows(xsRow(1, "", "Spec"), xsRow(2, "ID", "CPU", "RAM"), xsRow(3, "#1", "8", "32")), []Merge{{Row1: 1, Col1: 2, Row2: 1, Col2: 3}}, nil)),
		one("a header merged over three columns inside the used range", xsDataset(nil, key, xsColumn("asset", false, "Asset")),
			xsSheet("S", xsRows(xsRow(1, "Asset", "Notes"), xsRow(2, "#1", "a", "b", "c", "d"), xsRow(3, "#2", "", "", "", "e")), []Merge{{Row1: 1, Col1: 2, Row2: 1, Col2: 4}}, nil)),
		one("a header merged over rows", xsDataset(&SheetOptions{HeaderRows: 2}, key, xsColumn("asset", false, "Asset"), xsColumn("zone", true, "Place Zone")),
			xsSheet("S", xsRows(xsRow(1, "Asset", "Place"), xsRow(2, "", "Zone"), xsRow(3, "#1", "A")), []Merge{{Row1: 1, Col1: 1, Row2: 2, Col2: 1}}, nil)),
		one("a missing column", xsDataset(nil, key, std...), xsSheet("S", xsRows(xsRow(1, "Asset", "Nom", "Zone"), xsRow(2, "#1", "a", "A")), nil, nil)),
		one("an optional column that is not there and extra columns", xsDataset(nil, key, std...), xsSheet("S", xsRows(
			xsRow(1, "Asset", "Name", "", "Rack", "Note", "Rack"), xsRow(2, "#1", "a", "x", "R", "n")), nil, nil)),
		one("a header with line breaks and white space", xsDataset(nil, key, std...), xsSheet("S", xsRows(
			xsRow(1, " Asset\n", "Name  of\tthe asset", "Zone"), xsRow(2, "#1", "a", "A")), nil, nil)),
		one("the sheet is named and is not there", xsDataset(&SheetOptions{Name: "Nope"}, key, std...), xsSheet("S", xsTableRows(1), nil, nil)),
		one("the sheet number is too big", xsDataset(&SheetOptions{Index: 3}, key, std...), xsSheet("S", xsTableRows(1), nil, nil)),
		{name: "the sheet is a chart", ds: xsDataset(nil, key, std...), wb: &Workbook{Sheets: []Sheet{{Name: "Chart", Index: 1, Kind: "chart", State: SheetVisible}}}},
		one("the sheet is hidden and has no rows", xsDataset(nil, key, std...), Sheet{Name: "Empty", Index: 1, Kind: SheetKindWorksheet, State: SheetHidden}),
		one("a sheet with only a header", xsDataset(nil, key, std...), xsSheet("S", xsTableRows(1), nil, nil)),
		one("a header row below the last row has no merge", xsDataset(&SheetOptions{HeaderRow: 5}, key, std...), xsSheet("S", xsRows(
			xsRow(1, "Asset", "Name"), xsRow(3, "#1", "a")), []Merge{{Row1: 1, Col1: 1, Row2: 5, Col2: 3}}, nil)),
		one("rows without a key are passed over when the dataset says so", func() Dataset {
			d := xsDataset(nil, key, std...)
			d.IgnoreRowsWithoutKey = true
			return d
		}(), xsSheet("S", xsTableRows(1, xsRow(2, "#1", "a", "A"), xsRow(3, "", "", "Z"), xsRow(4, "", "b", "B"), xsRow(5, "Total", "", "")), nil, nil)),
		one("a natural key of two columns", xsDataset(nil, []string{"asset", "name"}, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(3, "#1", "", "B"), xsRow(4, "", "", "C"), xsRow(5, "#2", "b", "!#N/A")), nil, nil)),
		one("a sheet that is the second of the workbook, named", xsDataset(&SheetOptions{Name: "Racks"}, key, std...),
			xsSheet("Other", xsTableRows(1, xsRow(2, "#9", "z", "Z")), nil, nil), xsSheet("Racks", xsTableRows(1, xsRow(2, "#1", "a", "A")), nil, nil)),
		one("dates and numbers keep their text", xsDataset(nil, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#10001", "@2026-03-14", "#1.5"), xsRow(3, "#10002", "@2026-03-15", "#2")), nil, nil)),
		one("rows with no cell between the data", xsDataset(&SheetOptions{StopAfterBlankRows: 3}, key, std...), xsSheet("S", xsRows(
			xsRow(1, "Asset", "Name", "Zone"), xsRow(2, "#1", "a", "A"), xsRow(3), xsRow(4), xsRow(5), xsRow(6, "#2", "b", "B")), nil, nil)),
		one("the header has no name at all", xsDataset(nil, key, std...), xsSheet("S", xsRows(xsRow(2, "#1", "a", "A")), nil, nil)),
		one("a range that ends before the first data row", xsDataset(&SheetOptions{Range: "A2:C2"}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(2, "#1", "a", "A"), xsRow(3, "#2", "b", "B")), nil, nil)),
		one("the first row of the data is beyond the end of the range", xsDataset(&SheetOptions{Range: "A2:C3"}, key, std...), xsSheet("S", xsTableRows(1,
			xsRow(8, "#1", "a", "A")), nil, nil)),
	}
	return cases
}

// xsPick is a number in [0, n).
func xsPick(rng *kernel.SeededRng, n int) int { return int(rng.Uint64() % uint64(n)) }

func xsYes(rng *kernel.SeededRng, percent int) bool { return xsPick(rng, 100) < percent }

// xsRandomCases makes count cases of sheets and datasets from the seed. A header merge never goes beyond the columns that
// have cells (when the data has no range), because the rule of the end of a merged header changed with the scanner.
func xsRandomCases(seed uint64, count int) []xsCase {
	rng := kernel.NewSeededRng(seed)
	out := make([]xsCase, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, xsRandomCase(rng, i))
	}
	return out
}

type xsGen struct {
	rng      *kernel.SeededRng
	width    int
	hr, nh   int
	decl     []int // the column of each declared column
	declName []string
	rows     map[int]*SheetRow
	order    []int
	merges   []Merge
}

func (g *xsGen) put(row, col int, s string) {
	c, ok := xsCellOf(col, s)
	if !ok {
		return
	}
	r := g.rows[row]
	if r == nil {
		r = &SheetRow{Number: row}
		g.rows[row] = r
		g.order = append(g.order, row)
	}
	r.Cells = append(r.Cells, c)
}

func xsRandomCase(rng *kernel.SeededRng, i int) xsCase {
	g := &xsGen{rng: rng, rows: map[int]*SheetRow{}}
	g.width = 3 + xsPick(rng, 6)
	g.hr, g.nh = 1+xsPick(rng, 3), 1+xsPick(rng, 2)
	if xsYes(rng, 10) {
		g.nh = 3
	}
	nd := 1 + xsPick(rng, min(g.width, 4))
	perm := make([]int, g.width)
	for k := range perm {
		perm[k] = k + 1
	}
	for k := len(perm) - 1; k > 0; k-- {
		j := xsPick(rng, k+1)
		perm[k], perm[j] = perm[j], perm[k]
	}
	g.decl = perm[:nd]
	isDecl := map[int]int{}
	for d, p := range g.decl {
		isDecl[p] = d + 1
	}
	parts := g.header(isDecl)
	cols, key, readCols := g.columns(parts, nd)
	opts := &SheetOptions{HeaderRow: g.hr, HeaderRows: g.nh}
	g.data(opts)
	if xsYes(rng, 40) {
		opts.MergedCells = MergeFill
	}
	opts.SkipHiddenRows = xsYes(rng, 20)
	if xsYes(rng, 40) {
		opts.StopAfterBlankRows = 1 + xsPick(rng, 3)
	}
	first := g.hr + g.nh
	if xsYes(rng, 15) {
		r1 := first + xsPick(rng, 2)
		c1 := 1 + xsPick(rng, 2)
		opts.Range = fmt.Sprintf("%s%d:%s%d", ColumnName(c1), r1, ColumnName(g.width+xsPick(rng, 2)), r1+xsPick(rng, 10))
	}
	g.clamp(opts.Range == "")
	sh := Sheet{Name: "Target", Kind: SheetKindWorksheet, State: SheetVisible, Merges: g.merges}
	if xsYes(rng, 10) {
		sh.State = SheetHidden
	}
	if xsYes(rng, 5) {
		sh.Kind = "chart"
		sh.Rows = nil
	} else {
		for _, n := range g.order {
			sh.Rows = append(sh.Rows, *g.rows[n])
		}
		sortRows(&sh)
		for _, r := range sh.Rows {
			if r.Hidden {
				sh.HiddenRows++
			}
		}
	}
	if xsYes(rng, 15) {
		lo := 1 + xsPick(rng, g.width)
		sh.HiddenCols = []ColSpan{{Min: lo, Max: lo + xsPick(rng, 3)}}
	}
	book := xsBook(xsDecoy("Before"), sh, xsDecoy("After"))
	opts.Index = 2
	switch {
	case xsYes(rng, 30):
		opts.Index, opts.Name = 0, "Target"
	case xsYes(rng, 3):
		opts.Index, opts.Name = 0, "Absent"
	case xsYes(rng, 3):
		opts.Index = 9
	}
	ds := Dataset{ID: "ds", Parser: ParserXLSX, NaturalKey: key, Columns: cols, Sheet: opts, IgnoreRowsWithoutKey: xsYes(rng, 25)}
	nd2, err := ds.normalized("src")
	if err != nil {
		panic(err)
	}
	return xsCase{name: fmt.Sprintf("random %d", i), ds: nd2, wb: book, read: readCols}
}

func xsDecoy(name string) Sheet {
	return xsSheet(name, xsRows(xsRow(1, "Asset", "Name"), xsRow(2, "#9", "decoy")), nil, nil)
}

func sortRows(sh *Sheet) { sh.Sorted() }

// header writes the header rows: each declared column has a text of its own in the last header row and may have a group
// text above it; some groups are merged over the columns that follow. It gives the text of every column in every row.
func (g *xsGen) header(isDecl map[int]int) [][]string {
	rng := g.rng
	parts := make([][]string, g.nh)
	for k := range parts {
		parts[k] = make([]string, g.width+1)
	}
	for j := 1; j <= g.width; j++ {
		for k := 0; k < g.nh; k++ {
			last := k == g.nh-1
			switch {
			case last && isDecl[j] > 0:
				parts[k][j] = fmt.Sprintf("Col%d", isDecl[j])
			case last && xsYes(rng, 60):
				parts[k][j] = fmt.Sprintf("x%d", j)
			case !last && xsYes(rng, 35):
				parts[k][j] = fmt.Sprintf("G%d", xsPick(rng, 3))
			}
		}
	}
	for k := 0; k < g.nh; k++ {
		if xsYes(rng, 30) {
			j := 1 + xsPick(rng, g.width)
			span := 2 + xsPick(rng, 2)
			end := min(j+span-1, g.width)
			ok := parts[k][j] != ""
			for c := j + 1; c <= end; c++ {
				ok = ok && parts[k][c] == "" && (k != g.nh-1 || isDecl[c] == 0)
			}
			if ok && end > j {
				g.merges = append(g.merges, Merge{Row1: g.hr + k, Col1: j, Row2: g.hr + k, Col2: end})
			}
		}
	}
	for k := 0; k < g.nh; k++ {
		for j := 1; j <= g.width; j++ {
			g.put(g.hr+k, j, parts[k][j])
		}
	}
	if g.hr > 1 && xsYes(rng, 40) {
		g.put(1, 1, "A title above the table")
	}
	return parts
}

// columns gives the declared columns with the paths the header makes, the natural key and the columns that are read.
func (g *xsGen) columns(parts [][]string, nd int) ([]Column, []string, map[int]bool) {
	name := func(col int) string {
		var out []string
		for k := range parts {
			text := parts[k][col]
			if text == "" {
				for _, m := range g.merges {
					if m.Row1 == g.hr+k && col >= m.Col1 && col <= m.Col2 {
						text = parts[k][m.Col1]
						break
					}
				}
			}
			if text != "" {
				out = append(out, text)
			}
		}
		return strings.Join(out, " ")
	}
	var cols []Column
	read := map[int]bool{}
	for d, p := range g.decl {
		cn := []string{"k", "a", "b", "c"}[d]
		cols = append(cols, Column{Name: cn, Paths: []string{name(p)}, Optional: d > 0 && xsYes(g.rng, 25)})
		read[p] = true
	}
	key := []string{"k"}
	if nd > 1 && xsYes(g.rng, 30) {
		key = []string{"k", "a"}
	}
	return cols, key, read
}

// data writes the rows below the header and the merges among them.
func (g *xsGen) data(opts *SheetOptions) {
	rng := g.rng
	row := g.hr + g.nh + xsPick(rng, 3)
	for m := xsPick(rng, 14); m > 0; m-- {
		row += 1 + func() int {
			if xsYes(rng, 25) {
				return xsPick(rng, 4)
			}
			return 0
		}()
		hidden := xsYes(rng, 10)
		switch kind := xsPick(rng, 100); {
		case kind < 60:
			g.dataRow(row, hidden, false)
		case kind < 68:
			g.dataRow(row, hidden, true)
		case kind < 76:
			for j := 1; j <= g.width; j++ {
				if xsYes(rng, 40) {
					g.put(row, j, []string{" ", "\t", "  "}[xsPick(rng, 3)])
				}
			}
		case kind < 80:
			g.put(row, 1+xsPick(rng, g.width), "?")
		case kind < 84:
			g.rows[row] = &SheetRow{Number: row}
			g.order = append(g.order, row)
		}
		if hidden && g.rows[row] != nil {
			g.rows[row].Hidden = true
		}
	}
	for n := xsPick(rng, 3); n > 0; n-- {
		r1 := g.hr + g.nh + xsPick(rng, max(row-g.hr-g.nh, 1))
		c := 1 + xsPick(rng, g.width)
		g.merges = append(g.merges, Merge{Row1: r1, Col1: c, Row2: r1 + xsPick(rng, 4), Col2: min(c+xsPick(rng, 2), g.width)})
	}
}

func (g *xsGen) dataRow(row int, hidden, noKey bool) {
	rng := g.rng
	for j := 1; j <= g.width; j++ {
		isKey := j == g.decl[0]
		switch {
		case isKey && noKey:
			if xsYes(rng, 50) {
				g.put(row, j, " ")
			}
		case isKey:
			g.put(row, j, fmt.Sprintf("K%d", xsPick(rng, 6)))
		case xsYes(rng, 25):
		default:
			g.put(row, j, g.value())
		}
	}
	if hidden && g.rows[row] != nil {
		g.rows[row].Hidden = true
	}
}

func (g *xsGen) value() string {
	switch xsPick(g.rng, 10) {
	case 0:
		return "!#N/A"
	case 1:
		return "?"
	case 2:
		return "#" + fmt.Sprint(xsPick(g.rng, 100))
	case 3:
		return fmt.Sprintf("@2026-01-%02d", 1+xsPick(g.rng, 28))
	case 4:
		return " "
	}
	return fmt.Sprintf("v%d", xsPick(g.rng, 50))
}

// clamp keeps a merge inside the columns that have cells, so the header ends where it always did. With a range the
// columns are the range's and nothing is cut.
func (g *xsGen) clamp(noRange bool) {
	if !noRange {
		return
	}
	used := 0
	for _, r := range g.rows {
		for _, c := range r.Cells {
			used = max(used, c.Col)
		}
	}
	kept := g.merges[:0]
	for _, m := range g.merges {
		m.Col2 = min(m.Col2, used)
		if m.Col2 >= m.Col1 {
			kept = append(kept, m)
		}
	}
	g.merges = kept
}
