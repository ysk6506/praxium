package httpapi

import (
	"sort"
	"strconv"
	"strings"
)

// row is one item of a list: the values filters and sorts read, and the JSON
// object that leaves the server.
type row struct {
	id string
	// vals holds a string, an int or a []string for each filterable or sortable field.
	vals map[string]any
	out  map[string]any
}

// listDef describes a list resource: its rows in their natural order and the
// closed lists of what may be filtered, sorted and selected (rest-api.md 3-4).
type listDef struct {
	rows []row
	// filters maps a field to the operations it allows (equals, in, prefix, range).
	filters map[string][]string
	// sorts lists the sortable fields; the natural order is the default.
	sorts []string
	// defSort is the default sort when the request gives none; nil keeps the natural order.
	defSort []string
	// outFields lists the names `fields` may select.
	outFields []string
}

func (d *listDef) filterNames() []string {
	names := make([]string, 0, len(d.filters))
	for name := range d.filters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// page is one page of a list.
type page struct {
	items []map[string]any
	total int
	// next is the cursor of the following page, empty on the last page.
	next string
}

// run filters, sorts and cuts one page. generation is what a cursor must have been issued at.
func (d *listDef) run(lq *listQuery, generation string) (page, *queryFailure, cursorError) {
	rows := d.filtered(lq)
	keys := lq.sort
	if len(keys) == 0 {
		for _, f := range d.defSort {
			keys = append(keys, sortKey{field: f})
		}
	}
	sortRows(rows, keys)
	fp := lq.fingerprint()
	start := 0
	if lq.cursor != "" {
		cur, status := decodeCursor(lq.cursor, generation, fp)
		if status != cursorOK {
			return page{}, nil, status
		}
		start = firstAfter(rows, keys, cur)
	}
	end := start + lq.limit
	if end > len(rows) {
		end = len(rows)
	}
	out := page{total: len(rows)}
	for _, r := range rows[start:end] {
		out.items = append(out.items, project(r.out, lq.fields))
	}
	if end < len(rows) {
		out.next = encodeCursor(cursorBody{Gen: generation, Query: fp, Keys: keyTexts(rows[end-1], keys), ID: rows[end-1].id})
	}
	return out, nil, cursorOK
}

func (d *listDef) filtered(lq *listQuery) []row {
	out := make([]row, 0, len(d.rows))
	for _, r := range d.rows {
		keep := true
		for _, c := range lq.filters {
			if !matchCondition(r.vals[c.field], c) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, r)
		}
	}
	return out
}

// matchCondition applies one filter to a value: a string, an int or a list of strings.
func matchCondition(value any, c condition) bool {
	switch v := value.(type) {
	case string:
		return matchText(v, c)
	case int:
		return matchInt(v, c)
	case []string:
		for _, item := range v {
			if matchText(item, c) {
				return true
			}
		}
	}
	return false
}

func matchText(v string, c condition) bool {
	switch c.op {
	case "equals":
		return len(c.values) == 1 && v == c.values[0]
	case "in":
		return contains(c.values, v)
	case "prefix":
		return len(c.values) == 1 && strings.HasPrefix(v, c.values[0])
	}
	return false
}

func matchInt(v int, c condition) bool {
	switch c.op {
	case "equals":
		return len(c.values) == 1 && strconv.Itoa(v) == c.values[0]
	case "in":
		return contains(c.values, strconv.Itoa(v))
	case "range":
		return len(c.values) == 1 && inRange(v, c.values[0])
	}
	return false
}

// inRange reads `from..to`; either side may be empty.
func inRange(v int, text string) bool {
	from, to, ok := strings.Cut(text, "..")
	if !ok {
		return false
	}
	if from != "" {
		if n, err := strconv.Atoi(from); err != nil || v < n {
			return false
		}
	}
	if to != "" {
		if n, err := strconv.Atoi(to); err != nil || v > n {
			return false
		}
	}
	return true
}

func compareVals(a, b any) int {
	switch x := a.(type) {
	case int:
		y, _ := b.(int)
		return cmpInt(x, y)
	case string:
		y, _ := b.(string)
		return strings.Compare(x, y)
	}
	return 0
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compareRows orders two rows by the sort keys and then by id.
func compareRows(a, b row, keys []sortKey) int {
	for _, k := range keys {
		c := compareVals(a.vals[k.field], b.vals[k.field])
		if k.desc {
			c = -c
		}
		if c != 0 {
			return c
		}
	}
	return strings.Compare(a.id, b.id)
}

func sortRows(rows []row, keys []sortKey) {
	sort.SliceStable(rows, func(i, j int) bool { return compareRows(rows[i], rows[j], keys) < 0 })
}

func keyTexts(r row, keys []sortKey) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		switch v := r.vals[k.field].(type) {
		case int:
			out = append(out, strconv.Itoa(v))
		case string:
			out = append(out, v)
		default:
			out = append(out, "")
		}
	}
	return out
}

// firstAfter finds the index of the first row after the position a cursor names.
func firstAfter(rows []row, keys []sortKey, cur cursorBody) int {
	probe := row{id: cur.ID, vals: map[string]any{}}
	for i, k := range keys {
		if i >= len(cur.Keys) {
			break
		}
		if n, err := strconv.Atoi(cur.Keys[i]); err == nil && isIntField(rows, k.field) {
			probe.vals[k.field] = n
		} else {
			probe.vals[k.field] = cur.Keys[i]
		}
	}
	return sort.Search(len(rows), func(i int) bool { return compareRows(rows[i], probe, keys) > 0 })
}

func isIntField(rows []row, field string) bool {
	if len(rows) == 0 {
		return false
	}
	_, ok := rows[0].vals[field].(int)
	return ok
}

// project keeps the selected fields of an item; no selection keeps all of it.
func project(item map[string]any, fields []string) map[string]any {
	if len(fields) == 0 {
		return item
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		if v, ok := item[f]; ok {
			out[f] = v
		}
	}
	return out
}
