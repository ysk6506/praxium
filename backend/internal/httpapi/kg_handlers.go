package httpapi

import (
	"net/url"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// kgView is the JSON of one KG (rest-api-resources-graph.md 1-1).
func kgView(kg graph.KG, head graph.Head) map[string]any {
	var alias any
	if kg.Role == graph.RoleProduction {
		alias = graph.MainAlias
	}
	return map[string]any{
		"kg_id": kg.ID.String(), "name": kg.Name, "alias": alias, "role": string(kg.Role), "state": string(kg.State),
		"definition_set": kg.DefinitionSetHash.String(), "generation": kg.Generation,
		"head_change_seq": head.AppliedSeq, "readable_through": head.ReadableThrough,
		"projection_state": "none", "created_at": kg.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// kgList serves GET /v1/kgs: the KGs, ordered by id, in pages.
func (c *call) kgList() {
	if !c.graphReady() {
		return
	}
	def := &listDef{filters: map[string][]string{"role": {"equals", "in"}, "state": {"equals", "in"}}}
	kgs, err := c.s.kgs.ListKGs(c.r.Context())
	if err != nil {
		c.failErr(err)
		return
	}
	sort.Slice(kgs, func(i, j int) bool { return kgs[i].ID.String() < kgs[j].ID.String() })
	for _, kg := range kgs {
		head, err := c.s.kgs.Head(c.r.Context(), kg.ID)
		if err != nil {
			c.failErr(err)
			return
		}
		def.rows = append(def.rows, row{id: kg.ID.String(), vals: map[string]any{"role": string(kg.Role), "state": string(kg.State)},
			out: kgView(kg, head)})
	}
	lq, fail := parseListQuery(c.r.URL.Query(), def)
	if fail != nil {
		c.fail(fail.code, fail.params)
		return
	}
	pg, fail, cerr := def.run(lq, "kgs")
	if fail != nil || cerr != cursorOK {
		c.fail("invalid_input.cursor_invalid", nil)
		return
	}
	items := pg.items
	if items == nil {
		items = []map[string]any{}
	}
	var next any
	if pg.next != "" {
		next = pg.next
	}
	c.kgOK(items, map[string]any{"result_complete": map[string]any{
		"total": pg.total, "returned": len(items), "truncated": false, "reason": nil, "next_cursor": next}})
}

// kgOne serves GET /v1/kgs/{kg}; {kg} is an id, a name or the alias main.
func (c *call) kgOne() {
	if !c.graphReady() {
		return
	}
	if _, bad := c.getQuery(nil); !bad {
		return
	}
	kg, err := c.s.kgs.GetKG(c.r.Context(), c.r.PathValue("kg"))
	if err != nil {
		c.failErr(err)
		return
	}
	head, err := c.s.kgs.Head(c.r.Context(), kg.ID)
	if err != nil {
		c.failErr(err)
		return
	}
	c.kgOK(kgView(kg, head), map[string]any{"allowed_actions": c.allowedActions("kg", kg.ID.String())})
}

// splitFilters takes the filter[..][..] keys out of a query; the rest goes to the list parser.
func splitFilters(q url.Values) (rest url.Values, filters map[string][]string) {
	rest, filters = url.Values{}, map[string][]string{}
	for key, values := range q {
		if strings.HasPrefix(key, "filter[") {
			filters[key] = values
		} else {
			rest[key] = values
		}
	}
	return rest, filters
}

// findInput reads the filters of GET /objects into the input of find.
func findInput(filters map[string][]string, in *query.FindInput) *queryFailure {
	keys := make([]string, 0, len(filters))
	for key := range filters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		field, op, ok := filterKey(key)
		if !ok {
			return bad("invalid_input.param_unknown", "param", key)
		}
		for _, value := range filters[key] {
			if fail := addFilter(in, field, op, value); fail != nil {
				return fail
			}
		}
	}
	return nil
}

func addFilter(in *query.FindInput, field, op, value string) *queryFailure {
	special := map[string]*string{"object_type": &in.ObjectType, "trait": &in.Trait, "text": &in.Text, "scope_id": &in.ScopeID}
	if dst, ok := special[field]; ok {
		if op != "equals" {
			return bad("invalid_input.filter_op_unknown", "field", field, "op", op, "allowed", []string{"equals"})
		}
		*dst = value
		return nil
	}
	if field == "identifier" {
		scheme, id, found := strings.Cut(value, ":")
		if op != "equals" || !found {
			return bad("invalid_input.filter_op_unknown", "field", field, "op", op, "allowed", []string{"equals"})
		}
		in.Identifier = &query.IdentifierFilter{Scheme: scheme, Value: id}
		return nil
	}
	f := query.AttrFilter{Name: field, Op: op, Values: []string{value}}
	switch op {
	case query.OpIn:
		f.Values = splitList(value)
	case query.OpRange:
		from, to, ok := strings.Cut(value, "..")
		if !ok {
			return bad("invalid_input.param_invalid", "param", "filter["+field+"][range]", "expected", "from..to")
		}
		f.Values = []string{from, to}
	}
	in.Attributes = append(in.Attributes, f)
	return nil
}

// objectsFind serves GET /v1/kgs/{kg}/objects: find in its short form.
func (c *call) objectsFind() {
	if !c.graphReady() {
		return
	}
	rest, filters := splitFilters(c.r.URL.Query())
	lq, fail := parseListQuery(rest, &listDef{}, append([]string{"include_subtypes"}, kgParams...)...)
	if fail != nil {
		c.fail(fail.code, fail.params)
		return
	}
	common, ok := commonOf(c, lq)
	if !ok {
		return
	}
	in := query.FindInput{Common: common}
	if v := lq.others["include_subtypes"]; v != "" {
		flag := v == "true"
		if v != "true" && v != "false" {
			c.fail("invalid_input.param_invalid", map[string]any{"param": "include_subtypes", "expected": "true or false"})
			return
		}
		in.IncludeSubtypes = &flag
	}
	if fail := findInput(filters, &in); fail != nil {
		c.fail(fail.code, fail.params)
		return
	}
	c.runFind(in)
}

// runFind asks find and writes the answer; the GET and the POST form share it.
func (c *call) runFind(in query.FindInput) {
	ans, err := c.s.queries.Find(c.r.Context(), in, c.readFilter())
	if err != nil {
		c.failErr(err)
		return
	}
	c.kgOK(ans.Data, metaOf(ans.Meta))
}

// objectGet serves GET /v1/kgs/{kg}/objects/{object_id}: get.
func (c *call) objectGet() {
	if !c.graphReady() {
		return
	}
	lq, fail := parseListQuery(c.r.URL.Query(), &listDef{}, append([]string{"include"}, kgParams...)...)
	if fail != nil {
		c.fail(fail.code, fail.params)
		return
	}
	common, ok := commonOf(c, lq)
	if !ok {
		return
	}
	c.runGet(query.GetInput{Common: common, ObjectID: c.r.PathValue("object_id"), Include: lq.include})
}

// runGet asks get and writes the answer; the GET and the POST form share it.
func (c *call) runGet(in query.GetInput) {
	ans, err := c.s.queries.Get(c.r.Context(), in, c.readFilter())
	if err != nil {
		c.failErr(err)
		return
	}
	meta := metaOf(ans.Meta)
	meta["allowed_actions"] = c.allowedActions("object", ans.Data.Object.ObjectID)
	c.kgOK(ans.Data, meta)
}

// objectLinks serves GET /v1/kgs/{kg}/objects/{object_id}/links: neighbors.
func (c *call) objectLinks() {
	if !c.graphReady() {
		return
	}
	def := &listDef{filters: map[string][]string{"link_type": {"in", "equals"}, "meaning": {"in", "equals"},
		"peer_type": {"in", "equals"}, "direction": {"equals"}}}
	lq, fail := parseListQuery(c.r.URL.Query(), def, append([]string{"max_steps"}, kgParams...)...)
	if fail != nil {
		c.fail(fail.code, fail.params)
		return
	}
	common, ok := commonOf(c, lq)
	if !ok {
		return
	}
	in := query.NeighborsInput{Common: common, ObjectID: c.r.PathValue("object_id")}
	if steps, err := optInt(lq.others["max_steps"]); err != nil {
		c.fail("invalid_input.param_invalid", map[string]any{"param": "max_steps"})
		return
	} else if steps != nil {
		in.MaxSteps = int(*steps)
	}
	for _, cond := range lq.filters {
		switch cond.field {
		case "link_type":
			in.LinkTypes = append(in.LinkTypes, cond.values...)
		case "meaning":
			in.Meanings = append(in.Meanings, cond.values...)
		case "peer_type":
			in.PeerTypes = append(in.PeerTypes, cond.values...)
		case "direction":
			in.Direction = cond.values[0]
		}
	}
	c.runNeighbors(in)
}

// runNeighbors asks neighbors and writes the answer; the GET and the POST form share it.
func (c *call) runNeighbors(in query.NeighborsInput) {
	ans, err := c.s.queries.Neighbors(c.r.Context(), in, c.readFilter())
	if err != nil {
		c.failErr(err)
		return
	}
	c.kgOK(ans.Data, metaOf(ans.Meta))
}
