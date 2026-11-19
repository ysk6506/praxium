package ingest

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// leftOutSuffix is what the problem of a cell that is wrong adds to the reason of the cell: the object is made without the
// value, as the current flow says it.
const leftOutSuffix = "; the object is made without this value (the attribute is empty)"

// scaleFormed is an identity key a row gave: its place in the feed's keys, the parts of the key itself (the properties or
// the identifier) and the parts of the key of each parent, nearest first.
type scaleFormed struct {
	ord     int
	own     []graph.KeyPart
	parents [][]graph.KeyPart
}

// ownFeed is the plan with which the parts of a key are formed: a feed of the type of the object, with this key only and
// without its context. formKeys reads cells by it, so the cells of a part mean what they mean everywhere else.
func ownFeed(typ model.ObjectType, kp *keyPlan) *feedPlan {
	return &feedPlan{typ: typ, typeOK: true, keys: []keyPlan{{name: kp.name, priority: kp.priority, parts: kp.parts, scheme: kp.scheme}}}
}

// formPart forms the parts of one key by formKeys.
func (pr *Preparer) formPart(own *feedPlan, row Row) ([]graph.KeyPart, []string, bool) {
	formed, why := formKeys(own, pr.plan.reg, row)
	if len(formed) == 0 {
		return nil, why, false
	}
	return formed[0].parts, nil, true
}

// formOne forms a key of a feed from the row: the parents it needs first, then its own parts. A key that a cell cannot
// give is left out, with why.
func (pr *Preparer) formOne(pk *preparedKey, row Row) (scaleFormed, []string, bool) {
	var f scaleFormed
	for _, pp := range pk.parents {
		parts, why, ok := pr.formPart(pp.own, row)
		if !ok {
			return f, why, false
		}
		f.parents = append(f.parents, parts)
	}
	own, why, ok := pr.formPart(pk.own, row)
	f.own = own
	return f, why, ok
}

// formAll forms every key of a feed that the row can give, in the order the feed tries them, and why when none can.
func (pr *Preparer) formAll(pf preparedFeed, row Row) (formed []scaleFormed, why []string) {
	for ord := range pf.keys {
		f, w, ok := pr.formOne(&pf.keys[ord], row)
		if !ok {
			why = append(why, w...)
			continue
		}
		f.ord = ord
		formed = append(formed, f)
	}
	return formed, why
}

// badField is the column that keeps the first key of a feed from being formed: the first column, parents first, whose
// cell is empty or does not fit its value type. It asks formKeys about each column alone, so the answer is the one
// formKeys gives for the key.
func (pr *Preparer) badField(pk *preparedKey, row Row) string {
	plans := make([]*feedPlan, 0, len(pk.parents)+1)
	for _, pp := range pk.parents {
		plans = append(plans, pp.own)
	}
	plans = append(plans, pk.own)
	for _, own := range plans {
		for _, part := range own.keys[0].parts {
			one := &feedPlan{typ: own.typ, keys: []keyPlan{{name: own.keys[0].name, parts: []keyPart{part}}}}
			if formed, _ := formKeys(one, pr.plan.reg, row); len(formed) == 0 {
				return part.column
			}
		}
	}
	return ""
}

// keyParts are the components of a key as the identity index has them: the context first, then the parts. The context has
// no value here: the parent is found by the key of the parent, whose id is not known yet.
func keyParts(kp *keyPlan, own []graph.KeyPart) []graph.KeyPart {
	if kp.ctx == nil {
		return own
	}
	return append([]graph.KeyPart{{Kind: graph.PartContext, Ref: kp.ctx.link.ID.String()}}, own...)
}

// keySignature is the signature of a key (the kind, the reference and the tidied value of each component, the same
// things the signature of the current flow holds, with no key name and no type: the type is a column of its own) and its hash.
func keySignature(parts []graph.KeyPart) (string, KeyHash, error) {
	items := make([]any, len(parts))
	texts := make([]string, 0, 3*len(parts))
	for i, p := range parts {
		items[i] = []any{string(p.Kind), p.Ref, p.Value}
		texts = append(texts, string(p.Kind), p.Ref, p.Value)
	}
	h, err := kernel.ContentHash(items)
	if err != nil {
		return "", KeyHash{}, err
	}
	var out KeyHash
	copy(out[:], h[:])
	return strings.Join(texts, keySep), out, nil
}

// hashText is the hash a lookup key or an identifier value is stored by.
func hashText(text string) (KeyHash, error) {
	h, err := kernel.ContentHash(text)
	if err != nil {
		return KeyHash{}, err
	}
	var out KeyHash
	copy(out[:], h[:])
	return out, nil
}

// feedRow prepares what one feed says of a row: the keys it gives, the identifiers it claims and, for a row that is not
// held, its problems. A row that gives no key is a missing identity; a creating feed whose cell is wrong is told which.
// A feed that only attaches values reports no cell here: whether the row has an object to attach to is not known yet.
func (pr *Preparer) feedRow(sp *StagePage, no int, pf preparedFeed, v rowView) error {
	formed, why := pr.formAll(pf, v.row)
	for _, f := range formed {
		if err := pr.emitKey(sp, no, pf, f, v.held); err != nil {
			return err
		}
	}
	if !v.held {
		switch {
		case len(formed) == 0:
			pr.rowProblem(sp, no, pf.fp, v.row, StageProblem{Kind: ProblemMissingIdentity, Field: pr.badField(&pf.keys[0], v.row), Reason: noIdentityReason(why)})
		case pf.fp.feed.CreatesObjects:
			pr.valueProblems(sp, no, pf.fp, v.row)
		}
	}
	return pr.claims(sp, no, pf, formed, v)
}

// emitKey adds the key, and the parents it needs, to the page.
func (pr *Preparer) emitKey(sp *StagePage, no int, pf preparedFeed, f scaleFormed, held bool) error {
	pk, fp := &pf.keys[f.ord], pf.fp
	sig, hash, err := keySignature(keyParts(pk.kp, f.own))
	if err != nil {
		return err
	}
	key := StageKey{No: no, Feed: fp.index, Ord: f.ord, ModelOrd: pk.kp.modelOrd, TypeNo: fp.typ.TypeNo, Held: held, Sig: sig, SigHash: hash}
	if pk.kp.ctx != nil {
		key.LinkRef = pk.kp.ctx.link.ID.String()
	} else if fp.feed.CreatesObjects {
		id, err := graph.NewObjectID(fp.typ.ID, f.own)
		if err != nil {
			return err
		}
		key.Cand = id.String()
	}
	sp.Keys = append(sp.Keys, key)
	for level, pp := range pk.parents {
		psig, phash, err := keySignature(keyParts(pp.plan, f.parents[level]))
		if err != nil {
			return err
		}
		sp.Parents = append(sp.Parents, StageParent{No: no, Feed: fp.index, Ord: f.ord, Level: level + 1, LinkRef: pp.link.ID.String(), TypeNo: pp.typ.TypeNo, Sig: psig, SigHash: phash})
	}
	return nil
}

// valueProblems reports the cells of a row, read by the attributes of a feed, that are wrong, by the function that reads them.
func (pr *Preparer) valueProblems(sp *StagePage, no int, fp *feedPlan, row Row) {
	for i := range fp.attrs {
		a := &fp.attrs[i]
		if _, _, reason := cellValue(a.valueSpec, row.Fields[a.column]); reason != "" {
			pr.rowProblem(sp, no, fp, row, StageProblem{Kind: ProblemInvalidValue, Field: a.column, Reason: reason + leftOutSuffix})
		}
	}
}

// claims adds the identifiers the row says its object has: the identifier of a key that is a scheme, and the cells of the
// identifier columns of the feed that are not empty and fit the scheme.
func (pr *Preparer) claims(sp *StagePage, no int, pf preparedFeed, formed []scaleFormed, v rowView) error {
	add := func(scheme, value string) error {
		h, err := hashText(value)
		sp.Claims = append(sp.Claims, StageClaim{No: no, Feed: pf.fp.index, Scheme: scheme, Value: value, ValueHash: h, Held: v.held})
		return err
	}
	for _, f := range formed {
		if sch := pf.keys[f.ord].kp.scheme; !sch.IsZero() {
			if err := add(sch.String(), f.own[0].Value); err != nil {
				return err
			}
		}
	}
	for _, ip := range pf.fp.idents {
		cell := strings.TrimSpace(v.row.Fields[ip.column])
		if cell == "" {
			continue
		}
		if value, err := model.NormalizeValue(ip.vt, cell); err == nil && value != "" {
			if err := add(ip.scheme.String(), value); err != nil {
				return err
			}
		}
	}
	return nil
}

// linkEnds adds the key each end of a link definition finds the other end by, for the ends whose feed reads this dataset.
// A row whose cells give no key makes no end, and that is no problem (a row may have no rack).
func (pr *Preparer) linkEnds(sp *StagePage, no int, views []rowView) error {
	for _, pl := range pr.links {
		lp := pl.lp
		for _, e := range []struct {
			side string
			fp   *feedPlan
			cols []string
		}{{"from", lp.from, lp.def.From.Columns}, {"to", lp.to, lp.def.To.Columns}} {
			if pr.plan.roots[e.fp.feed.Dataset] != pr.dataset.ID {
				continue
			}
			v := views[pr.ids[e.fp.feed.Dataset]]
			lookup, ok := lp.linkKey(v.row, e.cols)
			if !ok {
				continue
			}
			h, err := hashText(lookup)
			if err != nil {
				return err
			}
			sp.Ends = append(sp.Ends, StageEnd{No: no, Def: pl.def, Side: e.side, Held: v.held, Lookup: lookup, LookupHash: h})
		}
	}
	return nil
}
