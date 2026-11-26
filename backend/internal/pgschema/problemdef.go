package pgschema

// The number of the definition a problem of a record arose in (record_problem.def_no and calc_problem.def_no, ADR-0128). The feeds of a
// mapping and its link definitions are each numbered from 0 by their place, so the two numbers alone would say the same thing of a feed and
// of a link definition: a problem of a feed keeps the number of the feed (0 and up) and a problem of a link definition keeps the number of
// the definition as -(n+1) (below 0). NULL is a problem of the record as a whole (a key that repeats in the batch). Every statement that
// writes or reads the column takes the number from here, so the two spaces are told apart in one place.

// ProblemDefKind says which definitions a number of a problem is of.
type ProblemDefKind int

// The kinds of the number of a problem: a feed, or a link definition.
const (
	ProblemDefFeed ProblemDefKind = 1
	ProblemDefLink ProblemDefKind = 2
)

// ProblemDefOfFeed is the number a problem of the feed keeps.
func ProblemDefOfFeed(feed int) int { return feed }

// ProblemDefOfLink is the number a problem of the link definition keeps.
func ProblemDefOfLink(def int) int { return -def - 1 }

// ProblemDefRead says which definition a number a problem keeps is of, and its number among them.
func ProblemDefRead(v int) (ProblemDefKind, int) {
	if v < 0 {
		return ProblemDefLink, -v - 1
	}
	return ProblemDefFeed, v
}

// ProblemDefFeedSQL is the number a problem keeps for the feed the SQL expression gives.
func ProblemDefFeedSQL(expr string) string { return "(" + expr + ")" }

// ProblemDefLinkSQL is the number a problem keeps for the link definition the SQL expression gives.
func ProblemDefLinkSQL(expr string) string { return "(-(" + expr + ") - 1)" }

// ProblemDefIsLinkSQL is the condition that the number a problem keeps (the SQL expression) is of a link definition, and
// ProblemDefIsFeedSQL that it is of a feed. A problem of the record as a whole is neither.
func ProblemDefIsLinkSQL(expr string) string { return "(" + expr + " < 0)" }

// ProblemDefIsFeedSQL is the condition that the number a problem keeps is of a feed.
func ProblemDefIsFeedSQL(expr string) string { return "(" + expr + " >= 0)" }
