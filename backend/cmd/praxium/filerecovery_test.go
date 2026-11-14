package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/filejournal"
)

// A start that drops a cut last record says so as a warning, with the size, and says what it read.
// [REQ-272]
func TestAStartThatDropsTheTailSaysSo(t *testing.T) {
	dir := t.TempDir()
	s := openFileStore(t, dir, 0)
	load(t, s, 1)
	copyDir := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(e.Name(), "log-") {
			b = append(b, bytes.Repeat([]byte{0xEE}, 50)...)
		}
		if err := os.WriteFile(filepath.Join(copyDir, e.Name()), b, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	_, closeStore, err := openStoreLogged(context.Background(), serveOptions{store: storeFile, storeDataDir: copyDir}, wiring{}, logger(&out))
	if err != nil {
		t.Fatal(err)
	}
	if err := closeStore(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{`"msg":"praxium file store recovered"`, `"level":"WARN"`, "dropped the last record", `"dropped_bytes":50`} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %s:\n%s", want, got)
		}
	}
}

// A clean start only reports what it read; a fall back to an older snapshot is a warning.
// [REQ-274]
func TestTheRecoveryLogWarnsOnlyWhenSomethingWasLost(t *testing.T) {
	var clean bytes.Buffer
	logRecovery(logger(&clean), filejournal.Report{SnapshotSeq: 7, Replayed: 3, LastRecord: 10})
	if strings.Contains(clean.String(), "WARN") {
		t.Fatalf("a clean start warned:\n%s", clean.String())
	}
	var old bytes.Buffer
	logRecovery(logger(&old), filejournal.Report{SnapshotSeq: 7, SkippedSnapshots: []string{"snap-00000000000000000009.snap"}})
	for _, want := range []string{`"level":"WARN"`, "older snapshot", "snap-00000000000000000009.snap"} {
		if !strings.Contains(old.String(), want) {
			t.Errorf("log lacks %s:\n%s", want, old.String())
		}
	}
}
