package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// readSet is the definition set a read command looks at: the head, or the set -set names, with its overlays. A folder
// that has no definitions has no set (hasSet false) and no overlays.
type readSet struct {
	st      *modelState
	set     definitions.Set
	hasSet  bool
	overlay []model.Overlay
}

func openReadSet(f modelFlags, w wiring) (readSet, error) {
	st, err := openModelState(f.dataDir, true, w.deps)
	if err != nil {
		return readSet{}, err
	}
	rs := readSet{st: st, set: st.head, hasSet: st.hasHead, overlay: st.stored}
	if f.set == 0 {
		return rs, nil
	}
	set, err := st.svc.Set(context.Background(), f.set)
	if err != nil {
		return readSet{}, kernel.Wrap(kernel.CodeNotFound, err, fmt.Sprintf("no definition set D-%d (praxium model history lists them)", f.set))
	}
	rs.set, rs.hasSet = set, true
	rs.overlay, err = st.overlaysOf(context.Background(), set.No)
	return rs, err
}

// showReport is what "model show --json" says: the set, and the properties in the form of the file.
type showReport struct {
	Set       int64           `json:"set"`
	Hash      string          `json:"hash"`
	CreatedAt string          `json:"created_at"`
	Message   string          `json:"message"`
	Model     json.RawMessage `json:"model"`
}

// modelShow is `praxium model show`.
func modelShow(f modelFlags, out io.Writer, w wiring) error {
	rs, err := openReadSet(f, w)
	if err != nil {
		return err
	}
	file, err := model.FormatOrgModelFile(rs.st.base, rs.overlay)
	if err != nil {
		return err
	}
	if f.asJSON {
		rep := showReport{Model: file}
		if rs.hasSet {
			rep.Set, rep.Hash, rep.Message, rep.CreatedAt = rs.set.No, rs.set.Hash, rs.set.Message, rs.set.CreatedAt.UTC().Format(time.RFC3339)
		}
		return printJSON(out, rep)
	}
	printShow(out, rs)
	return nil
}

// modelExport is `praxium model export`: the file, and nothing else, on the output.
func modelExport(f modelFlags, out io.Writer, w wiring) error {
	rs, err := openReadSet(f, w)
	if err != nil {
		return err
	}
	file, err := model.FormatOrgModelFile(rs.st.base, rs.overlay)
	if err != nil {
		return err
	}
	_, err = out.Write(file)
	return err
}

// historyRow is one definition set of "model history".
type historyRow struct {
	Set       int64  `json:"set"`
	Hash      string `json:"hash"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	Message   string `json:"message"`
	Added     int    `json:"added"`
	Changed   int    `json:"changed"`
}

// modelHistory is `praxium model history`.
func modelHistory(f modelFlags, out io.Writer, w wiring) error {
	st, err := openModelState(f.dataDir, true, w.deps)
	if err != nil {
		return err
	}
	limit := f.limit
	if limit < 1 {
		limit = 20
	}
	sets, err := st.svc.History(context.Background(), 0, limit)
	if err != nil {
		return err
	}
	rows := make([]historyRow, 0, len(sets))
	for _, s := range sets {
		row, err := st.historyRow(s)
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	if f.asJSON {
		return printJSON(out, rows)
	}
	printHistory(out, rows)
	return nil
}

// historyRow describes a set and what it changed against the set before it.
func (st *modelState) historyRow(s definitions.Set) (historyRow, error) {
	ctx := context.Background()
	cur, err := st.overlaysOf(ctx, s.No)
	if err != nil {
		return historyRow{}, err
	}
	var before []model.Overlay
	if s.ParentNo > 0 {
		if before, err = st.overlaysOf(ctx, s.ParentNo); err != nil {
			return historyRow{}, err
		}
	}
	added, changed := diffOverlays(before, cur)
	return historyRow{Set: s.No, Hash: s.Hash, CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339), CreatedBy: s.CreatedBy,
		Message: s.Message, Added: added, Changed: changed}, nil
}

// diffOverlays counts the properties added and the properties changed from one list of overlays to the next.
func diffOverlays(before, after []model.Overlay) (added, changed int) {
	prev := map[string]model.Overlay{}
	for _, o := range before {
		prev[o.ItemID()] = o
	}
	for _, o := range after {
		touched := map[string]bool{}
		for _, c := range model.ClassifyOverlay(prev[o.ItemID()], o) {
			if c.Kind == model.ChangeAdded {
				added++
			} else {
				touched[c.Property.String()] = true
			}
		}
		changed += len(touched)
	}
	return added, changed
}
