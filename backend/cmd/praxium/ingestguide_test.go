package main

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-330: `ingest init` writes the guide to the format of the definitions next to the examples, and the copy in the
// program is the original under docs/guides, byte for byte, so the two cannot drift apart.
func TestInitWritesTheFormatGuideEqualToTheOriginal(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ex")
	if code, _, stderr := cmdRun(t, "ingest", "init", "-out", out); code != exitOK {
		t.Fatalf("init failed (%d): %s", code, stderr)
	}
	written, err := os.ReadFile(filepath.Join(out, "ingest-definitions.md"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "guides", "ingest-definitions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(original) {
		t.Fatal("the guide that ingest init writes differs from docs/guides/ingest-definitions.md: copy the original to backend/internal/ingestcheck/guide/ingest-definitions.md")
	}
}
