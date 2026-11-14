package identitycheck

import (
	"bytes"
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// idcFlawed is a small store with one flaw at a time. Without a flaw it keeps the rules of the port, so each case of
// the suite that is meant to notice a flaw is shown to fail on it and to pass on the store without it.
type idcFlawed struct {
	flaw     string
	mu       sync.Mutex
	rows     map[string]idcRow
	scanning bool
}

type idcRow struct {
	data    []byte
	version int64
	order   int
}

func idcNewFlawed(flaw string) *idcFlawed { return &idcFlawed{flaw: flaw, rows: map[string]idcRow{}} }

func (s *idcFlawed) Get(_ context.Context, kind, id string) ([]byte, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flaw == "get fails" || (s.flaw == "get fails inside a scan" && s.scanning) {
		return nil, 0, kernel.Errorf(kernel.CodeInternal, "get")
	}
	r, ok := s.rows[kind+"\x00"+id]
	if !ok {
		if s.flaw == "missing is another error" {
			return nil, 0, kernel.Errorf(kernel.CodeInternal, "no")
		}
		return nil, 0, kernel.Errorf(kernel.CodeNotFound, "no")
	}
	if s.flaw == "result shares memory" {
		return r.data, r.version, nil
	}
	return append([]byte(nil), r.data...), r.version, nil
}

func (s *idcFlawed) Put(_ context.Context, kind, id string, data []byte, expect int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := kind + "\x00" + id
	r, ok := s.rows[key]
	if s.flaw == "put fails" || (s.flaw == "odd ids are refused" && strings.ContainsAny(id, "'\"")) {
		return 0, kernel.Errorf(kernel.CodeInternal, "put")
	}
	if s.flaw == "conflict still writes" {
		defer func() { s.rows[key] = idcRow{data: append([]byte(nil), data...), version: r.version + 1} }()
	}
	if s.flaw == "the right version is refused" && ok && expect == r.version {
		return 0, kernel.Errorf(kernel.CodeConflict, "version")
	}
	if s.flaw != "bad text is stored" && (!utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0) {
		return 0, kernel.Errorf(kernel.CodeInvalidValue, "text")
	}
	switch {
	case !ok && expect != 0 && s.flaw != "a future version is accepted":
		return 0, kernel.Errorf(kernel.CodeConflict, "not there")
	case ok && expect != r.version && s.flaw != "a stale change is accepted" && !(s.flaw == "a second create wins" && expect == 0):
		return 0, kernel.Errorf(kernel.CodeConflict, "version")
	}
	step := int64(1)
	if s.flaw == "versions skip" {
		step = 2
	}
	stored := data
	if s.flaw != "input is kept" {
		stored = append([]byte(nil), data...)
	}
	if s.flaw == "text is changed" {
		stored = []byte(strings.ReplaceAll(string(stored), "'", "`"))
	}
	s.rows[key] = idcRow{data: stored, version: r.version + step, order: len(s.rows)}
	return r.version + step, nil
}

func (s *idcFlawed) Scan(_ context.Context, kind string, fn func(string, []byte, int64) error) error {
	s.mu.Lock()
	type item struct {
		id  string
		row idcRow
	}
	var items []item
	for key, r := range s.rows {
		k, id, _ := strings.Cut(key, "\x00")
		if (k == kind || s.flaw == "scan lists other kinds") && !(s.flaw == "scan skips spaces" && strings.Contains(id, " ")) {
			items = append(items, item{id: id, row: r})
		}
	}
	if s.flaw == "scan invents a record" && len(items) == 0 {
		items = append(items, item{id: "ghost"})
	}
	s.scanning = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.scanning = false; s.mu.Unlock() }()
	if s.flaw == "scan is in the order of writing" {
		sort.Slice(items, func(i, j int) bool { return items[i].row.order > items[j].row.order })
	} else {
		sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	}
	for _, it := range items {
		if err := fn(it.id, append([]byte(nil), it.row.data...), it.row.version); err != nil && s.flaw != "scan swallows the error" {
			return err
		}
	}
	return nil
}

// [REQ-310]
func TestEachCaseOfTheSuiteFailsOnTheFlawItIsMadeFor(t *testing.T) {
	flaws := []struct{ flaw, caseName string }{
		{"missing is another error", "a missing record is not_found"},
		{"a second create wins", "a record that must not exist cannot be made twice"},
		{"a stale change is accepted", "a change needs the version that was read"},
		{"a future version is accepted", "a version that does not exist yet cannot be expected"},
		{"versions skip", "versions go up by one at each write"},
		{"input is kept", "what is returned is a copy"},
		{"result shares memory", "what is returned is a copy"},
		{"scan is in the order of writing", "scan lists one kind in the order of the IDs and no other"},
		{"scan lists other kinds", "scan lists one kind in the order of the IDs and no other"},
		{"scan swallows the error", "scan stops at the error of its function and that store is still usable inside it"},
		{"text is changed", "unusual IDs and unicode text are kept exactly"},
		{"bad text is stored", "text that cannot be stored is refused and leaves nothing"},
		{"put fails", "a record put is read back with version 1"},
		{"get fails", "a record put is read back with version 1"},
		{"put fails", "a record that must not exist cannot be made twice"},
		{"put fails", "a change needs the version that was read"},
		{"the right version is refused", "a change needs the version that was read"},
		{"conflict still writes", "a record that must not exist cannot be made twice"},
		{"conflict still writes", "a change needs the version that was read"},
		{"conflict still writes", "a version that does not exist yet cannot be expected"},
		{"put fails", "scan lists one kind in the order of the IDs and no other"},
		{"scan invents a record", "scan lists one kind in the order of the IDs and no other"},
		{"put fails", "scan stops at the error of its function and that store is still usable inside it"},
		{"get fails inside a scan", "scan stops at the error of its function and that store is still usable inside it"},
		{"odd ids are refused", "unusual IDs and unicode text are kept exactly"},
		{"scan skips spaces", "unusual IDs and unicode text are kept exactly"},
		{"put fails", "of many writers of one record exactly one wins"},
		{"a second create wins", "of many writers of one record exactly one wins"},
		{"put fails", "what is returned is a copy"},
		{"put fails", "versions go up by one at each write"},
	}
	byName := map[string]Case{}
	for _, c := range Cases() {
		byName[c.Name] = c
	}
	for _, f := range flaws {
		c, ok := byName[f.caseName]
		if !ok {
			t.Fatalf("no case named %q", f.caseName)
		}
		if !idcFails(c, func(TB) Store { return idcNewFlawed(f.flaw) }) {
			t.Errorf("the case %q does not fail on the flaw %q", f.caseName, f.flaw)
		}
	}
	for _, c := range Cases() {
		if idcFails(c, func(TB) Store { return idcNewFlawed("") }) {
			t.Errorf("the case %q fails on a store without a flaw", c.Name)
		}
	}
}
