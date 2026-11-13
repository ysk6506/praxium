package httpapi

import (
	"sort"
)

// routeSpec is one route the server registers: the same facts the contract
// states in x-required-role. The conformance test compares this table with the
// contract's paths in both directions.
type routeSpec struct {
	method string
	path   string
	roles  []string
	handle func(*call)
}

// Route is the public face of a routeSpec: what `praxium api routes` prints.
type Route struct {
	Method string   `json:"method"`
	Path   string   `json:"path"`
	Roles  []string `json:"roles"`
}

// Routes returns the routes a server registers, sorted by path and method.
// It builds the table without a server, so a check can list the routes cheaply.
func Routes() []Route {
	s := &Server{}
	specs := s.routeSpecs()
	out := make([]Route, 0, len(specs))
	for _, spec := range specs {
		out = append(out, Route{Method: spec.method, Path: spec.path, Roles: spec.roles})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

func get(path string, roles []string, h func(*call)) routeSpec {
	return routeSpec{method: "GET", path: path, roles: roles, handle: h}
}

var (
	anyone = []string{RoleAnonymous}
	authed = []string{RoleAuthenticated}
	reader = []string{RoleRead}
)

// routeSpecs is the route table of the first alpha. A route that is not here
// is either in the contract's planned registry or not designed yet.
func (s *Server) routeSpecs() []routeSpec {
	specs := []routeSpec{
		get("/v1/health/live", anyone, (*call).healthLive),
		get("/v1/health/ready", anyone, (*call).healthReady),
		get("/v1/health/started", anyone, (*call).healthStarted),
		get("/v1/contract", authed, (*call).contractDocument),
		get("/v1/problems/{code}", anyone, (*call).problemDetail),
		get("/v1/catalog", authed, listHandler(func(l *registryLists) *listDef { return l.catalogs })),
		get("/v1/catalog/link-meanings", authed, listHandler(func(l *registryLists) *listDef { return l.meanings })),
		get("/v1/catalog/channels", authed, listHandler(func(l *registryLists) *listDef { return l.channels })),
		get("/v1/catalog/error-codes", authed, listHandler(func(l *registryLists) *listDef { return l.errorCodes })),
		get("/v1/catalog/reason-codes", authed, listHandler(func(l *registryLists) *listDef { return l.reasonCodes })),
		get("/v1/roles", authed, listHandler(func(l *registryLists) *listDef { return l.roles })),
		get("/v1/value-types", reader, listHandler(func(l *registryLists) *listDef { return l.valueTypes })),
		get("/v1/layers", reader, listHandler(func(l *registryLists) *listDef { return l.layers })),
	}
	return append(specs, registryRoutes()...)
}

// registryRoutes are the read routes of the type registry (rest-api-resources-data.md).
func registryRoutes() []routeSpec {
	return []routeSpec{
		get("/v1/object-types", reader, listHandler(func(l *registryLists) *listDef { return l.objectTypes })),
		get("/v1/object-types/{type_id}", reader, (*call).objectTypeDetail),
		get("/v1/link-types", reader, listHandler(func(l *registryLists) *listDef { return l.linkTypes })),
		get("/v1/link-types/{link_type_id}", reader, (*call).linkTypeDetail),
		get("/v1/link-types/{link_type_id}/bindings", reader, (*call).linkTypeBindings),
		get("/v1/action-types", reader, listHandler(func(l *registryLists) *listDef { return l.actionTypes })),
		get("/v1/action-types/{action_type_id}", reader, (*call).actionTypeDetail),
		get("/v1/traits", reader, listHandler(func(l *registryLists) *listDef { return l.traits })),
		get("/v1/traits/{trait_id}", reader, (*call).traitDetail),
		get("/v1/identifier-schemes", reader, listHandler(func(l *registryLists) *listDef { return l.schemes })),
		get("/v1/identifier-schemes/{scheme_id}", reader, (*call).schemeDetail),
	}
}
