package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// printReport prints one run: a line for the run, a line for each dataset and the problems it set aside.
func printReport(out io.Writer, r ingest.Report) {
	fmt.Fprintf(out, "ingest source=%s run=%d objects=%d links=%d volatile=%d retired_objects=%d retired_links=%d changed=%t new_version=%t version=%s graph_hash=%s\n",
		r.SourceID, r.Run, r.Objects, r.Links, r.VolatileValues, r.RetiredObjects, r.RetiredLinks, r.Changed, r.NewVersion, r.VersionID, r.GraphHash)
	for _, d := range r.Datasets {
		fmt.Fprintf(out, "  dataset %s collected=%t received=%d unreadable=%d read=%d used=%d filtered=%d problem=%d unused=%d raw_added=%d raw_modified=%d raw_unchanged=%d raw_deleted=%d raw_unseen=%d%s\n",
			d.Dataset, d.Collected, d.Received, d.Unreadable, d.Read, d.Used, d.Filtered, d.Problem, d.Unused,
			d.Raw.Added, d.Raw.Modified, d.Raw.Unchanged, d.Raw.Deleted, d.Raw.Unseen, sheetFields(d))
	}
	kinds := make([]string, 0, len(r.ProblemCounts))
	for k := range r.ProblemCounts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Fprintf(out, "  problems %s=%d\n", k, r.ProblemCounts[k])
	}
	for i, p := range r.Problems {
		if i == 20 {
			fmt.Fprintf(out, "  ... %d more listed in the report\n", len(r.Problems)-i)
			break
		}
		fmt.Fprintf(out, "  problem %s dataset=%s key=%s line=%d%s: %s\n", p.Kind, p.Dataset, p.Key, p.Line, whereField(p), p.Reason)
	}
}

// whereField is the place of a problem in a workbook, when it has one.
func whereField(p ingest.Problem) string {
	if p.Where == "" {
		return ""
	}
	return " where=" + p.Where
}

// sheetFields is what a dataset read from a workbook adds to its line: the sheet and whether it is hidden, the
// hidden rows and columns that were read, and the rows that were passed over with their reasons.
func sheetFields(d ingest.DatasetReport) string {
	if d.Sheet == "" {
		return ""
	}
	out := fmt.Sprintf(" sheet=%q sheet_state=%s", d.Sheet, d.SheetState)
	if d.HiddenRowsRead > 0 || d.HiddenColsRead > 0 {
		out += fmt.Sprintf(" hidden_rows_read=%d hidden_columns_read=%d", d.HiddenRowsRead, d.HiddenColsRead)
	}
	reasons := make([]string, 0, len(d.Skipped))
	for k := range d.Skipped {
		reasons = append(reasons, k)
	}
	sort.Strings(reasons)
	for _, k := range reasons {
		out += fmt.Sprintf(" skipped_%s=%d", k, d.Skipped[k])
	}
	return out
}

// countProblems is every problem of a run, also those the list left out.
func countProblems(r ingest.Report) int {
	n := 0
	for _, c := range r.ProblemCounts {
		n += c
	}
	return n
}

// printEnd is the last lines of a run: the verdict, and whether the result was kept.
func printEnd(out io.Writer, problems int, dataDir string) {
	if problems == 0 {
		fmt.Fprintln(out, "completed without problems")
	} else {
		noun := "problems"
		if problems == 1 {
			noun = "problem"
		}
		fmt.Fprintf(out, "completed with %d %s (see \"problem\" lines above)\n", problems, noun)
	}
	if dataDir == "" {
		fmt.Fprintln(out, "The result was not kept: the KG lived in memory only while this command ran. Pass -data-dir <folder> to keep it, then look at it with praxium local -data-dir <folder>.")
		return
	}
	fmt.Fprintf(out, "The result is kept in %s (KG main). Look at it with: praxium local -data-dir %s\n", dataDir, dataDir)
}
