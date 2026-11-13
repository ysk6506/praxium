package worldgen

import (
	"bytes"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

func loadReg(t testing.TB) *model.Registry {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// smallWorld builds a world of about the given scale.
func smallWorld(t testing.TB, seed uint64, scale int64, tweak func(*Params)) *World {
	t.Helper()
	p := DefaultParams(seed, scale)
	if tweak != nil {
		tweak(&p)
	}
	w, err := Build(loadReg(t), p)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// runAll runs a world into memory with every output.
func runAll(t testing.TB, w *World) (*Manifest, *MemStore) {
	t.Helper()
	st := NewMemStore()
	m, err := w.Run(st, RunOptions{Truth: true, Views: true, Changes: w.P.Changes.Events > 0, Shape: true})
	if err != nil {
		t.Fatal(err)
	}
	return m, st
}

func truthOf(t testing.TB, w *World) ([]byte, TruthInfo) {
	t.Helper()
	var buf bytes.Buffer
	tw := NewTruthWriter(&buf)
	if err := w.Generate(tw); err != nil {
		t.Fatal(err)
	}
	info, err := tw.Close()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), info
}
