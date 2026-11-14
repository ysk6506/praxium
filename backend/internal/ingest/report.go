package ingest

import "strings"

// The kinds of problem a run reports. A problem never stops the run: the row or the value it is about is set
// aside with its reason and the rest goes on (pipeline.md 6 and 7).
const (
	ProblemUnreadable       = "unreadable_line"
	ProblemDuplicateKey     = "duplicate_key"
	ProblemShapeChanged     = "shape_changed"
	ProblemIncomplete       = "incomplete_capture"
	ProblemInvalidValue     = "invalid_value"
	ProblemMissingIdentity  = "missing_identity"
	ProblemOrphanValue      = "value_without_object"
	ProblemSourceConflict   = "source_conflict"
	ProblemIdentityConflict = "identity_conflict"
	ProblemLinkTarget       = "link_target_missing"
	ProblemLinkAmbiguous    = "link_target_ambiguous"
	ProblemCardinality      = "cardinality"
	ProblemExtraColumn      = "undeclared_column"
	// The problems of a cell of a workbook that gave no value (the row is still read).
	ProblemCellError      = "cell_error"
	ProblemFormulaNoValue = "formula_without_value"
)

// CellIssue is a cell of a mapped column that gave no value: an error the cell shows (#N/A), or a formula the file
// stored no value for.
type CellIssue struct {
	Kind   string
	Where  string
	Row    int
	Reason string
}

// Problem is one thing set aside. Key is the natural key of the row (its parts joined with |).
type Problem struct {
	Kind    string `json:"kind"`
	Dataset string `json:"dataset,omitempty"`
	Feed    string `json:"feed,omitempty"`
	Key     string `json:"key,omitempty"`
	Line    int    `json:"line,omitempty"`
	// Where is the place in a workbook: the sheet and the cell or the range of the row (Servers!C12).
	Where  string `json:"where,omitempty"`
	Reason string `json:"reason"`
}

// maxProblemSamples bounds the problems a report lists; the counts by kind are always complete.
const maxProblemSamples = 200

// DatasetReport is the row count of one dataset in one run (pipeline.md 7): the lines received are the lines read
// plus the lines that could not be read, and the lines read are used, filtered by a rule, a problem or unused.
type DatasetReport struct {
	Dataset     string `json:"dataset"`
	Collected   bool   `json:"collected"`
	CaptureHash string `json:"capture_hash,omitempty"`
	// SameCapture is true when the bytes are those of the previous run: the file was given again.
	SameCapture bool `json:"same_capture,omitempty"`
	Applied     bool `json:"applied"`
	Received    int  `json:"received"`
	Unreadable  int  `json:"unreadable"`
	Read        int  `json:"read"`
	Used        int  `json:"used"`
	Filtered    int  `json:"filtered"`
	Problem     int  `json:"problem"`
	Unused      int  `json:"unused"`
	// NotRead says why the file of a collected dataset was not read at all: the sheet is not there, or the header lacks
	// a column the dataset declares. The same text is the shape_changed problem; the counts of the line are then 0.
	NotRead string `json:"not_read,omitempty"`
	// UsedWithProblem is the rows counted as used that also had a problem; it is not part of the sum.
	UsedWithProblem int        `json:"used_with_problem"`
	Raw             RawChanges `json:"raw"`
	// The rest is for a dataset read from a workbook: the sheet, its state, how many hidden rows and hidden columns
	// the mapping read, and the rows passed over (blank_row, hidden_row, after_end), which are no part of the count.
	Sheet          string         `json:"sheet,omitempty"`
	SheetState     string         `json:"sheet_state,omitempty"`
	HiddenRowsRead int            `json:"hidden_rows_read,omitempty"`
	HiddenColsRead int            `json:"hidden_columns_read,omitempty"`
	Skipped        map[string]int `json:"skipped,omitempty"`
}

// Balanced tells whether the counts add up as pipeline.md 7 says.
func (d DatasetReport) Balanced() bool {
	return d.Received == d.Read+d.Unreadable && d.Read == d.Used+d.Filtered+d.Problem+d.Unused
}

// Report says what one run did.
type Report struct {
	SourceID string          `json:"source_id"`
	Run      int             `json:"run"`
	Datasets []DatasetReport `json:"datasets"`
	Problems []Problem       `json:"problems,omitempty"`
	// ProblemCounts counts every problem by kind, also those the list left out.
	ProblemCounts map[string]int `json:"problem_counts,omitempty"`

	Objects        int `json:"objects"`
	Links          int `json:"links"`
	VolatileValues int `json:"volatile_values"`
	RetiredObjects int `json:"retired_objects"`
	RetiredLinks   int `json:"retired_links"`

	// Changed is true when the batch changed the KG; the change numbers bound what it used.
	Changed   bool   `json:"changed"`
	FromSeq   int64  `json:"from_seq,omitempty"`
	ToSeq     int64  `json:"to_seq,omitempty"`
	IngestSeq int64  `json:"ingest_seq,omitempty"`
	VersionID string `json:"version_id,omitempty"`
	// NewVersion is true when the run sealed a version the KG did not have.
	NewVersion bool   `json:"new_version"`
	GraphHash  string `json:"graph_hash,omitempty"`
}

func (r *Report) dataset(id string) *DatasetReport {
	for i := range r.Datasets {
		if r.Datasets[i].Dataset == id {
			return &r.Datasets[i]
		}
	}
	r.Datasets = append(r.Datasets, DatasetReport{Dataset: id})
	return &r.Datasets[len(r.Datasets)-1]
}

func (r *Report) problem(p Problem) {
	p.Key = strings.ReplaceAll(p.Key, keySep, "|")
	if r.ProblemCounts == nil {
		r.ProblemCounts = map[string]int{}
	}
	r.ProblemCounts[p.Kind]++
	if len(r.Problems) < maxProblemSamples {
		r.Problems = append(r.Problems, p)
	}
}
