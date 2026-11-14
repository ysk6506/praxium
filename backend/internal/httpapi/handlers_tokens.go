package httpapi

import (
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
)

type tokenBody struct {
	Owner         string   `json:"owner"`
	Description   string   `json:"description"`
	Roles         []string `json:"roles"`
	ExpiresInDays int      `json:"expires_in_days"`
}

// getToken answers GET /v1/tokens/{token_id}. The id "current" is the token of this request and then the answer
// also says who is calling. Any other token is shown to its owner and to an administrator, and for anyone else it
// is not found, as if it did not exist. The word current works without a directory: the development authenticator
// has a subject but no token record.
func (c *call) getToken() {
	id := c.r.PathValue("token_id")
	c.w.Header().Set("Cache-Control", "no-store")
	if id != "current" {
		c.otherToken(id)
		return
	}
	var token any
	if c.s.dir != nil && c.subject.TokenID != "" {
		if info, err := c.s.dir.GetToken(c.r.Context(), c.subject.TokenID); err == nil {
			token = tokenView(info)
		}
	}
	c.ok(map[string]any{
		"subject": map[string]any{
			"id": c.subject.ID, "kind": string(c.subject.Kind), "display_name": c.subject.DisplayName,
			"roles": roleNames(c.subject.Roles),
		},
		"token": token,
	}, nil)
}

func (c *call) otherToken(id string) {
	dir, ok := c.directory()
	if !ok {
		return
	}
	info, err := dir.GetToken(c.r.Context(), id)
	if err != nil {
		c.idError(err)
		return
	}
	if info.Owner != c.subject.ID && !c.isAdmin() {
		c.notFound("resource", c.r.URL.Path)
		return
	}
	c.ok(map[string]any{"token": tokenView(info)}, nil)
}

// isAdmin says whether the caller may see and revoke the tokens of others. The roles of a subject are the ones its
// token carries, so a token narrowed to a read role does not manage.
func (c *call) isAdmin() bool { return c.subject.Holds(access.RoleAdmin) }

func (c *call) listTokens() {
	dir, ok := c.directory()
	if !ok {
		return
	}
	owner := c.subject.ID
	if c.isAdmin() {
		owner = ""
	}
	items, err := dir.ListTokens(c.r.Context(), owner)
	if err != nil {
		c.idError(err)
		return
	}
	c.serveList(tokenList(items), "identity", "", nil)
}

func (c *call) issueToken() {
	dir, ok := c.directory()
	if !ok {
		return
	}
	var body tokenBody
	if !c.readIdempotent(&body) {
		return
	}
	req := access.TokenRequest{Owner: body.Owner, Description: body.Description, Lifetime: time.Duration(body.ExpiresInDays) * 24 * time.Hour}
	if body.ExpiresInDays < 0 || body.ExpiresInDays > 36500 {
		c.fail("invalid_input.field", nil, field("/expires_in_days", "invalid_input.field_format", map[string]any{"expected": "a number of days"}))
		return
	}
	for _, r := range body.Roles {
		role, known := access.ParseRole(r)
		if !known {
			c.fail("invalid_input.field", nil, field("/roles", "invalid_input.field_format", map[string]any{"reason": "unknown role " + r}))
			return
		}
		req.Roles = append(req.Roles, role)
	}
	issued, err := dir.IssueToken(c.r.Context(), c.subject, req)
	if err != nil {
		c.idError(err)
		return
	}
	view := tokenView(issued.Info)
	view["token"] = issued.Secret
	c.idem.secret = true
	c.created(view)
}

func (c *call) revokeToken() {
	dir, ok := c.directory()
	if !ok {
		return
	}
	id, ok := c.actionID("token_action", "revoke")
	if !ok {
		return
	}
	info, err := dir.GetToken(c.r.Context(), id)
	if err != nil {
		c.idError(err)
		return
	}
	if info.Owner != c.subject.ID && !c.isAdmin() {
		c.notFound("resource", c.r.URL.Path)
		return
	}
	if !c.readIdempotent(&struct{}{}) {
		return
	}
	info, err = dir.RevokeToken(c.r.Context(), c.subject, id)
	if err != nil {
		c.idError(err)
		return
	}
	c.ok(tokenView(info), nil)
}
