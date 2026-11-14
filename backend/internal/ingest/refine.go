package ingest

import (
	"slices"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// checkOp checks one operation against the columns of its input.
func checkOp(op Op, cols []string) error {
	if !slices.Contains(cols, op.Column) {
		return bad("column %q is not in the input", op.Column)
	}
	switch op.Op {
	case OpFilter:
		if !op.NotEmpty && len(op.In) == 0 && len(op.NotIn) == 0 {
			return bad("a filter needs a test: not_empty, in or not_in")
		}
	case OpFillEmpty:
		if op.Value == "" {
			return bad("fill_empty needs a value")
		}
	case OpReplace:
		return checkFunc(op)
	default:
		return kernel.Errorf(kernel.CodeUnsupported, "operation %q is not in this build (filter, fill_empty, replace)", op.Op)
	}
	return nil
}

func checkFunc(op Op) error {
	switch op.Func {
	case FuncTrim, FuncLower, FuncUpper:
		return nil
	case FuncMap:
		if len(op.Map) == 0 {
			return bad("the map function needs a map")
		}
		return nil
	}
	return kernel.Errorf(kernel.CodeUnsupported, "function %q is not in the closed list (trim, lower, upper, map)", op.Func)
}

// keep says whether a row passes the filter of an operation. The spaces at both ends of the cell and of the listed
// values are of no account (as for not_empty and for the natural key), so "on " is in ["on"] (ADR-0052).
func (op Op) keep(cell string) bool {
	trimmed := strings.TrimSpace(cell)
	switch {
	case op.NotEmpty && trimmed == "":
		return false
	case len(op.In) > 0 && !listed(op.In, trimmed):
		return false
	case len(op.NotIn) > 0 && listed(op.NotIn, trimmed):
		return false
	}
	return true
}

// listed tells whether a value is one of the listed values, the spaces at the ends of the listed ones set aside.
func listed(list []string, trimmed string) bool {
	return slices.ContainsFunc(list, func(v string) bool { return strings.TrimSpace(v) == trimmed })
}

// rewrite gives the cell after a fill or a replace.
func (op Op) rewrite(cell string) string {
	switch op.Op {
	case OpFillEmpty:
		if strings.TrimSpace(cell) == "" {
			return op.Value
		}
	case OpReplace:
		switch op.Func {
		case FuncTrim:
			return strings.TrimSpace(cell)
		case FuncLower:
			return strings.ToLower(cell)
		case FuncUpper:
			return strings.ToUpper(cell)
		case FuncMap:
			if to, ok := op.Map[cell]; ok {
				return to
			}
		}
	}
	return cell
}

// refineRows runs the operations of a refinement over rows. The rows it drops are returned by key with the
// operation that dropped them (the row count of pipeline.md 7 says "filtered by a rule", and which). Rows are
// copied only when a cell changes, so the input is never touched.
func refineRows(r Refinement, rows []Row) (kept []Row, dropped map[string]string) {
	dropped = map[string]string{}
	kept = rows
	for i, op := range r.Ops {
		next := make([]Row, 0, len(kept))
		for _, row := range kept {
			cell := row.Fields[op.Column]
			if op.Op == OpFilter {
				if op.keep(cell) {
					next = append(next, row)
				} else {
					dropped[row.Key] = r.ID + "#" + itoa(i+1)
				}
				continue
			}
			if to := op.rewrite(cell); to != cell {
				row = row.with(op.Column, to)
			}
			next = append(next, row)
		}
		kept = next
	}
	return kept, dropped
}
