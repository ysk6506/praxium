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
// across all properties, whoever owns them. The property numbers have two
// ranges: the standard and sys properties are at most OrgNumberBase-1, the
// properties of an organisation (namespace org or ext) are OrgNumberBase or
// more. The standard may then add numbers in any version without meeting a
// number an organisation has put in its rows.

// OrgNumberBase is the first number of the range of organisation properties.
const OrgNumberBase = 100000

// isOrgRange tells whether a property takes its number from the range of an organisation.
func isOrgRange(ns Namespace) bool { return ns == NamespaceOrg || ns == NamespaceExt }

// numberSlot is one definition that has, or needs, a number.
type numberSlot struct {
	id  string
	no  *int
	use string
	// ranged is set for properties, whose numbers have two ranges; high says which one.
	ranged, high bool
}

// slots lists the number-bearing definitions of each of the three kinds.
func (d *Definitions) slots() (types, links, attrs []numberSlot) {
	for i := range d.ObjectTypes {
		t := &d.ObjectTypes[i]
		types = append(types, numberSlot{id: t.ID.String(), no: &t.TypeNo, use: "object type " + t.ID.String()})
	}
	for i := range d.LinkTypes {
		l := &d.LinkTypes[i]
		links = append(links, numberSlot{id: l.ID.String(), no: &l.LinkTypeNo, use: "link type " + l.ID.String()})
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
		slots = append(slots, numberSlot{id: props[i].ID.String(), no: &props[i].AttrNo, use: "property " + props[i].ID.String(), ranged: true, high: isOrgRange(props[i].Namespace)})
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
		case slot.ranged && slot.high && no < OrgNumberBase:
			c.fail(CodeNumber, "%s has %s %d: an organisation property takes a number of %d or more", slot.use, name, no, OrgNumberBase)
		case slot.ranged && !slot.high && no >= OrgNumberBase:
			c.fail(CodeNumber, "%s has %s %d: a standard property takes a number below %d", slot.use, name, no, OrgNumberBase)
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
	if len(slots) > 0 && slots[0].ranged {
		assignRange(slots, reserved, false)
		assignRange(slots, reserved, true)
		return
	}
	assignRange(slots, reserved, false)
}

// assignRange gives the slots of one range (the only one, for types and link
// types) without a number the next free one above everything in use in it.
func assignRange(slots []numberSlot, reserved []int, high bool) {
	highest, missing := highestIn(slots, reserved, high)
	sort.Slice(missing, func(i, j int) bool { return missing[i].id < missing[j].id })
	for _, slot := range missing {
		highest++
		*slot.no = highest
	}
}

// inRange tells whether a number belongs to the range: the high one is
// OrgNumberBase and above, the low one is everything below it.
func inRange(no int, ranged, high bool) bool { return !ranged || high == (no >= OrgNumberBase) }

// highestIn is the highest number in use or reserved in a range (the base
// minus one for the high range when none is used) and the slots of the range
// that still need a number.
func highestIn(slots []numberSlot, reserved []int, high bool) (int, []numberSlot) {
	ranged := len(slots) > 0 && slots[0].ranged
	highest := 0
	if ranged && high {
		highest = OrgNumberBase - 1
	}
	for _, no := range reserved {
		if inRange(no, ranged, high) {
			highest = max(highest, no)
		}
	}
	var missing []numberSlot
	for _, slot := range slots {
		if ranged && slot.high != high {
			continue
		}
		highest = max(highest, *slot.no)
		if *slot.no == 0 {
			missing = append(missing, slot)
		}
	}
	return highest, missing
}

// NextNumbers are the numbers the next definition of each kind would get.
type NextNumbers struct {
	TypeNo, LinkTypeNo, AttrNo int
}

// Next returns the numbers a new object type, link type and property would
// get from this registry: one above the highest in use or reserved. The
// property number is the next one in the range of the standard (NextOrgAttrNo
// gives the next one of an organisation).
func (r *Registry) Next() NextNumbers {
	types, links, attrs := r.defs.slots()
	return NextNumbers{
		TypeNo:     nextNo(types, r.defs.Reserved.TypeNos, false),
		LinkTypeNo: nextNo(links, r.defs.Reserved.LinkTypeNos, false),
		AttrNo:     nextNo(attrs, r.defs.Reserved.AttrNos, false),
	}
}

// NextOrgAttrNo is the number the next organisation property would get: one
// above the highest in use or reserved in the range from OrgNumberBase.
func (r *Registry) NextOrgAttrNo() int {
	_, _, attrs := r.defs.slots()
	return nextNo(attrs, r.defs.Reserved.AttrNos, true)
}

func nextNo(slots []numberSlot, reserved []int, high bool) int {
	highest, _ := highestIn(slots, reserved, high)
	return highest + 1
}
