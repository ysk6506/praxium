package ingest

import (
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The definitions of ingestion in the definition store (REQ-438, ADR-0132): a source definition is one item of the kind
// source and a mapping definition one item of the kind mapping, each named by the id of its document. The kinds are
// apart, so a source and a mapping may have the same id.
const (
	ItemKindSource  = "source"
	ItemKindMapping = "mapping"
	// ItemSchemaVersion is the schema version of an item that holds a document; the document carries its own version.
	ItemSchemaVersion = 1
)

var setCodeTable []*errCode

// regSet adds a code that only the checks of a whole definition set give (CompileSet, SecretValues). They are kept in a
// table of their own, as the codes of the new compile are, because a test of the first table runs one document at a time.
func regSet(code string, k kernel.Code, doc, meaning, params string) *errCode {
	ec := &errCode{code: code, kernel: k, doc: doc, meaning: meaning}
	if params != "" {
		ec.params = strings.Split(params, ",")
	}
	setCodeTable = append(setCodeTable, ec)
	return ec
}

// SetErrorCodes is the table of the codes only the checks of a definition set give, sorted by code.
func SetErrorCodes() []ErrorCode {
	out := make([]ErrorCode, 0, len(setCodeTable))
	for _, ec := range setCodeTable {
		out = append(out, ErrorCode{Code: ec.code, Kernel: ec.kernel, Document: ec.doc, Meaning: ec.meaning, Params: append([]string{}, ec.params...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

var (
	ecSetNoSource   = regSet("mapping.source.missing", kernel.CodeInvalidValue, "mapping", "No source of the set defines a dataset the mapping reads.", "mapping,datasets")
	ecSetMixed      = regSet("mapping.source.mixed", kernel.CodeInvalidValue, "mapping", "The datasets the mapping reads are defined by different sources; a mapping reads the datasets of one source.", "mapping,datasets,sources")
	ecSetAmbiguous  = regSet("mapping.source.ambiguous", kernel.CodeInvalidValue, "mapping", "More than one source defines every dataset the mapping reads, and none of them has the id of the mapping.", "mapping,sources")
	ecSetFeedTaken  = regSet("mapping.feed.id_taken", kernel.CodeInvalidValue, "mapping", "A feed has the id of a feed of another mapping of the set.", "id,mapping,other")
	ecSetRefTaken   = regSet("mapping.refinement.id_taken", kernel.CodeInvalidValue, "mapping", "A refinement has the id of a refinement of another mapping of the set.", "id,mapping,other")
	ecSetLinkTaken  = regSet("mapping.link.id_taken", kernel.CodeInvalidValue, "mapping", "A link definition has the id of a link definition of another mapping of the set.", "id,mapping,other")
	ecSetNoMapID    = regSet("mapping.id.missing_in_set", kernel.CodeInvalidValue, "mapping", "A mapping of a set has no id; the mapping of a set is named by its id.", "")
	setTakenOfKinds = []struct {
		ec   *errCode
		list string
		word string
	}{{ecSetRefTaken, "refinements", "refinement"}, {ecSetFeedTaken, "feeds", "feed"}, {ecSetLinkTaken, "links", "link definition"}}
)

// DefinitionSet is every source and every mapping of a definition set. Each is named by its id; a mapping without an id
// is given the id of its item before it is put here.
type DefinitionSet struct {
	Sources  []Source
	Mappings []Mapping
}

// SetError is a definition error of one document of a set: the kind of the document is the Document of the error and
// Name is its id.
type SetError struct {
	Name string `json:"name"`
	DefinitionError
}

// SetErrors is the error the check of a set returns: the errors of its documents, at most MaxDefinitionErrors of them,
// and how many more there were.
type SetErrors struct {
	List    []SetError `json:"errors"`
	Omitted int        `json:"omitted,omitempty"`
}

// Error is one line for each error: the document, its id, the place and the sentence.
func (e *SetErrors) Error() string {
	lines := make([]string, 0, len(e.List))
	for _, s := range e.List {
		lines = append(lines, s.Line())
	}
	if e.Omitted > 0 {
		lines = append(lines, (&DefinitionErrors{Omitted: e.Omitted}).Error())
	}
	return strings.Join(lines, "\n")
}

// Unwrap gives the first error as a kernel error, so that kernel.CodeOf works.
func (e *SetErrors) Unwrap() error {
	if len(e.List) == 0 {
		return nil
	}
	return kernel.Errorf(e.List[0].kernel, "%s", e.List[0].Message)
}

// Line is the error as a person reads it: "mapping m_servers at /feeds/0/dataset: <sentence>".
func (s SetError) Line() string {
	where := ""
	if s.Pointer != "" {
		where = " at " + s.Pointer
	}
	return s.Document + " " + s.Name + where + ": " + s.Message
}

// SetErrorsOf takes the list out of an error the check of a set returned.
func SetErrorsOf(err error) (*SetErrors, bool) {
	var se *SetErrors
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}

// setCollector gathers the errors of the documents of a set, in the order they are found.
type setCollector struct{ out SetErrors }

func (c *setCollector) take(name string, list []DefinitionError, omitted int) {
	for _, e := range list {
		if len(c.out.List) >= MaxDefinitionErrors {
			c.out.Omitted++
			continue
		}
		c.out.List = append(c.out.List, SetError{Name: name, DefinitionError: e})
	}
	c.out.Omitted += omitted
}

// CompileSet compiles every mapping of the set with the source it reads (ADR-0132): the source with the id of the
// mapping when it defines what the mapping reads, else the one source that defines every dataset the mapping reads. The
// ids of the refinements, feeds and link definitions are unique among the mappings of the set. It tells every mistake
// of every document at once, as a *SetErrors; a document that is not one of the set is never looked at, so the check is
// the same whatever the order of the documents. The compile is that of the new flow (CompileScale). Until the plans of
// several sources (<source id>.<dataset id>) come, a mapping reads the datasets of one source.
func CompileSet(reg *model.Registry, set DefinitionSet) ([]*Plan, error) {
	sources := slices.Clone(set.Sources)
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	mappings := slices.Clone(set.Mappings)
	sort.SliceStable(mappings, func(i, j int) bool { return mappings[i].ID < mappings[j].ID })
	c := &setCollector{}
	owners := datasetOwners(sources)
	taken := map[string]map[string]string{}
	var plans []*Plan
	for _, m := range mappings {
		mc := newCollector(DocumentMapping)
		if m.ID == "" {
			mc.add(ecSetNoMapID, ptr("id"), nil, "a mapping of a definition set needs an id")
		}
		checkTaken(mc, m, taken)
		src, paired := pairSource(mc, m, sources, owners)
		c.take(m.ID, mc.list, mc.omitted)
		if !paired {
			continue
		}
		p, err := CompileScale(reg, src, m)
		if err != nil {
			// a compile tells nothing but the errors of the documents (compile returns its collector's error)
			de := err.(*DefinitionErrors)
			c.take(m.ID, de.List, de.Omitted)
			continue
		}
		if !mc.any() {
			plans = append(plans, p)
		}
	}
	if len(c.out.List) > 0 {
		return nil, &c.out
	}
	return plans, nil
}

// datasetOwners are the ids of the sources that define each dataset id, in the order of the sources.
func datasetOwners(sources []Source) map[string][]string {
	out := map[string][]string{}
	for _, s := range sources {
		for _, d := range s.Datasets {
			if !slices.Contains(out[d.ID], s.ID) {
				out[d.ID] = append(out[d.ID], s.ID)
			}
		}
	}
	return out
}

// datasetsRead are the datasets a mapping reads: what its feeds and refinements read that is not one of its refinements.
func datasetsRead(m Mapping) []string {
	own := map[string]bool{}
	for _, r := range m.Refinements {
		own[r.ID] = true
	}
	var out []string
	add := func(id string) {
		if id != "" && !own[id] && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, r := range m.Refinements {
		add(r.Input)
	}
	for _, f := range m.Feeds {
		add(f.Dataset)
	}
	sort.Strings(out)
	return out
}

// pairSource finds the source a mapping is compiled with, or records why there is none.
func pairSource(c *collector, m Mapping, sources []Source, owners map[string][]string) (Source, bool) {
	reads := datasetsRead(m)
	var known, defining []string
	count := map[string]int{}
	for _, ds := range reads {
		if len(owners[ds]) == 0 {
			continue
		}
		known = append(known, ds)
		for _, s := range owners[ds] {
			count[s]++
		}
	}
	for _, s := range sources {
		if len(known) > 0 && count[s.ID] == len(known) {
			defining = append(defining, s.ID)
		}
	}
	byID := map[string]Source{}
	for _, s := range sources {
		byID[s.ID] = s
	}
	// A mapping beside the source of its id (an old mapping takes the id of its source): its compile says what of the
	// source it does not find, which is more than this could.
	if same, ok := byID[m.ID]; ok && m.ID != "" && (len(defining) == 0 || slices.Contains(defining, m.ID)) {
		return same, true
	}
	switch {
	case len(defining) == 1:
		return byID[defining[0]], true
	case len(known) == 0:
		c.add(ecSetNoSource, "", Params{"mapping": m.ID, "datasets": nonNil(reads)},
			"no source defines the datasets mapping %s reads (%s); import the source with it", m.ID, strings.Join(reads, ", "))
	case len(defining) == 0:
		var all []string
		for _, ds := range known {
			for _, s := range owners[ds] {
				if !slices.Contains(all, s) {
					all = append(all, s)
				}
			}
		}
		sort.Strings(all)
		c.add(ecSetMixed, "", Params{"mapping": m.ID, "datasets": known, "sources": all},
			"mapping %s reads datasets of the sources %s; a mapping reads the datasets of one source", m.ID, strings.Join(all, ", "))
	default:
		c.add(ecSetAmbiguous, "", Params{"mapping": m.ID, "sources": defining},
			"the sources %s all define the datasets mapping %s reads; give the mapping the id of its source", strings.Join(defining, ", "), m.ID)
	}
	return Source{}, false
}

// checkTaken records the refinements, feeds and link definitions of a mapping whose ids an earlier mapping of the set
// has. taken holds, for each list, the mapping that has each id first.
func checkTaken(c *collector, m Mapping, taken map[string]map[string]string) {
	ids := map[string][]string{}
	for _, r := range m.Refinements {
		ids["refinements"] = append(ids["refinements"], r.ID)
	}
	for _, f := range m.Feeds {
		ids["feeds"] = append(ids["feeds"], f.ID)
	}
	for _, l := range m.Links {
		ids["links"] = append(ids["links"], l.ID)
	}
	for _, k := range setTakenOfKinds {
		if taken[k.list] == nil {
			taken[k.list] = map[string]string{}
		}
		for i, id := range ids[k.list] {
			other, seen := taken[k.list][id]
			switch {
			case id == "":
			case !seen:
				taken[k.list][id] = m.ID
			case other != m.ID:
				c.add(k.ec, ptr(k.list, i, "id"), Params{"id": id, "mapping": m.ID, "other": other},
					"%s %s of mapping %s is also a %s of mapping %s; the ids are unique among the mappings of a KG", k.word, id, m.ID, k.word, other)
			}
		}
	}
}
