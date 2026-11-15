package main

import (
	"flag"
	"net/url"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/devauth"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/kgread"
	"github.com/ysk6506/praxium/backend/internal/mcpapi"
	"github.com/ysk6506/praxium/backend/internal/model"
	"log/slog"
)

// mcpOptions are the flags of the MCP interface (ADR-0062, ADR-0068): whether it is on, and which browser origins may
// call it. The interface is read-only and answers at POST /mcp of the same server and port.
type mcpOptions struct {
	// on is the value of -mcp; set says whether the flag was given. Not given, the mode decides: production is off,
	// the development modes (api serve -mode development, demo, local) are on.
	on, set bool
	// origins is the comma-separated list of -mcp-allowed-origins.
	origins string
}

// addMCPFlags puts the flags on a command. defaultOn is the default of -mcp for the command; api serve decides by
// mode instead (see mcpOptions.set), so it shows the production default.
func addMCPFlags(fs *flag.FlagSet, o *mcpOptions, defaultOn bool) {
	fs.BoolVar(&o.on, "mcp", defaultOn, "serve the read-only MCP interface at POST /mcp (default: on in demo, local and -mode development, off in production)")
	fs.StringVar(&o.origins, "mcp-allowed-origins", "", "origins of browsers that may call /mcp, comma separated, such as https://console.example (default: none)")
}

// noteMCPFlags remembers which of the flags were given on the command line.
func noteMCPFlags(fs *flag.FlagSet, o *mcpOptions) {
	fs.Visit(func(f *flag.Flag) { o.set = o.set || f.Name == "mcp" })
}

// enabledFor says whether the interface is on for a server in the mode.
func (o mcpOptions) enabledFor(mode string) bool {
	if o.set {
		return o.on
	}
	return strings.ToLower(strings.TrimSpace(mode)) != devauth.ModeProduction
}

// allowedOrigins reads the list of origins and says what is wrong with an entry that is not an origin.
func (o mcpOptions) allowedOrigins() ([]string, error) {
	var out []string
	for _, entry := range strings.Split(o.origins, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		u, err := url.Parse(entry)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "-mcp-allowed-origins wants origins such as https://console.example (a scheme, a host and no path), got %q", entry)
		}
		out = append(out, entry)
	}
	return out, nil
}

// mcpMounts builds the handler of /mcp for a server and says where to mount it. The interface is built even when it is
// off, so /mcp and /.well-known/ answer a JSON 404 and never the page of the web UI.
func mcpMounts(p serverPlan, kit authKit, d wiring, log *slog.Logger, reg *model.Registry, _ graph.Store) ([]httpapi.Mount, error) {
	origins, err := p.opts.mcp.allowedOrigins()
	if err != nil {
		return nil, err
	}
	production := strings.ToLower(strings.TrimSpace(p.opts.mode)) == devauth.ModeProduction
	h, err := mcpapi.New(mcpapi.Config{
		Enabled:             p.opts.mcp.enabledFor(p.opts.mode),
		Gate:                kgread.Gate{Authn: kit.authn, Authz: access.RoleTable{}, Limiter: kit.limiter, Log: log},
		ServiceAccountsOnly: production,
		AllowedOrigins:      origins,
		LoopbackBound:       isLoopbackAddr(p.opts.addr),
		TrustProxy:          p.opts.behindProxy,
		Info:                mcpapi.Info{Name: "praxium", Version: version, ModelVersion: strconv.Itoa(reg.ModelVersion()), ModelHash: reg.Hash().String()},
		Directory:           kit.dir,
		Clock:               d.clock, IDs: d.ids, Log: log,
	})
	if err != nil {
		return nil, err
	}
	return []httpapi.Mount{{Match: h.Match, Handler: h}}, nil
}
