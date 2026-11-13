package devload

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-258]
func TestTheTextOfAScalarIsTheTextTheKeyReads(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{"abc", "abc"},
		{json.Number("12.5"), "12.5"},
		{true, "true"},
		{false, "false"},
		{[]any{"x"}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := asText(tt.in); got != tt.want {
			t.Errorf("asText(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// [REQ-258]
func TestALoadNeedsAKGItCanLeaseAndATruthItCanReadAndALoadAgainChangesNothing(t *testing.T) {
	g, _ := sharedWorld(t, 0)
	reg := standard(t)
	clock := kernel.NewManualClock(loadedAt)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(9))
	store := graph.NewScratchStore()
	ctx := context.Background()
	loader := Loader{Store: store, Reg: reg, Clock: clock, IDs: ids}
	missing, err := kernel.NewID[graph.KGKind](ids, "kg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(ctx, missing, bytes.NewReader(g.truth)); err == nil {
		t.Errorf("a KG that does not exist took a load")
	}
	kg, err := NewKG(ctx, store, reg, ids, clock, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(ctx, kg.ID, bytes.NewReader([]byte("not json\n"))); err == nil {
		t.Errorf("a truth that is no JSON Lines took a load")
	}
	first, err := loader.Load(ctx, kg.ID, bytes.NewReader(g.truth))
	if err != nil {
		t.Fatal(err)
	}
	// the same truth loaded again finds every object and link as it is
	again, err := loader.Load(ctx, kg.ID, bytes.NewReader(g.truth))
	if err != nil {
		t.Fatal(err)
	}
	if again.GraphHash != first.GraphHash {
		t.Errorf("a second load of the same truth changed the graph: %s -> %s", first.GraphHash, again.GraphHash)
	}
}

// failing refuses the writes of a store after the first n chunks, or the seal.
type failing struct {
	graph.Store
	chunks   int
	failSeal bool
}

func (f *failing) ApplyChunk(ctx context.Context, w graph.ChunkWrite) error {
	if f.chunks == 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the store refuses")
	}
	f.chunks--
	return f.Store.ApplyChunk(ctx, w)
}

func (f *failing) Seal(ctx context.Context, w graph.SealWrite) error {
	if f.failSeal {
		return kernel.Errorf(kernel.CodeInvalidValue, "the store refuses to seal")
	}
	return f.Store.Seal(ctx, w)
}

// [REQ-258]
func TestAStoreThatRefusesStopsTheLoadWithTheBatchItReached(t *testing.T) {
	g, _ := sharedWorld(t, 0)
	for _, tt := range []struct {
		name string
		f    *failing
	}{
		{"at the first batch", &failing{Store: graph.NewScratchStore(), chunks: 0}},
		{"at a later batch", &failing{Store: graph.NewScratchStore(), chunks: 1}},
		{"at the seal", &failing{Store: graph.NewScratchStore(), chunks: 1 << 20, failSeal: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reg := standard(t)
			clock := kernel.NewManualClock(loadedAt)
			ids := kernel.NewRandomIDs(kernel.NewSeededRng(3))
			ctx := context.Background()
			kg, err := NewKG(ctx, tt.f, reg, ids, clock, "dev")
			if err != nil {
				t.Fatal(err)
			}
			_, err = Loader{Store: tt.f, Reg: reg, Clock: clock, IDs: ids, BatchItems: 50}.Load(ctx, kg.ID, bytes.NewReader(g.truth))
			if err == nil {
				t.Fatal("the load went on over a store that refuses")
			}
		})
	}
}
