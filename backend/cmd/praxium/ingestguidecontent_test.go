package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// [REQ-364]
// The guide that `ingest init` writes names what this change brought: the row limit and its field, the new
// summary items, and the memory table that tells a user what a big file costs.
func TestTheGuideThatInitWritesSpeaksOfTheNewBehaviour(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ex")
	if code, _, stderr := cmdRun(t, "ingest", "init", "-out", out); code != exitOK {
		t.Fatalf("init failed (%d): %s", code, stderr)
	}
	b, err := os.ReadFile(filepath.Join(out, "ingest-definitions.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(b)
	for _, want := range []string{
		"max_rows_per_dataset", "waiting_objects", "used_with_problem", "cells_left_out", "2,637MB", "key_name",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("the guide does not mention %q", want)
		}
	}
}
