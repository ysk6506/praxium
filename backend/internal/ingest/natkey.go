package ingest

import (
	"io"
	"strings"
)

// ParseSourceLoose reads a source definition without checking it as a whole: the fields must be known and of the right
// kind, but a natural key that names no column, say, is not refused. It is for the one who wants to say more about
// why a definition was refused.
func ParseSourceLoose(r io.Reader) (Source, error) {
	var s Source
	err := decodeStrict(r, DocumentSource, "source definition", &s)
	return s, err
}

// HasKeyPlaceholder tells whether the natural key of the dataset is still the placeholder a draft leaves.
func (d Dataset) HasKeyPlaceholder() bool {
	for _, k := range d.NaturalKey {
		if k == NaturalKeyPlaceholder {
			return true
		}
	}
	return false
}

// NaturalKeyCandidates reads the file a person gave for a dataset the way the dataset says (the sheet, the header row,
// the delimiter) and names the columns of the dataset that are filled in every row and different in each. They are
// what `ingest inspect -emit-source` told when it drafted the dataset. It is empty when no single column is.
func NaturalKeyCandidates(d Dataset, data []byte, books WorkbookReader) ([]string, error) {
	o := InspectOptions{Format: formatOf(d.Parser), Delimiter: d.Delimiter}
	if sh := d.Sheet; sh != nil {
		o.Sheet, o.HeaderRow, o.HeaderRows = sh.Name, sh.HeaderRow, sh.HeaderRows
		if o.Sheet == "" && sh.Index > 0 {
			o.Sheet = itoa(sh.Index)
		}
	}
	fp, err := Inspect(data, o, books)
	if err != nil {
		return nil, err
	}
	if len(fp.Tables) == 0 {
		return nil, nil
	}
	named := map[string]string{}
	for _, c := range d.Columns {
		paths := c.Paths
		if len(paths) == 0 {
			paths = []string{c.Name}
		}
		for _, p := range paths {
			named[p], named[NormalizeHeader(p)] = c.Name, c.Name
		}
	}
	return keyCandidates(fp.Tables[0], named), nil
}

func formatOf(parser string) string {
	switch parser {
	case ParserXLSX:
		return FormatXLSX
	case ParserJSONLines:
		return FormatJSONLines
	}
	return FormatCSV
}

// KeyCandidatesNote is the sentence that tells the candidates, or that there is none.
func KeyCandidatesNote(dataset string, cands []string) string {
	if len(cands) == 0 {
		return "In the file given for dataset " + dataset + " no single column is filled in every row and different in each, so use the columns that together tell one row from another."
	}
	return "In the file given for dataset " + dataset + " the columns filled in every row and different in each are: " + strings.Join(cands, ", ") + "."
}
