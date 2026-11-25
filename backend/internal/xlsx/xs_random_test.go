package xlsx

import (
	"fmt"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// xsRandomCases makes count workbooks from the seed: the same seed always makes the same files. The writer of the package writes them,
// so what varies is the content and the options of the tools.
func xsRandomCases(seed uint64, count int) []xsCase {
	rng := kernel.NewSeededRng(seed)
	out := make([]xsCase, count)
	for i := range out {
		out[i] = xsCase{name: fmt.Sprintf("random %d", i), data: xsBookBytes(xsRandomBook(rng))}
	}
	return out
}

func xsPick(rng *kernel.SeededRng, n int) int { return int(rng.Uint64() % uint64(n)) }

func xsYes(rng *kernel.SeededRng, percent int) bool { return xsPick(rng, 100) < percent }

func xsRandomBook(rng *kernel.SeededRng) *Book {
	b := &Book{
		Date1904: xsYes(rng, 20), SharedStrings: xsYes(rng, 50), RichText: xsYes(rng, 30), AbsoluteTargets: xsYes(rng, 30),
		ExplicitN: xsYes(rng, 30), WordBool: xsYes(rng, 30), Macro: xsYes(rng, 10),
	}
	if xsYes(rng, 25) {
		b.Prefix = "x"
	}
	for s := 0; s <= xsPick(rng, 4); s++ {
		p := b.Sheet(fmt.Sprintf("Sheet %d", s+1))
		switch xsPick(rng, 10) {
		case 0:
			p.State = "hidden"
		case 1:
			p.State = "veryHidden"
		case 2:
			p.Chart = true
			continue
		}
		xsFill(rng, p)
	}
	return b
}

func xsFill(rng *kernel.SeededRng, p *Page) {
	rows, cols := 1+xsPick(rng, 40), 1+xsPick(rng, 12)
	for r := 1; r <= rows; r++ {
		if xsYes(rng, 25) {
			continue
		}
		for c := 1; c <= cols; c++ {
			if cell := xsRandomCell(rng); cell.kind != "" || cell.formula != "" {
				p.Set(ingest.CellRef(r, c), cell)
			}
		}
	}
	for range xsPick(rng, 4) {
		r1, c1 := 1+xsPick(rng, rows), 1+xsPick(rng, cols)
		p.Merges = append(p.Merges, ingest.CellRef(r1, c1)+":"+ingest.CellRef(r1+xsPick(rng, 3), c1+xsPick(rng, 3)))
	}
	for range xsPick(rng, 3) {
		p.HiddenRows = append(p.HiddenRows, 1+xsPick(rng, rows))
	}
	if xsYes(rng, 30) {
		lo := xsPick(rng, cols)
		p.HiddenCols = append(p.HiddenCols, ingest.ColumnName(1+lo)+":"+ingest.ColumnName(1+lo+xsPick(rng, 3)))
	}
}

var xsWords = []string{"alpha", "Beta", "  gamma  ", "a & b", "<tag>", "가나다", "x\ty", "", " ", "line\nbreak", "_x0041_"}

func xsRandomCell(rng *kernel.SeededRng) C {
	switch xsPick(rng, 14) {
	case 0, 1, 2, 3:
		return Str(xsWords[xsPick(rng, len(xsWords))])
	case 4, 5:
		return Num(fmt.Sprint(xsPick(rng, 100000)))
	case 6:
		return Num(fmt.Sprintf("%d.%dE+0%d", 1+xsPick(rng, 9), xsPick(rng, 1000), xsPick(rng, 9)))
	case 7:
		return Bool(xsYes(rng, 50))
	case 8:
		return Err([]string{"#N/A", "#REF!", "#DIV/0!"}[xsPick(rng, 3)])
	case 9:
		return Dated(fmt.Sprint(40000+xsPick(rng, 9000)), `yyyy\-mm\-dd`)
	case 10:
		return DatedID(fmt.Sprint(40000+xsPick(rng, 9000)), 14+xsPick(rng, 8))
	case 11:
		return ISODate(fmt.Sprintf("20%02d-%02d-%02d", 10+xsPick(rng, 20), 1+xsPick(rng, 12), 1+xsPick(rng, 28)))
	case 12:
		return Formula("A1+1", Num(fmt.Sprint(xsPick(rng, 50))))
	}
	return C{}
}
