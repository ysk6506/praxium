package query

import (
	"math/big"
	"slices"
	"strings"
)

// compare tests one stored value, in text form, against a condition. Numbers
// compare as numbers (they are stored as whole numbers or decimal strings),
// everything else as text; times in the RFC 3339 form sort as text.
func compare(c attrCond, got string) bool {
	switch c.Op {
	case OpEquals:
		return got == c.Values[0]
	case OpIn:
		return slices.Contains(c.Values, got)
	case OpPrefix:
		return strings.HasPrefix(got, c.Values[0])
	case OpRange:
		return inRange(got, c.Values[0], c.Values[1])
	}
	return false
}

func inRange(got, from, to string) bool {
	g, gok := number(got)
	f, fok := number(from)
	t, tok := number(to)
	numeric := gok && (from == "" || fok) && (to == "" || tok)
	if numeric {
		return (from == "" || g.Cmp(f) >= 0) && (to == "" || g.Cmp(t) <= 0)
	}
	return (from == "" || got >= from) && (to == "" || got <= to)
}

func number(s string) (*big.Rat, bool) {
	if s == "" {
		return nil, false
	}
	return new(big.Rat).SetString(s)
}
