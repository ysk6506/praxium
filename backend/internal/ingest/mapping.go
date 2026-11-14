package ingest

import "io"

// The operations of a refinement that this slice has. The design's fourteen are
// a closed list (pipeline.md 2.2); these three need no state between rows.
const (
	OpFilter    = "filter"
	OpFillEmpty = "fill_empty"
	OpReplace   = "replace"
)

// The pure functions of the replace operation (a closed list; pipeline.md 2.2).
const (
	FuncTrim  = "trim"
	FuncLower = "lower"
	FuncUpper = "upper"
	FuncMap   = "map"
)

// Op is one operation of a refinement. A filter keeps the rows that pass every test it has; a fill gives an empty
// cell a value; a replace rewrites the cells of a column with a function of the closed list.
type Op struct {
	Op       string            `json:"op"`
	Column   string            `json:"column"`
	In       []string          `json:"in,omitempty"`
	NotIn    []string          `json:"not_in,omitempty"`
	NotEmpty bool              `json:"not_empty,omitempty"`
	Value    string            `json:"value,omitempty"`
	Func     string            `json:"function,omitempty"`
	Map      map[string]string `json:"map,omitempty"`
}

// Refinement is a refined dataset: the rows of an input dataset (or of an earlier refinement) after the
// operations, in order. Rows keep their natural key.
type Refinement struct {
	ID    string `json:"id"`
	Input string `json:"input"`
	Ops   []Op   `json:"ops"`
}

// AttrMap maps one column to one attribute of the object type. Rank says which feed wins when two give a
// value: the lower is first (pipeline.md 3.2).
type AttrMap struct {
	Attribute string `json:"attribute"`
	Column    string `json:"column"`
	Rank      int    `json:"rank,omitempty"`
}

// KeyComponent maps a column to one property that is a component of an identity key.
type KeyComponent struct {
	Property string `json:"property"`
	Column   string `json:"column"`
}

// IdentityMap maps columns to one identity key of the standard model: a scheme with the column of its identifier,
// or the properties of a key made of properties. Priority orders the keys of a feed: the lowest is tried first.
type IdentityMap struct {
	KeyName    string         `json:"key_name,omitempty"`
	Scheme     string         `json:"scheme,omitempty"`
	Column     string         `json:"column,omitempty"`
	Components []KeyComponent `json:"components,omitempty"`
	Priority   int            `json:"priority,omitempty"`
}

// IdentifierMap gives an object an identifier of a scheme that is not one of its keys.
type IdentifierMap struct {
	Scheme string `json:"scheme"`
	Column string `json:"column"`
}

// Feed maps a dataset to one object type (pipeline.md 3.1). A feed that creates objects is the one that makes an
// object exist; one that does not only attaches its values to the objects other feeds made.
type Feed struct {
	ID             string          `json:"id"`
	Dataset        string          `json:"dataset"`
	ObjectType     string          `json:"object_type"`
	Attributes     []AttrMap       `json:"attributes"`
	Identity       []IdentityMap   `json:"identity"`
	Identifiers    []IdentifierMap `json:"identifiers,omitempty"`
	CreatesObjects bool            `json:"creates_objects,omitempty"`
	RetiresObjects bool            `json:"retires_objects,omitempty"`
	Basis          string          `json:"basis"`
	Approximate    bool            `json:"approximate,omitempty"`
}

// LinkEnd says which rows of which feed one end of a link definition reads: the feed and the columns whose
// tidied values make the key the condition compares.
type LinkEnd struct {
	Feed    string   `json:"feed"`
	Columns []string `json:"columns"`
}

// The conditions of a link definition that this slice has; the design lists six (pipeline.md 5.2).
const ConditionSameKey = "same_key"

// LinkDef makes the links of one link type from two feeds: a link goes from the object of a row of From to the
// object of the row of To that has the same key. KeyType is the value type that tidies the key (the name of a
// value type of the model); empty means the text with its ends trimmed.
type LinkDef struct {
	ID        string  `json:"id"`
	LinkType  string  `json:"link_type"`
	From      LinkEnd `json:"from"`
	To        LinkEnd `json:"to"`
	Condition string  `json:"condition"`
	KeyType   string  `json:"key_type,omitempty"`
	Basis     string  `json:"basis,omitempty"`
}

// Mapping is the whole mapping definition: what the refinements, the feeds and the link definitions are.
type Mapping struct {
	Refinements []Refinement `json:"refinements,omitempty"`
	Feeds       []Feed       `json:"feeds"`
	Links       []LinkDef    `json:"links,omitempty"`
}

// ParseMapping reads a mapping definition. It is checked against the registry when it is compiled.
func ParseMapping(r io.Reader) (Mapping, error) {
	var m Mapping
	if err := decodeStrict(r, "mapping definition", &m); err != nil {
		return Mapping{}, err
	}
	return m, nil
}
