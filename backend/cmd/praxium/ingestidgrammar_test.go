package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-402]
// The examples `ingest init` writes and the draft `ingest inspect -emit-source` gives have "version": 1 and ids that follow
// the grammar of an id, and a draft of a file with an awkward name still does.
func TestIdgIngestToolsWriteTheVersionAndIdsOfTheGrammar(t *testing.T) {
	ex := initExamples(t)
	for _, name := range []string{"csv/source.json", "csv/mapping.json", "excel/source.json", "excel/mapping.json"} {
		data, err := os.ReadFile(filepath.Join(ex, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(data)), "{\n  \"version\": 1,") {
			t.Errorf("%s does not begin with its version", name)
		}
		if strings.HasSuffix(name, "source.json") {
			src, err := ingest.ParseSource(strings.NewReader(string(data)))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			ids := []string{src.ID}
			for _, d := range src.Datasets {
				ids = append(ids, d.ID)
			}
			for _, id := range ids {
				if !ingest.ValidID(id) {
					t.Errorf("%s: %q", name, id)
				}
			}
		}
	}
	odd := putFile(t, t.TempDir(), "Éxport 2026.Q1 "+strings.Repeat("ledger ", 15)+".csv", []byte("Asset No,Name\nA-1,x\n"))
	code, out, se := cmdRun(t, "ingest", "inspect", "-input", odd, "-emit-source")
	if code != exitOK || !strings.HasPrefix(out, "{\n  \"version\": 1,") {
		t.Fatalf("%d %s %s", code, out, se)
	}
	src, err := ingest.ParseSource(strings.NewReader(strings.Replace(out, "FILL IN: the column that tells one row from another", "asset_no", 1)))
	if err != nil || !ingest.ValidID(src.ID) || !ingest.ValidID(src.Datasets[0].ID) {
		t.Fatalf("%v %q %q", err, src.ID, src.Datasets[0].ID)
	}
}
