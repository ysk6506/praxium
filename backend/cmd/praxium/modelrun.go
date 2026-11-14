package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/filejournal"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// readModelFile reads the file of organization properties, at most as large as the format allows.
func readModelFile(w wiring, path string) ([]byte, error) {
	in, err := readDefinition(w, path)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	data, err := io.ReadAll(io.LimitReader(in, model.MaxOrgModelFileBytes+1))
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read "+path)
	}
	return data, nil
}

// standardState is the model with nothing stored: what check compares with when it has no data folder.
func standardState(_ deps) (*modelState, error) {
	std, err := model.LoadStandard()
	if err != nil {
		return nil, err
	}
	base, err := model.StandardDefinitions()
	if err != nil {
		return nil, err
	}
	return &modelState{base: base, std: std}, nil
}

// checkReport is what "model check" says.
type checkReport struct {
	OK         bool            `json:"ok"`
	File       string          `json:"file"`
	StoredSet  int64           `json:"stored_set"`
	Overlays   int             `json:"overlays"`
	Properties int             `json:"properties"`
	Errors     []reportProblem `json:"errors"`
	Warnings   []string        `json:"warnings"`
	Changes    []planChange    `json:"changes"`
	Breaking   int             `json:"breaking"`
	Added      int             `json:"added"`
	Modified   int             `json:"changed"`
}

// reportProblem is one error of the model, with its code.
type reportProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newCheckReport(file string, st *modelState, p *modelPlan) checkReport {
	r := checkReport{File: file, Overlays: len(p.wanted), Changes: p.changes, Errors: []reportProblem{}, Warnings: []string{}}
	if st.hasHead {
		r.StoredSet = st.head.No
	}
	for _, o := range p.wanted {
		r.Properties += len(o.Properties)
	}
	for _, e := range p.errs {
		r.Errors = append(r.Errors, reportProblem{Code: string(e.Code), Message: errText(e)})
	}
	for _, wn := range p.warnings {
		r.Warnings = append(r.Warnings, wn.Message)
	}
	if r.Changes == nil {
		r.Changes = []planChange{}
	}
	r.Breaking = len(p.breaking())
	r.Added, r.Modified = p.count()
	r.OK = len(r.Errors) == 0 && r.Breaking == 0
	return r
}

// modelCheck is `praxium model check`.
func modelCheck(f modelFlags, out io.Writer, w wiring) error {
	data, err := readModelFile(w, f.file)
	if err != nil {
		return err
	}
	st, err := checkState(f.dataDir, w.deps)
	if err != nil {
		return err
	}
	p, err := planModel(st, data)
	if err != nil {
		return err
	}
	rep := newCheckReport(f.file, st, p)
	if f.asJSON {
		err = printJSON(out, rep)
	} else {
		printCheck(out, rep)
	}
	if err != nil {
		return err
	}
	if !rep.OK {
		return kernel.Errorf(kernel.CodeInvalidValue, "the file has %d errors and %d breaking changes; praxium model apply would refuse it", len(rep.Errors), rep.Breaking)
	}
	return nil
}

// checkState is what check compares the file with: the stored definitions of the folder when there is one, else the
// standard model alone.
func checkState(dir string, d deps) (*modelState, error) {
	if dir != "" {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return openModelState(dir, true, d)
		}
	}
	return standardState(d)
}

// applyReport is what "model apply" says.
type applyReport struct {
	Applied  bool   `json:"applied"`
	Set      int64  `json:"set"`
	Hash     string `json:"hash"`
	Added    int    `json:"added"`
	Modified int    `json:"changed"`
	Message  string `json:"message"`
}

// modelApply is `praxium model apply`: the checks of "model check", then one atomic write to the definitions.
func modelApply(f modelFlags, out io.Writer, w wiring) error {
	data, err := readModelFile(w, f.file)
	if err != nil {
		return err
	}
	disk, err := openDefinitionsDisk(f.dataDir, false)
	if err != nil {
		return err
	}
	unlock, err := disk.Lock()
	if err != nil {
		return applyLocked(f.dataDir, err)
	}
	defer func() { _ = unlock() }()
	st, err := openModelState(f.dataDir, false, w.deps)
	if err != nil {
		return err
	}
	p, err := planModel(st, data)
	if err != nil {
		return err
	}
	if rep := newCheckReport(f.file, st, p); !rep.OK {
		return refuseApply(out, rep, f.asJSON)
	}
	return writeApply(f, out, st, p, w)
}

// applyLocked says why a folder in use cannot be changed, and what to do.
func applyLocked(dir string, err error) error {
	if kernel.CodeOf(err) != filejournal.CodeLocked {
		return err
	}
	return kernel.Errorf(filejournal.CodeLocked, "the data folder %s is in use by another praxium (a server such as praxium local, demo or api serve, or an ingest run). "+
		"A server reads the definitions only when it starts, so they are not changed under it: stop it, run praxium model apply, and start it again", dir)
}

// refuseApply prints why the file is not stored and returns the error that ends the command.
func refuseApply(out io.Writer, rep checkReport, asJSON bool) error {
	if asJSON {
		if err := printJSON(out, rep); err != nil {
			return err
		}
	} else {
		printCheck(out, rep)
	}
	return kernel.Errorf(kernel.CodeInvalidValue, "nothing was stored: the file has %d errors and %d breaking changes", len(rep.Errors), rep.Breaking)
}

// writeApply stores the changed overlays, with the numbers they were given, in one write.
func writeApply(f modelFlags, out io.Writer, st *modelState, p *modelPlan, w wiring) error {
	if len(p.changed) == 0 {
		return printApplied(out, f.asJSON, applyReport{Applied: false, Set: st.head.No, Hash: st.head.Hash,
			Message: fmt.Sprintf("nothing changed: the stored definitions (D-%d) already say this", st.head.No)})
	}
	in := definitions.ApplyInput{ExpectHead: st.head.No, Origin: definitions.OriginChange, Baseline: st.std.Hash().String(),
		Actor: definitions.Actor{ID: applyActor(w)}, Message: f.message}
	for _, o := range p.changed {
		body, err := o.Body()
		if err != nil {
			return err
		}
		in.Changes = append(in.Changes, definitions.Change{ItemID: o.ItemID(), Kind: model.OverlayKind,
			Name: targetNameOf(st.base, o.Target), SchemaVersion: model.OverlaySchemaVersion, Body: body})
	}
	if p.issued > 0 {
		in.Counters = map[string]int64{attrCounter: int64(p.nextNo)}
	}
	res, err := st.svc.Apply(context.Background(), in)
	if err != nil {
		return applyFailed(err)
	}
	added, modified := p.count()
	return printApplied(out, f.asJSON, applyReport{Applied: res.Changed, Set: res.Set.No, Hash: res.Set.Hash, Added: added, Modified: modified,
		Message: fmt.Sprintf("stored D-%d (%s): %d properties added, %d changed", res.Set.No, short(res.Set.Hash), added, modified)})
}

// applyFailed names what to do about a head that moved under the apply.
func applyFailed(err error) error {
	if kernel.CodeOf(err) == kernel.CodeConflict {
		return kernel.Wrap(kernel.CodeConflict, err, "the definitions changed while the file was being checked; run praxium model check again, then apply")
	}
	return err
}

// applyActor is who is recorded as the author of a change: the person at the command line.
func applyActor(w wiring) string {
	for _, name := range []string{"USER", "USERNAME"} {
		if w.env.Lookup != nil {
			if v, ok := w.env.Lookup(name); ok && v != "" {
				return "cli:" + v
			}
		}
	}
	return "cli"
}

func short(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

func printApplied(out io.Writer, asJSON bool, rep applyReport) error {
	if asJSON {
		return printJSON(out, rep)
	}
	fmt.Fprintln(out, rep.Message)
	if rep.Applied {
		fmt.Fprintln(out, "A running praxium reads the definitions when it starts: start praxium local, demo or api serve again for the change to show.")
	}
	return nil
}
