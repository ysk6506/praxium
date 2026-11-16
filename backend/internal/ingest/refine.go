package ingest

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// opCheck is one operation of a refinement being checked: where it is, for the errors it gives.
type opCheck struct {
	c   *collector
	ref Refinement
	n   int
	at  string
}

// add records an error of the operation. The sentence has always been the one of the operation's own error inside the
// sentence of the refinement, so it carries the kernel code word of the inner one ("invalid_value: ...").
func (o opCheck) add(ec *errCode, field string, params Params, format string, args ...any) {
	params["refinement"], params["operation"] = o.ref.ID, o.n
	inner := fmt.Sprintf(format, args...)
	o.c.add(ec, o.at+ptr(field), params, "refinement %s, operation %d: %s: %s", o.ref.ID, o.n, o.kind(ec), inner)
}

func (o opCheck) kind(ec *errCode) kernel.Code {
	if ec == ecMapOpUnsupported || ec == ecMapOpFunc {
		return kernel.CodeUnsupported
	}
	return kernel.CodeInvalidValue
}

// checkOp checks one operation against the columns of its input. Every mistake in it is told, not the first only.
func checkOp(c *collector, r Refinement, ri, index int, cols []string) {
	op := r.Ops[index]
	o := opCheck{c: c, ref: r, n: index + 1, at: ptr("refinements", ri, "ops", index)}
	if !slices.Contains(cols, op.Column) {
		o.add(ecMapOpColumn, "column", Params{"column": op.Column, "columns": cols}, "column %q is not in the input", op.Column)
	}
	switch op.Op {
	case OpFilter:
		if !op.NotEmpty && len(op.In) == 0 && len(op.NotIn) == 0 {
			o.add(ecMapOpFilter, "op", Params{}, "a filter needs a test: not_empty, in or not_in")
		}
	case OpFillEmpty:
		if op.Value == "" {
			o.add(ecMapOpFill, "value", Params{}, "fill_empty needs a value")
		}
	case OpReplace:
		o.checkFunc(op)
	default:
		o.add(ecMapOpUnsupported, "op", Params{"op": op.Op, "supported": []string{OpFilter, OpFillEmpty, OpReplace}},
			"operation %q is not in this build (filter, fill_empty, replace)", op.Op)
	}
}

func (o opCheck) checkFunc(op Op) {
	switch op.Func {
	case FuncTrim, FuncLower, FuncUpper:
	case FuncMap:
		if len(op.Map) == 0 {
			o.add(ecMapOpMap, "map", Params{}, "the map function needs a map")
		}
	default:
		o.add(ecMapOpFunc, "function", Params{"function": op.Func, "supported": []string{FuncTrim, FuncLower, FuncUpper, FuncMap}},
			"function %q is not in the closed list (trim, lower, upper, map)", op.Func)
	}
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
// operation that dropped them (the row count of pipeline.md 7 says "filtered by a rule", and which). A dropped row
// is not forgotten: it goes on through the operations that rewrite cells and comes out in held, together with the
// rows an earlier refinement dropped (heldIn), so the run can tell whether an object lives only by rows a rule
// filtered (ADR-0052). Rows are copied only when a cell changes, so the input is never touched.
func refineRows(r Refinement, rows, heldIn []Row) (kept, held []Row, dropped map[string]string) {
	dropped = map[string]string{}
	kept, held = rows, slices.Clone(heldIn)
	for i, op := range r.Ops {
		next := make([]Row, 0, len(kept))
		for _, row := range kept {
			cell := row.Fields[op.Column]
			if op.Op == OpFilter {
				if op.keep(cell) {
					next = append(next, row)
				} else {
					dropped[row.Key] = r.ID + "#" + itoa(i+1)
					held = append(held, row)
				}
				continue
			}
			next = append(next, op.rewritten(row, cell))
		}
		kept = next
		if op.Op != OpFilter {
			held = rewriteAll(op, held)
		}
	}
	return kept, held, dropped
}

// rewritten gives the row after a fill or a replace of its cell.
func (op Op) rewritten(row Row, cell string) Row {
	if to := op.rewrite(cell); to != cell {
		return row.with(op.Column, to)
	}
	return row
}

func rewriteAll(op Op, rows []Row) []Row {
	out := make([]Row, len(rows))
	for i, row := range rows {
		out[i] = op.rewritten(row, row.Fields[op.Column])
	}
	return out
}
