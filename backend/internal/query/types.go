package query

import (
	"encoding/json"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// typeInfo is what the queries need about one object type, computed once.
type typeInfo struct {
	typ       model.ObjectType
	byAttr    map[int]model.Property
	byName    map[string]model.Property
	ancestors map[model.ObjectTypeID]bool
	traits    map[model.TraitID]bool
}

// typeIndex indexes the registry by row number.
type typeIndex struct {
	byNo   map[int]*typeInfo
	byName map[string]*typeInfo
	all    []*typeInfo
}

func newTypeIndex(reg *model.Registry) *typeIndex {
	ix := &typeIndex{byNo: map[int]*typeInfo{}, byName: map[string]*typeInfo{}}
	traits := reg.Traits()
	for _, t := range reg.ObjectTypes() {
		info := &typeInfo{typ: t, byAttr: map[int]model.Property{}, byName: map[string]model.Property{},
			ancestors: map[model.ObjectTypeID]bool{t.ID: true}, traits: map[model.TraitID]bool{}}
		for _, p := range reg.EffectiveProperties(t.ID) {
			info.byAttr[p.AttrNo] = p
			info.byName[p.Name] = p
			for _, a := range p.Aliases {
				info.byName[a] = p
			}
		}
		for _, a := range reg.Ancestors(t.ID) {
			info.ancestors[a] = true
		}
		for _, tr := range traits {
			if reg.HasTrait(t.ID, tr.ID) {
				info.traits[tr.ID] = true
			}
		}
		ix.byNo[t.TypeNo] = info
		ix.byName[t.Name] = info
		for _, a := range t.Aliases {
			ix.byName[a] = info
		}
		ix.all = append(ix.all, info)
	}
	return ix
}

// valueText is the text form a filter compares with: strings as they are,
// whole numbers and booleans in decimal and true/false, anything else as JSON.
func valueText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}
