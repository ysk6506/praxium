package ingest

import (
	"context"
)

// The prepared rows of the new ingestion flow (ADR-0086): what one page of raw rows becomes before it is stored. Every
// slice holds rows of a fixed set of columns, so that a store can put it in a staging table with one COPY; a row that
// belongs to a record names it by No, the number of the record in the page. Nothing here knows a database, and nothing
// here decides what an object is: the ids of the parents, the objects, the changes against what is stored all come
// later, in the stage that has the identity index.

// KeyHash is the 16 bytes a signature or a lookup key is stored by.
type KeyHash [16]byte

// StageRecord is the row of the dataset as it was received: its natural key, where it stood in the file, the hash of its
// cells, and the cells of the declared columns in the order of the columns (Fields). Filtered says that a refinement of
// the mapping filtered the row out (by the operation in FilteredBy, as refinement#operation); a filtered row is still a
// record and its object is waited for, not closed at once (ADR-0052).
type StageRecord struct {
	No         int
	Key        string
	Line       int
	Where      string
	Filtered   bool
	FilteredBy string
	RawHash    string
	Fields     []string
}

// StageKey is one identity key a feed gives a record, the keys of a feed in the order the feed tries them (Ord) with the
// place the model gives each (ModelOrd; the first key of that order names a new object). Sig is the signature of the key,
// what the identity index matches on: for each part, its kind, what it refers to and its tidied value, the context part
// having no value; SigHash is its hash. Cand is the id a new object would get from this key, set for a key with no context
// when the feed creates objects: an object under a parent gets its id when the parent is known. Held says the record is one
// a rule filtered out and the key is of the row as the feed would see it, for an object that is waited for.
type StageKey struct {
	No       int
	Feed     int
	Ord      int
	ModelOrd int
	TypeNo   int
	Held     bool
	Sig      string
	SigHash  KeyHash
	Cand     string
	// LinkRef is the link type of the context of the key, empty for a key with none.
	LinkRef string
}

// StageParent is one parent a context key needs, from the nearest (Level 1) to the farthest: the parent's object type
// and the signature of the key the child's row gives for it. The id of the parent is not decided here; the identity
// index finds the parent by this key and the stage that resolves it goes up the levels.
type StageParent struct {
	No      int
	Feed    int
	Ord     int
	Level   int
	LinkRef string
	TypeNo  int
	Sig     string
	SigHash KeyHash
}

// StageEnd is the key one end of a link definition finds its other end by: the tidied cells of the end's columns, as the
// definition reads them. Def is the place of the definition in the mapping; Side is from or to.
type StageEnd struct {
	No         int
	Def        int
	Side       string
	Held       bool
	Lookup     string
	LookupHash KeyHash
}

// StageClaim is an identifier of a scheme that a record says its object has: from a key of the scheme or from an
// identifier column of the feed.
type StageClaim struct {
	No        int
	Feed      int
	Scheme    string
	Value     string
	ValueHash KeyHash
	Held      bool
}

// StageProblem is something about a record, or about the page, that was set aside: the kind (the closed list of
// pipeline.md 6), the field of the record it is about (a column name, empty when it is about the whole record or the
// dataset) and the reason. No is -1 for a problem that is not about a record of the page.
type StageProblem struct {
	No     int
	Key    string
	Line   int
	Where  string
	Feed   string
	Kind   string
	Field  string
	Reason string
}

// StagePage is one page of prepared rows with the number of the page and the token to go on after it. Last and End say,
// for the last page of a dataset, whether the whole dataset was read.
type StagePage struct {
	Dataset  string
	Page     int
	Token    []byte
	Last     bool
	End      Completeness
	Records  []StageRecord
	Keys     []StageKey
	Parents  []StageParent
	Ends     []StageEnd
	Claims   []StageClaim
	Problems []StageProblem
	// Skipped counts the lines passed over on purpose (no_key), which are no part of the lines received.
	Skipped map[string]int
	// Unreadable counts the lines that could not become a record.
	Unreadable int
}

// StageSink is the receiving half of the workbench: it stores one page of prepared rows together with the number of the
// page and its token, in one step, so that a run that stops can go on after the last page that was stored. It is given
// one page at a time and does not keep the page after it returns.
type StageSink interface {
	Stage(ctx context.Context, page StagePage) error
}
