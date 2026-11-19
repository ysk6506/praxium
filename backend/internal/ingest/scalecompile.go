package ingest

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The compile of the new ingestion flow (ADR-0086) reads what the current one refuses: an identity key that begins with
// a context component, the key of an object that is unique only inside its parent. Everything else of a mapping is
// checked by the same functions as before; this file adds the context component and the codes of its errors.

// MaxContextDepth is how many parents may nest in a context key: the parent's key may itself begin with a context
// component, and so on up to this many levels (4, a starting value).
const MaxContextDepth = 4

// ctxPlan is a context component after the check: the link type that leads from the object to its parent, the
// parent's object type and the identity key the child's row gives for the parent (which may have a context of its own).
type ctxPlan struct {
	link   model.LinkType
	parent model.ObjectType
	key    keyPlan
}

var scaleCodeTable []*errCode

// regScale adds a code of the new flow to its own table. The codes of the current flow are in codeTable and a test of
// that table runs the current compile only, so the codes the new compile adds are kept apart.
func regScale(code string, k kernel.Code, meaning, params string) *errCode {
	ec := &errCode{code: code, kernel: k, doc: "mapping", meaning: meaning}
	if params != "" {
		ec.params = strings.Split(params, ",")
	}
	scaleCodeTable = append(scaleCodeTable, ec)
	return ec
}

// ScaleErrorCodes is the table of the codes only the compile of the new flow gives, sorted by code.
func ScaleErrorCodes() []ErrorCode {
	out := make([]ErrorCode, 0, len(scaleCodeTable))
	for _, ec := range scaleCodeTable {
		out = append(out, ErrorCode{Code: ec.code, Kernel: ec.kernel, Document: ec.doc, Meaning: ec.meaning, Params: append([]string{}, ec.params...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

var (
	ecScaleCtxShape      = regScale("mapping.identity.context_component_invalid", kernel.CodeInvalidValue, "A context component is not the first and only context of its key, has no parent, or also names a property or a column.", "feed,reason")
	ecScaleCtxLink       = regScale("mapping.identity.context_unknown_link_type", kernel.CodeInvalidValue, "The link type of a context component is not one of the model.", "feed,link_type,suggestions")
	ecScaleCtxParentType = regScale("mapping.identity.context_unknown_parent_type", kernel.CodeInvalidValue, "The parent object type of a context component is not one of the model.", "feed,object_type,suggestions")
	ecScaleCtxBinding    = regScale("mapping.identity.context_link_not_allowed", kernel.CodeInvalidValue, "The link type of a context component does not allow a link from the object type to its parent type.", "feed,link_type,from_type,to_type")
	ecScaleCtxDepth      = regScale("mapping.identity.context_too_deep", kernel.CodeInvalidValue, "Parents nest deeper than a mapping may write.", "feed,limit")
	ecScaleSetKey        = regScale("mapping.identity.set_key_unsupported", kernel.CodeUnsupported, "The identity keys of the object type name a set of objects, which a mapping cannot write in this build.", "feed,type")
	ecScaleLinkDup       = regScale("mapping.link.duplicates_context", kernel.CodeInvalidValue, "A link definition makes the link that the context component of the same feed already makes.", "link,feed,link_type")
)

// CompileScale is Compile for the new flow: it also reads identity keys that begin with a context component (REQ-639).
// A definition the current Compile accepts gives the same plan here; the current Compile still refuses a context key.
func CompileScale(reg *model.Registry, src Source, m Mapping) (*Plan, error) {
	return compile(reg, src, m, true)
}

func hasContextComponent(im IdentityMap) bool {
	return slices.ContainsFunc(im.Components, func(c KeyComponent) bool { return c.Context != "" || c.Parent != nil })
}

// onlySetKeys tells whether every key the model gives a type names a set of objects, so that no mapping could identify it.
func (p *Plan) onlySetKeys(registry []model.IdentityKey) bool {
	if len(registry) == 0 {
		return false
	}
	return !slices.ContainsFunc(registry, func(k model.IdentityKey) bool {
		return !slices.ContainsFunc(k.Components, func(c model.KeyComponent) bool { return c.Set })
	})
}

// contextKey checks a key whose first component is a context: the context itself, then the properties after it.
func (p *Plan) contextKey(fp *feedPlan, im IdentityMap, at string, registry []model.IdentityKey, props map[string]model.Property) (keyPlan, bool) {
	if !p.checkContextShape(fp, im, at) {
		return keyPlan{}, false
	}
	ctx, ctxOK := p.compileContext(fp, im.Components[0], at+ptr("components", 0))
	parts, names, partsOK := p.contextRest(fp, im.Components[1:], at, props)
	if !ctxOK || !partsOK || !fp.typeOK {
		return keyPlan{}, false
	}
	for ord, k := range registry {
		if k.Scheme.IsZero() && p.sameContextKey(k, ctx.link.ID, names) {
			return keyPlan{name: keyName(im, fmt.Sprint(append([]string{"context " + ctx.link.Name}, names...))), priority: im.Priority, parts: parts, modelOrd: ord, ctx: ctx}, true
		}
	}
	if p.onlySetKeys(registry) {
		p.errs.add(ecScaleSetKey, at+ptr("components"), Params{"feed": fp.feed.ID, "type": fp.typ.Name},
			"feed %s: the identity keys of type %s name a set of objects, which a mapping cannot write in this build", fp.feed.ID, fp.typ.Name)
		return keyPlan{}, false
	}
	p.errs.add(ecMapIdentNoKey, at+ptr("components"), Params{"feed": fp.feed.ID, "type": fp.typ.Name, "properties": names},
		"feed %s: type %s has no identity key made of the context %s and the properties %v, in that order. The identity keys of %s are: %s",
		fp.feed.ID, fp.typ.Name, ctx.link.Name, names, fp.typ.Name, strings.Join(p.reg.DescribeIdentityKeys(fp.typ.ID), "; "))
	return keyPlan{}, false
}

// checkContextShape checks the form of the components of a key that has a context: the context is the first component
// and the only one, it has a parent, and it names no property or column of its own.
func (p *Plan) checkContextShape(fp *feedPlan, im IdentityMap, at string) bool {
	bad := func(i int, reason string) bool {
		p.errs.add(ecScaleCtxShape, at+ptr("components", i), Params{"feed": fp.feed.ID, "reason": reason},
			"feed %s: %s", fp.feed.ID, reason)
		return false
	}
	if i := slices.IndexFunc(im.Components, func(c KeyComponent) bool { return c.Context != "" || c.Parent != nil }); i > 0 {
		return bad(i, "a context component of a key is the first component of the key")
	}
	c0 := im.Components[0]
	switch {
	case c0.Context == "" || c0.Parent == nil:
		return bad(0, "a context component of a key is the first component and has a context (the link type) and a parent (its object type and identity)")
	case c0.Property != "" || c0.Column != "":
		return bad(0, "a context component names a link type and a parent, and no property or column")
	}
	for i, c := range im.Components[1:] {
		if c.Context != "" || c.Parent != nil {
			return bad(i+1, "a key has at most one context component, and it is the first")
		}
	}
	return true
}

// compileContext checks a context component: the link type, the parent type, that the model lets the type link to the
// parent through that link type, and the identity key of the parent.
func (p *Plan) compileContext(fp *feedPlan, c KeyComponent, at string) (*ctxPlan, bool) {
	ok := true
	lt, ltOK := p.reg.LinkTypeByName(c.Context)
	if !ltOK {
		ok = false
		names := linkTypeNames(p.reg)
		p.errs.add(ecScaleCtxLink, at+ptr("context"), Params{"feed": fp.feed.ID, "link_type": c.Context, "suggestions": nonNil(Similar(c.Context, names))},
			"feed %s: %q is no link type of the model%s", fp.feed.ID, c.Context, p.didYouMean(c.Context, names, "praxium types list --kind link --all"))
	}
	pt, ptOK := p.reg.ObjectTypeByName(c.Parent.ObjectType)
	if !ptOK {
		ok = false
		names := objectTypeNames(p.reg)
		p.errs.add(ecScaleCtxParentType, at+ptr("parent", "object_type"), Params{"feed": fp.feed.ID, "object_type": c.Parent.ObjectType, "suggestions": nonNil(Similar(c.Parent.ObjectType, names))},
			"feed %s: %q is no object type of the model%s", fp.feed.ID, c.Parent.ObjectType, p.didYouMean(c.Parent.ObjectType, names, "praxium types list --all"))
	}
	if ltOK && ptOK && fp.typeOK {
		if _, allowed := p.reg.BindingFor(fp.typ.ID, pt.ID, lt.ID); !allowed {
			ok = false
			p.errs.add(ecScaleCtxBinding, at+ptr("context"), Params{"feed": fp.feed.ID, "link_type": lt.Name, "from_type": fp.typ.Name, "to_type": pt.Name},
				"feed %s: %s does not allow a link from %s to its parent %s", fp.feed.ID, lt.Name, fp.typ.Name, pt.Name)
		}
	}
	if p.depth >= MaxContextDepth {
		p.errs.add(ecScaleCtxDepth, at+ptr("parent"), Params{"feed": fp.feed.ID, "limit": MaxContextDepth},
			"feed %s: parents nest deeper than %d levels", fp.feed.ID, MaxContextDepth)
		return nil, false
	}
	pk, pkOK := p.parentKey(fp, pt, ptOK, c.Parent.Identity, at+ptr("parent", "identity"))
	if !ok || !pkOK {
		return nil, false
	}
	return &ctxPlan{link: lt, parent: pt, key: pk}, true
}

// parentKey checks the identity key of a parent as the child's row gives it, by the same functions as the key of a
// feed: the parent is a type of its own whose columns are those of the dataset of the feed.
func (p *Plan) parentKey(fp *feedPlan, pt model.ObjectType, ptOK bool, im IdentityMap, at string) (keyPlan, bool) {
	if !ptOK {
		return keyPlan{}, false
	}
	parent := &feedPlan{feed: fp.feed, typ: pt, typeOK: true, cols: fp.cols, colsOK: fp.colsOK}
	p.depth++
	defer func() { p.depth-- }()
	return p.keyFor(parent, im, at, p.reg.IdentityKeys(pt.ID), propertiesByName(p.reg, pt))
}

// contextRest checks the components after the context: properties of the type read from columns. It returns their
// parts and names; whether the type has such a key is asked by the caller.
func (p *Plan) contextRest(fp *feedPlan, comps []KeyComponent, at string, props map[string]model.Property) ([]keyPart, []string, bool) {
	names := make([]string, len(comps))
	parts := make([]keyPart, len(comps))
	ok := fp.typeOK
	for i, c := range comps {
		cat := at + ptr("components", i+1)
		pr, found := p.componentProperty(fp, c, cat, props)
		ok = ok && found
		if fp.colsOK && !slices.Contains(fp.cols, c.Column) {
			ok = false
			p.errs.add(ecMapIdentCompColumn, cat+ptr("column"), Params{"feed": fp.feed.ID, "property": c.Property, "column": c.Column, "columns": fp.cols},
				"feed %s: identity component %s reads column %q, which the dataset lacks; its columns are: %s", fp.feed.ID, c.Property, c.Column, columnList(fp.cols))
		}
		names[i] = c.Property
		parts[i] = keyPart{kind: graph.PartProperty, ref: pr.ID.String(), vt: pr.ValueType, column: c.Column}
	}
	return parts, names, ok
}

// sameContextKey tells whether the model's key is a context through this link type and then exactly these properties.
func (p *Plan) sameContextKey(k model.IdentityKey, link model.LinkTypeID, names []string) bool {
	if len(k.Components) != len(names)+1 || k.Components[0].Context != link || k.Components[0].Set {
		return false
	}
	for i, c := range k.Components[1:] {
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

// checkContextLinks refuses a link definition that makes the link a context component already makes: the declaration of
// the context makes the link from the object to its parent, so that no object is identified inside one parent and
// linked to another (ADR-0086).
func (p *Plan) checkContextLinks() {
	for i, lp := range p.links {
		if lp.from == nil || lp.lt.ID.IsZero() {
			continue
		}
		for _, k := range lp.from.keys {
			if k.ctx != nil && k.ctx.link.ID == lp.lt.ID {
				p.errs.add(ecScaleLinkDup, ptr("links", i, "link_type"), Params{"link": lp.def.ID, "feed": lp.from.feed.ID, "link_type": lp.lt.Name},
					"link definition %s: feed %s already makes the %s link from its context component, so it is not written as a link definition", lp.def.ID, lp.from.feed.ID, lp.lt.Name)
				break
			}
		}
	}
}
