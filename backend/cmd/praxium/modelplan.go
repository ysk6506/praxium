package main

import (
	"bytes"
	"context"
	"slices"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// attrCounter is the counter of the ledger of the definitions that hands out the numbers of organization properties.
// Its value is the next number to give; it only goes up, and it goes up in the same write as the revision that
// uses the numbers, so a number is never given twice.
const attrCounter = "next_attr_no"

// modelPlan is what a file of organization properties would do to what is stored: the properties as they would be
// stored (with ids and numbers), what changes against the stored ones, and every error and warning found. Nothing is
// written to make it; check shows it and apply writes it.
type modelPlan struct {
	// wanted are the overlays of the file with ids and numbers filled in; final adds the stored overlays the file left
	// out, which is the model the stored definitions plus the file would make.
	base          model.Definitions
	wanted, final []model.Overlay
	// changed are the overlays whose stored content differs from the wanted one (all of the new ones).
	changed  []model.Overlay
	changes  []planChange
	errs     []*kernel.Error
	warnings []model.OverlayWarning
	// nextNo is the value of the counter after the numbers this plan gives; issued is how many it gives.
	nextNo, issued int
}

// planChange is one change to one property of one overlay, in the words the output uses.
type planChange struct {
	Overlay  string           `json:"overlay"`
	Target   string           `json:"target"`
	Property string           `json:"property"`
	Name     string           `json:"name"`
	Kind     model.ChangeKind `json:"kind"`
	Field    string           `json:"field"`
	Detail   string           `json:"detail"`
}

// planModel works out what the file would do. A file that cannot be read at all is the only error returned; what is
// wrong with the contents is in the plan, all of it at once.
func planModel(st *modelState, data []byte) (*modelPlan, error) {
	wanted, err := model.ParseOrgModelFile(data, st.base)
	if err != nil {
		return nil, err
	}
	next, err := st.nextNumber()
	if err != nil {
		return nil, err
	}
	p := &modelPlan{base: st.base, nextNo: next}
	storedBy := map[string]model.Overlay{}
	for _, o := range st.stored {
		storedBy[o.ItemID()] = o
	}
	wanted = withoutEmptyNew(wanted, storedBy)
	keepOldNames(wanted, storedBy)
	p.wanted = model.FillOverlays(st.base, st.stored, wanted, func() int {
		n := p.nextNo
		p.nextNo++
		p.issued++
		return n
	})
	p.final = append([]model.Overlay(nil), p.wanted...)
	p.classify(st, storedBy)
	_, p.errs = model.ComposeRegistry(st.base, p.final)
	p.warnings = model.OverlayWarnings(st.base, p.final)
	return p, nil
}

// keepOldNames makes a rename what it should be: a property of the file that has the id of a stored one and another name
// keeps the stored name as an alias, so the old name goes on working in mappings and in questions. The file does not
// have to say so.
func keepOldNames(wanted []model.Overlay, storedBy map[string]model.Overlay) {
	for _, o := range wanted {
		prev, had := storedBy[o.ItemID()]
		if !had {
			continue
		}
		for i := range o.Properties {
			p := &o.Properties[i]
			for _, old := range prev.Properties {
				if old.ID == p.ID && !p.ID.IsZero() && old.Name != p.Name && !slices.Contains(p.Aliases, old.Name) {
					p.Aliases = append(p.Aliases, old.Name)
				}
			}
		}
	}
}

// withoutEmptyNew drops an overlay that has no property and is not stored: it would add nothing.
func withoutEmptyNew(wanted []model.Overlay, storedBy map[string]model.Overlay) []model.Overlay {
	var out []model.Overlay
	for _, o := range wanted {
		if _, ok := storedBy[o.ItemID()]; ok || len(o.Properties) > 0 {
			out = append(out, o)
		}
	}
	return out
}

// classify compares every wanted overlay with the stored one and every stored overlay the file leaves out with an
// empty one: the file says the whole state, so a property it does not mention is a property gone.
func (p *modelPlan) classify(st *modelState, storedBy map[string]model.Overlay) {
	seen := map[string]bool{}
	for _, o := range p.wanted {
		seen[o.ItemID()] = true
		prev, had := storedBy[o.ItemID()]
		p.note(o, prev, model.ClassifyOverlay(prev, o))
		if !had || !sameBody(prev, o) {
			p.changed = append(p.changed, o)
		}
	}
	for _, prev := range st.stored {
		if seen[prev.ItemID()] {
			continue
		}
		gone := prev.Clone()
		gone.Properties = nil
		p.note(prev, prev, model.ClassifyOverlay(prev, gone))
		p.final = append(p.final, prev)
	}
	sort.SliceStable(p.final, func(i, j int) bool { return p.final[i].ItemID() < p.final[j].ItemID() })
}

// note records the changes of an overlay, naming each property by its new name, or its old one when it is gone.
func (p *modelPlan) note(o, prev model.Overlay, changes []model.Change) {
	target := targetNameOf(p.base, o.Target)
	for _, c := range changes {
		name := c.Property.String()
		for _, pr := range append(append([]model.Property(nil), prev.Properties...), o.Properties...) {
			if pr.ID == c.Property {
				name = pr.Name
			}
		}
		p.changes = append(p.changes, planChange{Overlay: o.ItemID(), Target: target, Property: c.Property.String(), Name: name,
			Kind: c.Kind, Field: c.Field, Detail: c.Detail})
	}
}

// sameBody says whether two overlays are stored as the same bytes.
func sameBody(a, b model.Overlay) bool {
	x, errA := a.Body()
	y, errB := b.Body()
	return errA == nil && errB == nil && bytes.Equal(x, y)
}

// breaking returns the changes that apply refuses.
func (p *modelPlan) breaking() []planChange {
	var out []planChange
	for _, c := range p.changes {
		if c.Kind == model.ChangeBreaking {
			out = append(out, c)
		}
	}
	return out
}

// count returns how many properties are added and how many have a compatible change.
func (p *modelPlan) count() (added, modified int) {
	touched := map[string]bool{}
	for _, c := range p.changes {
		switch c.Kind {
		case model.ChangeAdded:
			added++
		case model.ChangeCompatible:
			touched[c.Overlay+"/"+c.Property] = true
		}
	}
	return added, len(touched)
}

// nextNumber is the number the next new property gets: the counter of the ledger, and never below the number after
// the highest one the stored model already uses, in case the ledger lags behind.
func (st *modelState) nextNumber() (int, error) {
	next := int64(model.OrgNumberBase)
	if st.svc != nil {
		var err error
		if next, err = st.svc.NextNumber(context.Background(), attrCounter, model.OrgNumberBase); err != nil {
			return 0, err
		}
	}
	if reg, errs := st.registry(); len(errs) == 0 && reg.NextOrgAttrNo() > int(next) {
		next = int64(reg.NextOrgAttrNo())
	}
	return int(next), nil
}

// targetNameOf is the name of the type or trait an overlay adds to, or its id when the model has no such target.
func targetNameOf(base model.Definitions, t model.OverlayTarget) string {
	switch t.Kind {
	case model.TargetObjectType:
		for _, ot := range base.ObjectTypes {
			if ot.ID.String() == t.ID {
				return ot.Name
			}
		}
	case model.TargetTrait:
		for _, tr := range base.Traits {
			if tr.ID.String() == t.ID {
				return tr.Name
			}
		}
	}
	return t.ID
}
