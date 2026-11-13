package graph

import (
	"context"
	"time"
)

// The storage port. Four roles; an adapter implements all of them as Store.
//
// What must be transactional (the PostgreSQL adapter does it in one
// transaction): ApplyChunk, AbandonApply and Seal each lock the head row,
// verify the expected head, the generation and the lease, write every row of
// the call, move the head, and publish the new readable_through and the
// feed positions only together with the head. Readers never see a row of a
// chunk that is not readable, and a reader at a change number sees the same
// state however many chunks are applied after it.
//
// The expected-head check: a write names the applied_seq it was planned
// against. The store refuses it (CodeHeadMoved) when the head is elsewhere.
// The lease check: a write names the owner and attempt of its lease. The
// store refuses it (CodeLeaseLost) when the lease is another's, was taken
// over (a higher attempt) or has expired at the write's Now, and refuses it
// (CodeSemanticTooLow) when the lease's semantic version is below the one
// the head has applied with.
type Store interface {
	Catalog
	Writer
	Reader
	Feed
}

// Catalog manages KG identities.
type Catalog interface {
	// CreateKG stores a new KG with generation 1 and an empty head.
	CreateKG(ctx context.Context, kg KG) (KG, error)
	// GetKG finds a KG by id or by name; MainAlias names the production KG.
	GetKG(ctx context.Context, ref string) (KG, error)
	// ListKGs lists the KGs by creation.
	ListKGs(ctx context.Context) ([]KG, error)
	// Restore takes the KG back to change number toSeq (a point-in-time
	// restore): later rows disappear, the generation goes up by one and the
	// event is kept. The head's hash state is given, since the store never computes one.
	Restore(ctx context.Context, id KGID, toSeq int64, head Head, now time.Time) (KG, error)
	// Generations lists the restores of a KG.
	Generations(ctx context.Context, id KGID) ([]GenerationEvent, error)
}

// ObjectChange closes the open row Old (if any) and opens New (if any).
type ObjectChange struct {
	Old *ObjectRow
	New *ObjectRow
}

// LinkChange closes the open row Old (if any) and opens New (if any).
type LinkChange struct {
	Old *LinkRow
	New *LinkRow
}

// IdentifierChange opens a record, or with Closes closes the open record of the same object, scheme and value.
type IdentifierChange struct {
	Record IdentifierRecord
	Closes bool
}

// ChunkWrite is one chunk of an apply: everything the store persists in one
// atomic step. Batch is nil for a final write that carries no rows; then no
// change number is used. Head is the new head as the applier computed it.
type ChunkWrite struct {
	KG          KGID
	Generation  int
	ExpectedSeq int64
	Lease       Lease
	Now         time.Time

	Batch       *ChangeBatch
	Objects     []ObjectChange
	Links       []LinkChange
	Volatile    []VolatileRow
	Light       []LightChange
	Identifiers []IdentifierChange
	Events      []IdentityEvent
	Entries     []ChangeEntry
	// Order is the ordering event, written with the final chunk.
	Order *OrderEvent

	Head Head
	// Serve flips the KG from building to serving (the final chunk of the first load).
	Serve bool
}

// AbandonWrite reverts an apply that is half done: every row after the
// head's readable_through goes (reopening the rows they closed) and Head,
// computed by the applier from the rollback state, becomes the head.
type AbandonWrite struct {
	KG          KGID
	Generation  int
	ExpectedSeq int64
	Lease       Lease
	Now         time.Time
	Head        Head
}

// SealWrite seals a version. The store verifies that applied_seq equals
// readable_through equals ExpectedSeq.
type SealWrite struct {
	KG          KGID
	Generation  int
	ExpectedSeq int64
	Lease       Lease
	Now         time.Time
	Version     Version
	Order       OrderEvent
	Head        Head
}

// Writer is the only way state changes. Callers other than the applier do not use it.
type Writer interface {
	// AcquireLease takes the lease of a KG: attempt goes up. It refuses while
	// another owner's lease is alive and an engine older than the applied one.
	AcquireLease(ctx context.Context, req LeaseRequest) (Lease, error)
	// RenewLease moves lease_until of the lease the caller still holds.
	RenewLease(ctx context.Context, lease Lease, ttl time.Duration, now time.Time) (Lease, error)
	// ReleaseLease gives the lease up; a lease already lost is not an error.
	ReleaseLease(ctx context.Context, lease Lease) error
	ApplyChunk(ctx context.Context, w ChunkWrite) error
	AbandonApply(ctx context.Context, w AbandonWrite) error
	Seal(ctx context.Context, w SealWrite) error
}

// ReadKind says which state a read point means.
type ReadKind int

// The read points. Latest is readable_through, what every reader may see and
// the only point with volatile values. Applied is applied_seq and is the
// applier's. Seq is any readable change number; Version is a sealed version.
const (
	ReadLatest ReadKind = iota
	ReadApplied
	ReadSeq
	ReadVersion
)

// ReadPoint is where a snapshot is taken. A Generation other than 0 must be
// the KG's current one (CodeGenerationChanged).
type ReadPoint struct {
	KG         KGID
	Generation int
	Kind       ReadKind
	Seq        int64
	Version    VersionID
}

// Direction is the side of a link seen from an object.
type Direction int

// The directions.
const (
	Out Direction = iota
	In
	Both
)

// VolatileAsOf says whether a snapshot carries volatile values.
type VolatileAsOf string

// The two values of volatile_as_of.
const (
	VolatileLatest  VolatileAsOf = "latest"
	VolatileOmitted VolatileAsOf = "omitted"
)

// SnapshotInfo says what a snapshot is.
type SnapshotInfo struct {
	KG         KGID
	Generation int
	// Seq is the change number the snapshot shows.
	Seq int64
	// Version is the sealed version of a ReadVersion snapshot.
	Version  VersionID
	Volatile VolatileAsOf
}

// Snapshot is one consistent state of a KG. It stays the same however much is applied after it.
type Snapshot interface {
	Info() SnapshotInfo
	Object(ctx context.Context, id ObjectID) (ObjectRow, bool, error)
	Link(ctx context.Context, id LinkID) (LinkRow, bool, error)
	// ScanObjects lists up to limit rows after the id, in id order.
	ScanObjects(ctx context.Context, after ObjectID, limit int) ([]ObjectRow, error)
	ScanLinks(ctx context.Context, after LinkID, limit int) ([]LinkRow, error)
	// LinksOf lists the links at an object in link id order.
	LinksOf(ctx context.Context, id ObjectID, dir Direction) ([]LinkRow, error)
	// Volatile returns the current value; a snapshot that is not latest answers CodeVolatileOmitted.
	Volatile(ctx context.Context, id ObjectID, attrNo int) (VolatileRow, bool, error)
	// Identifiers lists the records that are valid in this state for the identifier.
	Identifiers(ctx context.Context, scheme, value string) ([]IdentifierRecord, error)
	// IdentifierHistory lists every interval of the identifier that began by this state.
	IdentifierHistory(ctx context.Context, scheme, value string) ([]IdentifierRecord, error)
	// IdentityEvents lists the identity events up to this state in change order.
	IdentityEvents(ctx context.Context) ([]IdentityEvent, error)
}

// Reader reads.
type Reader interface {
	Head(ctx context.Context, kg KGID) (Head, error)
	Lease(ctx context.Context, kg KGID) (Lease, bool, error)
	Snapshot(ctx context.Context, rp ReadPoint) (Snapshot, error)
	Versions(ctx context.Context, kg KGID) ([]Version, error)
}

// AppliedChange is one applied chunk with its entries.
type AppliedChange struct {
	Batch   ChangeBatch
	Entries []ChangeEntry
}

// Feed lets followers read what was applied after a position. Everything
// stops at readable_through.
type Feed interface {
	// AppliedChanges lists the chunks with change number above after, up to limit.
	AppliedChanges(ctx context.Context, kg KGID, after int64, limit int) ([]AppliedChange, error)
	// LightChanges lists the light change lines with change number above after.
	LightChanges(ctx context.Context, kg KGID, after int64, limit int) ([]LightChange, error)
	// OrderEvents lists the ordering events with ingest_seq above after.
	OrderEvents(ctx context.Context, kg KGID, after int64, limit int) ([]OrderEvent, error)
	// WaitReadable returns the readable_through once it reaches minSeq, or
	// CodeWaitTimeout when waitTimeout passes first. A zero timeout does not wait.
	WaitReadable(ctx context.Context, kg KGID, minSeq int64, waitTimeout time.Duration) (int64, error)
}
