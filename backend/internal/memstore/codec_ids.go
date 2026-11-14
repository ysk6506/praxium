package memstore

import (
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// gob cannot carry an id (its text form is not one gob uses), so the wire types that hold ids hold their text.
// These are the mirrors of the types of the port that have an id in them; the file format is these types.

type wKG struct {
	ID, Name, BaseKGID string
	Role               graph.Role
	State              graph.State
	DefinitionSetHash  kernel.Hash
	Generation         int
	CreatedAt          time.Time
}

type wHead struct {
	KG                                     string
	AppliedSeq, ReadableThrough, IngestSeq int64
	Hash                                   graph.HashState
	InputDigest                            kernel.Hash
	ApplyID                                string
	NextChunkNo, ChunkCount                int
	Rollback                               *graph.HashState
	LastApplyID                            string
	AppliedSemanticVersion                 int
}

type wLight struct {
	Seq       int64
	Object    string
	AttrNo    int
	ValueMark string
}

type wIdentRec struct {
	Object, Scheme, Value, Scope string
	SeqFrom, SeqTo               int64
}

type wIdentChange struct {
	Record wIdentRec
	Closes bool
}

type wLinkMap struct{ Old, New string }

type wEvent struct {
	Seq           int64
	Kind          graph.IdentityEventKind
	Retired, Into string
	LinkMap       []wLinkMap
}

type wVersion struct {
	KG                string
	No                int
	ID, Prev          string
	SealedSeq         int64
	Generation        int
	DefinitionSetHash kernel.Hash
	EngineVersion     int
	HashRuleVersion   int
	InputDigest       kernel.Hash
	GraphHash         graph.Sum
	Rows              int64
	Parts             map[graph.PartKey]graph.Part
	SealedAt          time.Time
	Kind              graph.SealKind
}

func wireKG(k graph.KG) wKG {
	return wKG{k.ID.String(), k.Name, k.BaseKGID.String(), k.Role, k.State, k.DefinitionSetHash, k.Generation, k.CreatedAt}
}

func wireHead(h graph.Head) wHead {
	return wHead{h.KG.String(), h.AppliedSeq, h.ReadableThrough, h.IngestSeq, h.Hash, h.InputDigest, h.ApplyID, h.NextChunkNo,
		h.ChunkCount, h.Rollback, h.LastApplyID, h.AppliedSemanticVersion}
}

func wireLight(l graph.LightChange) wLight {
	return wLight{l.Seq, l.Object.String(), l.AttrNo, l.ValueMark}
}

func wireIdent(r graph.IdentifierRecord) wIdentRec {
	return wIdentRec{r.Object.String(), r.Scheme, r.Value, r.Scope, r.SeqFrom, r.SeqTo}
}

func wireEvent(e graph.IdentityEvent) wEvent {
	out := wEvent{Seq: e.Seq, Kind: e.Kind, Retired: e.Retired.String(), Into: e.Into.String()}
	for _, m := range e.LinkMap {
		out.LinkMap = append(out.LinkMap, wLinkMap{m.Old.String(), m.New.String()})
	}
	return out
}

func wireVersion(v graph.Version) wVersion {
	return wVersion{v.KG.String(), v.No, v.ID.String(), v.Prev.String(), v.SealedSeq, v.Generation, v.DefinitionSetHash, v.EngineVersion,
		v.HashRuleVersion, v.InputDigest, v.GraphHash, v.Rows, v.Parts, v.SealedAt, v.Kind}
}

// ids turns the text of ids back into ids, keeping the first error.
type ids struct{ err error }

func idOf[K any](d *ids, s string) kernel.ID[K] {
	if s == "" || d.err != nil {
		return kernel.ID[K]{}
	}
	id, err := kernel.ParseID[K](s)
	d.err = err
	return id
}

func (d *ids) kg(s string) graph.KGID         { return idOf[graph.KGKind](d, s) }
func (d *ids) object(s string) graph.ObjectID { return idOf[graph.ObjectKind](d, s) }
func (d *ids) link(s string) graph.LinkID     { return idOf[graph.LinkKind](d, s) }
func (d *ids) version(s string) graph.VersionID {
	return idOf[graph.VersionKind](d, s)
}

func (d *ids) kgRow(w wKG) graph.KG {
	return graph.KG{ID: d.kg(w.ID), Name: w.Name, Role: w.Role, State: w.State, BaseKGID: d.kg(w.BaseKGID),
		DefinitionSetHash: w.DefinitionSetHash, Generation: w.Generation, CreatedAt: w.CreatedAt}
}

func (d *ids) head(w wHead) graph.Head {
	h := graph.Head{KG: d.kg(w.KG), AppliedSeq: w.AppliedSeq, ReadableThrough: w.ReadableThrough, IngestSeq: w.IngestSeq,
		Hash: w.Hash, InputDigest: w.InputDigest, ApplyID: w.ApplyID, NextChunkNo: w.NextChunkNo, ChunkCount: w.ChunkCount,
		Rollback: w.Rollback, LastApplyID: w.LastApplyID, AppliedSemanticVersion: w.AppliedSemanticVersion}
	return h.Clone()
}

func (d *ids) light(w wLight) graph.LightChange {
	return graph.LightChange{Seq: w.Seq, Object: d.object(w.Object), AttrNo: w.AttrNo, ValueMark: w.ValueMark}
}

func (d *ids) ident(w wIdentRec) graph.IdentifierRecord {
	return graph.IdentifierRecord{Object: d.object(w.Object), Scheme: w.Scheme, Value: w.Value, Scope: w.Scope, SeqFrom: w.SeqFrom, SeqTo: w.SeqTo}
}

func (d *ids) event(w wEvent) graph.IdentityEvent {
	e := graph.IdentityEvent{Seq: w.Seq, Kind: w.Kind, Retired: d.object(w.Retired), Into: d.object(w.Into)}
	for _, m := range w.LinkMap {
		e.LinkMap = append(e.LinkMap, graph.LinkMapping{Old: d.link(m.Old), New: d.link(m.New)})
	}
	return e
}

func (d *ids) versionRow(w wVersion) graph.Version {
	return graph.Version{KG: d.kg(w.KG), No: w.No, ID: d.version(w.ID), Prev: d.version(w.Prev), SealedSeq: w.SealedSeq,
		Generation: w.Generation, DefinitionSetHash: w.DefinitionSetHash, EngineVersion: w.EngineVersion,
		HashRuleVersion: w.HashRuleVersion, InputDigest: w.InputDigest, GraphHash: w.GraphHash, Rows: w.Rows,
		Parts: w.Parts, SealedAt: w.SealedAt, Kind: w.Kind}
}
