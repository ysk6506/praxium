package httpapi

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/contract"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

var (
	opsEq        = []string{"equals"}
	opsEqIn      = []string{"equals", "in"}
	opsText      = []string{"equals", "prefix"}
	opsNumber    = []string{"equals", "in", "range"}
	typeSortable = []string{"type_no", "name", "layer"}
)

// registryLists holds every list of the model and catalogue routes, built once:
// the registry and the catalogues do not change while the server runs.
type registryLists struct {
	generation  string
	objectTypes *listDef
	linkTypes   *listDef
	actionTypes *listDef
	traits      *listDef
	schemes     *listDef
	valueTypes  *listDef
	layers      *listDef
	roles       *listDef
	meanings    *listDef
	channels    *listDef
	errorCodes  *listDef
	reasonCodes *listDef
	catalogs    *listDef
	// coverage maps a type id to the ids of the types an end naming it covers (itself and its subtypes).
	typeIDs []string
}

func buildRegistryLists(reg *model.Registry) *registryLists {
	l := &registryLists{generation: reg.Hash().String()[:16]}
	for _, t := range reg.ObjectTypes() {
		l.typeIDs = append(l.typeIDs, t.ID.String())
	}
	l.objectTypes = objectTypeList(reg)
	l.linkTypes = linkTypeList(reg, l)
	l.actionTypes = actionTypeList(reg, l)
	l.traits = traitList(reg)
	l.schemes = schemeList(reg)
	l.valueTypes = valueTypeList(reg)
	l.layers = layerList(reg)
	l.roles = roleList()
	l.meanings = meaningList()
	l.channels = channelList()
	l.errorCodes = errorCodeList()
	l.reasonCodes = reasonCodeList()
	l.catalogs = catalogList()
	return l
}

// finish fixes the closed lists of a definition from its rows.
func finish(d *listDef) *listDef {
	if len(d.rows) > 0 {
		for name := range d.rows[0].out {
			d.outFields = append(d.outFields, name)
		}
		sort.Strings(d.outFields)
	}
	return d
}

// objectTypeSummary is the shape of an item of GET /v1/object-types.
func objectTypeSummary(t model.ObjectType) map[string]any {
	return map[string]any{
		"id": t.ID.String(), "type_no": t.TypeNo, "name": t.Name, "aliases": strs(t.Aliases),
		"namespace": string(t.Namespace), "layer": string(t.Layer), "tier": string(t.EffectiveTier()),
		"parent": opt(t.Parent.String()), "traits": traitIDs(t.Traits), "status": string(t.Status),
		"superseded_by": opt(t.SupersededBy.String()), "description": t.Description,
		"standard_ref": t.StandardRef, "display_names": displayNames(t.DisplayNames),
	}
}

func traitIDs(in []model.TraitID) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		out = append(out, id.String())
	}
	return out
}

func objectTypeList(reg *model.Registry) *listDef {
	d := &listDef{
		filters: map[string][]string{
			"layer": opsEqIn, "namespace": opsEqIn, "status": opsEqIn, "tier": opsEqIn, "name": opsText,
			"trait": opsEqIn, "parent": opsEqIn, "subtype_of": opsEq, "type_no": opsNumber,
		},
		sorts: typeSortable, defSort: []string{"type_no"},
	}
	traits := reg.Traits()
	for _, t := range reg.ObjectTypes() {
		var carried []string
		for _, tr := range traits {
			if reg.HasTrait(t.ID, tr.ID) {
				carried = append(carried, tr.ID.String())
			}
		}
		subtypeOf := []string{t.ID.String()}
		for _, a := range reg.Ancestors(t.ID) {
			subtypeOf = append(subtypeOf, a.String())
		}
		d.rows = append(d.rows, row{
			id: t.ID.String(), out: objectTypeSummary(t),
			vals: map[string]any{
				"layer": string(t.Layer), "namespace": string(t.Namespace), "status": string(t.Status),
				"tier": string(t.EffectiveTier()), "name": t.Name, "trait": carried, "parent": t.Parent.String(),
				"subtype_of": subtypeOf, "type_no": t.TypeNo,
			},
		})
	}
	return finish(d)
}

// covered lists the ids of the object types an end of a binding or an action covers:
// a type and its subtypes, the types that carry a trait, or every type.
func covered(reg *model.Registry, all []string, e model.Endpoint) []string {
	switch e.Kind {
	case model.EndpointAny:
		return all
	case model.EndpointType:
		out := []string{e.Ref}
		id, err := kernel.ParseID[model.ObjectTypeKind](e.Ref)
		if err != nil {
			return out
		}
		for _, sub := range reg.Subtypes(id) {
			out = append(out, sub.ID.String())
		}
		return out
	case model.EndpointTrait:
		id, err := kernel.ParseID[model.TraitKind](e.Ref)
		if err != nil {
			return nil
		}
		var out []string
		for _, t := range reg.TypesWithTrait(id) {
			out = append(out, t.ID.String())
		}
		return out
	}
	return nil
}

func linkTypeSummary(l model.LinkType) map[string]any {
	params := map[string]string{}
	for k, v := range l.MeaningParams {
		params[k] = v
	}
	return map[string]any{
		"id": l.ID.String(), "link_type_no": l.LinkTypeNo, "name": l.Name, "aliases": strs(l.Aliases),
		"namespace": string(l.Namespace), "meaning": string(l.Meaning), "meaning_params": params,
		"delay_class": string(l.EffectiveDelay()), "status": string(l.Status),
		"superseded_by": opt(l.SupersededBy.String()), "description": l.Description, "binding_count": len(l.Bindings),
	}
}

func linkTypeList(reg *model.Registry, l *registryLists) *listDef {
	d := &listDef{
		filters: map[string][]string{
			"meaning": opsEqIn, "namespace": opsEqIn, "status": opsEqIn, "delay_class": opsEqIn, "name": opsText,
			"from_type": opsEq, "to_type": opsEq, "link_type_no": opsNumber,
		},
		sorts: []string{"link_type_no", "name"}, defSort: []string{"link_type_no"},
	}
	for _, lt := range reg.LinkTypes() {
		from, to := map[string]bool{}, map[string]bool{}
		for _, b := range lt.Bindings {
			for _, id := range covered(reg, l.typeIDs, b.From) {
				from[id] = true
			}
			for _, id := range covered(reg, l.typeIDs, b.To) {
				to[id] = true
			}
		}
		d.rows = append(d.rows, row{
			id: lt.ID.String(), out: linkTypeSummary(lt),
			vals: map[string]any{
				"meaning": string(lt.Meaning), "namespace": string(lt.Namespace), "status": string(lt.Status),
				"delay_class": string(lt.EffectiveDelay()), "name": lt.Name, "link_type_no": lt.LinkTypeNo,
				"from_type": sortedKeys(from), "to_type": sortedKeys(to),
			},
		})
	}
	return finish(d)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func actionTypeView(a model.ActionType) map[string]any {
	return map[string]any{
		"id": a.ID.String(), "name": a.Name, "aliases": strs(a.Aliases), "namespace": string(a.Namespace),
		"applies_to": toAny(endpointsOf(a.AppliesTo)), "mutates": a.Mutates, "reversibility": opt(string(a.Reversibility)),
		"disruption": string(a.Disruption), "physical": a.Physical, "declared_effects": strs(a.DeclaredEffects),
		"inverse": opt(a.Inverse.String()), "description": a.Description, "status": string(a.Status),
		"superseded_by": opt(a.SupersededBy.String()),
	}
}

// toAny passes a view through JSON so it nests inside a map like any other value.
func toAny(v any) any {
	m := toMap(map[string]any{"v": v})
	return m["v"]
}

func actionTypeList(reg *model.Registry, l *registryLists) *listDef {
	d := &listDef{
		filters: map[string][]string{
			"namespace": opsEqIn, "status": opsEqIn, "disruption": opsEqIn, "name": opsText, "applies_to": opsEq,
		},
		sorts: []string{"name"}, defSort: []string{"name"},
	}
	for _, a := range reg.ActionTypes() {
		cover := map[string]bool{}
		for _, e := range a.AppliesTo {
			for _, id := range covered(reg, l.typeIDs, e) {
				cover[id] = true
			}
		}
		d.rows = append(d.rows, row{
			id: a.ID.String(), out: actionTypeView(a),
			vals: map[string]any{
				"namespace": string(a.Namespace), "status": string(a.Status), "disruption": string(a.Disruption),
				"name": a.Name, "applies_to": sortedKeys(cover),
			},
		})
	}
	return finish(d)
}

func traitView(t model.Trait) map[string]any {
	return map[string]any{
		"id": t.ID.String(), "name": t.Name, "aliases": strs(t.Aliases), "description": t.Description,
		"properties": toAny(propertiesOf(t.Properties)), "needs": toAny(needsOf(t.Needs)),
	}
}

func traitList(reg *model.Registry) *listDef {
	d := &listDef{filters: map[string][]string{"name": opsText}, sorts: []string{"name"}, defSort: []string{"name"}}
	for _, t := range reg.Traits() {
		d.rows = append(d.rows, row{id: t.ID.String(), out: traitView(t), vals: map[string]any{"name": t.Name}})
	}
	return finish(d)
}

func schemeView(s model.IdentifierScheme) map[string]any {
	return map[string]any{
		"id": s.ID.String(), "name": s.Name, "aliases": strs(s.Aliases), "namespace": string(s.Namespace),
		"normalizer": s.Normalizer.String(), "description": s.Description,
	}
}

func schemeList(reg *model.Registry) *listDef {
	d := &listDef{
		filters: map[string][]string{"namespace": opsEqIn, "name": opsText},
		sorts:   []string{"name"}, defSort: []string{"name"},
	}
	for _, s := range reg.IdentifierSchemes() {
		d.rows = append(d.rows, row{
			id: s.ID.String(), out: schemeView(s),
			vals: map[string]any{"namespace": string(s.Namespace), "name": s.Name},
		})
	}
	return finish(d)
}

func valueTypeList(reg *model.Registry) *listDef {
	d := &listDef{
		filters: map[string][]string{"base": opsEqIn, "name": opsText, "builtin": opsEq},
		sorts:   []string{"name"},
	}
	for _, v := range reg.ValueTypes() {
		builtin := v.ID == model.BaseValueTypeID(v.Base)
		text := "false"
		if builtin {
			text = "true"
		}
		d.rows = append(d.rows, row{
			id: v.ID.String(),
			out: map[string]any{
				"id": v.ID.String(), "name": v.Name, "base": string(v.Base), "description": v.Description, "builtin": builtin,
			},
			vals: map[string]any{"base": string(v.Base), "name": v.Name, "builtin": text},
		})
	}
	return finish(d)
}

func layerList(reg *model.Registry) *listDef {
	d := &listDef{}
	for _, layer := range model.Layers() {
		d.rows = append(d.rows, row{
			id: string(layer), vals: map[string]any{},
			out: map[string]any{"layer": string(layer), "type_count": len(reg.TypesInLayer(layer))},
		})
	}
	return finish(d)
}

func roleList() *listDef {
	d := &listDef{}
	for _, r := range access.Roles() {
		abilities := []string{}
		for _, a := range access.AbilitiesOf(r) {
			abilities = append(abilities, string(a))
		}
		d.rows = append(d.rows, row{
			id: string(r), vals: map[string]any{},
			out: map[string]any{"role": string(r), "includes_read": r.IncludesRead(), "abilities": abilities},
		})
	}
	return finish(d)
}

func meaningList() *listDef {
	d := &listDef{}
	for _, spec := range model.MeaningSpecs() {
		params := []map[string]any{}
		for _, p := range spec.Params {
			params = append(params, map[string]any{"name": p.Name, "kind": string(p.Kind), "values": strs(p.Values)})
		}
		d.rows = append(d.rows, row{
			id: string(spec.Meaning), vals: map[string]any{},
			out: map[string]any{
				"meaning": string(spec.Meaning), "from_channel": opt(string(spec.From)), "to_channel": opt(string(spec.To)),
				"from_param": opt(spec.FromParam), "spreads": spec.Spreads(), "params": toAny(params),
			},
		})
	}
	return finish(d)
}

func channelList() *listDef {
	d := &listDef{}
	states := []string{string(model.StateUp), string(model.StateDown), string(model.StateUnknown)}
	for _, ch := range model.Channels() {
		d.rows = append(d.rows, row{
			id: string(ch), vals: map[string]any{}, out: map[string]any{"channel": string(ch), "states": states},
		})
	}
	return finish(d)
}

func errorCodeList() *listDef {
	d := &listDef{filters: map[string][]string{"group": opsEqIn, "status": opsNumber, "code": {"prefix"}}}
	for _, p := range contract.ProblemCodes() {
		d.rows = append(d.rows, row{
			id:   p.Code,
			vals: map[string]any{"group": p.Group, "status": p.Status, "code": p.Code},
			out:  map[string]any{"code": p.Code, "group": p.Group, "status": p.Status, "meaning": p.Meaning},
		})
	}
	return finish(d)
}

func reasonCodeList() *listDef {
	d := &listDef{}
	for _, r := range contract.ReasonCodes() {
		d.rows = append(d.rows, row{
			id: r.Code, vals: map[string]any{}, out: map[string]any{"code": r.Code, "meaning": r.Meaning},
		})
	}
	return finish(d)
}

// catalogEntries are the catalogues the server serves; the planned ones are in the contract's registry of planned routes.
var catalogEntries = []struct{ name, href, about string }{
	{"link-meanings", "/v1/catalog/link-meanings", "The eleven meanings of a link and how each spreads state."},
	{"channels", "/v1/catalog/channels", "The six channels of state an object has."},
	{"value-types", "/v1/value-types", "The value types a property can have."},
	{"roles", "/v1/roles", "The eight roles and what each may do."},
	{"error-codes", "/v1/catalog/error-codes", "The closed catalogue of error codes."},
	{"reason-codes", "/v1/catalog/reason-codes", "The reasons an action in allowed_actions is refused."},
}

func catalogList() *listDef {
	d := &listDef{}
	for _, e := range catalogEntries {
		d.rows = append(d.rows, row{
			id: e.name, vals: map[string]any{},
			out: map[string]any{"name": e.name, "href": e.href, "description": e.about},
		})
	}
	return finish(d)
}
