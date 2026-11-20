package pgingest

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-559]
func TestPgiNothingOfThisUnitLeavesStateOnASessionAndOnlyTheCopyPreparesAStatement(t *testing.T) {
	rest := fstest.MapFS{}
	names, err := fs.Glob(ownSources, "*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == "copy.go" {
			continue
		}
		b, err := fs.ReadFile(ownSources, n)
		if err != nil {
			t.Fatal(err)
		}
		rest[n] = &fstest.MapFile{Data: b}
	}
	got, err := pgconn.SessionStateViolations(rest)
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
	// copy.go prepares the COPY on a transaction and runs it row by row on the statement; what the scan finds in it is that, and
	// only that (the statement is not a variable named tx).
	only := fstest.MapFS{"copy.go": &fstest.MapFile{Data: mustRead(t, "copy.go")}}
	got, err = pgconn.SessionStateViolations(only)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range got {
		if !strings.Contains(v, "a prepared statement") && !strings.Contains(v, "an Exec that does not run on a transaction") {
			t.Errorf("copy.go does more than prepare a statement: %s", v)
		}
	}
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(ownSources, name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// [REQ-700]
func TestPgiTheNamesTheCoreAndTheSchemaGiveForStagesClassesAndProblemsAreTheSame(t *testing.T) {
	var stages []string
	for _, s := range ingest.RunStages() {
		stages = append(stages, string(s))
	}
	if strings.Join(stages, ",") != strings.Join(pgschema.RunStages(), ",") {
		t.Errorf("stages: %v and %v", stages, pgschema.RunStages())
	}
	var over []string
	for _, s := range ingest.RunStages() {
		if s.Over() {
			over = append(over, string(s))
		}
	}
	if strings.Join(over, ",") != strings.Join(pgschema.RunOver(), ",") {
		t.Errorf("the stages that are an end: %v and %v", over, pgschema.RunOver())
	}
	var classes []string
	for _, c := range ingest.EndClasses() {
		classes = append(classes, string(c))
	}
	if strings.Join(classes, ",") != strings.Join(pgschema.EndClasses(), ",") {
		t.Errorf("classes: %v and %v", classes, pgschema.EndClasses())
	}
	kinds := map[string]bool{}
	for _, k := range pgschema.ProblemKinds() {
		kinds[k] = true
	}
	words := []string{ingest.ProblemUnreadable, ingest.ProblemDuplicateKey, ingest.ProblemShapeChanged, ingest.ProblemIncomplete,
		ingest.ProblemInvalidValue, ingest.ProblemMissingIdentity, ingest.ProblemOrphanValue, ingest.ProblemSourceConflict,
		ingest.ProblemIdentityConflict, ingest.ProblemLinkTarget, ingest.ProblemLinkAmbiguous, ingest.ProblemCardinality,
		ingest.ProblemExtraColumn, ingest.ProblemCellError, ingest.ProblemFormulaNoValue}
	for _, w := range words {
		if !kinds[w] {
			t.Errorf("the kind %q of the core is not one the schema accepts", w)
		}
	}
	if len(kinds) != len(words)+2 {
		t.Errorf("the schema has %d kinds, the core names %d and the two of the later steps", len(kinds), len(words))
	}
}
