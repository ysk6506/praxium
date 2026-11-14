package httpapi

import (
	"net/http"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var admins = []string{"admin"}

// identityRoutes are the routes of the management of users, service accounts and tokens (ADR-0044). The actions
// are routed on a wildcard segment and read their verb in the handler (see routeSpec.mux).
func identityRoutes() []routeSpec {
	action := func(path, wild string, h func(*call)) routeSpec {
		base := path[:strings.LastIndex(path, "/")]
		return routeSpec{method: "POST", path: path, roles: admins, handle: h, mux: base + "/{" + wild + "}"}
	}
	revoke := routeSpec{method: "POST", path: "/v1/tokens/{token_id}:revoke", roles: authed, handle: (*call).revokeToken, mux: "/v1/tokens/{token_action}"}
	return []routeSpec{
		get("/v1/tokens/{token_id}", authed, (*call).getToken),
		get("/v1/users", admins, listPrincipals(access.SubjectPerson)),
		post("/v1/users", admins, createPrincipal(access.SubjectPerson)),
		action("/v1/users/{user_id}:disable", "user_action", disablePrincipal(access.SubjectPerson, "user_action")),
		get("/v1/system-accounts", admins, listPrincipals(access.SubjectSystemAccount)),
		post("/v1/system-accounts", admins, createPrincipal(access.SubjectSystemAccount)),
		action("/v1/system-accounts/{account_id}:disable", "account_action", disablePrincipal(access.SubjectSystemAccount, "account_action")),
		get("/v1/tokens", authed, (*call).listTokens),
		post("/v1/tokens", admins, (*call).issueToken),
		revoke,
	}
}

// directory is the management port, or a 404 when the server has none (the development authenticator).
func (c *call) directory() (access.Directory, bool) {
	if c.s.dir == nil {
		c.notFound("resource", c.r.URL.Path)
		return nil, false
	}
	c.w.Header().Set("Cache-Control", "no-store")
	return c.s.dir, true
}

// idError answers an error of the directory as a problem. The text of an invalid or conflicting request is the
// rule it broke; it never holds a secret, so it is safe to show. Any other error is the store's, and the caller
// is told to retry, not what is wrong inside.
func (c *call) idError(err error) {
	switch kernel.CodeOf(err) {
	case kernel.CodeInvalidValue:
		c.fail("invalid_input.field", map[string]any{"message": plainMessage(err)})
	case kernel.CodeNotFound:
		c.notFound("resource", c.r.URL.Path)
	case kernel.CodeConflict:
		c.fail("conflict.state_not_allowed", map[string]any{"message": plainMessage(err)})
	default:
		c.s.log.Error("the identity store failed", "error", err.Error(), "trace_id", c.trace)
		c.fail("server.unavailable", nil)
	}
}

// created answers 201 with the envelope.
func (c *call) created(data any) {
	c.writeJSON(http.StatusCreated, jsonMediaType, envelope{Data: data, Meta: map[string]any{"trace_id": c.trace}})
}

// actionID reads the id of `{id}:verb` from the wildcard segment; any other shape is a path that does not exist.
func (c *call) actionID(wild, verb string) (string, bool) {
	id, found := strings.CutSuffix(c.r.PathValue(wild), ":"+verb)
	if !found || id == "" {
		c.notFound("route", c.r.URL.Path)
		return "", false
	}
	return id, true
}

type principalBody struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Roles       []string `json:"roles"`
}

func listPrincipals(kind access.SubjectKind) func(*call) {
	return func(c *call) {
		dir, ok := c.directory()
		if !ok {
			return
		}
		items, err := dir.ListPrincipals(c.r.Context(), kind)
		if err != nil {
			c.idError(err)
			return
		}
		c.serveList(principalList(items), "identity", "", nil)
	}
}

func createPrincipal(kind access.SubjectKind) func(*call) {
	return func(c *call) {
		dir, ok := c.directory()
		if !ok {
			return
		}
		var body principalBody
		if !c.readIdempotent(&body) {
			return
		}
		req := access.PrincipalRequest{Kind: kind, Name: body.Name, DisplayName: body.DisplayName, Description: body.Description}
		for _, r := range body.Roles {
			role, known := access.ParseRole(r)
			if !known {
				c.fail("invalid_input.field", nil, field("/roles", "invalid_input.field_format", map[string]any{"reason": "unknown role " + r}))
				return
			}
			req.Roles = append(req.Roles, role)
		}
		p, err := dir.CreatePrincipal(c.r.Context(), c.subject, req)
		if err != nil {
			c.idError(err)
			return
		}
		c.created(principalView(p))
	}
}

func disablePrincipal(kind access.SubjectKind, wild string) func(*call) {
	return func(c *call) {
		dir, ok := c.directory()
		if !ok {
			return
		}
		id, ok := c.actionID(wild, "disable")
		if !ok || !c.readIdempotent(&struct{}{}) {
			return
		}
		p, err := dir.DisablePrincipal(c.r.Context(), c.subject, kind, id)
		if err != nil {
			c.idError(err)
			return
		}
		c.ok(principalView(p), nil)
	}
}
