package ingest

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// NaturalKeyPlaceholder is what a drafted dataset has as its natural key: nothing in the file says which column tells
// one row from another, so the draft names no column. Running a definition that still has it fails and lists the
// columns of the dataset, which says what to put there.
const NaturalKeyPlaceholder = "FILL IN: the column that tells one row from another"

// Draft is a source definition made from the profile of a file, and what a person should know about it.
type Draft struct {
	Source Source
	// Notes are lines for the person who edits the draft: what was left out and what is left to decide.
	Notes []string
}

// DraftSource makes a source definition from what Inspect found: a dataset for the file (CSV, JSON Lines) or for each
// visible sheet of a workbook, with the header row, the columns and a note of what the values look like. The draft is
// valid JSON that the definition reader accepts; only the natural key of every dataset is left for the person to
// choose (NaturalKeyPlaceholder). file is the name of the file, which names the source.
func DraftSource(p FileProfile, file, delimiter string) Draft {
	stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	id := "src_" + slug(stem, "file")
	d := Draft{Source: Source{ID: id, Name: stem, Adapter: "file"}}
	used := map[string]bool{}
	for _, t := range p.Tables {
		info, isSheet := profileSheetOf(p, t.Name)
		if isSheet && (info.State != SheetVisible || info.Kind != SheetKindWorksheet) {
			continue
		}
		ds, note := draftDataset(p.Format, t, info, isSheet, stem, delimiter, used)
		d.Source.Datasets = append(d.Source.Datasets, ds)
		d.Notes = append(d.Notes, note)
	}
	for _, s := range p.Sheets {
		if s.State != SheetVisible && s.Rows > 0 {
			d.Notes = append(d.Notes, fmt.Sprintf("the sheet %q is %s, so it has no dataset in the draft", s.Name, strings.ReplaceAll(s.State, "_", " ")))
		}
	}
	return d
}

// draftDataset makes the dataset of one table, and the note about its natural key.
func draftDataset(format string, t TableProfile, info SheetInfo, isSheet bool, stem, delimiter string, used map[string]bool) (Dataset, string) {
	ds := Dataset{ID: uniqueID("ds_"+slug(datasetStem(t, stem, isSheet), fmt.Sprintf("sheet_%d", info.Index)), used), Name: t.Name, Parser: parserOf(format)}
	switch {
	case isSheet:
		ds.Sheet = &SheetOptions{Name: t.Name, HeaderRow: t.HeaderRow}
		if t.HeaderRows > 1 {
			ds.Sheet.HeaderRows = t.HeaderRows
		}
	case ds.Parser == ParserCSV && delimiter != "" && delimiter != ",":
		ds.Delimiter = delimiter
	}
	var named map[string]string
	ds.Columns, named = draftColumns(t)
	ds.NaturalKey = []string{NaturalKeyPlaceholder}
	return ds, naturalKeyNote(ds.ID, t, named)
}

func datasetStem(t TableProfile, stem string, isSheet bool) string {
	if isSheet {
		return t.Name
	}
	return stem
}

func parserOf(format string) string {
	switch format {
	case FormatXLSX:
		return ParserXLSX
	case FormatJSONLines:
		return ParserJSONLines
	}
	return ParserCSV
}

func profileSheetOf(p FileProfile, name string) (SheetInfo, bool) {
	for _, s := range p.Sheets {
		if s.Name == name {
			return s, true
		}
	}
	return SheetInfo{}, false
}

// draftColumns is a column for every header that has a name. Its name is the header in lower case with underscores
// when that is plain ASCII, and the header itself otherwise.
func draftColumns(t TableProfile) ([]Column, map[string]string) {
	var cols []Column
	names := map[string]bool{}
	named := map[string]string{}
	for _, c := range t.Columns {
		if strings.HasPrefix(c.Name, "(column ") {
			continue
		}
		name := uniqueID(slug(c.Name, c.Name), names)
		col := Column{Name: name, Note: columnNote(c)}
		if name != c.Name {
			col.Paths = []string{c.Name}
		}
		cols = append(cols, col)
		named[c.Name] = name
	}
	return cols, named
}

// columnNote says what the values of a column look like: their type and the first of the frequent ones.
func columnNote(c ColumnProfile) string {
	out := c.ValueType
	var eg []string
	for i, v := range c.TopValues {
		if i == 3 {
			break
		}
		eg = append(eg, clip(v.Value, 20))
	}
	if len(eg) > 0 {
		out += "; e.g. " + strings.Join(eg, ", ")
	}
	if c.EmptyRatio > 0 {
		out += fmt.Sprintf("; %s empty", percent(c.EmptyRatio))
	}
	return out
}

func percent(r float64) string { return fmt.Sprintf("%g%%", r*100) }

// naturalKeyNote names the columns that are filled in every row and different in each: the candidates for the key.
func naturalKeyNote(id string, t TableProfile, named map[string]string) string {
	var cands []string
	if t.Rows > 0 && t.Rows <= maxDistinct {
		for _, c := range t.Columns {
			if name, ok := named[c.Name]; ok && c.EmptyRatio == 0 && c.DistinctCount == t.Rows {
				cands = append(cands, name)
			}
		}
	}
	if len(cands) == 0 {
		return id + ": natural_key is left to you: no single column is filled in every row and different in each, so use the columns that together tell one row from another"
	}
	return fmt.Sprintf("%s: natural_key is left to you; the columns filled in every row and different in each are: %s", id, strings.Join(cands, ", "))
}

// slug writes a header as a lower case ASCII name with underscores, or the fallback when nothing of it is plain.
func slug(s, fallback string) string {
	var b strings.Builder
	pending := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if pending && b.Len() > 0 {
				b.WriteByte('_')
			}
			pending = false
			b.WriteRune(r)
		default:
			pending = true
		}
	}
	if b.Len() == 0 || strings.IndexFunc(s, func(r rune) bool { return r >= unicode.MaxASCII }) >= 0 {
		return fallback
	}
	return b.String()
}

// uniqueID makes a name that is not in the set yet by adding _2, _3, and records it.
func uniqueID(name string, used map[string]bool) string {
	out := name
	for i := 2; used[out]; i++ {
		out = fmt.Sprintf("%s_%d", name, i)
	}
	used[out] = true
	return out
}
