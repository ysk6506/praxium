package ingest

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The parsers of the fixed list this slice has (ingestion.md 3.4). The others of the list come later.
const (
	ParserCSV       = "csv"
	ParserJSONLines = "json_lines"
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
}

// Dataset is a table a source gives (ingestion.md 3.1). The slice has raw datasets that are received as a whole
// (snapshot) from a file.
type Dataset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	ReceiveMode string   `json:"receive_mode,omitempty"`
	Parser      string   `json:"parser"`
	NaturalKey  []string `json:"natural_key"`
	Deletion    string   `json:"deletion,omitempty"`
	Columns     []Column `json:"columns"`
}

// Source is a place data comes from (ingestion.md 3.1) with the datasets it gives.
type Source struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Adapter   string    `json:"adapter"`
	Collector string    `json:"collector,omitempty"`
	Datasets  []Dataset `json:"datasets"`
}

// decodeStrict reads one JSON document and refuses a field it does not know: a misspelt field of a definition
// must not be ignored.
func decodeStrict(r io.Reader, what string, into any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the "+what)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return kernel.Wrap(kernel.CodeInvalidValue, err, "the "+what+" is not a valid definition")
	}
	if dec.More() {
		return kernel.Errorf(kernel.CodeInvalidValue, "the %s has more than one JSON document", what)
	}
	return nil
}

// ParseSource reads and checks a source definition.
func ParseSource(r io.Reader) (Source, error) {
	var s Source
	if err := decodeStrict(r, "source definition", &s); err != nil {
		return Source{}, err
	}
	return s.normalized()
}

// normalized fills the defaults and checks the whole source.
func (s Source) normalized() (Source, error) {
	if s.ID == "" {
		return Source{}, kernel.Errorf(kernel.CodeInvalidValue, "a source needs an id")
	}
	if s.Adapter != "file" {
		return Source{}, kernel.Errorf(kernel.CodeUnsupported, "source %s: adapter %q is not one this build has (only file)", s.ID, s.Adapter)
	}
	if s.Collector == "" {
		s.Collector = "in_product"
	}
	if len(s.Datasets) == 0 {
		return Source{}, kernel.Errorf(kernel.CodeInvalidValue, "source %s has no dataset", s.ID)
	}
	seen := map[string]bool{}
	ds := make([]Dataset, len(s.Datasets))
	for i, d := range s.Datasets {
		nd, err := d.normalized(s.ID)
		if err != nil {
			return Source{}, err
		}
		if seen[nd.ID] {
			return Source{}, kernel.Errorf(kernel.CodeInvalidValue, "source %s: dataset %s appears twice", s.ID, nd.ID)
		}
		seen[nd.ID] = true
		ds[i] = nd
	}
	s.Datasets = ds
	return s, nil
}

func (d Dataset) normalized(source string) (Dataset, error) {
	where := "source " + source + ", dataset " + d.ID
	if d.ID == "" {
		return d, kernel.Errorf(kernel.CodeInvalidValue, "source %s: a dataset needs an id", source)
	}
	if d.Kind == "" {
		d.Kind = "raw"
	}
	if d.ReceiveMode == "" {
		d.ReceiveMode = "snapshot"
	}
	if d.Deletion == "" {
		d.Deletion = DeletionAbsentInFull
	}
	if d.Kind != "raw" || d.ReceiveMode != "snapshot" {
		return d, kernel.Errorf(kernel.CodeUnsupported, "%s: only raw datasets received as snapshot exist in this build", where)
	}
	if d.Parser != ParserCSV && d.Parser != ParserJSONLines {
		return d, kernel.Errorf(kernel.CodeUnsupported, "%s: parser %q is not in this build (csv, json_lines)", where, d.Parser)
	}
	if d.Deletion != DeletionAbsentInFull && d.Deletion != DeletionNone {
		return d, kernel.Errorf(kernel.CodeUnsupported, "%s: deletion %q is not in this build", where, d.Deletion)
	}
	cols, err := d.checkColumns(where)
	if err != nil {
		return d, err
	}
	if len(d.NaturalKey) == 0 {
		return d, kernel.Errorf(kernel.CodeInvalidValue, "%s: natural_key is empty, so the order of the rows would decide the result", where)
	}
	for _, k := range d.NaturalKey {
		if !slices.Contains(cols, k) {
			return d, kernel.Errorf(kernel.CodeInvalidValue, "%s: natural_key names column %q that the dataset does not have", where, k)
		}
	}
	return d, nil
}

// checkColumns checks the names and returns them. A column with no path is read by its own name.
func (d *Dataset) checkColumns(where string) ([]string, error) {
	if len(d.Columns) == 0 {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s has no column", where)
	}
	var names []string
	for i := range d.Columns {
		c := &d.Columns[i]
		if c.Name == "" || slices.Contains(names, c.Name) {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s: column name %q is empty or repeated", where, c.Name)
		}
		if len(c.Paths) == 0 {
			c.Paths = []string{c.Name}
		}
		names = append(names, c.Name)
	}
	return names, nil
}
