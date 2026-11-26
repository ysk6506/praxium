package ingest

import (
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// IsolationMaker is the IsolateMaker of a model: it decides who keeps an identifier (ElectIdent) and, for the cardinality, which binding a
// link is on (the one the graph checks a link by, model.Registry.BindingFor of the types of its ends) and which places of that binding the
// link takes. It is not safe for use by several goroutines.
type IsolationMaker struct {
	reg   *model.Registry
	binds map[isoBindKey]isoBind
}

type isoBindKey struct{ lt, src, dst int }

// isoBind is the binding of a link of a type between objects of two types: its id and whether it holds the start (out) or the end (in)
// to one link.
type isoBind struct {
	id      string
	card    string
	out, in bool
	ok      bool
}

var _ IsolateMaker = (*IsolationMaker)(nil)

// NewIsolationMaker makes the maker of the model of a KG.
func NewIsolationMaker(reg *model.Registry) *IsolationMaker {
	return &IsolationMaker{reg: reg, binds: map[isoBindKey]isoBind{}}
}

// NewIsolationMakerOf makes the maker of the model the plans of the sources of a KG were compiled with (one model: plans of two models are
// refused, since a binding of one is not the other's).
func NewIsolationMakerOf(plans map[int64]*Plan) (*IsolationMaker, error) {
	var reg *model.Registry
	for _, p := range plans {
		if reg != nil && p.reg.Hash() != reg.Hash() {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the plans of the sources of a KG are of two models")
		}
		reg = p.reg
	}
	if reg == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the fifth step is given no plan")
	}
	return NewIsolationMaker(reg), nil
}

// IsolateCardsOf says what the fifth step needs of the link types of the model of the plans: the ones that hold an end to one.
func IsolateCardsOf(plans map[int64]*Plan) []ExpandCard {
	srcs := make([]int64, 0, len(plans))
	for src := range plans {
		srcs = append(srcs, src)
	}
	sort.Slice(srcs, func(i, j int) bool { return srcs[i] < srcs[j] })
	if len(srcs) == 0 {
		return nil
	}
	return ExpandCards(plans[srcs[0]].reg)
}

// MakeIsolated decides a page: the identifiers in the phase idents, the places in the phases kept and fresh. The phase ends has nothing
// for Go and is given back as it is.
func (m *IsolationMaker) MakeIsolated(page IsolatePage) (IsolatedPage, error) {
	out := IsolatedPage{Unit: page.Unit, Phase: page.Phase, Through: page.Through, Last: page.Last}
	switch page.Phase {
	case IsolatePhaseIdents:
		return m.idents(page, out)
	case IsolatePhaseKept, IsolatePhaseFresh:
		out.Seats, out.Lost = m.seats(page)
	}
	return out, nil
}

// idents decides the identifiers of a page.
func (m *IsolationMaker) idents(page IsolatePage, out IsolatedPage) (IsolatedPage, error) {
	for _, k := range page.Keys {
		_, byKG, losers := ElectIdent(k)
		out.Keys++
		if len(losers) == 0 {
			continue
		}
		no, ok := page.Schemes[k.Scheme]
		if !ok {
			return IsolatedPage{}, kernel.Errorf(kernel.CodeInternal, "the scheme %q of a staged identifier has no number", k.Scheme)
		}
		h, err := hashText(k.Value)
		if err != nil {
			return IsolatedPage{}, err
		}
		for _, obj := range losers {
			out.Idents = append(out.Idents, IdentLoss{ObjNo: obj, Scheme: k.Scheme, Value: k.Value, Scope: k.Scope, SchemeNo: no, Hash: h, ByKG: byKG})
		}
	}
	return out, nil
}

// bindOf is the binding a link of the type lt from an object of the type src to one of the type dst is on.
func (m *IsolationMaker) bindOf(lt, src, dst int) isoBind {
	key := isoBindKey{lt, src, dst}
	if b, ok := m.binds[key]; ok {
		return b
	}
	var b isoBind
	l, okL := m.reg.LinkTypeByNo(lt)
	s, okS := m.reg.ObjectTypeByNo(src)
	d, okD := m.reg.ObjectTypeByNo(dst)
	if okL && okS && okD {
		if found, ok := m.reg.BindingFor(s.ID, d.ID, l.ID); ok {
			card := string(found.Cardinality)
			b = isoBind{id: found.ID.String(), card: card, out: strings.HasSuffix(card, ":1"), in: strings.HasPrefix(card, "1:"), ok: true}
		}
	}
	m.binds[key] = b
	return b
}

// placesOf are the places a link takes: the start when its binding holds the start to one, the end when it holds the end to one. A link
// that has no binding (the graph refuses it, which is not the cardinality) takes none.
func (m *IsolationMaker) placesOf(l SeatLink) (isoBind, []Seat) {
	b := m.bindOf(l.TypeNo, l.SrcType, l.DstType)
	var out []Seat
	if b.ok && b.out {
		out = append(out, Seat{Side: SeatOut, EndNo: l.Src, BindID: b.id, LinkNo: l.LinkNo})
	}
	if b.ok && b.in {
		out = append(out, Seat{Side: SeatIn, EndNo: l.Dst, BindID: b.id, LinkNo: l.LinkNo})
	}
	return b, out
}

type isoPlace struct {
	side   int16
	end    int64
	bindID string
}

// seats goes through the links of a page in their order: a link whose places are all free takes them, and a link one of whose places a
// link before it took (in an earlier page, in this page, or a link the KG keeps that the run does not touch) loses. Going through all the
// links once in one order is what makes the outcome hang on the order alone (a link that loses takes no place, so it does not keep a
// place from a link after it).
func (m *IsolationMaker) seats(page IsolatePage) ([]Seat, []SeatLoss) {
	taken := map[isoPlace]bool{}
	for _, s := range page.Taken {
		taken[isoPlace{s.Side, s.EndNo, s.BindID}] = true
	}
	for _, f := range page.Fixed {
		_, places := m.placesOf(f)
		for _, p := range places {
			taken[isoPlace{p.Side, p.EndNo, p.BindID}] = true
		}
	}
	var seats []Seat
	var lost []SeatLoss
	for _, l := range page.Links {
		b, places := m.placesOf(l)
		free := true
		for _, p := range places {
			if taken[isoPlace{p.Side, p.EndNo, p.BindID}] {
				free = false
			}
		}
		if !free {
			lost = append(lost, SeatLoss{LinkNo: l.LinkNo, Ref: l.Ref, Reason: m.cardinalityReason(l, b)})
			continue
		}
		for _, p := range places {
			taken[isoPlace{p.Side, p.EndNo, p.BindID}] = true
			seats = append(seats, p)
		}
	}
	return seats, lost
}

// cardinalityReason is the reason of the old flow (cardinalityReason of linkbuild.go): the link by the names of its type and of its objects.
func (m *IsolationMaker) cardinalityReason(l SeatLink, b isoBind) string {
	name := ""
	if lt, ok := m.reg.LinkTypeByNo(l.TypeNo); ok {
		name = lt.Name
	}
	return "the link " + name + " from " + l.SrcName + " to " + l.DstName + " would break the cardinality " + b.card +
		", so it was not made (the object already has such a link from another row)"
}
