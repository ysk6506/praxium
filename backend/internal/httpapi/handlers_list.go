package httpapi

// registryETag is the entity tag of an answer from the registry: the model's
// hash and the query, so the same URL keeps its tag until the model changes
// (rest-api.md 3-6).
func (c *call) registryETag() string {
	return quote(c.s.lists.generation + "." + fingerprint(c.r.URL.Path, c.r.URL.Query().Encode()))
}

// listHandler serves a list resource: filter, sort, fields and cursor in the
// closed grammar, the answer as data array and meta.page.
func listHandler(pick func(*registryLists) *listDef) func(*call) {
	return func(c *call) { listWith(c, pick(c.s.lists)) }
}

// listWith serves one list definition of the registry.
func listWith(c *call, def *listDef) {
	c.serveList(def, c.s.lists.generation, c.registryETag(), map[string]any{"model_hash": c.s.lists.generation})
}

// serveList filters, sorts and cuts one page of def and writes it. An empty etag means the answer is not cached
// (the lists of users and tokens). generation is what a cursor of this list must have been issued at.
func (c *call) serveList(def *listDef, generation, etag string, extra map[string]any) {
	lq, fail := parseListQuery(c.r.URL.Query(), def)
	if fail != nil {
		c.fail(fail.code, fail.params)
		return
	}
	pg, fail, cerr := def.run(lq, generation)
	if fail != nil {
		c.fail(fail.code, fail.params)
		return
	}
	switch cerr {
	case cursorMalformed:
		c.fail("invalid_input.cursor_invalid", nil)
		return
	case cursorStaleGeneration:
		c.fail("conflict.generation_changed", nil)
		return
	}
	if etag != "" && c.notModified(etag) {
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
	meta := map[string]any{
		"result_complete": map[string]any{
			"total": pg.total, "returned": len(items), "truncated": false, "reason": nil, "next_cursor": next,
		},
	}
	for k, v := range extra {
		meta[k] = v
	}
	c.ok(items, meta)
}

// getQuery reads the query of a single resource: only include, from a closed list.
func (c *call) getQuery(allowed []string) ([]string, bool) {
	var include []string
	for key, values := range c.r.URL.Query() {
		if key != "include" {
			c.fail("invalid_input.param_unknown", map[string]any{"param": key})
			return nil, false
		}
		include = splitList(values[len(values)-1])
	}
	for _, name := range include {
		if !contains(allowed, name) {
			c.fail("invalid_input.include_unknown", map[string]any{"include": name, "allowed": allowed})
			return nil, false
		}
	}
	return include, true
}

// one writes a single resource of the registry: ETag, Content-Location when
// the request used a name or an old name, allowed_actions.
func (c *call) one(kind, canonical, requested string, data map[string]any) {
	if c.notModified(c.registryETag()) {
		return
	}
	if canonical != requested {
		c.w.Header().Set("Content-Location", c.r.URL.Path[:len(c.r.URL.Path)-len(requested)]+canonical)
	}
	c.ok(data, map[string]any{
		"allowed_actions": c.allowedActions(kind, canonical),
		"model_hash":      c.s.lists.generation,
	})
}
