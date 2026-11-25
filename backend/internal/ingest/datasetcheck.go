package ingest

import (
	"regexp"
	"sort"
	"strings"
)

// The checks of the datasets of an HTTP source and of a PostgreSQL source.

// checkAdapterFields tells each field of the dataset that belongs to another adapter than the source's.
func (d Dataset) checkAdapterFields(c *collector, source, adapter, at string) {
	http := []namedSet{{"request", d.Request != nil}, {"pagination", d.Pagination != nil}, {"post_reads", d.PostReads}, {"response_format", d.ResponseFormat != ""}}
	records := []namedSet{{"records_path", d.RecordsPath != ""}, {"missing_array", d.MissingArray != ""}, {"max_elements", d.MaxElements != 0}}
	pg := []namedSet{{"table", d.Table != ""}, {"sql", d.SQL != ""}, {"key_collation", d.KeyCollation != ""}}
	file := []namedSet{{"delimiter", d.Delimiter != ""}, {"sheet", d.Sheet != nil}, {"ignore_undeclared_columns", d.IgnoreUndeclaredColumns}}
	var other []namedSet
	switch adapter {
	case AdapterFile:
		other = append(http, pg...)
		if !d.readsRecordsPath(c, adapter) {
			other = append(other, records...)
		}
	case AdapterHTTP:
		other = append(pg, file...)
	case AdapterPostgres:
		other = append(append(http, records...), file...)
	}
	for _, f := range other {
		if f.set {
			c.add(ecAdDsField, at+ptr(f.name), Params{"source": source, "dataset": d.ID, "adapter": adapter, "field": f.name},
				"source %s, dataset %s: %s is not a field of a dataset of the %s adapter", source, d.ID, f.name, adapter)
		}
	}
}

// checkRecords checks what says where the records of the dataset are: the request of an HTTP dataset, the table or the
// statement of a PostgreSQL one. It gives the dataset with its defaults.
func (d Dataset) checkRecords(c *collector, source, adapter, at string) Dataset {
	if d.Parser != "" {
		c.add(ecAdDsParser, at+ptr("parser"), Params{"source": source, "dataset": d.ID, "adapter": adapter},
			"source %s, dataset %s: a dataset of the %s adapter has no parser; its records are JSON objects already", source, d.ID, adapter)
	}
	if adapter == AdapterHTTP {
		return d.checkHTTP(c, source, at)
	}
	d.checkPostgres(c, source, at)
	return d
}

// checkColumnPaths checks the paths of the columns by the grammar of a path into a record.
func (d Dataset) checkColumnPaths(c *collector, source, at string) {
	outer := d.OpensArray()
	for i, col := range d.Columns {
		for j, p := range col.Paths {
			_, err := ParseColumnPath(p, outer)
			if cp, e2 := ParseColumnPath(p, true); err == nil && !outer && c.adapterFlow && e2 == nil && cp.Up {
				err = &PathError{Reason: `begins with ^, which reads the record the elements are in only where records_path has [*]; a key that is named ^ is written ["^"]`}
			}
			c.pathProblem(source, d.ID, at+ptr("columns", i, "paths", j), "column "+clip(col.Name, idClipAt)+" path", p, err)
		}
	}
}

func (d Dataset) checkHTTP(c *collector, source, at string) Dataset {
	if d.Request == nil {
		c.add(ecAdRequest, at+ptr("request"), Params{"source": source, "dataset": d.ID}, "source %s, dataset %s: a dataset of an http source needs a request", source, d.ID)
	} else {
		r := d.Request.normalize(c, source, d, at+ptr("request"))
		d.Request = &r
	}
	d.checkRecordsPath(c, source, at)
	p := Pagination{Kind: PageNone}
	if d.Pagination != nil {
		p = *d.Pagination
	}
	d.Pagination = p.normalize(c, source, d, at+ptr("pagination"))
	d.checkResponseFormat(c, source, at)
	return d
}

// normalize checks the request of a dataset and gives it with the method it has by default.
func (r Request) normalize(c *collector, source string, d Dataset, at string) Request {
	if r.Method == "" {
		r.Method = MethodGet
	}
	switch {
	case r.Method != MethodGet && r.Method != MethodPost:
		c.add(ecAdMethod, at+ptr("method"), Params{"source": source, "dataset": d.ID, "method": clip(r.Method, idClipAt), "supported": []string{MethodGet, MethodPost}},
			"source %s, dataset %s: the method %q is neither GET nor POST", source, d.ID, clip(r.Method, idClipAt))
	case r.Method == MethodPost && !d.PostReads:
		c.add(ecAdPostRead, at+ptr("method"), Params{"source": source, "dataset": d.ID},
			"source %s, dataset %s: the request is a POST, so the dataset has to say \"post_reads\": true, that this POST only reads", source, d.ID)
	}
	if r.Body != nil && r.Method != MethodPost {
		c.add(ecAdBodyPost, at+ptr("body"), Params{"source": source, "dataset": d.ID}, "source %s, dataset %s: a body is for a POST", source, d.ID)
	}
	if reason := requestPathProblem(r.Path); reason != "" {
		c.add(ecAdReqPath, at+ptr("path"), Params{"source": source, "dataset": d.ID, "field": "request.path", "reason": reason},
			"source %s, dataset %s: request.path %s", source, d.ID, reason)
	} else {
		c.checkText(source, at+ptr("path"), "request.path", r.Path)
	}
	names := make([]string, 0, len(r.Query))
	for k := range r.Query {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		c.checkText(source, at+ptr("query", k), "request.query", k)
		c.checkText(source, at+ptr("query", k), "request.query."+clip(k, idClipAt), r.Query[k])
	}
	c.checkBody(source, at+ptr("body"), r.Body)
	return r
}

// checkBody checks every name and every text of a body, at any depth.
func (c *collector) checkBody(source, at string, v any) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c.checkText(source, at+ptr(k), "request.body", k)
			c.checkBody(source, at+ptr(k), x[k])
		}
	case []any:
		for i, e := range x {
			c.checkBody(source, at+ptr(i), e)
		}
	case string:
		if _, ok := ParsePlaceholder(x); ok {
			return // a whole value; whether the authentication gives what it stands for is checked with the source
		}
		c.checkText(source, at, "request.body", x)
	}
}

// requestPathProblem says why a text is no path of a request, or "". A path begins with a slash and goes under the base
// address; it has no scheme, no host, no query, no fragment and no step that goes up.
func requestPathProblem(p string) string {
	switch {
	case p == "":
		return "is empty"
	case p[0] != '/':
		return "has to begin with /"
	case hasControl(p):
		return "has a space or a control character"
	case strings.Contains(p, "//"), strings.Contains(p, `\`), strings.ContainsAny(p, "?#"):
		return "has //, a backslash, a ? or a # in it; the query is its own field and the path never names another host"
	case !pathOK(p):
		return "has a . or .. step"
	}
	return ""
}

var paginationReads = map[string][]string{
	PageNone:       {},
	PagePage:       {"page_param", "size_param", "size", "first_page", "total_path"},
	PageOffset:     {"offset_param", "size_param", "size", "total_path"},
	PageCursor:     {"cursor_param", "cursor_in", "next_cursor_path", "size_param", "size"},
	PageNextURL:    {"next_path", "size_param", "size"},
	PageLinkHeader: {"rel", "size_param", "size"},
}

var paginationNeeds = map[string][]string{
	PagePage:    {"page_param", "size_param", "size", "first_page"},
	PageOffset:  {"offset_param", "size_param", "size"},
	PageCursor:  {"cursor_param", "cursor_in", "next_cursor_path"},
	PageNextURL: {"next_path"},
}

var paginationKinds = []string{PageNone, PagePage, PageOffset, PageCursor, PageNextURL, PageLinkHeader}

func (p Pagination) fieldsSet() []namedSet {
	return []namedSet{{"page_param", p.PageParam != ""}, {"offset_param", p.OffsetParam != ""}, {"size_param", p.SizeParam != ""}, {"size", p.Size != 0},
		{"first_page", p.FirstPage != nil}, {"total_path", p.TotalPath != ""}, {"cursor_param", p.CursorParam != ""}, {"cursor_in", p.CursorIn != ""},
		{"next_cursor_path", p.NextCursorPath != ""}, {"next_path", p.NextPath != ""}, {"rel", p.Rel != ""}}
}

// normalize checks the pagination of a dataset and gives it with its defaults.
func (p Pagination) normalize(c *collector, source string, d Dataset, at string) *Pagination {
	if !containsString(paginationKinds, p.Kind) {
		c.add(ecAdPageKind, at+ptr("kind"), Params{"source": source, "dataset": d.ID, "kind": clip(p.Kind, idClipAt), "supported": paginationKinds},
			"source %s, dataset %s: the kind of the pagination %q is none of none, page, offset, cursor, next_url, link_header", source, d.ID, clip(p.Kind, idClipAt))
		return &p
	}
	set := map[string]bool{}
	for _, f := range p.fieldsSet() {
		set[f.name] = f.set
		if f.set && !containsString(paginationReads[p.Kind], f.name) {
			c.add(ecAdPageNot, at+ptr(f.name), Params{"source": source, "dataset": d.ID, "kind": p.Kind, "field": f.name},
				"source %s, dataset %s: pagination.%s is not read by the kind %s", source, d.ID, f.name, p.Kind)
		}
	}
	for _, need := range paginationNeeds[p.Kind] {
		if !set[need] {
			c.add(ecAdPageMissing, at+ptr(need), Params{"source": source, "dataset": d.ID, "kind": p.Kind, "field": need},
				"source %s, dataset %s: the kind %s needs pagination.%s", source, d.ID, p.Kind, need)
		}
	}
	p.checkPair(c, source, d, at, set)
	p.checkValues(c, source, d, at)
	p.checkParams(c, source, d, at)
	if p.Kind == PageLinkHeader && p.Rel == "" {
		p.Rel = "next"
	}
	if !c.checkRange(ecAdPageRange, Params{"source": source, "dataset": d.ID}, "pagination.max_pages", at+ptr("max_pages"), float64(p.MaxPages), numBounds{1, 1_000_000}) || p.MaxPages == 0 {
		p.MaxPages = DefaultMaxPages
	}
	return &p
}

// checkPair: a page size that is optional is given by its parameter and its number together.
func (p Pagination) checkPair(c *collector, source string, d Dataset, at string, set map[string]bool) {
	if p.Kind == PagePage || p.Kind == PageOffset || !containsString(paginationReads[p.Kind], "size") || set["size"] == set["size_param"] {
		return
	}
	missing := "size"
	if set["size"] {
		missing = "size_param"
	}
	c.add(ecAdPageMissing, at+ptr(missing), Params{"source": source, "dataset": d.ID, "kind": p.Kind, "field": missing},
		"source %s, dataset %s: pagination.size and pagination.size_param go together; %s is not given", source, d.ID, missing)
}

func (p Pagination) checkValues(c *collector, source string, d Dataset, at string) {
	field := func(name string) string { return "pagination." + name }
	if p.Size != 0 {
		c.checkRange(ecAdPageRange, Params{"source": source, "dataset": d.ID}, field("size"), at+ptr("size"), float64(p.Size), numBounds{1, 100000})
	}
	if p.FirstPage != nil && *p.FirstPage != 0 && *p.FirstPage != 1 {
		c.add(ecAdPageRange, at+ptr("first_page"), Params{"source": source, "dataset": d.ID, "field": "pagination.first_page", "value": *p.FirstPage, "min": 0, "max": 1},
			"source %s, dataset %s: pagination.first_page is 0 or 1 (the number the first page has)", source, d.ID)
	}
	p.checkCursor(c, source, d, at)
	for _, f := range []struct{ name, text string }{{"total_path", p.TotalPath}, {"next_cursor_path", p.NextCursorPath}, {"next_path", p.NextPath}} {
		if f.text != "" {
			c.checkPath(source, d.ID, at+ptr(f.name), field(f.name), f.text)
		}
	}
}

// checkCursor checks where the cursor goes: in the query, or in the body of a POST.
func (p Pagination) checkCursor(c *collector, source string, d Dataset, at string) {
	if p.CursorIn != "" && p.CursorIn != InQuery && p.CursorIn != InBody {
		c.add(ecAdPageParam, at+ptr("cursor_in"), Params{"source": source, "dataset": d.ID, "field": "pagination.cursor_in", "reason": "is neither query nor body"},
			"source %s, dataset %s: pagination.cursor_in is query or body", source, d.ID)
	}
	if p.CursorIn == InBody && (d.Request == nil || d.Request.Method != MethodPost) {
		c.add(ecAdBodyPost, at+ptr("cursor_in"), Params{"source": source, "dataset": d.ID}, "source %s, dataset %s: a cursor sent in the body needs a POST request", source, d.ID)
	}
}

var paramNameRe = regexp.MustCompile(`^[A-Za-z0-9_.\-\[\]]{1,64}$`)

// checkParams checks the names of the parameters the pagination adds: they are names, and no two are the same or the same
// as a parameter of the query the definition wrote (which the pagination would overwrite).
func (p Pagination) checkParams(c *collector, source string, d Dataset, at string) {
	taken := map[string]bool{}
	if d.Request != nil {
		for k := range d.Request.Query {
			taken[k] = true
		}
	}
	for _, f := range []struct{ name, value string }{{"page_param", p.PageParam}, {"offset_param", p.OffsetParam}, {"size_param", p.SizeParam}, {"cursor_param", p.CursorParam}} {
		if f.value == "" {
			continue
		}
		reason := ""
		switch {
		case !paramNameRe.MatchString(f.value):
			reason = "is not a parameter name (letters, digits and _ - . [ ], up to 64)"
		case taken[f.value] && !(f.name == "cursor_param" && p.CursorIn == InBody):
			reason = "is the name of another parameter of the pagination or of the query"
		}
		taken[f.value] = true
		if reason != "" {
			c.add(ecAdPageParam, at+ptr(f.name), Params{"source": source, "dataset": d.ID, "field": "pagination." + f.name, "reason": reason},
				"source %s, dataset %s: pagination.%s %s", source, d.ID, f.name, reason)
		}
	}
	if p.Rel != "" && !isToken(p.Rel) {
		c.add(ecAdPageParam, at+ptr("rel"), Params{"source": source, "dataset": d.ID, "field": "pagination.rel", "reason": "is not a link relation name"},
			"source %s, dataset %s: pagination.rel is not a link relation name", source, d.ID)
	}
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]{0,62}$`)
var collationRe = regexp.MustCompile(`^[A-Za-z0-9_.@\-]{1,63}$`)

func (d Dataset) checkPostgres(c *collector, source, at string) {
	switch {
	case d.Table != "" && d.SQL != "":
		c.add(ecAdTableSQL, at+ptr("sql"), Params{"source": source, "dataset": d.ID, "reason": "has both"}, "source %s, dataset %s: give table or sql, not both", source, d.ID)
	case d.Table == "" && d.SQL == "":
		c.add(ecAdTableSQL, at, Params{"source": source, "dataset": d.ID, "reason": "has neither"}, "source %s, dataset %s: a dataset of a postgres source gives table or sql", source, d.ID)
	}
	if d.Table != "" {
		parts := strings.Split(d.Table, ".")
		bad := len(parts) > 2
		for _, p := range parts {
			bad = bad || !identRe.MatchString(p)
		}
		if bad {
			c.add(ecAdTable, at+ptr("table"), Params{"source": source, "dataset": d.ID, "table": clip(d.Table, idClipAt)},
				"source %s, dataset %s: table %q is not a name or a schema and a name of plain identifiers (letters, digits, _ and $, no quotes)", source, d.ID, clip(d.Table, idClipAt))
		}
	}
	if d.SQL != "" {
		d.checkSQL(c, source, at)
	}
	if d.KeyCollation != "" && !collationRe.MatchString(d.KeyCollation) {
		c.add(ecAdCollation, at+ptr("key_collation"), Params{"source": source, "dataset": d.ID, "key_collation": clip(d.KeyCollation, idClipAt)},
			"source %s, dataset %s: key_collation %q is not the name of a collation", source, d.ID, clip(d.KeyCollation, idClipAt))
	}
}

func (d Dataset) checkSQL(c *collector, source, at string) {
	if strings.TrimSpace(d.SQL) == "" || strings.ContainsRune(d.SQL, 0) {
		c.add(ecAdSQL, at+ptr("sql"), Params{"source": source, "dataset": d.ID, "reason": "is empty or has a NUL"}, "source %s, dataset %s: sql is empty or has a NUL character", source, d.ID)
		return
	}
	c.checkText(source, at+ptr("sql"), "sql of dataset "+d.ID, d.SQL)
}
