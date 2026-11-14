package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/definitionsfile"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// modelState is what a data folder says about the model: the standard model, and the organization properties that
// the head of the stored definitions adds to it (ADR-0053).
type modelState struct {
	dir     string
	base    model.Definitions
	std     *model.Registry
	svc     *definitions.Service
	store   *definitionsfile.Store
	head    definitions.Set
	hasHead bool
	// stored are the overlays of the head, in the order of their item IDs.
	stored []model.Overlay
}

// openModelState reads the definitions of the folder. A reader holds no lock; a writer (readOnly false) must hold the
// lock of the folder, which the caller takes.
func openModelState(dir string, readOnly bool, d deps) (*modelState, error) {
	std, err := model.LoadStandard()
	if err != nil {
		return nil, err
	}
	base, err := model.StandardDefinitions()
	if err != nil {
		return nil, err
	}
	disk, err := openDefinitionsDisk(dir, readOnly)
	if err != nil {
		return nil, err
	}
	store, err := definitionsfile.Open(disk)
	if err != nil {
		return nil, err
	}
	svc, err := definitions.New(definitions.Config{Store: store, Clock: d.clock, IDs: d.ids})
	if err != nil {
		return nil, err
	}
	st := &modelState{dir: dir, base: base, std: std, svc: svc, store: store}
	head, ok, err := svc.Head(context.Background())
	if err != nil {
		return nil, err
	}
	if ok {
		st.head, st.hasHead = head, true
		if st.stored, err = st.overlaysOf(context.Background(), head.No); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// overlaysOf reads the overlays of a definition set. Items of another kind belong to other definitions that share the
// store and are not looked at.
func (st *modelState) overlaysOf(ctx context.Context, setNo int64) ([]model.Overlay, error) {
	items, err := st.svc.SetItems(ctx, setNo)
	if err != nil {
		return nil, err
	}
	var out []model.Overlay
	for _, it := range items {
		if it.Kind != model.OverlayKind {
			continue
		}
		o, err := model.ParseOverlay(it.Body)
		if err != nil {
			return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "the item "+it.ItemID+" of "+definitionsfile.FileName)
		}
		if o.ItemID() != it.ItemID {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the item %s of %s holds the overlay of %s", it.ItemID, definitionsfile.FileName, o.Target.ID)
		}
		out = append(out, o)
	}
	return out, nil
}

// registry is the standard model with the stored organization properties added.
func (st *modelState) registry() (*model.Registry, []*kernel.Error) {
	if len(st.stored) == 0 {
		return st.std, nil
	}
	return model.ComposeRegistry(st.base, st.stored)
}

// loadModel is the model a command starts with (ADR-0053). A command with no data folder, and a folder with no
// definitions, start with the standard model alone, exactly as before there were organization properties. Stored
// definitions that cannot be put together with the standard model refuse the start, with the reason: going on
// would read rows with a model that does not know their values.
func loadModel(dataDir string, d deps) (*model.Registry, error) {
	if dataDir == "" || !definitionsExist(dataDir) {
		return model.LoadStandard()
	}
	st, err := openModelState(dataDir, true, d)
	if err != nil {
		return nil, startRefused(dataDir, err)
	}
	reg, errs := st.registry()
	if len(errs) > 0 {
		return nil, startRefused(dataDir, joinErrors(errs))
	}
	return reg, nil
}

// definitionsExist says whether the folder holds a definitions file.
func definitionsExist(dir string) bool {
	_, err := os.Stat(fileDisk{dir: dir}.path(definitionsfile.FileName))
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

// startRefused is the answer of a start that cannot use the stored definitions, with the way out.
func startRefused(dir string, cause error) error {
	return kernel.Errorf(kernel.CodeInvalidValue, "cannot start: the organization properties stored in %s do not fit the model (%s). "+
		"Look at them with praxium model show -data-dir %s and apply a corrected file with praxium model apply; "+
		"or move %s away to start with the standard model alone", dir, errText(cause), dir, definitionsfile.FileName)
}

// joinErrors makes one error of the problems the model found, at most five of them in the text.
func joinErrors(errs []*kernel.Error) error {
	const most = 5
	var lines []string
	for i, e := range errs {
		if i == most {
			lines = append(lines, "and more")
			break
		}
		lines = append(lines, errText(e))
	}
	return kernel.Errorf(kernel.CodeInvalidValue, "%d problems: %s", len(errs), strings.Join(lines, "; "))
}
