package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// readFlags are the flags the reads of a KG share: which KG, which point in its
// history, which view, and how many pages.
type readFlags struct {
	kg      string
	atSeq   int64
	version string
	view    string
	minSeq  int64
	wait    string
	limit   int
	all     bool
	summary bool
}

func newReadFlags(name string) (*flag.FlagSet, *readFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rf := &readFlags{}
	fs.StringVar(&rf.kg, "kg", "main", "")
	fs.Int64Var(&rf.atSeq, "at-seq", -1, "")
	fs.StringVar(&rf.version, "version", "", "")
	fs.StringVar(&rf.view, "view", "", "")
	fs.Int64Var(&rf.minSeq, "min-seq", 0, "")
	fs.StringVar(&rf.wait, "wait", "", "")
	fs.IntVar(&rf.limit, "limit", 0, "")
	fs.BoolVar(&rf.all, "all", false, "")
	fs.BoolVar(&rf.summary, "summary", false, "")
	return fs, rf
}

// body is the common part of the body of a question.
func (rf *readFlags) body() map[string]any {
	b := map[string]any{"shape": "items"}
	if rf.summary {
		b["shape"] = "summary"
	}
	set := func(name string, value any, ok bool) {
		if ok {
			b[name] = value
		}
	}
	set("change_seq", rf.atSeq, rf.atSeq >= 0)
	set("kg_version_id", rf.version, rf.version != "")
	set("view", rf.view, rf.view != "")
	set("min_seq", rf.minSeq, rf.minSeq > 0)
	set("wait_timeout", rf.wait, rf.wait != "")
	set("limit", rf.limit, rf.limit > 0)
	return b
}

// query is the common part of the query of a GET.
func (rf *readFlags) query() url.Values {
	q := url.Values{"shape": {"items"}}
	if rf.summary {
		q.Set("shape", "summary")
	}
	if rf.atSeq >= 0 {
		q.Set("change_seq", strconv.FormatInt(rf.atSeq, 10))
	}
	for name, value := range map[string]string{"kg_version_id": rf.version, "view": rf.view, "wait_timeout": rf.wait} {
		if value != "" {
			q.Set(name, value)
		}
	}
	if rf.minSeq > 0 {
		q.Set("min_seq", strconv.FormatInt(rf.minSeq, 10))
	}
	if rf.limit > 0 {
		q.Set("limit", strconv.Itoa(rf.limit))
	}
	return q
}

func kgPath(kg string) string { return "/v1/kgs/" + url.PathEscape(kg) }

func runKG(ctx context.Context, c *Client, opts options, w io.Writer) error {
	if len(opts.rest) < 2 {
		return usageError{msg: "kg needs list or get"}
	}
	switch opts.rest[1] {
	case "list":
		fs := flag.NewFlagSet("kg list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		role, state := fs.String("role", "", ""), fs.String("state", "", "")
		limit, all := fs.Int("limit", 0, ""), fs.Bool("all", false, "")
		if _, err := parseMixed(fs, opts.rest[2:]); err != nil {
			return err
		}
		query := url.Values{}
		if *role != "" {
			query.Set("filter[role][equals]", *role)
		}
		if *state != "" {
			query.Set("filter[state][equals]", *state)
		}
		if *limit > 0 {
			query.Set("limit", strconv.Itoa(*limit))
		}
		columns := []string{"kg_id", "name", "alias", "role", "state", "generation", "head_change_seq", "readable_through"}
		return listAll(ctx, c, listRequest{path: "/v1/kgs", query: query, all: *all, asJSON: opts.asJSON, columns: columns}, w)
	case "get":
		if len(opts.rest) > 3 {
			return usageError{msg: "kg get takes one KG"}
		}
		ref := "main"
		if len(opts.rest) == 3 {
			ref = opts.rest[2]
		}
		env, err := c.Get(ctx, kgPath(ref), nil)
		if err != nil {
			return err
		}
		return printOne(w, env, opts.asJSON)
	}
	return usageError{msg: fmt.Sprintf("kg has no %q", opts.rest[1])}
}

func runObject(ctx context.Context, c *Client, opts options, w io.Writer) error {
	if len(opts.rest) < 2 {
		return usageError{msg: "object needs get or links"}
	}
	fs, rf := newReadFlags("object " + opts.rest[1])
	include := fs.String("include", "", "")
	direction := fs.String("direction", "", "")
	steps := fs.Int("steps", 0, "")
	var linkTypes multiFlag
	fs.Var(&linkTypes, "link-type", "")
	positional, err := parseMixed(fs, opts.rest[2:])
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return usageError{msg: "object " + opts.rest[1] + " needs one object id"}
	}
	path := kgPath(rf.kg) + "/objects/" + url.PathEscape(positional[0])
	switch opts.rest[1] {
	case "get":
		q := rf.query()
		if *include != "" {
			q.Set("include", *include)
		}
		env, err := c.Get(ctx, path, q)
		if err != nil {
			return err
		}
		return printEnvelope(w, env, opts.asJSON, printGetData)
	case "links":
		q := rf.query()
		if *direction != "" {
			q.Set("filter[direction][equals]", *direction)
		}
		if len(linkTypes) > 0 {
			q.Set("filter[link_type][in]", strings.Join(linkTypes, ","))
		}
		if *steps > 0 {
			q.Set("max_steps", strconv.Itoa(*steps))
		}
		return runPages(rf, opts, w, printNeighborsData, func(cursor string) (*Envelope, error) {
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			return c.Get(ctx, path+"/links", q)
		})
	}
	return usageError{msg: fmt.Sprintf("object has no %q", opts.rest[1])}
}

func runQuery(ctx context.Context, c *Client, opts options, w io.Writer) error {
	if len(opts.rest) < 2 {
		return usageError{msg: "query needs find, get or neighbors"}
	}
	switch opts.rest[1] {
	case "find":
		return askFind(ctx, c, opts, w)
	case "get":
		return askGet(ctx, c, opts, w)
	case "neighbors":
		return askNeighbors(ctx, c, opts, w)
	}
	return usageError{msg: fmt.Sprintf("query has no %q", opts.rest[1])}
}

// identifierBody reads scheme:value.
func identifierBody(text string) (map[string]any, error) {
	scheme, value, ok := strings.Cut(text, ":")
	if !ok || scheme == "" || value == "" {
		return nil, usageError{msg: fmt.Sprintf("--identifier %q: write scheme:value", text)}
	}
	return map[string]any{"scheme": scheme, "value": value}, nil
}

// attributeBody reads name.op=value; for in and range the value is a comma list or from..to.
func attributeBody(text string) (map[string]any, error) {
	key, value, ok := strings.Cut(text, "=")
	name, op, dotted := strings.Cut(key, ".")
	if !ok || !dotted || name == "" || op == "" {
		return nil, usageError{msg: fmt.Sprintf("--attr %q: write name.op=value, such as model.equals=M1", text)}
	}
	values := []string{value}
	switch op {
	case "in":
		values = strings.Split(value, ",")
	case "range":
		from, to, found := strings.Cut(value, "..")
		if !found {
			return nil, usageError{msg: fmt.Sprintf("--attr %q: a range is from..to", text)}
		}
		values = []string{from, to}
	}
	return map[string]any{"name": name, "op": op, "values": values}, nil
}

func askFind(ctx context.Context, c *Client, opts options, w io.Writer) error {
	fs, rf := newReadFlags("query find")
	typ := fs.String("type", "", "")
	noSub := fs.Bool("no-subtypes", false, "")
	trait, text := fs.String("trait", "", ""), fs.String("text", "", "")
	identifier := fs.String("identifier", "", "")
	var attrs multiFlag
	fs.Var(&attrs, "attr", "")
	if _, err := parseMixed(fs, opts.rest[2:]); err != nil {
		return err
	}
	body := rf.body()
	for name, value := range map[string]string{"object_type": *typ, "trait": *trait, "text": *text} {
		if value != "" {
			body[name] = value
		}
	}
	if *noSub {
		body["include_subtypes"] = false
	}
	if *identifier != "" {
		id, err := identifierBody(*identifier)
		if err != nil {
			return err
		}
		body["identifier"] = id
	}
	for _, a := range attrs {
		item, err := attributeBody(a)
		if err != nil {
			return err
		}
		body["attributes"] = append(sliceOf(body["attributes"]), item)
	}
	return runPages(rf, opts, w, printFindData, func(cursor string) (*Envelope, error) {
		if cursor != "" {
			body["cursor"] = cursor
		}
		return c.Post(ctx, kgPath(rf.kg)+"/queries:find", body)
	})
}

func sliceOf(v any) []any {
	s, _ := v.([]any)
	return s
}

func askGet(ctx context.Context, c *Client, opts options, w io.Writer) error {
	fs, rf := newReadFlags("query get")
	include := fs.String("include", "", "")
	identifier := fs.String("identifier", "", "")
	positional, err := parseMixed(fs, opts.rest[2:])
	if err != nil {
		return err
	}
	if (len(positional) == 1) == (*identifier != "") {
		return usageError{msg: "query get needs an object id or --identifier scheme:value"}
	}
	body := rf.body()
	if len(positional) == 1 {
		body["object_id"] = positional[0]
	} else if body["identifier"], err = identifierBody(*identifier); err != nil {
		return err
	}
	if *include != "" {
		body["include"] = strings.Split(*include, ",")
	}
	env, err := c.Post(ctx, kgPath(rf.kg)+"/queries:get", body)
	if err != nil {
		return err
	}
	return printEnvelope(w, env, opts.asJSON, printGetData)
}

func askNeighbors(ctx context.Context, c *Client, opts options, w io.Writer) error {
	fs, rf := newReadFlags("query neighbors")
	direction := fs.String("direction", "", "")
	steps := fs.Int("steps", 0, "")
	var linkTypes, meanings, peerTypes multiFlag
	fs.Var(&linkTypes, "link-type", "")
	fs.Var(&meanings, "meaning", "")
	fs.Var(&peerTypes, "peer-type", "")
	positional, err := parseMixed(fs, opts.rest[2:])
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return usageError{msg: "query neighbors needs one object id"}
	}
	body := rf.body()
	body["object_id"] = positional[0]
	for name, list := range map[string]multiFlag{"link_types": linkTypes, "meanings": meanings, "peer_types": peerTypes} {
		if len(list) > 0 {
			body[name] = []string(list)
		}
	}
	if *direction != "" {
		body["direction"] = *direction
	}
	if *steps > 0 {
		body["max_steps"] = *steps
	}
	return runPages(rf, opts, w, printNeighborsData, func(cursor string) (*Envelope, error) {
		if cursor != "" {
			body["cursor"] = cursor
		}
		return c.Post(ctx, kgPath(rf.kg)+"/queries:neighbors", body)
	})
}

// runPages reads one page, or every page with --all, merges the items and prints the result.
func runPages(rf *readFlags, opts options, w io.Writer, show func(io.Writer, map[string]any) error, fetch func(string) (*Envelope, error)) error {
	var data map[string]any
	var items []any
	var last map[string]any
	cursor := ""
	for {
		env, err := fetch(cursor)
		if err != nil {
			return err
		}
		var page map[string]any
		if err := json.Unmarshal(env.Data, &page); err != nil {
			return transportError{err: err}
		}
		if data == nil {
			data = page
		}
		items = append(items, sliceOf(page["items"])...)
		last = env.Meta
		if cursor = nextCursor(env.Meta); !rf.all || cursor == "" {
			break
		}
	}
	if _, has := data["items"]; has {
		data["items"] = items
	}
	if opts.asJSON {
		return printJSON(w, data)
	}
	if err := show(w, data); err != nil {
		return err
	}
	return printFooter(w, last, len(items))
}

// printFooter says how much of the answer is shown when there is more.
func printFooter(w io.Writer, meta map[string]any, shown int) error {
	done, _ := meta["result_complete"].(map[string]any)
	total, _ := done["total"].(float64)
	if next := nextCursor(meta); next != "" {
		_, err := fmt.Fprintf(w, "\n%d shown of %d; --all reads every page\n", shown, int(total))
		return err
	}
	return nil
}

// printEnvelope prints the data of one answer, as JSON or in the human form.
func printEnvelope(w io.Writer, env *Envelope, asJSON bool, show func(io.Writer, map[string]any) error) error {
	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return transportError{err: err}
	}
	if asJSON {
		return printJSON(w, data)
	}
	return show(w, data)
}

func rowsOf(v any) []map[string]any {
	var rows []map[string]any
	for _, item := range sliceOf(v) {
		if row, ok := item.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func printFindData(w io.Writer, data map[string]any) error {
	if err := printTable(w, rowsOf(data["counts_by_type"]), []string{"object_type", "count"}); err != nil {
		return err
	}
	if _, has := data["items"]; !has {
		return nil
	}
	fmt.Fprintln(w)
	return printTable(w, rowsOf(data["items"]), []string{"object_id", "object_type", "display_name"})
}

func printNeighborsData(w io.Writer, data map[string]any) error {
	if obj, ok := data["object"].(map[string]any); ok {
		fmt.Fprintf(w, "%s  %s  %s\n\n", scalar(obj["object_id"]), scalar(obj["object_type"]), scalar(obj["display_name"]))
	}
	if err := printTable(w, rowsOf(data["groups"]), []string{"link_type", "meaning", "direction", "peer_type", "count"}); err != nil {
		return err
	}
	if _, has := data["items"]; !has {
		return nil
	}
	fmt.Fprintln(w)
	return printTable(w, rowsOf(data["items"]), []string{"step", "link_type", "direction", "peer_object_id", "peer_type", "peer_display_name", "grade"})
}

func printGetData(w io.Writer, data map[string]any) error {
	if obj, ok := data["object"].(map[string]any); ok {
		if err := printFields(w, obj); err != nil {
			return err
		}
	}
	for _, part := range []struct {
		key     string
		columns []string
	}{
		{"attributes", []string{"name", "value", "unit", "grade", "basis", "seen_at", "stale"}},
		{"identifiers", []string{"scheme", "value", "scope"}},
	} {
		if _, has := data[part.key]; has {
			fmt.Fprintf(w, "\n%s\n", part.key)
			if err := printTable(w, rowsOf(data[part.key]), part.columns); err != nil {
				return err
			}
		}
	}
	if v, ok := data["link_counts"].(map[string]any); ok {
		fmt.Fprintf(w, "\nlink_counts  out %s  in %s\n", scalar(v["out"]), scalar(v["in"]))
		if err := printTable(w, rowsOf(v["groups"]), []string{"link_type", "direction", "count"}); err != nil {
			return err
		}
	}
	if v, ok := data["evidence"].(map[string]any); ok {
		counts, _ := v["counts"].(map[string]any)
		fmt.Fprintf(w, "\nevidence  min_grade %s  fact %s  assumed %s  unknown %s\n", scalar(v["min_grade"]),
			scalar(counts["fact"]), scalar(counts["assumed"]), scalar(counts["unknown"]))
		if worst, ok := v["worst_freshness"].(map[string]any); ok {
			fmt.Fprintf(w, "oldest  %s (%s s)\n", scalar(worst["attribute"]), scalar(worst["oldest_age_seconds"]))
		}
	}
	return nil
}
