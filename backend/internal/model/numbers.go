package model

import "sort"

// Stable numbers (REQ-233). A type, a link type and a property each get a
// small positive integer that rows and projections use instead of the name.
// The rule that keeps them stable:
//
//   - a number written in the data is kept: it never changes;
//   - a definition without a number gets the next free one: above every number
//     in use and every reserved number, in ascending order of the ids of the
//     definitions that need one, so the same set of definitions always gets the
//     same numbers, whatever order the data files list them in;
//   - a number of a retired definition is listed as reserved and is never given again.
//
// Types, link types and properties count apart. A property number is unique
// across all properties, whoever owns them.

// numberSlot is one definition that has, or needs, a number.
type numberSlot struct {
	id  string
	no  *int
	use string
}

// slots lists the number-bearing definitions of each of the three kinds.
func (d *Definitions) slots() (types, links, attrs []numberSlot) {
	for i := range d.ObjectTypes {
		t := &d.ObjectTypes[i]
		types = append(types, numberSlot{t.ID.String(), &t.TypeNo, "object type " + t.ID.String()})
	}
	for i := range d.LinkTypes {
		l := &d.LinkTypes[i]
		links = append(links, numberSlot{l.ID.String(), &l.LinkTypeNo, "link type " + l.ID.String()})
		attrs = appendProps(attrs, l.Properties)
	}
	for i := range d.Traits {
		attrs = appendProps(attrs, d.Traits[i].Properties)
	}
	for i := range d.ObjectTypes {
		attrs = appendProps(attrs, d.ObjectTypes[i].Properties)
	}
	return types, links, attrs
}

func appendProps(slots []numberSlot, props []Property) []numberSlot {
	for i := range props {
		slots = append(slots, numberSlot{props[i].ID.String(), &props[i].AttrNo, "property " + props[i].ID.String()})
	}
	return slots
}

func (c *checker) checkNumbers() {
	types, links, attrs := c.defs.slots()
	c.checkNumberKind("type_no", types, c.defs.Reserved.TypeNos)
	c.checkNumberKind("link_type_no", links, c.defs.Reserved.LinkTypeNos)
	c.checkNumberKind("attr_no", attrs, c.defs.Reserved.AttrNos)
}

// checkNumberKind checks the numbers written in the data: positive, unique,
// not reserved. A zero is a number still to be assigned.
func (c *checker) checkNumberKind(name string, slots []numberSlot, reserved []int) {
	isReserved := map[int]bool{}
	for _, no := range reserved {
		if no < 1 {
			c.fail(CodeNumber, "reserved %s %d is not positive", name, no)
		}
		isReserved[no] = true
	}
	owner := map[int]string{}
	for _, slot := range slots {
		no := *slot.no
		switch {
		case no == 0:
		case no < 0:
			c.fail(CodeNumber, "%s has %s %d, which is not positive", slot.use, name, no)
		case isReserved[no]:
			c.fail(CodeNumber, "%s has %s %d, which is reserved and never given again", slot.use, name, no)
		case owner[no] != "":
			c.fail(CodeNumber, "%s and %s have the same %s %d", owner[no], slot.use, name, no)
		default:
			owner[no] = slot.use
		}
	}
}

// assignNumbers gives every definition without a number the next free one.
func assignNumbers(d *Definitions) {
	types, links, attrs := d.slots()
	assignKind(types, d.Reserved.TypeNos)
	assignKind(links, d.Reserved.LinkTypeNos)
	assignKind(attrs, d.Reserved.AttrNos)
}

func assignKind(slots []numberSlot, reserved []int) {
	highest := 0
	for _, no := range reserved {
		highest = max(highest, no)
	}
	var missing []numberSlot
	for _, slot := range slots {
		highest = max(highest, *slot.no)
		if *slot.no == 0 {
			missing = append(missing, slot)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].id < missing[j].id })
	for _, slot := range missing {
		highest++
		*slot.no = highest
	}
}

// NextNumbers are the numbers the next definition of each kind would get.
type NextNumbers struct {
	TypeNo, LinkTypeNo, AttrNo int
}

// Next returns the numbers a new object type, link type and property would
// get from this registry: one above the highest in use or reserved.
func (r *Registry) Next() NextNumbers {
	next := func(used map[int]bool, reserved []int) int {
		highest := 0
		for no := range used {
			highest = max(highest, no)
		}
		for _, no := range reserved {
			highest = max(highest, no)
		}
		return highest + 1
	}
	used := func(slots []numberSlot) map[int]bool {
		out := map[int]bool{}
		for _, slot := range slots {
			out[*slot.no] = true
		}
		return out
	}
	types, links, attrs := r.defs.slots()
	return NextNumbers{
		TypeNo:     next(used(types), r.defs.Reserved.TypeNos),
		LinkTypeNo: next(used(links), r.defs.Reserved.LinkTypeNos),
		AttrNo:     next(used(attrs), r.defs.Reserved.AttrNos),
	}
}
