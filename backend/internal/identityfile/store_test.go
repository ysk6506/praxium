package identityfile

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var ifCtx = context.Background()

func ifOpen(t *testing.T, d *MemDisk) *Store {
	t.Helper()
	s, err := Open(d)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func ifPut(t *testing.T, s *Store, kind, id, data string, expect int64) int64 {
	t.Helper()
	v, err := s.Put(ifCtx, kind, id, []byte(data), expect)
	if err != nil {
		t.Fatalf("put %s/%s: %v", kind, id, err)
	}
	return v
}

// [REQ-311]
func TestWhatWasWrittenIsThereAfterTheFileIsOpenedAgain(t *testing.T) {
	d := NewMemDisk()
	s := ifOpen(t, d)
	if len(d.Names()) != 0 {
		t.Fatalf("opening an empty folder wrote %v", d.Names())
	}
	ifPut(t, s, "subject", "ann", `{"v":1}`, 0)
	v := ifPut(t, s, "token", "t1", "one", 0)
	ifPut(t, s, "token", "t1", "two", v)
	again := ifOpen(t, d)
	data, version, err := again.Get(ifCtx, "token", "t1")
	if err != nil || string(data) != "two" || version != 2 {
		t.Fatalf("after reopening: %q at %d, %v", data, version, err)
	}
	if names := d.Names(); len(names) != 1 || names[0] != FileName {
		t.Fatalf("the folder holds %v, want only %s", names, FileName)
	}
	raw, _ := d.Bytes(FileName)
	if !strings.Contains(string(raw), `"format": "praxium-identity"`) || !strings.Contains(string(raw), `"version": 1`) {
		t.Fatalf("the file has no format and version:\n%s", raw)
	}
}

// [REQ-311]
func TestAWriteThatFailsAtAnyStepLeavesTheOldFileAndTheOldRecords(t *testing.T) {
	for _, step := range []string{"create", "write", "sync", "close", "rename", "syncdir"} {
		t.Run(step, func(t *testing.T) {
			d := NewMemDisk()
			s := ifOpen(t, d)
			ifPut(t, s, "token", "t1", "kept", 0)
			before, _ := d.Bytes(FileName)
			d.Fail[step] = errors.New("disk trouble")
			if _, err := s.Put(ifCtx, "token", "t1", []byte("lost"), 1); kernel.CodeOf(err) != kernel.CodeInternal {
				t.Fatalf("a put with a failing %s: %v", step, err)
			}
			if _, err := s.Put(ifCtx, "token", "t2", []byte("new"), 0); err == nil {
				t.Fatalf("a create with a failing %s worked", step)
			}
			if step == "syncdir" {
				// The new file is in place and only its durability is in doubt: the store keeps agreeing with the file.
				if data, _, err := s.Get(ifCtx, "token", "t1"); err != nil || string(data) != "lost" {
					t.Fatalf("after a failed sync of the folder: %q, %v", data, err)
				}
				return
			}
			data, version, err := s.Get(ifCtx, "token", "t1")
			if err != nil || string(data) != "kept" || version != 1 {
				t.Fatalf("the record after the failure: %q at %d, %v", data, version, err)
			}
			if _, _, err := s.Get(ifCtx, "token", "t2"); kernel.CodeOf(err) != kernel.CodeNotFound {
				t.Fatalf("the record that was not written is there: %v", err)
			}
			after, _ := d.Bytes(FileName)
			if string(after) != string(before) {
				t.Fatalf("the file changed although %s failed", step)
			}
			if leftovers := d.Names(); len(leftovers) != 1 {
				t.Fatalf("leftover files: %v", leftovers)
			}
			d.Fail = map[string]error{}
			ifPut(t, s, "token", "t1", "later", 1)
		})
	}
}

// [REQ-311]
func TestOpenRefusesAFileThatIsNotWhole(t *testing.T) {
	tests := map[string]struct {
		file string
		fail map[string]error
		code kernel.Code
	}{
		"not JSON":          {file: "hello", code: kernel.CodeInvalidValue},
		"another format":    {file: `{"format":"other","version":1}`, code: kernel.CodeInvalidValue},
		"a newer version":   {file: `{"format":"praxium-identity","version":2}`, code: kernel.CodeUnsupported},
		"a failing read":    {file: "x", fail: map[string]error{"read": errors.New("io")}, code: kernel.CodeInternal},
		"a failing cleanup": {file: "x", fail: map[string]error{"remove": errors.New("io")}, code: kernel.CodeInternal},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			d := NewMemDisk()
			d.Put(FileName, []byte(tt.file))
			d.Fail = tt.fail
			if _, err := Open(d); kernel.CodeOf(err) != tt.code {
				t.Fatalf("error %v, want code %s", err, tt.code)
			}
			if got, _ := d.Bytes(FileName); string(got) != tt.file {
				t.Fatalf("a refused file was changed to %q", got)
			}
		})
	}
}

// [REQ-311]
func TestTheLeftoverOfAnInterruptedWriteIsRemovedAndTheOldFileIsKept(t *testing.T) {
	d := NewMemDisk()
	s := ifOpen(t, d)
	ifPut(t, s, "token", "t1", "kept", 0)
	d.Put(tmpName, []byte("half a file"))
	again := ifOpen(t, d)
	if data, _, err := again.Get(ifCtx, "token", "t1"); err != nil || string(data) != "kept" {
		t.Fatalf("the old file was not used: %q, %v", data, err)
	}
	if _, ok := d.Bytes(tmpName); ok {
		t.Fatal("the leftover is still there")
	}
}

// [REQ-311]
func TestAFileWithoutRecordsIsAnEmptyStoreAndTextThatIsNotTextIsRefused(t *testing.T) {
	d := NewMemDisk()
	d.Put(FileName, []byte(`{"format":"praxium-identity","version":1}`))
	s := ifOpen(t, d)
	if _, _, err := s.Get(ifCtx, "k", "i"); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("get from a file without records: %v", err)
	}
	if _, err := s.Put(ifCtx, "k", "i", []byte{0xff}, 0); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("a put of bytes that are not text: %v", err)
	}
	if _, err := s.Put(ifCtx, "k", "i", []byte("a\x00b"), 0); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("a put with NUL: %v", err)
	}
	if _, err := d.ReadWhole("nothing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the memory disk: %v", err)
	}
}
