package ingest

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// formedKey is one identity key of a row with its values tidied by the value types of its parts.
type formedKey struct {
	plan  *keyPlan
	parts []graph.KeyPart
	sig   string
}

// formKeys forms every identity key of a feed that the row can give, in priority order. A key the row cannot give
// (a part is empty, or its value does not fit the value type) is left out, and why is returned for the problem
// list when no key can be formed at all.
func formKeys(fp *feedPlan, reg *model.Registry, row Row) (formed []formedKey, why []string) {
	for i := range fp.keys {
		k := &fp.keys[i]
		fk := formedKey{plan: k}
		ok := true
		sigParts := []string{fp.typ.ID.String(), k.name}
		for _, kp := range k.parts {
			cell := strings.TrimSpace(row.Fields[kp.column])
			if cell == "" {
				why = append(why, "key "+k.name+": column "+kp.column+" is empty")
				ok = false
				break
			}
			v, err := model.NormalizeValue(kp.vt, cell)
			if err != nil || v == "" {
				why = append(why, "key "+k.name+": column "+kp.column+" has a value that does not fit its value type")
				ok = false
				break
			}
			fk.parts = append(fk.parts, graph.KeyPart{Kind: kp.kind, Ref: kp.ref, Value: v})
			sigParts = append(sigParts, v)
		}
		if ok {
			fk.sig = strings.Join(sigParts, keySep)
			formed = append(formed, fk)
		}
	}
	return formed, why
}

// resolution is the result of identifying a row: the object it is, whether the object is new, and the keys of
// the row that stand for another object than the one chosen.
type resolution struct {
	id        graph.ObjectID
	key       []graph.KeyPart
	isNew     bool
	conflicts []string
}

// identify matches the formed keys of a row with the identity index in priority order (kg-model.md 10.2, the
// deterministic rules only). The first key that is known gives the object; when none is known and the feed may
// create objects, a new object is made whose id comes from the first key; the other keys of the row are bound to the
// same object. A key that is already bound to another object is reported and left alone: merging two objects is
// not in the slice (ADR-0033).
func identify(fp *feedPlan, st *State, formed []formedKey) (resolution, bool, error) {
	var res resolution
	found := false
	for _, fk := range formed {
		if text, ok := st.Identity[fk.sig]; ok {
			id, err := parseObject(text)
			if err != nil {
				return res, false, err
			}
			if !found {
				res.id, found = id, true
			} else if id != res.id {
				res.conflicts = append(res.conflicts, "key "+fk.plan.name+" already stands for another object")
			}
		}
	}
	if !found {
		if !fp.feed.CreatesObjects {
			return res, false, nil
		}
		id, err := graph.NewObjectID(fp.typ.ID, formed[0].parts)
		if err != nil {
			return res, false, err
		}
		res.id, res.key, res.isNew, found = id, formed[0].parts, true, true
	}
	for _, fk := range formed {
		if _, ok := st.Identity[fk.sig]; !ok {
			st.Identity[fk.sig] = res.id.String()
		}
	}
	return res, found, nil
}
