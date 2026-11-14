package identitycheck

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// TB is the small part of *testing.T the suite uses.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
	Logf(format string, args ...any)
	Cleanup(func())
}

// Store is the port under test: the same methods as the Store of the identity unit, which this unit does not import.
type Store interface {
	Get(ctx context.Context, kind, id string) (data []byte, version int64, err error)
	Put(ctx context.Context, kind, id string, data []byte, expect int64) (version int64, err error)
	Scan(ctx context.Context, kind string, fn func(id string, data []byte, version int64) error) error
}

// Factory returns a new, empty store. The suite calls it once per case.
type Factory func(tb TB) Store

// Case is one case of the suite. A store's test runs each as a subtest:
//
//	for _, c := range identitycheck.Cases() {
//		t.Run(c.Name, func(t *testing.T) { c.Run(t, factory) })
//	}
type Case struct {
	Name string
	Run  func(tb TB, newStore Factory)
}

// Cases returns the whole suite in a fixed order.
func Cases() []Case {
	return []Case{
		{"a record put is read back with version 1", readBack},
		{"a missing record is not_found", missing},
		{"a record that must not exist cannot be made twice", createTwice},
		{"a change needs the version that was read", staleChange},
		{"a version that does not exist yet cannot be expected", futureVersion},
		{"versions go up by one at each write", versionsRise},
		{"what is returned is a copy", copies},
		{"scan lists one kind in the order of the IDs and no other", scanOrder},
		{"scan stops at the error of its function and that store is still usable inside it", scanStops},
		{"unusual IDs and unicode text are kept exactly", oddNames},
		{"text that cannot be stored is refused and leaves nothing", badText},
		{"of many writers of one record exactly one wins", racing},
	}
}

var ctx = context.Background()

func codeIs(err error, want kernel.Code) bool { return err != nil && kernel.CodeOf(err) == want }

func readBack(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	v, err := s.Put(ctx, "subject", "alice", []byte(`{"a":1}`), 0)
	if err != nil || v != 1 {
		tb.Fatalf("first put: version %d, error %v", v, err)
	}
	data, got, err := s.Get(ctx, "subject", "alice")
	if err != nil || got != 1 || string(data) != `{"a":1}` {
		tb.Fatalf("get: %q version %d, error %v", data, got, err)
	}
}

func missing(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	if _, _, err := s.Get(ctx, "subject", "nobody"); !codeIs(err, kernel.CodeNotFound) {
		tb.Fatalf("a missing record: %v", err)
	}
}

func createTwice(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	if _, err := s.Put(ctx, "token", "t1", []byte("first"), 0); err != nil {
		tb.Fatalf("first put: %v", err)
	}
	if _, err := s.Put(ctx, "token", "t1", []byte("second"), 0); !codeIs(err, kernel.CodeConflict) {
		tb.Fatalf("a second create: %v", err)
	}
	if data, _, _ := s.Get(ctx, "token", "t1"); string(data) != "first" {
		tb.Fatalf("the loser changed the record: %q", data)
	}
}

func staleChange(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	if _, err := s.Put(ctx, "token", "t1", []byte("one"), 0); err != nil {
		tb.Fatalf("put: %v", err)
	}
	if _, err := s.Put(ctx, "token", "t1", []byte("two"), 1); err != nil {
		tb.Fatalf("the change with the version read: %v", err)
	}
	if _, err := s.Put(ctx, "token", "t1", []byte("three"), 1); !codeIs(err, kernel.CodeConflict) {
		tb.Fatalf("the change with an old version: %v", err)
	}
	if data, v, _ := s.Get(ctx, "token", "t1"); string(data) != "two" || v != 2 {
		tb.Fatalf("after the refused change: %q at %d", data, v)
	}
}

func futureVersion(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	if _, err := s.Put(ctx, "token", "t1", []byte("x"), 5); !codeIs(err, kernel.CodeConflict) {
		tb.Fatalf("a put that expects a version of a record that is not there: %v", err)
	}
	if _, _, err := s.Get(ctx, "token", "t1"); !codeIs(err, kernel.CodeNotFound) {
		tb.Fatalf("the refused put made the record: %v", err)
	}
}

func versionsRise(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	var v int64
	for want := int64(1); want <= 4; want++ {
		got, err := s.Put(ctx, "meta", "m", []byte(fmt.Sprint(want)), v)
		if err != nil || got != want {
			tb.Fatalf("write %d gave version %d, error %v", want, got, err)
		}
		v = got
	}
}

func copies(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	in := []byte("abc")
	if _, err := s.Put(ctx, "k", "id", in, 0); err != nil {
		tb.Fatalf("put: %v", err)
	}
	in[0] = 'X'
	out, _, _ := s.Get(ctx, "k", "id")
	if string(out) != "abc" {
		tb.Fatalf("the store kept the caller's slice: %q", out)
	}
	out[0] = 'Y'
	again, _, _ := s.Get(ctx, "k", "id")
	if string(again) != "abc" {
		tb.Fatalf("a result shares memory with the store: %q", again)
	}
}

func scanOrder(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	for _, id := range []string{"b", "a", "c", "B"} {
		if _, err := s.Put(ctx, "subject", id, []byte("s-"+id), 0); err != nil {
			tb.Fatalf("put: %v", err)
		}
	}
	if _, err := s.Put(ctx, "token", "t", []byte("t"), 0); err != nil {
		tb.Fatalf("put: %v", err)
	}
	var ids []string
	err := s.Scan(ctx, "subject", func(id string, data []byte, v int64) error {
		if string(data) != "s-"+id || v != 1 {
			tb.Errorf("scan gave %q %q at %d", id, data, v)
		}
		ids = append(ids, id)
		return nil
	})
	if err != nil || fmt.Sprint(ids) != "[B a b c]" {
		tb.Fatalf("scan of the subjects: %v, error %v (want the order of the bytes)", ids, err)
	}
	none := 0
	if err := s.Scan(ctx, "nothing", func(string, []byte, int64) error { none++; return nil }); err != nil || none != 0 {
		tb.Fatalf("a kind with no records: %d visited, error %v", none, err)
	}
}

func scanStops(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := s.Put(ctx, "k", id, []byte(id), 0); err != nil {
			tb.Fatalf("put: %v", err)
		}
	}
	stop := kernel.Errorf(kernel.CodeInternal, "stop")
	seen := 0
	err := s.Scan(ctx, "k", func(id string, _ []byte, _ int64) error {
		seen++
		if _, _, gerr := s.Get(ctx, "k", id); gerr != nil {
			tb.Errorf("the store cannot be used inside a scan: %v", gerr)
		}
		return stop
	})
	if err != stop || seen != 1 {
		tb.Fatalf("scan: %d visited, error %v", seen, err)
	}
}

func oddNames(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	ids := []string{"it's", `a"b`, `back\slash`, "a b", "한글-이름", "x'; DROP TABLE identity_record; --", "%_"}
	text := []byte("{\"name\":\"여기 \\\"따옴표\\\" 'x' \\\\ é中\"}\n\ttab")
	for _, id := range ids {
		if _, err := s.Put(ctx, "subject", id, text, 0); err != nil {
			tb.Fatalf("put %q: %v", id, err)
		}
	}
	for _, id := range ids {
		data, _, err := s.Get(ctx, "subject", id)
		if err != nil || !bytes.Equal(data, text) {
			tb.Fatalf("get %q: %q, error %v", id, data, err)
		}
	}
	count := 0
	if err := s.Scan(ctx, "subject", func(string, []byte, int64) error { count++; return nil }); err != nil || count != len(ids) {
		tb.Fatalf("scan: %d records, error %v", count, err)
	}
}

func badText(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	for name, data := range map[string][]byte{"a byte that is not UTF-8": {0xff, 0xfe}, "a NUL": []byte("a\x00b")} {
		if _, err := s.Put(ctx, "k", "id", data, 0); err == nil {
			tb.Fatalf("%s was stored", name)
		}
		if _, _, err := s.Get(ctx, "k", "id"); !codeIs(err, kernel.CodeNotFound) {
			tb.Fatalf("%s left a record: %v", name, err)
		}
	}
}

func racing(tb TB, newStore Factory) {
	tb.Helper()
	s := newStore(tb)
	const writers = 8
	var wg sync.WaitGroup
	wins, conflicts := make(chan int, writers), make(chan int, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Put(ctx, "meta", "bootstrap", []byte(fmt.Sprint(i)), 0)
			switch {
			case err == nil:
				wins <- i
			case codeIs(err, kernel.CodeConflict):
				conflicts <- i
			default:
				tb.Errorf("writer %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if len(wins) != 1 || len(conflicts) != writers-1 {
		tb.Fatalf("%d writers won and %d lost of %d", len(wins), len(conflicts), writers)
	}
	data, v, _ := s.Get(ctx, "meta", "bootstrap")
	if v != 1 || string(data) != fmt.Sprint(<-wins) {
		tb.Fatalf("the record is %q at %d, not the winner's", data, v)
	}
}
