package httpapi

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Limits of a page (rest-api.md 3-3, defaults.md).
const (
	defaultLimit = 100
	maxLimit     = 1000
)

// condition is one filter[<field>][<op>]=<value> of a request.
type condition struct {
	field  string
	op     string
	values []string
}

// listQuery is a parsed list request. Parse errors leave it as a problem the
// caller sends: code, params and the field errors.
type listQuery struct {
	limit   int
	cursor  string
	filters []condition
	sort    []sortKey
	fields  []string
	include []string
	others  map[string]string
}

type sortKey struct {
	field string
	desc  bool
}

// queryFailure is a request that cannot be served: the problem to send.
type queryFailure struct {
	code   string
	params map[string]any
}

func bad(code string, kv ...any) *queryFailure {
	params := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		params[kv[i].(string)] = kv[i+1]
	}
	return &queryFailure{code: code, params: params}
}

// filterKey splits `filter[<field>][<op>]`.
func filterKey(key string) (field, op string, ok bool) {
	if !strings.HasPrefix(key, "filter[") || !strings.HasSuffix(key, "]") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(key, "filter["), "]"), "][")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// parseListQuery reads the closed grammar of rest-api.md 3-3 and 3-4. Only the
// parameters in extra (besides limit, cursor, filter[..][..], sort, fields,
// include as the listDef allows) are accepted; anything else is refused.
func parseListQuery(q url.Values, def *listDef, extra ...string) (*listQuery, *queryFailure) {
	out := &listQuery{limit: defaultLimit, others: map[string]string{}}
	keys := make([]string, 0, len(q))
	for key := range q {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if fail := out.take(key, q[key], def, extra); fail != nil {
			return nil, fail
		}
	}
	return out, nil
}

func (lq *listQuery) take(key string, values []string, def *listDef, extra []string) *queryFailure {
	switch {
	case key == "limit":
		return lq.takeLimit(values)
	case key == "cursor":
		lq.cursor = values[len(values)-1]
	case key == "sort":
		return lq.takeSort(values[len(values)-1], def)
	case key == "fields":
		return lq.takeFields(values[len(values)-1], def)
	case key == "include":
		lq.include = splitList(values[len(values)-1])
	case strings.HasPrefix(key, "filter["):
		return lq.takeFilter(key, values, def)
	case contains(extra, key):
		lq.others[key] = values[len(values)-1]
	default:
		return bad("invalid_input.param_unknown", "param", key)
	}
	return nil
}

func (lq *listQuery) takeLimit(values []string) *queryFailure {
	n, err := strconv.Atoi(values[len(values)-1])
	if err != nil || n < 1 || n > maxLimit {
		return bad("invalid_input.limit_invalid", "min", 1, "max", maxLimit)
	}
	lq.limit = n
	return nil
}

func (lq *listQuery) takeSort(text string, def *listDef) *queryFailure {
	for _, item := range splitList(text) {
		key := sortKey{field: strings.TrimPrefix(item, "-"), desc: strings.HasPrefix(item, "-")}
		if !contains(def.sorts, key.field) {
			return bad("invalid_input.sort_field_unknown", "field", key.field, "allowed", def.sorts)
		}
		lq.sort = append(lq.sort, key)
	}
	return nil
}

func (lq *listQuery) takeFields(text string, def *listDef) *queryFailure {
	for _, name := range splitList(text) {
		if !contains(def.outFields, name) {
			return bad("invalid_input.fields_unknown", "field", name, "allowed", def.outFields)
		}
		lq.fields = append(lq.fields, name)
	}
	return nil
}

func (lq *listQuery) takeFilter(key string, values []string, def *listDef) *queryFailure {
	field, op, ok := filterKey(key)
	if !ok {
		return bad("invalid_input.param_unknown", "param", key)
	}
	ops, filterable := def.filters[field]
	if !filterable {
		return bad("invalid_input.filter_field_unknown", "field", field, "allowed", def.filterNames())
	}
	if !contains(ops, op) {
		return bad("invalid_input.filter_op_unknown", "field", field, "op", op, "allowed", ops)
	}
	for _, v := range values {
		lq.filters = append(lq.filters, condition{field: field, op: op, values: splitList(v)})
	}
	return nil
}

// fingerprint identifies the filter, sort and fields of a query, not its page.
func (lq *listQuery) fingerprint() string {
	parts := []string{}
	for _, f := range lq.filters {
		parts = append(parts, "f:"+f.field+":"+f.op+":"+strings.Join(f.values, ","))
	}
	sort.Strings(parts)
	for _, s := range lq.sort {
		parts = append(parts, "s:"+s.field+":"+strconv.FormatBool(s.desc))
	}
	parts = append(parts, "x:"+strings.Join(lq.fields, ","))
	return fingerprint(parts...)
}

func splitList(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, ",")
}

func contains(list []string, item string) bool {
	for _, s := range list {
		if s == item {
			return true
		}
	}
	return false
}
