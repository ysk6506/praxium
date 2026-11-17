package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
)

// The parsers of the fixed list this slice has (ingestion.md 3.4). The others of the list come later.
const (
	ParserCSV       = "csv"
	ParserJSONLines = "json_lines"
	ParserXLSX      = "xlsx"
)

// The deletion rules of a dataset (ingestion.md 3.1); the slice has the first and the last.
const (
	DeletionAbsentInFull = "absent_in_full"
	DeletionNone         = "none"
)

// Column is one column of a dataset. Paths say where the value is: the header names of a CSV file, or the dotted
// paths into a JSON line; the first that exists is used, so a renamed column can keep its old path as a fallback.
// With no path the name itself is the path.
type Column struct {
	Name     string   `json:"name"`
	Paths    []string `json:"paths,omitempty"`
	Optional bool     `json:"optional,omitempty"`
	// Note is free text for the person who reads the definition (what `ingest inspect -emit-source` guessed the values
	// to be); nothing reads it.
	Note string `json:"note,omitempty"`
}

// Dataset is a table a source gives (ingestion.md 3.1). The slice has raw datasets that are received as a whole
// (snapshot) from a file.
type Dataset struct {
	// ID follows the grammar of an id (ADR-0075).
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	ReceiveMode string   `json:"receive_mode,omitempty"`
	Parser      string   `json:"parser"`
	NaturalKey  []string `json:"natural_key"`
	Deletion    string   `json:"deletion,omitempty"`
	Columns     []Column `json:"columns"`
	// Delimiter is the separator of the fields of a CSV file: a comma (default), a tab, a semicolon or a vertical
	// bar, written as the character or as comma, tab, semicolon or pipe. Only the csv parser has it.
	Delimiter string `json:"delimiter,omitempty"`
	// IgnoreUndeclaredColumns leaves out the columns of the file that no column of the dataset reads, instead of
	// reporting each as undeclared_column. A declared column that is gone is still reported.
	IgnoreUndeclaredColumns bool `json:"ignore_undeclared_columns,omitempty"`
	// IgnoreRowsWithoutKey passes over, without a problem, the rows whose natural key columns are all empty (the
	// total line under a table, say). A row with only some of them empty is still reported as unreadable. The rows
	// passed over are counted in the summary as skipped_no_key and are no part of the rows received.
	IgnoreRowsWithoutKey bool `json:"ignore_rows_without_key,omitempty"`
	// Sheet says which sheet of a workbook the dataset is and how its rows are laid out. Only the xlsx parser has it.
	Sheet *SheetOptions `json:"sheet,omitempty"`
}

// Source is a place data comes from (ingestion.md 3.1) with the datasets it gives.
type Source struct {
	// Version is the version of the document; none is version 1 (ADR-0075).
	Version   *int      `json:"version,omitempty"`
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Adapter   string    `json:"adapter"`
	Collector string    `json:"collector,omitempty"`
	Datasets  []Dataset `json:"datasets"`
	// Limits are the safety limits of the readers of workbook files; an unset one is the default.
	Limits *Limits `json:"limits,omitempty"`
}

// decodeStrict reads one JSON document and refuses a field it does not know: a misspelt field of a definition
// must not be ignored. The error it returns is a DefinitionErrors of one error: the first thing the decoder met.
func decodeStrict(r io.Reader, doc, what string, into any) error {
	c := newCollector(doc)
	data, err := io.ReadAll(r)
	if err != nil {
		c.add(ecDefUnreadable, "", Params{"what": what}, "cannot read the %s: %s", what, err)
		return c.err()
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		decodeProblem(c, data, what, into, err)
		return c.err()
	}
	if dec.More() {
		c.add(ecDefManyDocuments, "", nil, "the %s has more than one JSON document", what)
		return c.err()
	}
	return nil
}

// decodeProblem records what the decoder refused, with the place of it in the document.
func decodeProblem(c *collector, data []byte, what string, into any, err error) {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := indexedPath(typeErr.Field)
		if typeErr.Field == "" {
			field = "the document"
		}
		line := lineOf(data, typeErr.Offset)
		c.add(ecDefWrongKind, pathPointer(typeErr.Field), Params{"field": field, "wanted": kindWanted(typeErr.Type), "given": kindGiven(typeErr.Value), "line": line},
			"the %s is not a valid definition: %s", what, wrongKindMessage(data, typeErr))
		return
	}
	name, unknown := strings.CutPrefix(err.Error(), "json: unknown field ")
	if !unknown {
		c.add(ecDefMalformed, "", Params{"detail": err.Error()}, "the %s is not a valid definition: %s", what, err)
		return
	}
	field := strings.Trim(name, `"`)
	path, allowed, found := locateUnknown(data, field, into)
	msg := err.Error()
	if found {
		msg += "; " + unknownFieldSentence(field, path, allowed)
	}
	where := ""
	if found {
		where = ptr(pathSteps(path)...)
	}
	params := Params{"field": field}
	if found {
		params["allowed"] = allowed
		where += ptr(field)
	}
	c.add(ecDefUnknownField, where, params, "the %s is not a valid definition: %s", what, msg)
}

// ParseSource reads and checks a source definition. A definition that is refused gives a *DefinitionErrors.
func ParseSource(r io.Reader) (Source, error) {
	var s Source
	if err := decodeStrict(r, DocumentSource, "source definition", &s); err != nil {
		return Source{}, err
	}
	return s.normalized()
}

// normalized fills the defaults and checks the whole source.
func (s Source) normalized() (Source, error) {
	c := newCollector(DocumentSource)
	s = s.normalize(c)
	if err := c.err(); err != nil {
		return Source{}, err
	}
	return s, nil
}

func (s Source) normalize(c *collector) Source {
	c.checkVersion(s.Version, "source definition")
	if s.ID == "" {
		c.add(ecSrcIDMissing, ptr("id"), nil, "a source needs an id")
	}
	c.checkID(ecSrcIDInvalid, ptr("id"), "source", s.ID, nil)
	if s.Adapter != "file" {
		c.add(ecSrcAdapter, ptr("adapter"), Params{"source": s.ID, "adapter": s.Adapter, "supported": []string{"file"}},
			"source %s: adapter %q is not one this build has (only file)", s.ID, s.Adapter)
	}
	if s.Collector == "" {
		s.Collector = "in_product"
	}
	s.Limits.check(c, s.ID)
	if len(s.Datasets) == 0 {
		c.add(ecSrcNoDatasets, ptr("datasets"), Params{"source": s.ID}, "source %s has no dataset", s.ID)
	}
	first := map[string]int{}
	ds := make([]Dataset, len(s.Datasets))
	for i, d := range s.Datasets {
		nd := d.normalize(c, s.ID, i)
		if at, dup := first[nd.ID]; dup && nd.ID != "" {
			c.add(ecSrcDatasetDup, ptr("datasets", i, "id"), Params{"source": s.ID, "dataset": nd.ID, "first": at},
				"source %s: dataset %s appears twice", s.ID, nd.ID)
		} else if nd.ID != "" {
			first[nd.ID] = i
		}
		ds[i] = nd
	}
	s.Datasets = ds
	return s
}

// normalized checks one dataset of the source and fills its defaults; the errors come as one error.
func (d Dataset) normalized(source string) (Dataset, error) {
	c := newCollector(DocumentSource)
	d = d.normalize(c, source, 0)
	return d, c.err()
}

func (d Dataset) normalize(c *collector, source string, i int) Dataset {
	at := ptr("datasets", i)
	where := "source " + source + ", dataset " + d.ID
	if d.ID == "" {
		c.add(ecSrcDatasetID, at+ptr("id"), Params{"source": source}, "source %s: a dataset needs an id", source)
	}
	c.checkID(ecSrcDatasetBad, at+ptr("id"), "dataset", d.ID, Params{"source": source})
	if d.Kind == "" {
		d.Kind = "raw"
	}
	if d.ReceiveMode == "" {
		d.ReceiveMode = "snapshot"
	}
	if d.Deletion == "" {
		d.Deletion = DeletionAbsentInFull
	}
	parserOK := d.checkSupported(c, at, where)
	cols := d.checkColumns(c, at, where)
	if parserOK {
		d.Sheet = d.Sheet.normalize(c, at+ptr("sheet"), d, where)
		d.Delimiter = normalizedDelimiterTo(c, at+ptr("delimiter"), d.Delimiter, d.Parser, d.ID, where)
	}
	if len(d.Columns) > 0 {
		d.checkNaturalKey(c, at, cols, where)
	}
	return d
}

// checkSupported refuses what this build does not have. It tells whether the parser is one of the build's.
func (d Dataset) checkSupported(c *collector, at, where string) bool {
	if d.Kind != "raw" || d.ReceiveMode != "snapshot" {
		c.add(ecSrcMode, at+ptr("kind"), Params{"dataset": d.ID, "kind": d.Kind, "receive_mode": d.ReceiveMode},
			"%s: only raw datasets received as snapshot exist in this build", where)
	}
	parserOK := d.Parser == ParserCSV || d.Parser == ParserJSONLines || d.Parser == ParserXLSX
	if !parserOK {
		c.add(ecSrcParser, at+ptr("parser"), Params{"dataset": d.ID, "parser": d.Parser, "supported": []string{ParserCSV, ParserJSONLines, ParserXLSX}},
			"%s: parser %q is not in this build (csv, json_lines, xlsx)", where, d.Parser)
	}
	if d.Deletion != DeletionAbsentInFull && d.Deletion != DeletionNone {
		c.add(ecSrcDeletion, at+ptr("deletion"), Params{"dataset": d.ID, "deletion": d.Deletion, "supported": []string{DeletionAbsentInFull, DeletionNone}},
			"%s: deletion %q is not in this build", where, d.Deletion)
	}
	return parserOK
}

// checkColumns checks the names and returns the good ones. A column with no path is read by its own name.
func (d *Dataset) checkColumns(c *collector, at, where string) []string {
	if len(d.Columns) == 0 {
		c.add(ecSrcNoColumns, at+ptr("columns"), Params{"dataset": d.ID}, "%s has no column", where)
		return nil
	}
	var names []string
	for i := range d.Columns {
		col := &d.Columns[i]
		if col.Name == "" || slices.Contains(names, col.Name) {
			c.add(ecSrcColumnName, at+ptr("columns", i, "name"), Params{"dataset": d.ID, "name": col.Name},
				"%s: column name %q is empty or repeated", where, col.Name)
			continue
		}
		if len(col.Paths) == 0 {
			col.Paths = []string{col.Name}
		}
		names = append(names, col.Name)
	}
	return names
}

// checkNaturalKey checks that the natural key names columns of the dataset. The message lists the columns, so a key
// that was left to be filled in (as `ingest inspect -emit-source` leaves it) tells what to put there.
func (d Dataset) checkNaturalKey(c *collector, at string, cols []string, where string) {
	const what = "natural_key is the column, or the columns together, whose value tells one row from another"
	if len(d.NaturalKey) == 0 {
		c.add(ecSrcKeyEmpty, at+ptr("natural_key"), Params{"dataset": d.ID, "columns": cols},
			"%s: natural_key is empty, so the order of the rows would decide the result; %s. The columns are: %s", where, what, columnList(cols))
		return
	}
	for i, k := range d.NaturalKey {
		if !slices.Contains(cols, k) {
			c.add(ecSrcKeyColumn, at+ptr("natural_key", i), Params{"dataset": d.ID, "column": k, "columns": cols},
				"%s: natural_key names column %q that the dataset does not have; %s. The columns are: %s", where, k, what, columnList(cols))
		}
	}
}
