package kernel_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

type object struct{}
type link struct{}

func TestParseID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"letters", "rack", true},
		{"digits first", "3d-view", true},
		{"all allowed punctuation", "a_b.c:d-e", true},
		{"longest", strings.Repeat("a", 128), true},
		{"empty", "", false},
		{"too long", strings.Repeat("a", 129), false},
		{"punctuation first", "-a", false},
		{"slash", "a/b", false},
		{"space", "a b", false},
		{"non-ASCII", "랙", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := kernel.ParseID[object](tt.raw)
			if (err == nil) != tt.ok {
				t.Fatalf("ParseID(%q) error = %v, want ok = %v", tt.raw, err, tt.ok)
			}
			if tt.ok && (id.String() != tt.raw || id.IsZero()) {
				t.Fatalf("ID = %q", id)
			}
			if !tt.ok && kernel.CodeOf(err) != kernel.CodeInvalidValue {
				t.Fatalf("code = %q", kernel.CodeOf(err))
			}
		})
	}
}

func TestZeroIDIsEmpty(t *testing.T) {
	var id kernel.ID[link]
	if !id.IsZero() || id.String() != "" {
		t.Fatalf("zero ID = %q", id)
	}
}

func TestIDJSON(t *testing.T) {
	type doc struct {
		Rack kernel.ID[object] `json:"rack"`
	}
	id, err := kernel.ParseID[object]("r-1")
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(doc{Rack: id})
	if err != nil || string(out) != `{"rack":"r-1"}` {
		t.Fatalf("Marshal = %s, %v", out, err)
	}
	var back doc
	if err := json.Unmarshal(out, &back); err != nil || back.Rack != id {
		t.Fatalf("Unmarshal = %+v, %v", back, err)
	}
	if err := json.Unmarshal([]byte(`{"rack":"a/b"}`), &back); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("Unmarshal of a bad ID = %v, want invalid_value", err)
	}
}

func TestSequentialIDs(t *testing.T) {
	ids := kernel.NewSequentialIDs()
	got := []string{ids.Next("obj"), ids.Next("obj"), ids.Next("lnk"), ids.Next("obj")}
	want := []string{"obj-000001", "obj-000002", "lnk-000001", "obj-000003"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("IDs = %v, want %v", got, want)
		}
	}
}

func TestSequentialIDsAreUniqueUnderConcurrency(t *testing.T) {
	ids := kernel.NewSequentialIDs()
	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := ids.Next("x")
			mu.Lock()
			defer mu.Unlock()
			if seen[id] {
				t.Errorf("duplicate ID %s", id)
			}
			seen[id] = true
		}()
	}
	wg.Wait()
	if len(seen) != 40 {
		t.Fatalf("%d distinct IDs, want 40", len(seen))
	}
}

func TestRandomIDsFollowTheRng(t *testing.T) {
	a := kernel.NewRandomIDs(kernel.NewSeededRng(0))
	b := kernel.NewRandomIDs(kernel.NewSeededRng(0))
	first := a.Next("job")
	if first != "job-e220a8397b1dcdaf" {
		t.Fatalf("first ID = %q", first)
	}
	if b.Next("job") != first || a.Next("job") == first {
		t.Fatal("same seed must repeat the sequence and the next ID must differ")
	}
}

func TestNewID(t *testing.T) {
	gen := kernel.NewSequentialIDs()
	id, err := kernel.NewID[object](gen, "obj")
	if err != nil || id.String() != "obj-000001" {
		t.Fatalf("NewID = %q, %v", id, err)
	}
	for _, prefix := range []string{"", "a/b", strings.Repeat("p", 120)} {
		if _, err := kernel.NewID[object](gen, prefix); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Fatalf("NewID with prefix %q = %v, want invalid_value", prefix, err)
		}
	}
	longest := strings.Repeat("p", 107)
	if id, err := kernel.NewID[object](kernel.NewRandomIDs(kernel.NewSeededRng(1)), longest); err != nil || len(id.String()) > 128 {
		t.Fatalf("longest prefix = %q, %v", id, err)
	}
}

var _ kernel.IDGenerator = (*kernel.SequentialIDs)(nil)
var _ kernel.IDGenerator = (*kernel.RandomIDs)(nil)
