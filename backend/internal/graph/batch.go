package graph

import (
	"github.com/ysk6506/praxium/backend/internal/model"
)

// BatchKind is the kind of a change batch (kg-storage.md 3.4).
type BatchKind string

// The kinds of batch.
const (
	BatchSync   BatchKind = "sync"
	BatchStream BatchKind = "stream"
	BatchMerge  BatchKind = "merge"
)

// Valid reports whether k is one of the three kinds.
func (k BatchKind) Valid() bool { return k == BatchSync || k == BatchStream || k == BatchMerge }

// OrderKind is one of the eight ordering events of kg-storage-apply.md 5. The
// list is closed: an event outside it may not change a result.
type OrderKind string

// The closed list of ordering events.
const (
	OrderSourceChange     OrderKind = "source_change"
	OrderClock            OrderKind = "clock_event"
	OrderSeal             OrderKind = "seal"
	OrderHeldRelease      OrderKind = "held_release"
	OrderHeldDiscard      OrderKind = "held_discard"
	OrderGuardConfirm     OrderKind = "guard_confirm"
	OrderRawExclude       OrderKind = "raw_exclude"
	OrderDefinitionSwitch OrderKind = "definition_switch"
)

// OrderKinds returns the eight ordering events in their documented order.
func OrderKinds() []OrderKind {
	return []OrderKind{OrderSourceChange, OrderClock, OrderSeal, OrderHeldRelease, OrderHeldDiscard,
		OrderGuardConfirm, OrderRawExclude, OrderDefinitionSwitch}
}

// Valid reports whether k is in the closed list.
func (k OrderKind) Valid() bool {
	for _, o := range OrderKinds() {
		if o == k {
			return true
		}
	}
	return false
}

// ObjectUpsert adds an object or replaces all its versioned attributes. The
// object's id is derived from Type and Key; Attrs hold versioned properties
// only (a volatile value goes in Batch.Volatile).
type ObjectUpsert struct {
	Type       model.ObjectTypeID
	Key        []KeyPart
	Name       string
	Attrs      map[int]Attr
	SourceRefs []string
}

// LinkUpsert adds a link or replaces its attributes. A link is never volatile.
type LinkUpsert struct {
	Type          model.LinkTypeID
	Src           ObjectID
	Dst           ObjectID
	Discriminator string
	Grade         model.Grade
	Basis         model.Basis
	Attrs         map[int]Attr
	SourceRefs    []string
}

// VolatileWrite sets the current value of a volatile attribute of an object.
type VolatileWrite struct {
	Object ObjectID
	AttrNo int
	Value  any
	Grade  model.Grade
	Basis  model.Basis
	SeenAt string
	Ref    string
}

// IdentifierWrite opens (or, with Close, closes) the interval in which an
// identifier names an object. Value is tidied by the scheme's normaliser.
type IdentifierWrite struct {
	Object ObjectID
	Scheme model.IdentifierSchemeID
	Value  string
	Scope  string
	Close  bool
}

// IdentityEventKind is the kind of an identity event.
type IdentityEventKind string

// The kinds of identity event.
const (
	EventMerge  IdentityEventKind = "merge"
	EventSplit  IdentityEventKind = "split"
	EventRevive IdentityEventKind = "revive"
)

// LinkMapping says that the link Old became the link New when objects merged.
type LinkMapping struct {
	Old LinkID
	New LinkID
}

// IdentityEvent records a merge, a split or a revival. The identity history
// is never rewound (kg-model.md 10.5).
type IdentityEvent struct {
	Seq     int64
	Kind    IdentityEventKind
	Retired ObjectID
	Into    ObjectID
	LinkMap []LinkMapping
}

// Batch is one change batch as the applier receives it. Retirements of a
// thing that is not live are no-ops. The ids of two items of one kind may not
// repeat. Identifier writes and identity events are applied in the last chunk
// together with the volatile writes.
type Batch struct {
	// ApplyID names this apply for retries. Empty: the applier issues one.
	ApplyID string
	Kind    BatchKind
	// SourceID names the source of the change (a source change batch).
	SourceID string
	// Event is the ordering event this batch is; empty means source_change.
	Event    OrderKind
	EventRef string

	Objects       []ObjectUpsert
	Links         []LinkUpsert
	RetireObjects []ObjectID
	RetireLinks   []LinkID
	Volatile      []VolatileWrite
	Identifiers   []IdentifierWrite
	Events        []IdentityEvent
}
