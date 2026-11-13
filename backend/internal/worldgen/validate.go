package worldgen

import (
	"bufio"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Record is one line of the truth file as a reader sees it.
type Record struct {
	Kind          string                     `json:"kind"`
	Format        string                     `json:"format"`
	Version       int                        `json:"version"`
	ID            string                     `json:"id"`
	Type          string                     `json:"type"`
	From          string                     `json:"from"`
	To            string                     `json:"to"`
	Props         map[string]json.RawMessage `json:"props"`
	Unknown       []string                   `json:"unknown"`
	Assumed       []string                   `json:"assumed"`
	Identifiers   map[string]string          `json:"identifiers"`
	Grade         string                     `json:"grade"`
	Basis         string                     `json:"basis"`
	NotIngestible string                     `json:"not_ingestible"`
}

// ReadTruth reads a truth file and calls fn for every object and link.
// The record is reused between calls.
func ReadTruth(r io.Reader, fn func(*Record) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<24)
	line := 0
	for sc.Scan() {
		line++
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return kernel.Errorf(kernel.CodeInvalidValue, "truth line %d: %v", line, err)
		}
		if line == 1 {
			if rec.Kind != "header" || rec.Format != TruthFormat || rec.Version != FormatVersion {
				return kernel.Errorf(kernel.CodeInvalidValue, "truth line 1: not a %s version %d header", TruthFormat, FormatVersion)
			}
			continue
		}
		if err := fn(&rec); err != nil {
			return err
		}
	}
	return sc.Err()
}

// Report is the result of checking a truth file against the registry.
type Report struct {
	Objects  int64
	Links    int64
	Problems []string
	Dropped  int64
}

// OK says that nothing was found.
func (r *Report) OK() bool { return len(r.Problems) == 0 && r.Dropped == 0 }

const maxProblems = 50

func (r *Report) problem(format string, args ...any) {
	if len(r.Problems) < maxProblems {
		r.Problems = append(r.Problems, kernel.Errorf(kernel.CodeInvalidValue, format, args...).Error())
	} else {
		r.Dropped++
	}
}

// objInfo is what the validator keeps of an object: its type, its identity
// key texts and, per context link, the parent.
type objInfo struct {
	t       *typeSpec
	props   map[string]string
	ids     map[string]string
	parents map[int]string
	ni      bool
	members int
	// sets holds, per set key link (twice the link type, plus one for the
	// links that arrive), the ids of the objects at the other end.
	sets map[int][]string
	// consumers counts the links of other meanings that arrive at a group.
	consumers int
	member    bool
}

type validator struct {
	sch  *schema
	rep  *Report
	objs map[string]*objInfo
	lids map[string]bool
	lnks []linkRef
}

type linkRef struct {
	lt       int
	from, to string
}

// Validate checks a truth file against the standard model: every type and
// property exists and holds a valid value, every link has a binding in the
// registry and endpoints that exist, identity keys are unique, and every
// redundancy group has at least min_alive members. It keeps the identity data
// of every object, so it is meant for worlds that fit in memory.
func Validate(reg *model.Registry, r io.Reader) (*Report, error) {
	sch, err := newSchemaFor(reg)
	if err != nil {
		return nil, err
	}
	v := &validator{sch: sch, rep: &Report{}, objs: map[string]*objInfo{}, lids: map[string]bool{}}
	if err := ReadTruth(r, v.record); err != nil {
		return v.rep, err
	}
	v.finish()
	return v.rep, nil
}

func (v *validator) record(rec *Record) error {
	switch rec.Kind {
	case "object":
		v.rep.Objects++
		v.object(rec)
	case "link":
		v.rep.Links++
		v.link(rec)
	default:
		v.rep.problem("unknown record kind %q", rec.Kind)
	}
	return nil
}

func (v *validator) object(rec *Record) {
	t, ok := v.sch.byID[rec.Type]
	if !ok {
		v.rep.problem("object %s: unknown type %s", rec.ID, rec.Type)
		return
	}
	if _, dup := v.objs[rec.ID]; dup {
		v.rep.problem("object %s: duplicate id", rec.ID)
		return
	}
	info := &objInfo{t: t, props: map[string]string{}, ids: rec.Identifiers, parents: map[int]string{}, ni: rec.NotIngestible != ""}
	v.objs[rec.ID] = info
	unknown := map[string]bool{}
	for _, n := range rec.Unknown {
		unknown[n] = true
	}
	for name, raw := range rec.Props {
		idx, ok := t.byName[name]
		if !ok {
			v.rep.problem("object %s: %s has no property %s", rec.ID, t.name, name)
			continue
		}
		info.props[name] = v.checkValue(rec.ID, &t.props[idx], raw)
	}
	for i := range t.props {
		ps := &t.props[i]
		if _, has := rec.Props[ps.name]; ps.required && !has && !unknown[ps.name] {
			v.rep.problem("object %s: required property %s is missing", rec.ID, ps.name)
		}
	}
}

// checkValue checks one value against its property and returns its text.
func (v *validator) checkValue(id string, ps *propSpec, raw json.RawMessage) string {
	var s string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			v.rep.problem("object %s: property %s: %v", id, ps.name, err)
			return ""
		}
	} else {
		s = string(raw)
	}
	v.checkKind(id, ps, s)
	if n, err := model.NormalizeValue(ps.vt, s); err != nil || n != s {
		v.rep.problem("object %s: property %s: %q is not a normal %s", id, ps.name, s, ps.vt)
	}
	return s
}

// checkKind checks the kinds of value that have a fixed form.
func (v *validator) checkKind(id string, ps *propSpec, s string) {
	switch ps.hint {
	case hEnum:
		if !containsString(ps.enum, s) {
			v.rep.problem("object %s: property %s: %q is not one of %v", id, ps.name, s, ps.enum)
		}
	case hInt:
		if _, err := strconv.ParseInt(s, 10, 64); err != nil {
			v.rep.problem("object %s: property %s: %q is not an integer", id, ps.name, s)
		}
	case hBool:
		if s != "true" && s != "false" {
			v.rep.problem("object %s: property %s: %q is not a boolean", id, ps.name, s)
		}
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (v *validator) link(rec *Record) {
	lt, ok := v.sch.linkBy[rec.Type]
	if !ok {
		v.rep.problem("link %s: unknown type %s", rec.ID, rec.Type)
		return
	}
	if v.lids[rec.ID] {
		v.rep.problem("link %s: duplicate id", rec.ID)
	}
	v.lids[rec.ID] = true
	for name := range rec.Props {
		if _, ok := lt.byName[name]; !ok {
			v.rep.problem("link %s: %s has no property %s", rec.ID, lt.name, name)
		}
	}
	v.lnks = append(v.lnks, linkRef{lt.idx, rec.From, rec.To})
}

// finish checks what needs the whole file: endpoints and bindings, the
// parents of context keys, the uniqueness of identity keys and the groups.
func (v *validator) finish() {
	for _, l := range v.lnks {
		from, to := v.objs[l.from], v.objs[l.to]
		if from == nil || to == nil {
			v.rep.problem("link %s %s -> %s: an end does not exist", linkNames[l.lt], l.from, l.to)
			continue
		}
		if !v.sch.bind(from.t.idx, to.t.idx, l.lt) {
			v.rep.problem("link %s %s -> %s: the standard model has no binding for %s to %s", linkNames[l.lt], l.from, l.to, from.t.name, to.t.name)
		}
		from.parents[l.lt] = l.to
		from.addSet(l.lt, false, from.t.setOut, l.to)
		to.addSet(l.lt, true, to.t.setIn, l.from)
		switch l.lt {
		case lMemberOf:
			to.members++
			from.member = true
		case lPartOf:
		default:
			to.consumers++
		}
	}
	v.supply()
	seen := map[string]string{}
	for id, o := range v.objs {
		v.keys(id, o, seen)
		if o.ni {
			continue
		}
		v.group(id, o)
	}
}

func (o *objInfo) addSet(lt int, incoming bool, mask uint32, other string) {
	if mask&(1<<uint(lt)) == 0 {
		return
	}
	if o.sets == nil {
		o.sets = map[int][]string{}
	}
	k := setKey(lt, incoming)
	o.sets[k] = append(o.sets[k], other)
}

func setKey(lt int, incoming bool) int {
	if incoming {
		return lt*2 + 1
	}
	return lt * 2
}

// supply checks that nothing is supplied by a single member of a group: a
// consumer depends on the group (power, cooling and network supply).
func (v *validator) supply() {
	for _, l := range v.lnks {
		if l.lt != lPoweredBy && l.lt != lCooledBy && l.lt != lUplinkedVia {
			continue
		}
		if to := v.objs[l.to]; to != nil && to.member && !to.ni {
			v.rep.problem("link %s %s -> %s: the end is a member of a group, so the link should go to the group", linkNames[l.lt], l.from, l.to)
		}
	}
}

// keys checks that the identity keys of an ingestible object are unique.
func (v *validator) keys(id string, o *objInfo, seen map[string]string) {
	if o.ni {
		return
	}
	for ki, k := range o.t.keys {
		text, ok := v.keyText(o, k)
		if !ok {
			continue
		}
		full := o.t.name + "#" + strconv.Itoa(ki) + "#" + text
		if other, dup := seen[full]; dup {
			v.rep.problem("objects %s and %s have the same identity key %s", other, id, text)
			continue
		}
		seen[full] = id
	}
}

func (v *validator) keyText(o *objInfo, k keySpec) (string, bool) {
	var parts []string
	if len(k.comps) == 0 {
		x, ok := o.ids[k.scheme]
		return x, ok
	}
	for _, c := range k.comps {
		if c.set {
			x, ok := o.setText(c)
			if !ok {
				return "", false
			}
			parts = append(parts, x)
			continue
		}
		if c.prop >= 0 {
			x, ok := o.props[o.t.props[c.prop].name]
			if !ok {
				return "", false
			}
			parts = append(parts, x)
			continue
		}
		p, ok := o.parents[c.ctx]
		if !ok {
			return "", false
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "|"), true
}

// setText is the key text of a set component: the sorted ids joined, or not
// formed when there are none or not the exact number the key demands.
func (o *objInfo) setText(c keyComp) (string, bool) {
	ids := slices.Clone(o.sets[setKey(c.ctx, c.incoming)])
	if len(ids) == 0 || (c.size > 0 && len(ids) != c.size) {
		return "", false
	}
	slices.Sort(ids)
	return strings.Join(ids, ","), true
}

// group checks the groups: min_alive is at least 1 and no more than the members.
func (v *validator) group(id string, o *objInfo) {
	s, ok := o.props["min_alive"]
	if !ok {
		return
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > o.members {
		v.rep.problem("group %s (%s): min_alive %s with %d members", id, o.t.name, s, o.members)
	}
	if o.consumers == 0 {
		v.rep.problem("group %s (%s): nothing depends on the group", id, o.t.name)
	}
}
