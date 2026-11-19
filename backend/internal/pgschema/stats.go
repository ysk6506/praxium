package pgschema

import "strings"

// StatsPerObject is how many expressions go in one statistics object. PostgreSQL takes eight at most.
const StatsPerObject = 8

// statsGroups cuts the standard attributes of a document, in ascending order, into groups of eight. The
// attributes of an organisation are left out: statistics are for the standard model, and an organisation's
// attribute gets an index when it is marked.
func statsGroups(docs []Column) [][]Column {
	var std []Column
	for _, c := range docs {
		if c.Standard() {
			std = append(std, c)
		}
	}
	var groups [][]Column
	for len(std) > 0 {
		n := min(StatsPerObject, len(std))
		groups = append(groups, std[:n])
		std = std[n:]
	}
	return groups
}

// statsStep is the statement that makes the statistics object of one group. A planner has no idea about an
// expression on a document unless a statistics object says it. With more than one expression the object also
// keeps the most common combinations (mcv): the other kinds (dependencies, n-distinct) grow with the number
// of expressions faster than they are worth. A single expression takes no kind.
func statsStep(schema string, typeNo int, group []Column) (Step, error) {
	exprs := make([]string, 0, len(group))
	for _, c := range group {
		e, err := Expr("doc", c.AttrNo, c.Kind)
		if err != nil {
			return Step{}, err
		}
		exprs = append(exprs, e)
	}
	name := StatsName(typeNo, group[0].AttrNo)
	kinds := ""
	if len(group) > 1 {
		kinds = " (mcv)"
	}
	sql := "CREATE STATISTICS " + Qualified(schema, name) + kinds + " ON " + strings.Join(exprs, ", ") + " FROM " + Qualified(schema, TypeTable(typeNo))
	return Step{Phase: InTx, What: WhatStatistics, Name: name, SQL: sql}, nil
}

// attrNos is the numbers of the attributes of a group.
func attrNos(group []Column) []int {
	out := make([]int, 0, len(group))
	for _, c := range group {
		out = append(out, c.AttrNo)
	}
	return out
}
