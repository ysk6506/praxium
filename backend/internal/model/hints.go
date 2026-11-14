package model

import (
	"sort"
	"strings"
)

// IdentifierSchemeNames returns the names of every identifier scheme, in alphabetical order. A message that refuses
// a scheme name lists them.
func (r *Registry) IdentifierSchemeNames() []string {
	out := make([]string, 0, len(r.defs.Schemes))
	for _, s := range r.defs.Schemes {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// SchemeKeyNames returns the names of the identifier schemes that are an identity key of a type, in the priority of
// its keys.
func (r *Registry) SchemeKeyNames(id ObjectTypeID) []string {
	var out []string
	for _, k := range r.IdentityKeys(id) {
		if k.Scheme.IsZero() || len(k.Components) > 0 {
			continue
		}
		if s, ok := r.IdentifierScheme(k.Scheme); ok {
			out = append(out, s.Name)
		}
	}
	return out
}

// DescribeIdentityKeys writes the identity keys of a type in priority order, one line each, in the words a person
// reads: "property asset_tag (weak)", "link located_in + property name (strong)", "scheme bmc_mac (weak)".
func (r *Registry) DescribeIdentityKeys(id ObjectTypeID) []string {
	var out []string
	for _, k := range r.IdentityKeys(id) {
		note := string(k.Strength)
		for _, c := range k.Components {
			if c.Property.IsZero() {
				note += "; not usable in file ingestion"
				break
			}
		}
		out = append(out, r.describeKey(k)+" ("+note+")")
	}
	return out
}

func (r *Registry) describeKey(k IdentityKey) string {
	if len(k.Components) == 0 {
		if s, ok := r.IdentifierScheme(k.Scheme); ok {
			return "scheme " + s.Name
		}
		return "scheme " + k.Scheme.String()
	}
	parts := make([]string, len(k.Components))
	for i, c := range k.Components {
		parts[i] = r.describeComponent(c)
	}
	return strings.Join(parts, " + ")
}

func (r *Registry) describeComponent(c KeyComponent) string {
	switch {
	case !c.Property.IsZero():
		if p, ok := r.Property(c.Property); ok {
			return "property " + p.Property.Name
		}
		return "property " + c.Property.String()
	case c.Set:
		return "the set of the objects a " + r.linkName(c.Context) + " link reaches"
	}
	return "link " + r.linkName(c.Context)
}

func (r *Registry) linkName(id LinkTypeID) string {
	if lt, ok := r.LinkType(id); ok {
		return lt.Name
	}
	return id.String()
}

// PropertyNames returns the names of the properties a type has, own and inherited, in the order of the type.
func (r *Registry) PropertyNames(id ObjectTypeID) []string {
	props := r.EffectiveProperties(id)
	out := make([]string, len(props))
	for i, p := range props {
		out[i] = p.Name
	}
	return out
}
