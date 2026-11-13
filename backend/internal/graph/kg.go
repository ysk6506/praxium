package graph

import (
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Role is the role of a KG (kg-storage.md 3.1). The API also calls the
// production KG "main".
type Role string

// The four roles.
const (
	RoleProduction Role = "production"
	RoleCandidate  Role = "candidate"
	RoleTest       Role = "test"
	RoleSystem     Role = "system"
)

// MainAlias is the name that stands for the production KG.
const MainAlias = "main"

// Valid reports whether r is one of the four roles.
func (r Role) Valid() bool {
	return r == RoleProduction || r == RoleCandidate || r == RoleTest || r == RoleSystem
}

// State is the state of a KG: building is invisible to readers and change
// feeds until the first load completes.
type State string

// The two states.
const (
	StateBuilding State = "building"
	StateServing  State = "serving"
)

// KG is the identity of a KG (table kg).
type KG struct {
	ID                KGID
	Name              string
	Role              Role
	State             State
	BaseKGID          KGID
	DefinitionSetHash kernel.Hash
	Generation        int
	CreatedAt         time.Time
}

// GenerationEvent records a restore: the generation it raised and the change
// number the KG went back to.
type GenerationEvent struct {
	Generation int
	RestoredTo int64
	At         time.Time
}

// Head is the head row of a KG (kg_head). Only the applier moves it.
type Head struct {
	KG KGID
	// AppliedSeq is the last applied change number; ahead of ReadableThrough in a chunked apply.
	AppliedSeq int64
	// ReadableThrough is the end of what readers may see.
	ReadableThrough int64
	// IngestSeq is the last ordering event (applied_through).
	IngestSeq int64
	// Hash is the running graph hash and its partial sums.
	Hash HashState
	// InputDigest is the chain over the ordering events since the last seal.
	InputDigest kernel.Hash
	// ApplyID and NextChunkNo mark a chunked apply in progress; ChunkCount is its size.
	ApplyID     string
	NextChunkNo int
	ChunkCount  int
	// Rollback is the hash state before the first chunk of the open apply.
	Rollback *HashState
	// LastApplyID is the apply that completed last (exactly-once for retries).
	LastApplyID            string
	AppliedSemanticVersion int
}

// Clone returns a copy that shares nothing with h.
func (h Head) Clone() Head {
	h.Hash = h.Hash.Clone()
	if h.Rollback != nil {
		r := h.Rollback.Clone()
		h.Rollback = &r
	}
	return h
}

// Open reports whether a chunked apply is half done.
func (h Head) Open() bool { return h.ApplyID != "" }

// Lease is the sole-applier mark of a KG (kg_apply_lease). Semantic is the
// semantic version of the engine that holds it.
type Lease struct {
	KG       KGID
	Owner    string
	Until    time.Time
	Attempt  int64
	Semantic int
}

// LeaseRequest asks for the lease of a KG.
type LeaseRequest struct {
	KG       KGID
	Owner    string
	Semantic int
	TTL      time.Duration
	Now      time.Time
}

// SealKind is why a version was sealed.
type SealKind string

// The seal kinds.
const (
	SealSync     SealKind = "sync"
	SealMerge    SealKind = "merge"
	SealPeriodic SealKind = "periodic"
)

// Valid reports whether k is one of the three kinds.
func (k SealKind) Valid() bool { return k == SealSync || k == SealMerge || k == SealPeriodic }

// Version is a sealed point of a KG (kg_version).
type Version struct {
	KG                KGID
	No                int
	ID                VersionID
	SealedSeq         int64
	Generation        int
	Prev              VersionID
	DefinitionSetHash kernel.Hash
	EngineVersion     int
	HashRuleVersion   int
	InputDigest       kernel.Hash
	GraphHash         Sum
	Rows              int64
	Parts             map[PartKey]Part
	SealedAt          time.Time
	Kind              SealKind
}

// OrderEvent is one entry of the ingest order (ingest_order). Digest is the
// input chain after the event.
type OrderEvent struct {
	IngestSeq int64
	Kind      OrderKind
	Ref       string
	Digest    kernel.Hash
	// ChangeSeq is the last change number the event produced, or 0.
	ChangeSeq int64
}

// ChangeBatch is the record of one applied chunk (change_batch).
type ChangeBatch struct {
	Seq        int64
	Kind       BatchKind
	SourceID   string
	ApplyID    string
	ChunkNo    int
	ChunkCount int
	IngestSeq  int64
	Attempt    int64
	AppliedAt  time.Time
	Objects    int
	Links      int
}

// ChangeKind is what happened to an entity in a change.
type ChangeKind string

// The three kinds of entry.
const (
	ChangeAdded   ChangeKind = "added"
	ChangeChanged ChangeKind = "changed"
	ChangeRetired ChangeKind = "retired"
)

// ChangeEntry is one entity of a change (change_entry).
type ChangeEntry struct {
	Seq     int64
	Entity  string // "object" or "link"
	ID      string
	Change  ChangeKind
	OldHash kernel.Hash
	NewHash kernel.Hash
}

// LightChange is one line of the light change line: a volatile value changed.
// ValueMark is a short hash of the new value; there is no history behind it.
type LightChange struct {
	Seq       int64
	Object    ObjectID
	AttrNo    int
	ValueMark string
}

// IdentifierRecord is one interval in which an identifier names an object (object_identifier).
type IdentifierRecord struct {
	Object  ObjectID
	Scheme  string
	Value   string
	Scope   string
	SeqFrom int64
	SeqTo   int64
}
