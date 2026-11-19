package pgschema

import (
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// TimeFunc is the immutable function that turns the text of a time (UTC, ending in Z) into a timestamptz. The
// shared migrations define it; a text-to-time cast is not immutable, so an index could not use it directly.
const TimeFunc = "public.praxium_ts"

// ColumnType is the type of the column that a value of this kind has when it is read out of a document or kept
// in a table of volatile values.
func ColumnType(kind model.BaseKind) (string, error) {
	switch kind {
	case model.BaseString, model.BaseEnum, model.BaseRef:
		return "text", nil
	case model.BaseInteger:
		return "bigint", nil
	case model.BaseQuantity:
		return "numeric", nil
	case model.BaseNumber:
		return "double precision", nil
	case model.BaseBoolean:
		return "boolean", nil
	case model.BaseTime:
		return "timestamptz", nil
	case model.BaseList, model.BaseMap:
		return "jsonb", nil
	}
	return "", kernel.Errorf(kernel.CodeInvalidValue, "no column type for the value kind %q", kind)
}

// Expr is the text that reads the value of an attribute out of a document column (doc is the column, with its
// table alias if it needs one). It is parenthesised, so it can stand as an index expression or a statistics
// expression as it is. Indexes, statistics and views all take their text from here: the planner uses an
// index or a statistic only if the expression is the same.
func Expr(doc string, attrNo int, kind model.BaseKind) (string, error) {
	v := doc + "->'" + strconv.Itoa(attrNo) + "'"
	switch kind {
	case model.BaseString, model.BaseEnum, model.BaseRef:
		return "(" + v + "->>'v')", nil
	case model.BaseInteger:
		return "((" + v + "->>'v')::bigint)", nil
	case model.BaseQuantity:
		return "((" + v + "->>'v')::numeric)", nil
	case model.BaseNumber:
		return "((" + v + "->>'v')::double precision)", nil
	case model.BaseBoolean:
		return "((" + v + "->>'v')::boolean)", nil
	case model.BaseTime:
		return "(" + TimeFunc + "(" + v + "->>'v'))", nil
	case model.BaseList, model.BaseMap:
		return "(" + v + "->'v')", nil
	}
	return "", kernel.Errorf(kernel.CodeInvalidValue, "no expression for the value kind %q", kind)
}

// Indexable reports whether a value of this kind can have a plain index on its expression. A list or a map
// is a document of its own, and an index on it is large and answers nothing a person asks.
func Indexable(kind model.BaseKind) bool { return kind != model.BaseList && kind != model.BaseMap }
