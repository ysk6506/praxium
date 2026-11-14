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

// The commands that manage who may call the server (ADR-0044): whoami, and admin user|account|token. They call the
// management routes of the server; they need an access token of an administrator (whoami, any token).

var (
	principalColumns = []string{"id", "kind", "roles", "state", "created_at", "display_name"}
	tokenColumns     = []string{"id", "owner", "roles", "state", "expires_at", "last_used_at", "description"}
)

// runWhoami says who the token is.
func runWhoami(ctx context.Context, c *Client, opts options, w io.Writer) error {
	if len(opts.rest) > 1 {
		return usageError{msg: "whoami takes no arguments"}
	}
	env, err := c.Get(ctx, "/v1/tokens/current", nil)
	if err != nil {
		return err
	}
	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return transportError{err: err}
	}
	if opts.asJSON {
		return printJSON(w, data)
	}
	flat := map[string]any{}
	if subject, ok := data["subject"].(map[string]any); ok {
		for k, v := range subject {
			flat[k] = v
		}
	}
	if token, ok := data["token"].(map[string]any); ok {
		for _, k := range []string{"state", "expires_at", "last_used_at", "description"} {
			flat["token_"+k] = token[k]
		}
		flat["token_id"] = token["id"]
	}
	return printFields(w, flat)
}

// adminResources are the resources `admin` manages and the routes of each.
var adminResources = map[string]struct{ path, noun string }{
	"user":    {"/v1/users", "user"},
	"account": {"/v1/system-accounts", "service account"},
}

func runAdmin(ctx context.Context, c *Client, opts options, w io.Writer) error {
	if len(opts.rest) < 3 {
		return usageError{msg: "admin needs a resource (user, account or token) and an action"}
	}
	resource, action := opts.rest[1], opts.rest[2]
	args := opts.rest[3:]
	if resource == "token" {
		return runAdminToken(ctx, c, adminCall{action: action, args: args, asJSON: opts.asJSON}, w)
	}
	res, ok := adminResources[resource]
	if !ok {
		return usageError{msg: fmt.Sprintf("unknown resource %q: user, account or token", resource)}
	}
	call := adminCall{action: action, args: args, asJSON: opts.asJSON}
	switch action {
	case "list":
		return adminList(ctx, c, call, adminListSpec{path: res.path, columns: principalColumns, filters: []string{"state"}}, w)
	case "create":
		return adminCreatePrincipal(ctx, c, call, res.path, w)
	case "disable":
		return adminDisable(ctx, c, call, res.path, w)
	}
	return usageError{msg: fmt.Sprintf("admin %s has no %q: list, create or disable", resource, action)}
}

type adminCall struct {
	action string
	args   []string
	asJSON bool
}

type adminListSpec struct {
	path    string
	columns []string
	filters []string
}

func adminFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// roleList turns the values of --role (repeated or with commas) into a list.
func roleList(values multiFlag) []string {
	var out []string
	for _, v := range values {
		for _, r := range strings.Split(v, ",") {
			if r = strings.TrimSpace(r); r != "" {
				out = append(out, r)
			}
		}
	}
	return out
}

func adminList(ctx context.Context, c *Client, call adminCall, spec adminListSpec, w io.Writer) error {
	fs := adminFlags("admin list")
	limit := fs.Int("limit", 0, "")
	all := fs.Bool("all", false, "")
	state := fs.String("state", "", "")
	owner := fs.String("owner", "", "")
	positional, err := parseMixed(fs, call.args)
	if err != nil {
		return err
	}
	if len(positional) > 0 {
		return usageError{msg: "list takes flags only"}
	}
	query := url.Values{}
	if *limit > 0 {
		query.Set("limit", strconv.Itoa(*limit))
	}
	if *state != "" {
		query.Set("filter[state][equals]", *state)
	}
	if *owner != "" {
		query.Set("filter[owner][equals]", *owner)
	}
	return listAll(ctx, c, listRequest{path: spec.path, query: query, all: *all, asJSON: call.asJSON, columns: spec.columns}, w)
}

func adminCreatePrincipal(ctx context.Context, c *Client, call adminCall, path string, w io.Writer) error {
	fs := adminFlags("admin create")
	var roles multiFlag
	fs.Var(&roles, "role", "")
	display := fs.String("display-name", "", "")
	description := fs.String("description", "", "")
	positional, err := parseMixed(fs, call.args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return usageError{msg: "create needs one name"}
	}
	body := map[string]any{"name": positional[0], "display_name": *display, "description": *description, "roles": roleList(roles)}
	env, err := c.Change(ctx, path, body)
	if err != nil {
		return err
	}
	return printOne(w, env, call.asJSON)
}

func adminDisable(ctx context.Context, c *Client, call adminCall, path string, w io.Writer) error {
	if len(call.args) != 1 || strings.HasPrefix(call.args[0], "-") {
		return usageError{msg: "disable needs one name"}
	}
	env, err := c.Change(ctx, path+"/"+url.PathEscape(call.args[0])+":disable", struct{}{})
	if err != nil {
		return err
	}
	return printOne(w, env, call.asJSON)
}

func runAdminToken(ctx context.Context, c *Client, call adminCall, w io.Writer) error {
	switch call.action {
	case "list":
		return adminList(ctx, c, call, adminListSpec{path: "/v1/tokens", columns: tokenColumns}, w)
	case "create":
		return adminCreateToken(ctx, c, call, w)
	case "revoke":
		if len(call.args) != 1 || strings.HasPrefix(call.args[0], "-") {
			return usageError{msg: "revoke needs one token id (from admin token list, not the token itself)"}
		}
		env, err := c.Change(ctx, "/v1/tokens/"+url.PathEscape(call.args[0])+":revoke", struct{}{})
		if err != nil {
			return err
		}
		return printOne(w, env, call.asJSON)
	}
	return usageError{msg: fmt.Sprintf("admin token has no %q: list, create or revoke", call.action)}
}

func adminCreateToken(ctx context.Context, c *Client, call adminCall, w io.Writer) error {
	fs := adminFlags("admin token create")
	owner := fs.String("owner", "", "")
	var roles multiFlag
	fs.Var(&roles, "role", "")
	days := fs.Int("expires-in-days", 0, "")
	description := fs.String("description", "", "")
	positional, err := parseMixed(fs, call.args)
	if err != nil {
		return err
	}
	if len(positional) != 0 || *owner == "" {
		return usageError{msg: "token create needs --owner NAME"}
	}
	body := map[string]any{"owner": *owner, "description": *description, "roles": roleList(roles), "expires_in_days": *days}
	env, err := c.Change(ctx, "/v1/tokens", body)
	if err != nil {
		return err
	}
	if err := printOne(w, env, call.asJSON); err != nil {
		return err
	}
	if !call.asJSON {
		_, err = fmt.Fprintln(w, "\nThis is the only time the token is shown. Keep it where only its owner can read it.")
	}
	return err
}
