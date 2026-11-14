package xlsx

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// styleTable gives every number format a cell uses a style index.
type styleTable struct {
	keys  []string
	index map[string]int
	codes []string // custom codes, id = 164 + position
}

func newStyles() *styleTable { return &styleTable{index: map[string]int{}} }

// of is the style index of a cell: 0 is General.
func (t *styleTable) of(c C) int {
	var key string
	switch {
	case c.fmtCode != "":
		key = "c:" + c.fmtCode
	case c.fmtID != 0:
		key = fmt.Sprintf("i:%d", c.fmtID)
	default:
		return 0
	}
	if i, ok := t.index[key]; ok {
		return i
	}
	if c.fmtCode != "" {
		t.codes = append(t.codes, c.fmtCode)
	}
	t.keys = append(t.keys, key)
	t.index[key] = len(t.keys)
	return len(t.keys)
}

func (b *Book) stylesXML(t *styleTable) string {
	var s strings.Builder
	s.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><` + b.el("styleSheet") + b.ns() + `>`)
	if len(t.codes) > 0 {
		s.WriteString(fmt.Sprintf(`<%s count="%d">`, b.el("numFmts"), len(t.codes)))
		for i, code := range t.codes {
			s.WriteString(fmt.Sprintf(`<%s numFmtId="%d" formatCode="%s"/>`, b.el("numFmt"), 164+i, esc(code)))
		}
		s.WriteString(`</` + b.el("numFmts") + `>`)
	}
	s.WriteString(`<` + b.el("cellStyleXfs") + ` count="1"><` + b.el("xf") + ` numFmtId="14"/></` + b.el("cellStyleXfs") + `>`)
	s.WriteString(fmt.Sprintf(`<%s count="%d"><%s numFmtId="0"/>`, b.el("cellXfs"), len(t.keys)+1, b.el("xf")))
	custom := 0
	for _, k := range t.keys {
		id := 0
		if strings.HasPrefix(k, "c:") {
			id = 164 + custom
			custom++
		} else {
			fmt.Sscanf(k, "i:%d", &id)
		}
		s.WriteString(fmt.Sprintf(`<%s numFmtId="%d"/>`, b.el("xf"), id))
	}
	s.WriteString(`</` + b.el("cellXfs") + `></` + b.el("styleSheet") + `>`)
	return s.String()
}

// sharedTable collects the shared strings of a workbook.
type sharedTable struct {
	index map[string]int
	list  []string
}

func (t *sharedTable) of(s string) int {
	if i, ok := t.index[s]; ok {
		return i
	}
	t.list = append(t.list, s)
	t.index[s] = len(t.list) - 1
	return len(t.list) - 1
}

func (b *Book) sstXML(t *sharedTable) string {
	var s strings.Builder
	s.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><` + b.el("sst") + b.ns() + fmt.Sprintf(` count="%d" uniqueCount="%d">`, len(t.list), len(t.list)))
	for _, text := range t.list {
		s.WriteString(`<` + b.el("si") + `>`)
		if b.RichText && len(text) > 1 {
			half := len(text) / 2
			for half > 0 && !isRuneStart(text[half]) {
				half--
			}
			run := func(x string) string {
				return `<` + b.el("r") + `><` + b.el("rPr") + `><` + b.el("sz") + ` val="11"/></` + b.el("rPr") + `><` + b.el("t") + ` xml:space="preserve">` + esc(x) + `</` + b.el("t") + `></` + b.el("r") + `>`
			}
			s.WriteString(run(text[:half]) + run(text[half:]))
			s.WriteString(`<` + b.el("rPh") + ` sb="0" eb="1"><` + b.el("t") + `>PHONETIC</` + b.el("t") + `></` + b.el("rPh") + `>`)
		} else {
			s.WriteString(`<` + b.el("t") + ` xml:space="preserve">` + esc(text) + `</` + b.el("t") + `>`)
		}
		s.WriteString(`</` + b.el("si") + `>`)
	}
	s.WriteString(`</` + b.el("sst") + `>`)
	return s.String()
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func (b *Book) sheetXML(p *Page, st *styleTable, sst *sharedTable) string {
	var s strings.Builder
	root := "worksheet"
	if p.Chart {
		root = "chartsheet"
	}
	s.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><` + b.el(root) + b.ns() + `>`)
	if p.Chart {
		return s.String() + `</` + b.el(root) + `>`
	}
	if len(p.HiddenCols) > 0 {
		s.WriteString(`<` + b.el("cols") + `>`)
		for _, h := range p.HiddenCols {
			from, to, _ := strings.Cut(h, ":")
			if to == "" {
				to = from
			}
			s.WriteString(fmt.Sprintf(`<%s min="%d" max="%d" width="9" hidden="%s" customWidth="1"/>`, b.el("col"), ingest.ColumnNumber(from), ingest.ColumnNumber(to), b.flag(true)))
		}
		s.WriteString(`</` + b.el("cols") + `>`)
	}
	s.WriteString(`<` + b.el("sheetData") + `>`)
	s.WriteString(b.rowsXML(p, st, sst))
	s.WriteString(`</` + b.el("sheetData") + `>`)
	if len(p.Merges) > 0 {
		s.WriteString(fmt.Sprintf(`<%s count="%d">`, b.el("mergeCells"), len(p.Merges)))
		for _, m := range p.Merges {
			s.WriteString(fmt.Sprintf(`<%s ref="%s"/>`, b.el("mergeCell"), m))
		}
		s.WriteString(`</` + b.el("mergeCells") + `>`)
	}
	s.WriteString(`</` + b.el(root) + `>`)
	return s.String()
}

func (b *Book) rowsXML(p *Page, st *styleTable, sst *sharedTable) string {
	rows := map[int][]int{}
	for k := range p.cells {
		rows[k[0]] = append(rows[k[0]], k[1])
	}
	for _, h := range p.HiddenRows {
		if _, ok := rows[h]; !ok {
			rows[h] = nil
		}
	}
	nums := make([]int, 0, len(rows))
	for r := range rows {
		nums = append(nums, r)
	}
	sort.Ints(nums)
	var s strings.Builder
	for _, r := range nums {
		hidden := ""
		for _, h := range p.HiddenRows {
			if h == r {
				hidden = fmt.Sprintf(` hidden="%s"`, b.flag(true))
			}
		}
		s.WriteString(fmt.Sprintf(`<%s r="%d"%s>`, b.el("row"), r, hidden))
		cols := rows[r]
		sort.Ints(cols)
		for _, c := range cols {
			s.WriteString(b.cellXML(ingest.CellRef(r, c), p.cells[[2]int{r, c}], st, sst))
		}
		s.WriteString(`</` + b.el("row") + `>`)
	}
	return s.String()
}

func (b *Book) cellXML(ref string, c C, st *styleTable, sst *sharedTable) string {
	attrs := fmt.Sprintf(` r="%s"`, ref)
	if i := st.of(c); i > 0 {
		attrs += fmt.Sprintf(` s="%d"`, i)
	}
	f := ""
	if c.formula != "" {
		f = `<` + b.el("f") + `>` + esc(c.formula) + `</` + b.el("f") + `>`
	}
	v := func(x string) string { return `<` + b.el("v") + `>` + esc(x) + `</` + b.el("v") + `>` }
	switch c.kind {
	case "s":
		if b.SharedStrings && c.formula == "" {
			return fmt.Sprintf(`<%s%s t="s">%s</%s>`, b.el("c"), attrs, v(fmt.Sprint(sst.of(c.raw))), b.el("c"))
		}
		if c.formula != "" {
			return fmt.Sprintf(`<%s%s t="str">%s%s</%s>`, b.el("c"), attrs, f, v(c.raw), b.el("c"))
		}
		return fmt.Sprintf(`<%s%s t="inlineStr"><%s><%s xml:space="preserve">%s</%s></%s></%s>`, b.el("c"), attrs, b.el("is"), b.el("t"), esc(c.raw), b.el("t"), b.el("is"), b.el("c"))
	case "b", "e", "d":
		return fmt.Sprintf(`<%s%s t="%s">%s%s</%s>`, b.el("c"), attrs, c.kind, f, v(c.raw), b.el("c"))
	}
	if b.ExplicitN {
		attrs += ` t="n"`
	}
	if c.noCache {
		return fmt.Sprintf(`<%s%s>%s<%s></%s></%s>`, b.el("c"), attrs, f, b.el("v"), b.el("v"), b.el("c"))
	}
	return fmt.Sprintf(`<%s%s>%s%s</%s>`, b.el("c"), attrs, f, v(c.raw), b.el("c"))
}
