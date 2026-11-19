package pgschema

import (
	"encoding/json"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// State says how far a schema has got. It is kept next to the manifest and is not in its hash, so a schema
// whose statistics are still to be made is not taken for a different schema.
type State string

// The states of a storage schema.
const (
	StateOK           State = "ok"
	StateIndexPending State = "index_pending"
	StateStatsPending State = "stats_pending"
)

// TypeSummary is what the manifest keeps of one type.
type TypeSummary struct {
	TypeNo int      `json:"type_no"`
	Docs   []Column `json:"docs"`
	State  []Column `json:"state,omitempty"`
	Search []int    `json:"search,omitempty"`
	// Stats are the groups of attributes of the statistics objects that exist. The groups depend on the
	// order in which attributes were added, so they are not in the hash.
	Stats [][]int `json:"stats,omitempty"`
}

// Manifest is the summary of a storage schema: what the generator and the model were, and what each type
// has. It is kept in the database; a server compares its own manifest with it, and Diff turns two of them
// into the steps that bring the older up to the newer.
type Manifest struct {
	GeneratorVersion int           `json:"generator_version"`
	KGNo             int64         `json:"kg_no"`
	RegistryHash     string        `json:"registry_hash"`
	NumberingHash    string        `json:"numbering_hash"`
	SearchHash       string        `json:"search_hash"`
	CommonState      []Column      `json:"common_state,omitempty"`
	Types            []TypeSummary `json:"types"`
	State            State         `json:"state"`
}

func newManifest(reg *model.Registry, sh shape, kgNo int64, search SearchSet) (Manifest, error) {
	numbering, err := reg.NumberingHash()
	if err != nil {
		return Manifest{}, err
	}
	searchHash, err := search.Hash()
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		GeneratorVersion: GeneratorVersion, KGNo: kgNo, RegistryHash: reg.Hash().String(),
		NumberingHash: numbering.String(), SearchHash: searchHash.String(), CommonState: sh.Common, State: StateOK,
	}
	for _, ts := range sh.Types {
		sum := TypeSummary{TypeNo: ts.TypeNo, Docs: ts.Docs, State: ts.State, Search: searchedOf(search, ts.TypeNo)}
		for _, g := range statsGroups(ts.Docs) {
			sum.Stats = append(sum.Stats, attrNos(g))
		}
		m.Types = append(m.Types, sum)
	}
	return m, nil
}

// Hash is the content hash of what decides the shape of the schema. It leaves out the number of the KG, the
// state and the grouping of the statistics, so the same model gives the same hash for every KG and at every
// stage of its making.
func (m Manifest) Hash() (kernel.Hash, error) {
	cols := func(cs []Column) []any {
		out := make([]any, 0, len(cs))
		for _, c := range cs {
			out = append(out, []any{c.AttrNo, string(c.Kind)})
		}
		return out
	}
	nos := func(ns []int) []any {
		out := make([]any, 0, len(ns))
		for _, n := range ns {
			out = append(out, n)
		}
		return out
	}
	types := make([]any, 0, len(m.Types))
	for _, t := range m.Types {
		types = append(types, map[string]any{
			"type_no": t.TypeNo, "docs": cols(t.Docs), "state": cols(t.State), "search": nos(t.Search),
		})
	}
	return kernel.ContentHash(map[string]any{
		"generator_version": m.GeneratorVersion,
		"registry":          m.RegistryHash,
		"numbering":         m.NumberingHash,
		"search":            m.SearchHash,
		"common_state":      cols(m.CommonState),
		"types":             types,
	})
}

// JSON is the stored form of the manifest.
func (m Manifest) JSON() ([]byte, error) { return json.Marshal(m) }

// ParseManifest reads a stored manifest.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, kernel.Wrap(kernel.CodeInvalidValue, err, "the stored manifest is not valid")
	}
	return m, nil
}

// Knows reports whether a table name is one the schema is meant to have: a table of the manifest or a name
// kept for the staged apply. A check of a schema calls everything else a stranger.
func (m Manifest) Knows(table string) bool {
	if IsReservedName(table) {
		return true
	}
	for _, f := range FixedTables() {
		if f == table {
			return true
		}
	}
	for _, t := range m.Types {
		if table == TypeTable(t.TypeNo) || (len(t.State) > 0 && table == StateTable(t.TypeNo)) {
			return true
		}
	}
	return false
}
