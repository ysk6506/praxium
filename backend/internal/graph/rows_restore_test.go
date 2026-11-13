package graph

import (
	"reflect"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func TestRowValidityAndCloning(t *testing.T) {
	o := ObjectRow{SeqFrom: 2, SeqTo: 5}
	l := LinkRow{SeqFrom: 2}
	if o.Open() || !l.Open() {
		t.Fatalf("Open: object %v link %v", o.Open(), l.Open())
	}
	for _, c := range []struct {
		seq  int64
		o, l bool
	}{{1, false, false}, {2, true, true}, {4, true, true}, {5, false, true}} {
		if o.ValidAt(c.seq) != c.o || l.ValidAt(c.seq) != c.l {
			t.Fatalf("ValidAt(%d): object %v link %v", c.seq, o.ValidAt(c.seq), l.ValidAt(c.seq))
		}
	}
	in := map[string]any{"a": []any{"x", map[string]any{"b": "y"}}}
	out := cloneValue(in).(map[string]any)
	out["a"].([]any)[0] = "changed"
	if in["a"].([]any)[0] != "x" || !reflect.DeepEqual(cloneValue("s"), "s") {
		t.Fatalf("clone shares storage with the original: %v", in)
	}
}

func TestReleaseAndRestoreRefusals(t *testing.T) {
	u := newUnit(t, nil, Options{})
	u.apply(u.fx.World(1, 1, 1))
	_, err := u.a.Restore(u.ctx, u.kg.ID, 99)
	wantCode(t, "restore past readable_through", err, CodeNotReadable)
	_, err = u.a.Restore(u.ctx, u.kg.ID, -1)
	wantCode(t, "restore before the start", err, CodeNotReadable)
	_, err = u.a.Restore(u.ctx, u.kg.ID, 1)
	wantCode(t, "restore on a store that cannot", err, kernel.CodeUnsupported)
	if err := u.a.Release(u.ctx, u.lease); err != nil {
		t.Fatal(err)
	}
}
