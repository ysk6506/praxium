package worldgen

import (
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Names of the files of the change source.
const (
	ChangesFile  = "changes/events.jsonl"
	ExpectedFile = "changes/expected.jsonl"
)

// baseTime is the source time of the first event: a fixed instant, never the clock.
var baseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Op is one effect of a planned change on the truth.
type Op struct {
	Op    string // added, modified, deleted
	Kind  string // object, link
	Type  string // type id
	ID    string // truth id
	From  string
	To    string
	Props []nameVal
	IDs   []ident
	At    int64 // seconds after baseTime
}

type nameVal struct {
	name string
	v    val
}

// Change is one planned mutation of the world: a kind and its effects, in plan order.
type Change struct {
	Seq  int
	Kind string
	Ops  []Op
}

// capture keeps what the generator makes, for the plan to turn into events.
type capture struct {
	objs []*Object
	lnks []*Link
}

func (c *capture) Scope(Scope, int, int, bool) error { return nil }
func (c *capture) Object(o *Object) error            { c.objs = append(c.objs, o.clone()); return nil }
func (c *capture) Link(l *Link) error                { c.lnks = append(c.lnks, l.clone()); return nil }

func propsOf(t *typeSpec, ps []propVal) []nameVal {
	out := make([]nameVal, 0, len(ps))
	for _, p := range ps {
		out = append(out, nameVal{t.props[p.idx].name, p.v})
	}
	return out
}

func lpropsOf(t *linkSpec, ps []propVal) []nameVal {
	out := make([]nameVal, 0, len(ps))
	for _, p := range ps {
		out = append(out, nameVal{t.props[p.idx].name, p.v})
	}
	return out
}

func (c *capture) ops(at int64) []Op {
	var out []Op
	for _, o := range c.objs {
		out = append(out, Op{Op: "added", Kind: "object", Type: o.T.id, ID: o.ID, Props: propsOf(o.T, o.Props), IDs: o.IDs, At: at})
	}
	for _, l := range c.lnks {
		out = append(out, Op{Op: "added", Kind: "link", Type: l.T.id, ID: l.ID, From: l.From.id, To: l.To.id, Props: lpropsOf(l.T, l.Props), At: at})
	}
	c.objs, c.lnks = c.objs[:0], c.lnks[:0]
	return out
}

// planner makes the plan: every target is found by arithmetic on the layout,
// so the plan needs no copy of the world.
type planner struct {
	w     *World
	g     *gen
	cap   *capture
	d     draw
	dead  map[string]bool
	host  map[string]string // current host of VMs that moved
	adds  map[string]int
	plan  []Change
	sites []int
}

// Plan makes the mutation plan of the world: ChangePlan.Events changes.
func (w *World) Plan() ([]Change, error) {
	cp := &capture{}
	pl := &planner{w: w, cap: cp, g: newGen(w.P, w.sch, w.keys, w.Lay, []Observer{cp}), d: newDraw(w.keys.change),
		dead: map[string]bool{}, host: map[string]string{}, adds: map[string]int{}}
	for s, sh := range w.Lay.Sites {
		if sh.VMs > 0 {
			pl.sites = append(pl.sites, s)
		}
	}
	if len(pl.sites) == 0 {
		return nil, nil
	}
	for i := 0; i < w.P.Changes.Events && pl.g.err == nil; i++ {
		pl.next(i)
	}
	return pl.plan, pl.g.err
}

func (pl *planner) kind() string {
	c := pl.w.P.Changes
	shares := []float64{c.VMMoveShare, c.ReplaceShare, c.LinkChangeShare, c.RetireShare, c.FlapShare, c.AddShare}
	names := []string{"vm_move", "replace", "link_change", "retire", "flap", "add"}
	total := 0.0
	for _, s := range shares {
		total += s
	}
	x := pl.d.float() * total
	for i, s := range shares {
		if x < s {
			return names[i]
		}
		x -= s
	}
	return names[len(names)-1]
}

func (pl *planner) next(seq int) {
	at := int64(seq)*60 + int64(pl.d.intn(10))
	k := pl.kind()
	vm := pl.target()
	if !vm.ok || pl.dead[vmID(vm.a, vm.srv, vm.v)] {
		return
	}
	var ops []Op
	switch k {
	case "vm_move":
		ops = pl.move(vm, at)
	case "replace":
		ops = pl.replace(vm, seq, at)
	case "link_change":
		ops = pl.linkChange(vm, at)
	case "retire":
		ops = pl.retire(vm, at)
	case "flap":
		ops = pl.flap(vm, at)
	default:
		ops = pl.add(vm, at)
	}
	if len(ops) > 0 {
		pl.plan = append(pl.plan, Change{Seq: seq, Kind: k, Ops: ops})
	}
}

func (pl *planner) target() vmAt {
	s := pl.sites[pl.d.intn(len(pl.sites))]
	return pl.g.pickVM(s, &pl.d)
}

func (pl *planner) hostOfVM(id string, at vmAt) string {
	if h, ok := pl.host[id]; ok {
		return h
	}
	return hostID(at.a, at.srv)
}

func (pl *planner) move(vm vmAt, at int64) []Op {
	id := vmID(vm.a, vm.srv, vm.v)
	to := pl.target()
	if !to.ok || to.a.s != vm.a.s {
		return nil
	}
	old, nw := pl.hostOfVM(id, vm), hostID(to.a, to.srv)
	if old == nw {
		return nil
	}
	pl.host[id] = nw
	lt := pl.g.sch.links[lRunsOn]
	return []Op{
		{Op: "deleted", Kind: "link", Type: lt.id, ID: linkID(lRunsOn, id, old), From: id, To: old, At: at},
		{Op: "added", Kind: "link", Type: lt.id, ID: linkID(lRunsOn, id, nw), From: id, To: nw, At: at,
			Props: []nameVal{{"placement", strVal("current")}}},
	}
}

func (pl *planner) replace(vm vmAt, seq int, at int64) []Op {
	srv := serverID(vm.a, vm.srv)
	old := child(srv, "mem", pl.d.intn(pl.w.P.MemoryModules))
	if pl.dead[old] {
		return nil
	}
	pl.dead[old] = true
	g := pl.g
	nid := old + "~r" + strconv.Itoa(seq)
	g.obj(tMemory, nid).set(kS("slot_name", "r"+strconv.Itoa(seq))).in(lPartOf, refOf(tServer, srv)).emit()
	ops := []Op{{Op: "deleted", Kind: "object", Type: g.sch.types[tMemory].id, ID: old, At: at}}
	return append(ops, pl.cap.ops(at+1)...)
}

func (pl *planner) linkChange(vm vmAt, at int64) []Op {
	srv := serverID(vm.a, vm.srv)
	side := pl.d.intn(2)
	tor := child(vm.a.id(), "tor", side)
	lt := pl.g.sch.links[lUplinkedVia]
	return []Op{{Op: "modified", Kind: "link", Type: lt.id, ID: linkID(lUplinkedVia, srv, tor), From: srv, To: tor, At: at,
		Props: []nameVal{{"network_role", strVal(pickOf([]string{"backup", "management", "storage"}, pl.d.u64()))}}}}
}

func (pl *planner) retire(vm vmAt, at int64) []Op {
	id := vmID(vm.a, vm.srv, vm.v)
	if vm.v == 0 && pl.w.Lay.hasNode(vm.a.s, vm.a.hall, vm.a.rk, vm.srv) {
		return nil
	}
	sch := pl.g.sch
	var ops []Op
	for _, c := range []struct {
		t   int
		tag string
	}{{tOS, "os"}, {tVNic, "vnic"}, {tVDisk, "vdsk"}} {
		ops = append(ops, Op{Op: "deleted", Kind: "object", Type: sch.types[c.t].id, ID: child(id, c.tag, 0), At: at})
		pl.dead[child(id, c.tag, 0)] = true
	}
	pl.dead[id] = true
	return append(ops, Op{Op: "deleted", Kind: "object", Type: sch.types[tVM].id, ID: id, At: at})
}

var powerStates = []string{"on", "off", "suspended"}

func (pl *planner) flap(vm vmAt, at int64) []Op {
	id := vmID(vm.a, vm.srv, vm.v)
	t := pl.g.sch.types[tVM]
	var ops []Op
	start := pl.d.intn(len(powerStates))
	for i := 0; i < pl.w.P.Changes.FlapsPerObject; i++ {
		ops = append(ops, Op{Op: "modified", Kind: "object", Type: t.id, ID: id, At: at + int64(i),
			Props: []nameVal{{"power_state", strVal(powerStates[(start+i)%len(powerStates)])}}})
	}
	return ops
}

func (pl *planner) add(vm vmAt, at int64) []Op {
	hostKey := serverID(vm.a, vm.srv)
	n := pl.adds[hostKey]
	pl.adds[hostKey]++
	v := pl.w.Lay.vmsOn(vm.a.s, vm.a.hall, vm.a.rk, vm.srv) + n
	id := vmID(vm.a, vm.srv, v)
	pl.g.obj(tVM, id).set(kS("name", "vm-"+id)).in(lRunsOn, refOf(tHost, hostID(vm.a, vm.srv))).emit()
	return pl.cap.ops(at)
}

// event is one line of the change source before it is written.
type event struct {
	id   string
	op   Op
	late bool
}

func eventTime(at int64) string {
	return baseTime.Add(time.Duration(at) * time.Second).Format(time.RFC3339)
}

func appendEvent(b []byte, id string, op Op) []byte {
	b = append(b, `{"event_id":`...)
	b = appendJSONString(b, id)
	b = append(b, `,"source":"`+SrcFeed+`","source_time":"`...)
	b = append(b, eventTime(op.At)...)
	b = append(b, `","op":"`...)
	b = append(b, op.Op...)
	b = append(b, `","kind":"`...)
	b = append(b, op.Kind...)
	b = append(b, `","type":"`...)
	b = append(b, op.Type...)
	b = append(b, `","id":`...)
	b = appendJSONString(b, feedID(op.Kind, op.ID))
	if op.Kind == "link" {
		b = append(b, `,"from":`...)
		b = appendJSONString(b, feedID("object", op.From))
		b = append(b, `,"to":`...)
		b = appendJSONString(b, feedID("object", op.To))
	}
	if len(op.Props) > 0 {
		b = append(b, `,"props":{`...)
		for i, p := range op.Props {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, p.name)
			b = append(b, ':')
			b = p.v.appendJSON(b)
		}
		b = append(b, '}')
	}
	if len(op.IDs) > 0 {
		b = append(b, `,"identifiers":{`...)
		for i, x := range op.IDs {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, x.scheme)
			b = append(b, ':')
			b = appendJSONString(b, x.value)
		}
		b = append(b, '}')
	}
	return append(b, '}', '\n')
}

func feedID(kind, id string) string {
	if kind == "link" {
		return srcID(SrcFeed, "fl", id)
	}
	return srcID(SrcFeed, "fd", id)
}

// WriteChanges writes the change stream and the expected final state. The
// stream is the plan in source-time order with the flaws of the parameters:
// late events, duplicates and unparseable lines; the flaws go to the log.
func (w *World) WriteChanges(st Store, log *ImperfectionLog) ([]FileInfo, error) {
	plan, err := w.Plan()
	if err != nil {
		return nil, err
	}
	fs := newFileSet(st)
	evs := w.events(plan)
	out := fs.open(ChangesFile, SrcFeed, "jsonl")
	im := w.P.Imperfections
	d := newDraw(mix(w.keys.change, 0x6368))
	var buf []byte
	for i, e := range evs {
		buf = appendEvent(buf[:0], e.id, e.op)
		if e.late {
			log.add(Imperfection{"late_event", SrcFeed, ChangesFile, e.id, "the event arrives after newer ones; where its source_time is older than what was already applied to the same entity it is ignored"})
		}
		if d.chance(im.UnparseableRatio) {
			out.write([]byte(`{"event_id":"` + e.id + `","source_time":"trunc` + "\n"))
			log.add(Imperfection{"unparseable_line", SrcFeed, ChangesFile, strconv.Itoa(i), "the line is rejected and counted; the stream goes on"})
		}
		out.write(buf)
		if d.chance(im.DuplicateEventRatio) {
			out.write(buf)
			log.add(Imperfection{"duplicate_event", SrcFeed, ChangesFile, e.id, "the second delivery has the same event_id and is ignored"})
		}
	}
	w.writeExpected(fs.open(ExpectedFile, SrcFeed, "jsonl"), plan)
	return fs.closeAll()
}

// events turns the plan into delivery order: by source time, except that some
// events arrive late (after newer ones of other entities).
func (w *World) events(plan []Change) []event {
	var evs []event
	var keys []float64
	d := newDraw(mix(w.keys.change, 0x6c617465))
	n := 0
	for _, c := range plan {
		for j, op := range c.Ops {
			id := "ev-" + strconv.Itoa(c.Seq) + "." + strconv.Itoa(j)
			key := float64(n)
			late := d.chance(w.P.Imperfections.LateEventRatio)
			if late {
				key += 2 + float64(d.intn(30)) + 0.5
			}
			evs = append(evs, event{id, op, late})
			keys = append(keys, key)
			n++
		}
	}
	idx := make([]int, len(evs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return keys[idx[a]] < keys[idx[b]] })
	out := make([]event, len(evs))
	for i, j := range idx {
		out[i] = evs[j]
	}
	return out
}

// writeExpected writes, for every entity the plan touched, its final state
// in truth ids: what a correct replay of the stream must end with.
func (w *World) writeExpected(o *outFile, plan []Change) {
	final := map[string]Op{}
	var order []string
	for _, c := range plan {
		for _, op := range c.Ops {
			key := op.Kind + ":" + op.ID
			prev, seen := final[key]
			if !seen {
				order = append(order, key)
				prev = Op{Kind: op.Kind, Type: op.Type, ID: op.ID, From: op.From, To: op.To, Op: "modified"}
			}
			prev.Op = op.Op
			prev.Props = mergeProps(prev.Props, op.Props)
			final[key] = prev
		}
	}
	var b []byte
	for _, key := range order {
		op := final[key]
		b = append(b[:0], `{"kind":"`+op.Kind+`","id":`...)
		b = appendJSONString(b, op.ID)
		b = append(b, `,"final":"`...)
		b = append(b, map[string]string{"added": "present", "modified": "present", "deleted": "absent"}[op.Op]...)
		b = append(b, `","type":"`...)
		b = append(b, op.Type...)
		b = append(b, `","props":{`...)
		for i, p := range op.Props {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, p.name)
			b = append(b, ':')
			b = p.v.appendJSON(b)
		}
		o.write(append(b, "}}\n"...))
	}
}

func mergeProps(a, b []nameVal) []nameVal {
	for _, p := range b {
		found := false
		for i := range a {
			if a[i].name == p.name {
				a[i], found = p, true
			}
		}
		if !found {
			a = append(a, p)
		}
	}
	return a
}

var _ io.Writer = nopFile{}
var _ = kernel.CodeNotFound
