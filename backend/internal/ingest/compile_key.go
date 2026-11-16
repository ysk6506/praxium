package ingest

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// compileKeys finds, for every identity map of the feed, the key of the standard model it stands for.
func (p *Plan) compileKeys(fp *feedPlan, at string) {
	if len(fp.feed.Identity) == 0 {
		p.errs.add(ecMapIdentEmpty, at+ptr("identity"), Params{"feed": fp.feed.ID}, "feed %s has no identity: no row could be told from another", fp.feed.ID)
		return
	}
	var registry []model.IdentityKey
	var props map[string]model.Property
	if fp.typeOK {
		registry, props = p.reg.IdentityKeys(fp.typ.ID), propertiesByName(p.reg, fp.typ)
	}
	for j, im := range fp.feed.Identity {
		if kp, ok := p.keyFor(fp, im, at+ptr("identity", j), registry, props); ok {
			fp.keys = append(fp.keys, kp)
		}
	}
	sort.SliceStable(fp.keys, func(i, j int) bool { return fp.keys[i].priority < fp.keys[j].priority })
}

func (p *Plan) keyFor(fp *feedPlan, im IdentityMap, at string, registry []model.IdentityKey, props map[string]model.Property) (keyPlan, bool) {
	if (im.Scheme == "") == (len(im.Components) == 0) {
		p.errs.add(ecMapIdentForm, at, Params{"feed": fp.feed.ID}, "feed %s: an identity key names a scheme or lists components, one of the two", fp.feed.ID)
		return keyPlan{}, false
	}
	if im.Scheme != "" {
		return p.schemeKey(fp, im, at, registry)
	}
	return p.componentKey(fp, im, at, registry, props)
}

// componentKey checks a key made of properties. The properties are looked up and the columns are checked one by one;
// whether the type has a key of exactly these properties is asked only when every one of them was found.
func (p *Plan) componentKey(fp *feedPlan, im IdentityMap, at string, registry []model.IdentityKey, props map[string]model.Property) (keyPlan, bool) {
	names := make([]string, len(im.Components))
	parts := make([]keyPart, len(im.Components))
	found, columnsOK := fp.typeOK, true
	for i, c := range im.Components {
		cat := at + ptr("components", i)
		pr, ok := p.componentProperty(fp, c, cat, props)
		found = found && ok
		if fp.colsOK && !slices.Contains(fp.cols, c.Column) {
			columnsOK = false
			p.errs.add(ecMapIdentCompColumn, cat+ptr("column"), Params{"feed": fp.feed.ID, "property": c.Property, "column": c.Column, "columns": fp.cols},
				"feed %s: identity component %s reads column %q, which the dataset lacks; its columns are: %s", fp.feed.ID, c.Property, c.Column, columnList(fp.cols))
		}
		names[i] = c.Property
		parts[i] = keyPart{kind: graph.PartProperty, ref: pr.ID.String(), vt: pr.ValueType, column: c.Column}
	}
	if !found {
		return keyPlan{}, false
	}
	for _, k := range registry {
		if k.Scheme.IsZero() && p.sameProperties(k, names) {
			return keyPlan{name: keyName(im, fmt.Sprint(names)), priority: im.Priority, parts: parts}, columnsOK
		}
	}
	p.errs.add(ecMapIdentNoKey, at+ptr("components"), Params{"feed": fp.feed.ID, "type": fp.typ.Name, "properties": names},
		"feed %s: type %s has no identity key made of the properties %v, in that order. %s", fp.feed.ID, fp.typ.Name, names, p.keysHint(fp))
	return keyPlan{}, false
}

// componentProperty finds the property a component names and refuses one that is no part of the standard model.
func (p *Plan) componentProperty(fp *feedPlan, c KeyComponent, at string, props map[string]model.Property) (model.Property, bool) {
	if !fp.typeOK {
		return model.Property{}, false
	}
	pr, ok := props[c.Property]
	if !ok {
		names := p.reg.PropertyNames(fp.typ.ID)
		p.errs.add(ecMapIdentProperty, at+ptr("property"), Params{"feed": fp.feed.ID, "property": c.Property, "type": fp.typ.Name, "properties": nonNil(names)},
			"feed %s: identity component names property %q, which type %s lacks; its properties are: %s", fp.feed.ID, c.Property, fp.typ.Name, strings.Join(names, ", "))
		return pr, false
	}
	if pr.Namespace == model.NamespaceOrg || pr.Namespace == model.NamespaceExt {
		p.errs.add(ecMapIdentOrg, at+ptr("property"), Params{"feed": fp.feed.ID, "property": pr.Name},
			"feed %s: identity component %s is an organization property; an identity key is made of the standard properties of the type (which keys the type has: see its identity keys in praxium types show), because changing what identifies the objects that are stored would lose their history", fp.feed.ID, pr.Name)
		return pr, false
	}
	return pr, true
}

func (p *Plan) schemeKey(fp *feedPlan, im IdentityMap, at string, registry []model.IdentityKey) (keyPlan, bool) {
	var sch model.IdentifierScheme
	schemeOK := false
	if fp.typeOK {
		sch, schemeOK = p.reg.IdentifierSchemeByName(im.Scheme)
		if !schemeOK {
			p.errs.add(ecMapIdentScheme, at+ptr("scheme"), Params{"feed": fp.feed.ID, "scheme": im.Scheme, "schemes": p.reg.IdentifierSchemeNames()},
				"feed %s: %q is no identifier scheme of the model. %s", fp.feed.ID, im.Scheme, p.schemesHint(fp))
		}
	}
	columnOK := true
	if fp.colsOK && !slices.Contains(fp.cols, im.Column) {
		columnOK = false
		p.errs.add(ecMapIdentColumn, at+ptr("column"), Params{"feed": fp.feed.ID, "scheme": im.Scheme, "column": im.Column, "columns": fp.cols},
			"feed %s: identity %s reads column %q, which the dataset lacks; its columns are: %s", fp.feed.ID, im.Scheme, im.Column, columnList(fp.cols))
	}
	if !schemeOK {
		return keyPlan{}, false
	}
	for _, k := range registry {
		if k.Scheme == sch.ID && len(k.Components) == 0 {
			return keyPlan{name: keyName(im, im.Scheme), priority: im.Priority, scheme: sch.ID,
				parts: []keyPart{{kind: graph.PartScheme, ref: sch.ID.String(), vt: sch.Normalizer, column: im.Column}}}, columnOK
		}
	}
	p.errs.add(ecMapIdentNotKey, at+ptr("scheme"), Params{"feed": fp.feed.ID, "scheme": im.Scheme, "type": fp.typ.Name},
		"feed %s: scheme %s is not an identity key of type %s. %s", fp.feed.ID, im.Scheme, fp.typ.Name, p.keysHint(fp))
	return keyPlan{}, false
}

func keyName(im IdentityMap, fallback string) string {
	if im.KeyName != "" {
		return im.KeyName
	}
	return fallback
}

// sameProperties tells whether the key is made of exactly these properties, in this order.
func (p *Plan) sameProperties(k model.IdentityKey, names []string) bool {
	if len(k.Components) != len(names) {
		return false
	}
	for i, c := range k.Components {
		if c.Property.IsZero() {
			return false
		}
		ref, ok := p.reg.Property(c.Property)
		if !ok || ref.Property.Name != names[i] {
			return false
		}
	}
	return true
}

func (p *Plan) compileIdentifiers(fp *feedPlan, at string) {
	for j, im := range fp.feed.Identifiers {
		iat := at + ptr("identifiers", j)
		sch, ok := model.IdentifierScheme{}, false
		if fp.typeOK {
			sch, ok = p.reg.IdentifierSchemeByName(im.Scheme)
			if !ok {
				p.errs.add(ecMapIdfScheme, iat+ptr("scheme"), Params{"feed": fp.feed.ID, "scheme": im.Scheme, "schemes": p.reg.IdentifierSchemeNames()},
					"feed %s: identifier names %q, which is no identifier scheme of the model. %s", fp.feed.ID, im.Scheme, p.schemesHint(fp))
			}
		}
		colOK := true
		if fp.colsOK && !slices.Contains(fp.cols, im.Column) {
			colOK = false
			p.errs.add(ecMapIdfColumn, iat+ptr("column"), Params{"feed": fp.feed.ID, "scheme": im.Scheme, "column": im.Column, "columns": fp.cols},
				"feed %s: identifier %s reads column %q, which the dataset lacks; its columns are: %s", fp.feed.ID, im.Scheme, im.Column, columnList(fp.cols))
		}
		if ok && colOK {
			fp.idents = append(fp.idents, identPlan{scheme: sch.ID, vt: sch.Normalizer, column: im.Column})
		}
	}
}
