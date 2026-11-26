package pgschema

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The names of the query schema (ADR-0113): what a user, a screen or an agent reads with SQL. A KG has one schema for each
// sensitivity grade, q_<kg_no>_<grade>, and one more set of three for the time an apply too large to be read half way is
// running, q_<kg_no>_<grade>_w. The roles that read them are made for the grade and carry the token of the installation in their
// names, because a role belongs to the cluster and not to the database.
const (
	prefixQuerySchema = "q_"
	suffixWatermark   = "_w"
	prefixQueryRole   = "pq_"
)

// The views and the function that every query schema of the full set has besides the views of the types and of the links. A type or a
// link type whose name is one of these would hide it, so a plan that meets one refuses.
const (
	ViewObject          = "object"
	ViewLink            = "link"
	ViewAttribute       = "attribute"
	ViewIdentifier      = "identifier"
	ViewKGVersion       = "kg_version"
	ViewChange          = "change"
	ViewValueChange     = "value_change"
	ViewModelType       = "model_type"
	ViewModelAttribute  = "model_attribute"
	ViewModelLinkType   = "model_link_type"
	ViewModelLinkRule   = "model_link_rule"
	FuncObjectDetail    = "object_detail"
	suffixAtFunction    = "_at"
	installTokenLength  = 12
	maxQueryColumnBytes = MaxIdentBytes
)

// reservedViewNames are the names a type or a link type may not take as its view.
func reservedViewNames() map[string]bool {
	names := []string{ViewObject, ViewLink, ViewAttribute, ViewIdentifier, ViewKGVersion, ViewChange, ViewValueChange,
		ViewModelType, ViewModelAttribute, ViewModelLinkType, ViewModelLinkRule, FuncObjectDetail}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// QuerySchemaName is the query schema of a KG for a grade; watermark says the set for the time of an apply too large to be read half
// way.
func QuerySchemaName(kgNo int64, grade model.Sensitivity, watermark bool) string {
	name := prefixQuerySchema + strconv.FormatInt(kgNo, 10) + "_" + string(grade)
	if watermark {
		name += suffixWatermark
	}
	return name
}

// QueryRoleName is the role that reads the query schemas of a KG up to a grade. The token is the one of the installation
// (CheckInstallToken), so two databases in one cluster never make the same role.
func QueryRoleName(token string, kgNo int64, grade model.Sensitivity) string {
	return prefixQueryRole + token + "_" + strconv.FormatInt(kgNo, 10) + "_" + string(grade)
}

// QueryRoleNames are the roles of a KG, the lowest grade first.
func QueryRoleNames(token string, kgNo int64) []string {
	var out []string
	for _, g := range QueryGrades() {
		out = append(out, QueryRoleName(token, kgNo, g))
	}
	return out
}

// QuerySchemaNames are the six query schemas of a KG: the three of the usual set, then the three of the set for a large apply.
func QuerySchemaNames(kgNo int64) []string {
	var out []string
	for _, w := range []bool{false, true} {
		for _, g := range QueryGrades() {
			out = append(out, QuerySchemaName(kgNo, g, w))
		}
	}
	return out
}

// SearchPath is the value of search_path that lets a caller of a grade write a bare name: the schemas of the grade and of those
// below it, the highest first, so a view that a higher grade makes for itself hides the one of the lower grade. The executor sets it
// for the transaction of a query.
func SearchPath(kgNo int64, grade model.Sensitivity, watermark bool) string {
	var names []string
	for _, g := range QueryGrades() {
		if gradeRank(g) <= gradeRank(grade) {
			names = append([]string{QuoteIdent(QuerySchemaName(kgNo, g, watermark))}, names...)
		}
	}
	return strings.Join(names, ", ")
}

var installTokenForm = regexp.MustCompile(`^[0-9a-f]{12}$`)

// CheckInstallToken refuses a token that is not the 12 lower-case hexadecimal digits the shared table makes: the token goes into the
// names of roles, so nothing else may reach them.
func CheckInstallToken(token string) error {
	if !installTokenForm.MatchString(token) {
		return kernel.Errorf(kernel.CodeInvalidValue, "the token of the installation is not %d lower-case hexadecimal digits", installTokenLength)
	}
	return nil
}

// QuoteLiteral is a text constant that reads the same whatever standard_conforming_strings says: an escape string with the
// backslash and the quote doubled. A text with a NUL byte has no such constant and is refused by the caller (checkText).
func QuoteLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return "E'" + strings.ReplaceAll(s, "'", "''") + "'"
}

var wordStart = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// TypeViewName is the name of the view of an object type: PhysicalServer is physical_server.
func TypeViewName(typeName string) string {
	return strings.ToLower(wordStart.ReplaceAllString(typeName, "${1}_${2}"))
}

// LinkViewName is the name of the view of a link type: MOUNTED_IN is mounted_in.
func LinkViewName(linkName string) string { return strings.ToLower(linkName) }

// AtFunctionName is the name of the function that reads a type as it was at a change number.
func AtFunctionName(view string) string { return view + suffixAtFunction }
