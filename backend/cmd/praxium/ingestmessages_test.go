package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-384]
func TestACutListOfUndeclaredColumnsSaysHowManyThereAre(t *testing.T) {
	rep := ingest.Report{SourceID: "s", ProblemCounts: map[string]int{ingest.ProblemExtraColumn: 30}}
	for i := 0; i < 30; i++ {
		rep.Problems = append(rep.Problems, ingest.Problem{Kind: ingest.ProblemExtraColumn, Dataset: "d", Reason: fmt.Sprintf("column %d", i)})
	}
	var out bytes.Buffer
	printReport(&out, rep)
	text := out.String()
	for _, want := range []string{"10 more listed in the report", "10 more undeclared_column problems are not listed (30 in all)", "ignore_undeclared_columns"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not in:\n%s", want, text)
		}
	}
	few := ingest.Report{SourceID: "s", ProblemCounts: map[string]int{ingest.ProblemExtraColumn: 1},
		Problems: []ingest.Problem{{Kind: ingest.ProblemExtraColumn, Dataset: "d", Reason: "column"}}}
	out.Reset()
	printReport(&out, few)
	if strings.Contains(out.String(), "more undeclared_column") {
		t.Fatalf("nothing was cut:\n%s", out.String())
	}
}

// [REQ-382]
func TestTheSummaryLineCountsTheRowsSkippedForHavingNoKey(t *testing.T) {
	rep := ingest.Report{SourceID: "s", Datasets: []ingest.DatasetReport{{Dataset: "d", Skipped: map[string]int{ingest.SkipNoKey: 3}}}}
	var out bytes.Buffer
	printReport(&out, rep)
	if !strings.Contains(out.String(), " skipped_no_key=3") {
		t.Fatalf("%s", out.String())
	}
}

// [REQ-392]
func TestARunOfTheDraftAsItIsNamesTheCandidatesOfTheKeyBeforeEveryColumn(t *testing.T) {
	ex := initExamples(t)
	servers := ex + "/csv/servers.csv"
	_, out, _ := cmdRun(t, "ingest", "inspect", "-input", servers, "-emit-source")
	source := putFile(t, t.TempDir(), "source.json", []byte(out))
	mapping := putFile(t, t.TempDir(), "mapping.json", []byte(`{"feeds":[]}`))
	code, _, se := cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping, "-input", "ds_servers="+servers)
	if code != exitFailure {
		t.Fatalf("%d %s", code, se)
	}
	cand := strings.Index(se, "the columns filled in every row and different in each are: asset_id")
	all := strings.Index(se, "All the columns of the dataset:")
	if cand < 0 || all < cand || !strings.Contains(se, `"serial_number"`) {
		t.Fatalf("the candidates come before the list of every column:\n%s", se)
	}
	// without a file for the dataset the error is the old one
	if _, _, se = cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping, "-input", "ds_other="+servers); strings.Contains(se, "filled in every row") || !strings.Contains(se, "The columns are:") {
		t.Fatalf("no file, no candidates:\n%s", se)
	}
}

// [REQ-384]
func TestUndeclaredColumnsComeAfterTheProblemsAboutTheData(t *testing.T) {
	rep := ingest.Report{SourceID: "s", ProblemCounts: map[string]int{ingest.ProblemExtraColumn: 25, ingest.ProblemLinkTarget: 1}}
	for i := 0; i < 25; i++ {
		rep.Problems = append(rep.Problems, ingest.Problem{Kind: ingest.ProblemExtraColumn, Dataset: "d", Reason: fmt.Sprintf("column %d", i)})
	}
	rep.Problems = append(rep.Problems, ingest.Problem{Kind: ingest.ProblemLinkTarget, Dataset: "d", Reason: "no row"})
	var out bytes.Buffer
	printReport(&out, rep)
	if !strings.Contains(out.String(), "problem link_target_missing") {
		t.Fatalf("the problem about the data is shown first:\n%s", out.String())
	}
}

// [REQ-393]
// The guide that `ingest init` writes names the option, the new summary item and the new messages.
func TestTheGuideThatInitWritesSpeaksOfTheMessageChanges(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ex")
	if code, _, stderr := cmdRun(t, "ingest", "init", "-out", out); code != exitOK {
		t.Fatalf("init failed (%d): %s", code, stderr)
	}
	b, err := os.ReadFile(filepath.Join(out, "ingest-definitions.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ignore_rows_without_key", "skipped_no_key", "undeclared_column problems are not listed", "types list --all", "BOM", "cardinality N:1"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the guide does not mention %q", want)
		}
	}
	_, help, _ := cmdRun(t, "ingest", "run", "--help")
	if !strings.Contains(help, "skipped_no_key") || !strings.Contains(help, "ignore_rows_without_key") {
		t.Errorf("ingest run --help does not explain the new summary item")
	}
}

// [REQ-392]
func TestTheCandidatesAreLeftOutWhenTheFileCannotTellOrTheErrorIsAnother(t *testing.T) {
	ex := initExamples(t)
	servers := ex + "/csv/servers.csv"
	_, out, _ := cmdRun(t, "ingest", "inspect", "-input", servers, "-emit-source")
	source := putFile(t, t.TempDir(), "source.json", []byte(out))
	mapping := putFile(t, t.TempDir(), "mapping.json", []byte(`{"feeds":[]}`))
	missing := filepath.Join(t.TempDir(), "gone.csv")
	if _, _, se := cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping, "-input", "ds_servers="+missing); strings.Contains(se, "filled in every row") || !strings.Contains(se, "natural_key") {
		t.Fatalf("a file that cannot be read gives the old error:\n%s", se)
	}
	utf16 := putFile(t, t.TempDir(), "u.csv", []byte("\xff\xfea\x00"))
	if _, _, se := cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping, "-input", "ds_servers="+utf16); strings.Contains(se, "filled in every row") {
		t.Fatalf("a file of another encoding gives the old error:\n%s", se)
	}
	other := putFile(t, t.TempDir(), "other.json", []byte(`{"id":"s","adapter":"file","datasets":[]}`))
	if _, _, se := cmdRun(t, "ingest", "run", "-source", other, "-mapping", mapping, "-input", "d="+servers); strings.Contains(se, "filled in every row") || !strings.Contains(se, "no dataset") {
		t.Fatalf("another error is as it was:\n%s", se)
	}
}

// [REQ-384]
func TestAReportThatHoldsOnlyItsFirstProblemsSaysSo(t *testing.T) {
	rep := ingest.Report{SourceID: "s", ProblemCounts: map[string]int{ingest.ProblemUnreadable: 500}}
	for i := 0; i < 200; i++ {
		rep.Problems = append(rep.Problems, ingest.Problem{Kind: ingest.ProblemUnreadable, Dataset: "d", Reason: "r"})
	}
	var out bytes.Buffer
	printReport(&out, rep)
	if !strings.Contains(out.String(), "500 problems in all; the report holds only the first 200") {
		t.Fatalf("%s", out.String())
	}
}

// [REQ-392]
func TestTheCandidatesAreGivenForTheDatasetThatStillHasThePlaceholder(t *testing.T) {
	ex := initExamples(t)
	servers := ex + "/csv/servers.csv"
	source := putFile(t, t.TempDir(), "source.json", []byte(`{"id":"s","adapter":"file","datasets":[
 {"id":"ds_ok","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"}]},
 {"id":"ds_servers","parser":"csv","natural_key":["FILL IN: the column that tells one row from another"],"columns":[{"name":"asset_id"},{"name":"serial_number"}]}]}`))
	mapping := putFile(t, t.TempDir(), "mapping.json", []byte(`{"feeds":[]}`))
	_, _, se := cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping, "-input", "ds_servers="+servers)
	if !strings.Contains(se, "dataset ds_servers the columns filled in every row and different in each are: asset_id") && !strings.Contains(se, "given for dataset ds_servers the columns filled in every row") {
		t.Fatalf("%s", se)
	}
}

// [REQ-382]
func TestTheSummaryLineSaysHowManyHiddenRowsAndColumnsWereRead(t *testing.T) {
	rep := ingest.Report{SourceID: "s", Datasets: []ingest.DatasetReport{{Dataset: "d", Sheet: "S", SheetState: "visible", HiddenRowsRead: 2, HiddenColsRead: 1}}}
	var out bytes.Buffer
	printReport(&out, rep)
	if !strings.Contains(out.String(), "hidden_rows_read=2 hidden_columns_read=1") {
		t.Fatalf("%s", out.String())
	}
}
