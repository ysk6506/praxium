package query

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Defaults of the paging and the walk. They are guesses until measured (defaults.md).
const (
	DefaultLimit    = 100
	MaxLimit        = 1000
	DefaultMaxSteps = 1
	MaxSteps        = 3
	scanPage        = 1000
)

// Views of kg-queries.md 3.
const (
	ViewAsObserved = "as_observed"
	ViewAsDesigned = "as_designed"
)

// Shapes of kg-queries.md 3.
const (
	ShapeSummary = "summary"
	ShapeItems   = "items"
)

// Source is the part of the storage port the queries read.
type Source interface {
	GetKG(ctx context.Context, ref string) (graph.KG, error)
	Snapshot(ctx context.Context, rp graph.ReadPoint) (graph.Snapshot, error)
	Versions(ctx context.Context, kg graph.KGID) ([]graph.Version, error)
	WaitReadable(ctx context.Context, kg graph.KGID, minSeq int64, wait time.Duration) (int64, error)
}

// Common is the input every query shares (kg-queries.md 3). Zero values mean the default.
type Common struct {
	// KG is the id of the KG or its alias "main".
	KG string
	// KGVersionID reads the KG as of a sealed version; ChangeSeq reads it as of a change number. Not both.
	KGVersionID string
	ChangeSeq   *int64
	View        string
	// MinSeq with WaitTimeout waits until the change number is readable.
	MinSeq       int64
	WaitTimeout  time.Duration
	MaxStaleness time.Duration
	Limit        int
	Cursor       string
	Shape        string
}

// Engine answers the queries over a Source.
type Engine struct {
	src   Source
	reg   *model.Registry
	clock kernel.Clock
	types *typeIndex
	links map[int]model.LinkType
}

// New builds an engine over a source, the registry that gives meaning to the
// row numbers, and the clock that ages values.
func New(src Source, reg *model.Registry, clock kernel.Clock) (*Engine, error) {
	if src == nil || reg == nil || clock == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "an engine needs a source, a registry and a clock")
	}
	e := &Engine{src: src, reg: reg, clock: clock, types: newTypeIndex(reg), links: map[int]model.LinkType{}}
	for _, lt := range reg.LinkTypes() {
		e.links[lt.LinkTypeNo] = lt
	}
	return e, nil
}

// read is one opened read: the KG, the snapshot and the facts the envelope needs.
type read struct {
	kg     graph.KG
	snap   graph.Snapshot
	info   graph.SnapshotInfo
	cur    *cursor
	filter ReadFilter
	use    *usage
	hid    *hiddenCounts
	c      Common
	cache  map[graph.ObjectID]visible
}

type visible struct {
	row graph.ObjectRow
	ok  bool
	dec Decision
}

func (e *Engine) normalize(c Common) (Common, error) {
	if c.Limit == 0 {
		c.Limit = DefaultLimit
	}
	if c.Limit < 1 || c.Limit > MaxLimit {
		return c, kernel.Errorf(CodeLimitInvalid, "limit %d is outside 1 to %d", c.Limit, MaxLimit)
	}
	switch c.View {
	case "":
		c.View = ViewAsObserved
	case ViewAsObserved, ViewAsDesigned:
	default:
		return c, kernel.Errorf(CodeInput, "view %q is not as_designed or as_observed", c.View)
	}
	switch c.Shape {
	case "":
		c.Shape = ShapeSummary
	case ShapeSummary, ShapeItems:
	default:
		return c, kernel.Errorf(CodeInput, "shape %q is not summary or items", c.Shape)
	}
	if c.KGVersionID != "" && c.ChangeSeq != nil {
		return c, kernel.Errorf(CodeInput, "give kg_version_id or change_seq, not both")
	}
	return c, nil
}

// open reads the KG at the point the input names: the cursor's own point on a
// later page, else the version, the change number, or the latest readable one.
func (e *Engine) open(ctx context.Context, c Common, fp string, f ReadFilter) (*read, error) {
	c, err := e.normalize(c)
	if err != nil {
		return nil, err
	}
	kg, err := e.src.GetKG(ctx, c.KG)
	if err != nil {
		return nil, err
	}
	r := &read{kg: kg, c: c, filter: f, use: newUsage(e.clock.Now()), hid: newHidden(), cache: map[graph.ObjectID]visible{}}
	rp, err := e.readPoint(ctx, r, fp)
	if err != nil {
		return nil, err
	}
	if r.snap, err = e.src.Snapshot(ctx, rp); err != nil {
		return nil, err
	}
	r.info = r.snap.Info()
	return r, nil
}

// readPoint picks the point to read from the input and, on a later page, from the cursor.
func (e *Engine) readPoint(ctx context.Context, r *read, fp string) (graph.ReadPoint, error) {
	c, kg := r.c, r.kg
	rp := graph.ReadPoint{KG: kg.ID, Generation: kg.Generation, Kind: graph.ReadLatest}
	switch {
	case c.Cursor != "":
		cur, err := decodeCursor(c.Cursor, fp)
		if err != nil {
			return rp, err
		}
		if cur.Gen != kg.Generation {
			return rp, kernel.Errorf(graph.CodeGenerationChanged, "the cursor is of generation %d, the KG is at %d", cur.Gen, kg.Generation)
		}
		r.cur = &cur
		rp.Kind, rp.Seq = graph.ReadSeq, cur.Seq
		if cur.Version != "" {
			vid, err := kernel.ParseID[graph.VersionKind](cur.Version)
			if err != nil {
				return rp, kernel.Errorf(CodeCursorInvalid, "the cursor names a bad version")
			}
			rp.Kind, rp.Version = graph.ReadVersion, vid
		}
	case c.KGVersionID != "":
		vid, err := kernel.ParseID[graph.VersionKind](c.KGVersionID)
		if err != nil {
			return rp, kernel.Errorf(CodeInput, "kg_version_id %q is not a version id", c.KGVersionID)
		}
		rp.Kind, rp.Version = graph.ReadVersion, vid
	case c.ChangeSeq != nil:
		rp.Kind, rp.Seq = graph.ReadSeq, *c.ChangeSeq
	case c.MinSeq > 0:
		if _, err := e.src.WaitReadable(ctx, kg.ID, c.MinSeq, c.WaitTimeout); err != nil {
			return rp, err
		}
	}
	return rp, nil
}

// asOf builds the as_of of the envelope; it names the sealed version when the
// read point is one (or equals the change number of one).
func (e *Engine) asOf(ctx context.Context, r *read) (AsOf, error) {
	a := AsOf{KGID: r.kg.ID.String(), Generation: r.info.Generation, ChangeSeq: r.info.Seq}
	if !r.info.Version.IsZero() {
		v := r.info.Version.String()
		a.SealedVersion = &v
		return a, nil
	}
	versions, err := e.src.Versions(ctx, r.kg.ID)
	if err != nil {
		return a, err
	}
	for _, v := range versions {
		if v.Generation == r.info.Generation && v.SealedSeq == r.info.Seq {
			s := v.ID.String()
			a.SealedVersion = &s
		}
	}
	return a, nil
}

func (e *Engine) envelope(ctx context.Context, r *read, rc ResultComplete) (Envelope, error) {
	asOf, err := e.asOf(ctx, r)
	if err != nil {
		return Envelope{}, err
	}
	vol := "current"
	if r.info.Volatile == graph.VolatileOmitted {
		vol = "omitted"
	}
	return Envelope{
		AsOf: asOf, VolatileAsOf: vol, ServedBy: ServedBy{Role: "api_sql"},
		Freshness: r.use.freshness(), Evidence: r.use.evidence(), ResultComplete: rc,
		Hidden: r.hid.result(r.filter.Exact()), ModelLimits: []string{}, Warnings: []string{},
	}, nil
}

// designed reports whether a value of this basis counts in the view.
func (r *read) designed(b model.Basis) bool {
	return r.c.View != ViewAsDesigned || b == model.BasisConfigured || b == model.BasisDeclared
}

// judge decides whether the caller may read an object row, and with what cap.
func (e *Engine) judge(r *read, row graph.ObjectRow) Decision {
	info := e.types.byNo[row.TypeNo]
	if info == nil {
		return Decision{}
	}
	t := Target{Type: info.typ}
	if r.filter.NeedsScopeKeys() {
		t.ScopeKeys = map[string]string{}
		for _, pid := range e.reg.ScopeKeys(info.typ.ID) {
			if ref, ok := e.reg.Property(pid); ok {
				if a, has := row.Attrs[ref.Property.AttrNo]; has {
					t.ScopeKeys[ref.Property.Name] = valueText(a.Value)
				}
			}
		}
	}
	return r.filter.Decide(t)
}

// object reads one object row and the read decision for it; ok is false for an
// object that is not there at the read point.
func (e *Engine) object(ctx context.Context, r *read, id graph.ObjectID) (visible, error) {
	if v, hit := r.cache[id]; hit {
		return v, nil
	}
	row, ok, err := r.snap.Object(ctx, id)
	if err != nil {
		return visible{}, err
	}
	v := visible{row: row, ok: ok}
	if ok {
		v.dec = e.judge(r, row)
	}
	r.cache[id] = v
	return v, nil
}
