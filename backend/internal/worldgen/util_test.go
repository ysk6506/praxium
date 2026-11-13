package worldgen

import (
	"io"
	"strings"
	"testing"
)

func TestCSVQuoting(t *testing.T) {
	got := string(appendCSV(nil, "a", "b,c", `d"e`, "f\ng", ""))
	if got != "a,\"b,c\",\"d\"\"e\",\"f\ng\",\n" {
		t.Fatalf("%q", got)
	}
}

func TestJSONStringEscapes(t *testing.T) {
	got := string(appendJSONString(nil, "a\"b\\c\nd\te\x01é"))
	if got != `"a\"b\\c\nd\te\u0001é"` {
		t.Fatalf("%s", got)
	}
}

func TestNumbersAndIDs(t *testing.T) {
	if fmtTenths(-25) != "-2.5" || fmtTenths(7) != "0.7" {
		t.Fatal("tenths")
	}
	if siteOf("rack.12.3/srv1") != 12 || siteOf("plain") != -1 || siteOf("x.") != -1 {
		t.Fatal("siteOf")
	}
	if parentID("a/b/c") != "a/b" || parentID("a") != "" || deviceOfIface("sw.1/if@x/y.0") != "sw.1" || deviceOfIface("x") != "" {
		t.Fatal("parents")
	}
	if ceilDiv(7, 2) != 4 || ceilDiv(0, 3) != 0 {
		t.Fatal("ceilDiv")
	}
	total := 0
	for i := 0; i < 4; i++ {
		total += splitEven(10, 4, i)
		if splitStart(10, 4, i) != total-splitEven(10, 4, i) {
			t.Fatal("splitStart")
		}
	}
	if total != 10 || splitEven(10, 0, 0) != 0 {
		t.Fatal("splitEven")
	}
	d := newDraw(1)
	for i := 0; i < 100; i++ {
		if v := d.between(3, 5); v < 3 || v > 5 {
			t.Fatal("between")
		}
	}
	if d.between(4, 4) != 4 || d.chance(0) || !d.chance(2) {
		t.Fatal("draw")
	}
	if l := linkID(lRunsOn, "a", "b"); len(l) != 25 || l == linkID(lRunsOn, "b", "a") || l == linkID(lCalls, "a", "b") {
		t.Fatalf("link id %q", l)
	}
	if pad("abcdef", 4) != "abc " || padLeft("ab", 4) != "  ab" || padLeft("abcde", 3) != "abcde" || pad("a", 3) != "a  " {
		t.Fatal("padding")
	}
	if pad4(7) != "0007" || bucketOf0() != 0 {
		t.Fatal("pad4")
	}
}

func bucketOf0() int { lo, _ := bucketOf(-3); return lo }

func TestBuckets(t *testing.T) {
	for v, want := range map[int][2]int{0: {0, 0}, 1: {1, 1}, 3: {2, 3}, 4: {4, 7}, 100: {64, 127}} {
		if lo, hi := bucketOf(v); lo != want[0] || hi != want[1] {
			t.Fatalf("%d -> %d..%d", v, lo, hi)
		}
	}
	d := distCounts{}
	if got := d.dist(); got.Count != 0 || len(got.Buckets) != 0 {
		t.Fatal("empty dist")
	}
	for _, v := range []int{1, 1, 2, 3, 10} {
		d.add(v)
	}
	got := d.dist()
	if got.Count != 5 || got.Max != 10 || got.P50 != 2 || got.Mean != 3.4 || len(got.Buckets) != 3 || got.Buckets[1].N != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestMemStoreAndFileSet(t *testing.T) {
	st := NewMemStore()
	fs := newFileSet(st)
	o := fs.open("a/b.txt", "src", "text")
	o.write([]byte("one\ntwo\n"))
	if fs.open("a/b.txt", "src", "text") != o {
		t.Fatal("open must return the same file")
	}
	fs.open("c.txt", "src", "text").write([]byte("x\n"))
	files, err := fs.closeAll()
	if err != nil || len(files) != 2 || files[0].Name != "a/b.txt" || files[0].Lines != 2 || files[0].Bytes != 8 || len(files[0].SHA256) != 64 {
		t.Fatalf("%+v %v", files, err)
	}
	if st.Text("a/b.txt") != "one\ntwo\n" || st.Text("none") != "" || strings.Join(st.Names(), ",") != "a/b.txt,c.txt" {
		t.Fatal("memstore")
	}
	w, err := discardStore{}.Create("x")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := w.Write([]byte("abc")); n != 3 || w.Close() != nil {
		t.Fatal("discard")
	}
}

type failStore struct{}

func (failStore) Create(string) (io.WriteCloser, error) { return nil, io.ErrClosedPipe }

func TestStoreFailuresAreReported(t *testing.T) {
	w := smallWorld(t, 1, 2000, nil)
	for _, opt := range []RunOptions{{Truth: true}, {Views: true}, {Changes: true}, {Shape: true}, {}} {
		if _, err := w.Run(failStore{}, opt); err == nil {
			t.Errorf("%+v: a failing store went unnoticed", opt)
		}
	}
}
