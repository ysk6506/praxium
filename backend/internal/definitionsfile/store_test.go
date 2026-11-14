package definitionsfile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var ctx = context.Background()
var alice = definitions.Actor{ID: "alice"}

func newService(t *testing.T, st definitions.Store) *definitions.Service {
	t.Helper()
	svc, err := definitions.New(definitions.Config{Store: st, Clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), IDs: kernel.NewSequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func change(name, body string) definitions.Change {
	return definitions.Change{Kind: "source", Name: name, SchemaVersion: 1, Body: json.RawMessage(body)}
}

func open(t *testing.T, disk Disk) *Store {
	t.Helper()
	st, err := Open(disk)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

func wantCode(t *testing.T, err error, want kernel.Code) {
	t.Helper()
	if kernel.CodeOf(err) != want || err == nil {
		t.Fatalf("want code %s, got %v", want, err)
	}
}

// [REQ-340]
func TestAFolderWithoutTheFileIsEmptyAndNothingIsWrittenUntilTheFirstWrite(t *testing.T) {
	disk := NewMemDisk()
	st := open(t, disk)
	if head, _ := st.Head(ctx); head != 0 || len(disk.Names()) != 0 {
		t.Fatalf("an empty folder: head %d, files %v", head, disk.Names())
	}
	svc := newService(t, st)
	if _, err := svc.Apply(ctx, definitions.ApplyInput{Actor: alice, Changes: []definitions.Change{change("a", `{"x":1}`)}}); err != nil {
		t.Fatal(err)
	}
	if names := disk.Names(); len(names) != 1 || names[0] != FileName {
		t.Fatalf("files after a write: %v", names)
	}
}

// [REQ-340]
func TestTheStateIsTheSameAfterTheFileIsOpenedAgain(t *testing.T) {
	disk := NewMemDisk()
	svc := newService(t, open(t, disk))
	set, err := svc.Apply(ctx, definitions.ApplyInput{Actor: alice, Baseline: "std", Counters: map[string]int64{"n": 100003},
		Changes: []definitions.Change{change("a", `{"x":1}`), change("b", `{"y":[1,2]}`)}})
	if err != nil {
		t.Fatal(err)
	}
	br, p, err := svc.CreateBranch(ctx, definitions.CreateBranchInput{Name: "work", Actor: alice})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.EditBranch(ctx, definitions.EditBranchInput{BranchID: br.ID, ExpectVersion: 1, Actor: alice,
		Changes: []definitions.Change{{ItemID: set.Set.Entries[0].ItemID, SchemaVersion: 1, Body: json.RawMessage(`{"x":2}`)}}}); err != nil {
		t.Fatal(err)
	}
	first := open(t, disk).Export()
	again := open(t, disk)
	x, _ := json.Marshal(first)
	y, _ := json.Marshal(again.Export())
	if string(x) != string(y) {
		t.Fatalf("the reopened store differs:\n%s\n%s", x, y)
	}
	svc2 := newService(t, again)
	got, err := svc2.Proposal(ctx, p.ID)
	if err != nil || got.State != definitions.ProposalDraft {
		t.Fatalf("proposal after the reopen: %+v %v", got, err)
	}
	head, ok, _ := svc2.Head(ctx)
	if !ok || head.Hash == "" || head.Baseline != "std" {
		t.Fatalf("head after the reopen: %+v", head)
	}
	if n, _ := svc2.NextNumber(ctx, "n", 1); n != 100003 {
		t.Fatalf("the ledger after the reopen: %d", n)
	}
}

// [REQ-340]
func TestTheFileHasOneRecordToALineAndAReformattedFileIsStillRead(t *testing.T) {
	disk := NewMemDisk()
	svc := newService(t, open(t, disk))
	if _, err := svc.Apply(ctx, definitions.ApplyInput{Actor: alice, Changes: []definitions.Change{change("a", `{"x":1}`), change("b", `{"x":2}`)}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := disk.Bytes(FileName)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 18 || !strings.HasPrefix(lines[0], `{"format":"praxium-definitions","version":1,"head":1,`) {
		t.Fatalf("%d lines:\n%s", len(lines), raw)
	}
	var tidy bytes.Buffer
	if err := json.Indent(&tidy, raw, "", "    "); err != nil {
		t.Fatal(err)
	}
	other := NewMemDisk()
	other.Put(FileName, tidy.Bytes())
	again := open(t, other)
	if got, _ := again.Head(ctx); got != 1 {
		t.Fatalf("head %d", got)
	}
}

// [REQ-340]
func TestAFileThatIsNotUsableIsRefusedWithAReasonAndNothingIsOverwritten(t *testing.T) {
	good := NewMemDisk()
	svc := newService(t, open(t, good))
	if _, err := svc.Apply(ctx, definitions.ApplyInput{Actor: alice, Changes: []definitions.Change{change("a", `{"x":1}`)}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := good.Bytes(FileName)
	cases := map[string]struct {
		file    string
		want    kernel.Code
		message string
	}{
		"not json":          {"hello", kernel.CodeInvalidValue, "not a definitions file"},
		"another format":    {`{"format":"praxium-identity","version":1}`, kernel.CodeInvalidValue, "not a definitions file"},
		"a newer version":   {`{"format":"praxium-definitions","version":2}`, kernel.CodeUnsupported, "version 2"},
		"cut short":         {string(raw[:len(raw)-40]), kernel.CodeInvalidValue, "not a definitions file"},
		"body changed":      {strings.Replace(string(raw), `{"x":1}`, `{"x":9}`, 1), kernel.CodeInvalidValue, "hash"},
		"head changed":      {strings.Replace(string(raw), `"head":1`, `"head":4`, 1), kernel.CodeInvalidValue, "head"},
		"a bad body":        {strings.Replace(string(raw), `{"x":1}`, `[1]`, 1), kernel.CodeInvalidValue, "body"},
		"an unknown entry":  {strings.Replace(string(raw), `["item-000001",1]`, `["item-000001",7]`, 1), kernel.CodeInvalidValue, "not in the file"},
		"a bad entry":       {strings.Replace(string(raw), `["item-000001",1]`, `["item-000001"]`, 1), kernel.CodeInvalidValue, "not valid"},
		"a bad entry value": {strings.Replace(string(raw), `["item-000001",1]`, `[1,1]`, 1), kernel.CodeInvalidValue, "not valid"},
	}
	for name, c := range cases {
		disk := NewMemDisk()
		disk.Put(FileName, []byte(c.file))
		_, err := Open(disk)
		if kernel.CodeOf(err) != c.want || err == nil || !strings.Contains(err.Error(), c.message) {
			t.Errorf("%s: %v", name, err)
		}
		if after, _ := disk.Bytes(FileName); string(after) != c.file {
			t.Errorf("%s: the file was overwritten", name)
		}
	}
	defer func(old int) { maxFileLen = old }(maxFileLen)
	maxFileLen = len(raw) - 1
	big := NewMemDisk()
	big.Put(FileName, raw)
	_, err := Open(big)
	wantCode(t, err, kernel.CodeInvalidValue)
}

// [REQ-340]
func TestABranchBodyThatIsNotJSONIsRefused(t *testing.T) {
	disk := NewMemDisk()
	svc := newService(t, open(t, disk))
	br, _, _ := svc.CreateBranch(ctx, definitions.CreateBranchInput{Name: "work", Actor: alice})
	if _, _, err := svc.EditBranch(ctx, definitions.EditBranchInput{BranchID: br.ID, ExpectVersion: 1, Actor: alice, Changes: []definitions.Change{change("a", `{"x":1}`)}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := disk.Bytes(FileName)
	broken := NewMemDisk()
	broken.Put(FileName, []byte(strings.Replace(string(raw), `"body":{"x":1}`, `"body":[1]`, 1)))
	if _, err := Open(broken); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("a branch body that is not an object: %v", err)
	}
}

// [REQ-340]
func TestTheDiskFailingAtAnyStepLeavesTheOldFileAndTheOldState(t *testing.T) {
	boom := errors.New("boom")
	for _, step := range []string{"create", "write", "sync", "close", "rename"} {
		disk := NewMemDisk()
		svc := newService(t, open(t, disk))
		if _, err := svc.Apply(ctx, definitions.ApplyInput{Actor: alice, Changes: []definitions.Change{change("a", `{"x":1}`)}}); err != nil {
			t.Fatal(err)
		}
		before, _ := disk.Bytes(FileName)
		disk.Fail[step] = boom
		_, err := svc.Apply(ctx, definitions.ApplyInput{ExpectHead: 1, Actor: alice, Changes: []definitions.Change{change("b", `{"x":2}`)}})
		if !errors.Is(err, boom) {
			t.Fatalf("%s: %v", step, err)
		}
		delete(disk.Fail, step)
		after, _ := disk.Bytes(FileName)
		if !bytes.Equal(before, after) || len(disk.Names()) != 1 {
			t.Fatalf("%s: the file or the folder changed: %v", step, disk.Names())
		}
		if head, _, _ := svc.Head(ctx); head.No != 1 {
			t.Fatalf("%s: the state moved to D-%d", step, head.No)
		}
		// The write works once the disk does.
		if _, err := svc.Apply(ctx, definitions.ApplyInput{ExpectHead: 1, Actor: alice, Changes: []definitions.Change{change("b", `{"x":2}`)}}); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
	}
}

// [REQ-340]
func TestAFailureToSyncTheFolderKeepsTheNewStateAndSaysSo(t *testing.T) {
	disk := NewMemDisk()
	svc := newService(t, open(t, disk))
	disk.Fail["syncdir"] = errors.New("no sync")
	_, err := svc.Apply(ctx, definitions.ApplyInput{Actor: alice, Changes: []definitions.Change{change("a", `{"x":1}`)}})
	if err == nil || !strings.Contains(err.Error(), "may not survive a crash") {
		t.Fatalf("apply: %v", err)
	}
	if head, _, _ := svc.Head(ctx); head.No != 1 {
		t.Fatalf("the file was replaced, so the state must be the new one: D-%d", head.No)
	}
	delete(disk.Fail, "syncdir")
	if got := open(t, disk); func() int64 { h, _ := got.Head(ctx); return h }() != 1 {
		t.Fatal("the new file was not the file")
	}
}

// [REQ-340]
func TestOpeningNeedsTheDiskToAnswerAndRemovesTheLeftoverOfAnInterruptedWrite(t *testing.T) {
	disk := NewMemDisk()
	disk.Put(tmpName, []byte("half"))
	open(t, disk)
	if len(disk.Names()) != 0 {
		t.Fatalf("the leftover stayed: %v", disk.Names())
	}
	disk.Fail["remove"] = errors.New("no")
	disk.Put(tmpName, []byte("half"))
	_, err := Open(disk)
	wantCode(t, err, kernel.CodeInternal)
	delete(disk.Fail, "remove")
	disk.Fail["read"] = errors.New("no")
	_, err = Open(disk)
	wantCode(t, err, kernel.CodeInternal)
}

// [REQ-340]
func TestTheMemoryDiskBehavesLikeADisk(t *testing.T) {
	d := NewMemDisk()
	if _, err := d.ReadWhole("x"); err == nil {
		t.Fatal("a file that is not there")
	}
	f, _ := d.Create("x")
	if _, err := d.Create("x"); err == nil {
		t.Fatal("create over a file")
	}
	_, _ = f.Write([]byte("ab"))
	_ = f.Sync()
	_ = f.Close()
	if err := d.Rename("nope", "y"); err == nil {
		t.Fatal("rename of nothing")
	}
	if err := d.Rename("x", "y"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.ReadWhole("y"); string(got) != "ab" {
		t.Fatalf("content %q", got)
	}
	if err := d.Remove("x"); err == nil {
		t.Fatal("remove of nothing")
	}
	if d.SyncDir() != nil {
		t.Fatal("sync")
	}
}
