package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
)

// IsClientCommand says whether the first word of the command line is a client command.
func IsClientCommand(word string) bool {
	return word == "health" || word == "types" || word == "catalog" || word == "kg" || word == "object" || word == "query"
}

// IsClientFlag says whether a word is one of the flags that may open a client command line.
func IsClientFlag(word string) bool {
	name, _, _ := strings.Cut(strings.TrimLeft(word, "-"), "=")
	if word == name {
		return false
	}
	return name == "endpoint" || name == "token" || name == "config" || name == "json"
}

// Run runs a client command line and returns the exit code. httpClient may be
// nil. Errors are written to stderr as one line; the exit code carries the kind.
// A wrong command line adds its one usage line and where the help is, never the whole list.
func Run(args []string, env Env, stdout, stderr io.Writer, httpClient *http.Client) int {
	opts, err := parseGlobal(args, env)
	if len(opts.rest) > 0 && localCommands[opts.rest[0]] {
		err = usageError{msg: unknownCommandMessage(opts.rest[0])}
	}
	if err == nil {
		if text, asked := helpOf(opts.rest); asked {
			fmt.Fprint(stdout, text)
			return ExitOK
		}
		err = run(opts, stdout, httpClient)
	}
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %v\n", err)
		if _, ok := err.(usageError); ok {
			path := commandWords(opts.rest)
			fmt.Fprintf(stderr, "%s\nFor details: praxium %s --help\n", UsageLine(path), strings.TrimSpace(strings.Join(firstWords(path, 1), " ")))
		}
	}
	return ExitCode(err)
}

// helpOf answers a command line that asks for help: -h or --help anywhere after the command.
func helpOf(rest []string) (string, bool) {
	for _, word := range rest {
		if !isHelpFlag(word) {
			continue
		}
		path := commandWords(rest)
		for n := len(path); n > 0; n-- {
			if text, ok := Help(path[:n]); ok {
				return text, true
			}
		}
	}
	return "", false
}

// commandWords are the words of the command before its first flag: "kg", "list".
func commandWords(rest []string) []string {
	var words []string
	for _, w := range rest {
		if strings.HasPrefix(w, "-") {
			break
		}
		words = append(words, w)
	}
	return firstWords(words, 2)
}

func firstWords(words []string, n int) []string {
	if len(words) > n {
		return words[:n]
	}
	return words
}

type options struct {
	settings Settings
	asJSON   bool
	rest     []string
}

// globalValue are the global flags that take a value; --json takes none.
var globalValue = map[string]bool{"endpoint": true, "token": true, "config": true}

// hoistGlobals takes the global flags that stand after the command out of its words, so the flags may be put
// before or after the command. It stops at "--".
func hoistGlobals(rest []string) (kept []string, found map[string]string, err error) {
	found = map[string]string{}
	for i := 0; i < len(rest); i++ {
		word := rest[i]
		if word == "--" {
			return append(kept, rest[i:]...), found, nil
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(word, "-"), "=")
		isFlag := strings.HasPrefix(word, "-") && (globalValue[name] || name == "json")
		switch {
		case !isFlag:
			kept = append(kept, word)
		case name == "json":
			found[name] = "true"
			if hasValue {
				found[name] = value
			}
		case hasValue:
			found[name] = value
		case i+1 < len(rest):
			i++
			found[name] = rest[i]
		default:
			return nil, nil, usageError{msg: "--" + name + " needs a value"}
		}
	}
	return kept, found, nil
}

func parseGlobal(args []string, env Env) (options, error) {
	fs := flag.NewFlagSet("praxium", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	endpoint := fs.String("endpoint", "", "")
	token := fs.String("token", "", "")
	configPath := fs.String("config", "", "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return options{}, usageError{msg: flagMessage(err)}
	}
	rest, later, err := hoistGlobals(fs.Args())
	if err != nil {
		return options{}, err
	}
	for name, dst := range map[string]*string{"endpoint": endpoint, "token": token, "config": configPath} {
		if v, ok := later[name]; ok {
			*dst = v
		}
	}
	if v, ok := later["json"]; ok {
		*asJSON = v != "false"
	}
	settings, err := Resolve(*endpoint, *token, *configPath, env)
	if err != nil {
		return options{rest: rest}, err
	}
	return options{settings: settings, asJSON: *asJSON, rest: rest}, nil
}

// flagMessage is the words of a flag error of the standard library, in the words of this program.
func flagMessage(err error) string {
	text := err.Error()
	if name, ok := strings.CutPrefix(text, "flag provided but not defined: "); ok {
		return "unknown flag " + name
	}
	return text
}

func run(opts options, stdout io.Writer, httpClient *http.Client) error {
	if len(opts.rest) == 0 {
		return usageError{msg: "no command"}
	}
	client := NewClient(opts.settings, httpClient)
	ctx := context.Background()
	switch opts.rest[0] {
	case "health":
		return runHealth(ctx, client, opts, stdout)
	case "types":
		return runTypes(ctx, client, opts, stdout)
	case "catalog":
		return runCatalog(ctx, client, opts, stdout)
	case "kg":
		return runKG(ctx, client, opts, stdout)
	case "object":
		return runObject(ctx, client, opts, stdout)
	case "query":
		return runQuery(ctx, client, opts, stdout)
	}
	return usageError{msg: unknownCommandMessage(opts.rest[0])}
}

// localCommands are the commands that run on this computer and never ask a server, so the flags that say which server
// to ask (--endpoint, --token, --config, --json) are not theirs.
var localCommands = map[string]bool{"ingest": true, "demo": true, "local": true, "worldgen": true, "api": true, "contract": true, "version": true, "dev": true}

// unknownCommandMessage names the command that is not one of the commands that ask a server. A command that does not
// ask one, written after the flags of one, is the mistake of a person who thinks it takes them.
func unknownCommandMessage(word string) string {
	if localCommands[word] {
		return fmt.Sprintf("%s does not ask a server, so it does not take --endpoint, --token, --config or --json; write %s first, then its own flags", word, word)
	}
	return fmt.Sprintf("unknown command %q", word)
}

func runHealth(ctx context.Context, c *Client, opts options, w io.Writer) error {
	results := map[string]any{}
	failed := false
	for _, probe := range []string{"live", "ready", "started"} {
		status, err := c.Status(ctx, "/v1/health/"+probe)
		if err != nil {
			return err
		}
		results[probe] = status
		failed = failed || status != http.StatusOK
	}
	if opts.asJSON {
		if err := printJSON(w, results); err != nil {
			return err
		}
	} else if err := printHealth(w, results); err != nil {
		return err
	}
	if failed {
		return &ProblemError{Status: http.StatusServiceUnavailable, Code: "server.unavailable", Title: "the server is not ready"}
	}
	return nil
}

// printHealth writes one line for each probe: its name and whether it answered 200.
func printHealth(w io.Writer, results map[string]any) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, probe := range []string{"live", "ready", "started"} {
		status, _ := results[probe].(int)
		fmt.Fprintf(tw, "%s\t%s\n", probe, healthText(status))
	}
	return tw.Flush()
}

// kindPaths maps the --kind of types to the route.
var kindPaths = map[string]string{
	"object": "/v1/object-types", "link": "/v1/link-types", "action": "/v1/action-types",
	"trait": "/v1/traits", "scheme": "/v1/identifier-schemes",
}

var kindColumns = map[string][]string{
	"object": {"id", "type_no", "name", "layer", "namespace", "status"},
	"link":   {"id", "link_type_no", "name", "meaning", "namespace", "status"},
	"action": {"id", "name", "namespace", "disruption", "status"},
	"trait":  {"id", "name", "description"},
	"scheme": {"id", "name", "namespace", "normalizer"},
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func runTypes(ctx context.Context, c *Client, opts options, w io.Writer) error {
	if len(opts.rest) < 2 {
		return usageError{msg: "types needs list or get"}
	}
	fs := flag.NewFlagSet("types", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	kind := fs.String("kind", "object", "")
	limit := fs.Int("limit", 0, "")
	all := fs.Bool("all", false, "")
	include := fs.String("include", "", "")
	var filters multiFlag
	fs.Var(&filters, "filter", "")
	positional, err := parseMixed(fs, opts.rest[2:])
	if err != nil {
		return err
	}
	path, ok := kindPaths[*kind]
	if !ok {
		return usageError{msg: fmt.Sprintf("unknown kind %q", *kind)}
	}
	switch opts.rest[1] {
	case "list":
		query, err := listQuery(filters, *limit)
		if err != nil {
			return err
		}
		return listAll(ctx, c, listRequest{path: path, query: query, all: *all, asJSON: opts.asJSON, columns: kindColumns[*kind]}, w)
	case "get":
		if len(positional) != 1 {
			return usageError{msg: "types get needs one id or name"}
		}
		query := url.Values{}
		if *include != "" {
			query.Set("include", *include)
		}
		readable := !opts.asJSON && *kind == "object"
		if readable && *include == "" {
			query.Set("include", "effective_properties")
		}
		env, err := c.Get(ctx, path+"/"+url.PathEscape(positional[0]), query)
		if err != nil {
			return err
		}
		if readable {
			return printTypeView(w, env)
		}
		return printOne(w, env, opts.asJSON)
	}
	return usageError{msg: fmt.Sprintf("types has no %q", opts.rest[1])}
}

// listQuery turns --filter field.op=value into filter[field][op]=value.
func listQuery(filters []string, limit int) (url.Values, error) {
	query := url.Values{}
	for _, f := range filters {
		key, value, ok := strings.Cut(f, "=")
		field, op, dotted := strings.Cut(key, ".")
		if !ok || !dotted || field == "" || op == "" {
			return nil, usageError{msg: fmt.Sprintf("--filter %q: write field.op=value, such as layer.equals=service", f)}
		}
		query.Add("filter["+field+"]["+op+"]", value)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	return query, nil
}

func runCatalog(ctx context.Context, c *Client, opts options, w io.Writer) error {
	paths := map[string]string{
		"": "/v1/catalog", "link-meanings": "/v1/catalog/link-meanings", "channels": "/v1/catalog/channels",
		"error-codes": "/v1/catalog/error-codes", "reason-codes": "/v1/catalog/reason-codes",
		"roles": "/v1/roles", "value-types": "/v1/value-types", "layers": "/v1/layers",
	}
	name := ""
	if len(opts.rest) > 1 {
		name = opts.rest[1]
	}
	if len(opts.rest) > 2 {
		return usageError{msg: "catalog takes one name"}
	}
	path, ok := paths[name]
	if !ok {
		return usageError{msg: fmt.Sprintf("unknown catalog %q", name)}
	}
	return listAll(ctx, c, listRequest{path: path, query: url.Values{}, all: true, asJSON: opts.asJSON}, w)
}

// listRequest is one list to read and print.
type listRequest struct {
	path    string
	query   url.Values
	all     bool
	asJSON  bool
	columns []string
}

// listAll reads one page, or every page with all, and prints it.
func listAll(ctx context.Context, c *Client, req listRequest, w io.Writer) error {
	path, query, all, asJSON, columns := req.path, req.query, req.all, req.asJSON, req.columns
	var rows []map[string]any
	for {
		env, err := c.Get(ctx, path, query)
		if err != nil {
			return err
		}
		var page []map[string]any
		if err := json.Unmarshal(env.Data, &page); err != nil {
			return transportError{err: err}
		}
		rows = append(rows, page...)
		next := nextCursor(env.Meta)
		if !all || next == "" {
			break
		}
		query.Set("cursor", next)
	}
	if asJSON {
		return printJSON(w, rows)
	}
	return printTable(w, rows, columns)
}

func nextCursor(meta map[string]any) string {
	done, _ := meta["result_complete"].(map[string]any)
	next, _ := done["next_cursor"].(string)
	return next
}

func printOne(w io.Writer, env *Envelope, asJSON bool) error {
	var obj map[string]any
	if err := json.Unmarshal(env.Data, &obj); err != nil {
		return transportError{err: err}
	}
	if asJSON {
		return printJSON(w, obj)
	}
	return printFields(w, obj)
}

// parseMixed parses flags that may come before or after the positional words.
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, usageError{msg: flagMessage(err)}
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	return positional, nil
}
