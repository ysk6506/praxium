package httpapi

import (
	"sort"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
)

// The views of the management API (ADR-0044). None of them can hold a secret or a hash: TokenInfo has no field for
// either, and only the answer to the issue of a token adds the token itself.

func timeText(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func roleNames(roles []access.Role) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, string(r))
	}
	return out
}

func principalView(p access.Principal) map[string]any {
	state := "active"
	if p.Disabled {
		state = "disabled"
	}
	return map[string]any{
		"id": p.ID, "kind": string(p.Kind), "display_name": p.DisplayName, "description": p.Description,
		"roles": roleNames(p.Roles), "state": state, "created_at": timeText(p.CreatedAt), "created_by": p.CreatedBy,
		"disabled_at": timeText(p.DisabledAt),
	}
}

func tokenView(t access.TokenInfo) map[string]any {
	var revokedBy any
	if t.RevokedBy != "" {
		revokedBy = t.RevokedBy
	}
	return map[string]any{
		"id": t.ID, "owner": t.Owner, "owner_kind": string(t.OwnerKind), "roles": roleNames(t.Roles),
		"description": t.Description, "state": string(t.State), "created_at": timeText(t.CreatedAt),
		"created_by": t.CreatedBy, "expires_at": timeText(t.ExpiresAt), "last_used_at": timeText(t.LastUsedAt),
		"revoked_at": timeText(t.RevokedAt), "revoked_by": revokedBy,
	}
}

func principalList(items []access.Principal) *listDef {
	d := &listDef{
		filters: map[string][]string{"kind": opsEqIn, "state": opsEqIn, "name": opsText, "role": opsEqIn},
		sorts:   []string{"name", "created_at"}, defSort: []string{"name"},
	}
	for _, p := range items {
		v := principalView(p)
		d.rows = append(d.rows, row{id: p.ID, out: v, vals: map[string]any{
			"kind": string(p.Kind), "state": v["state"], "name": p.ID, "role": roleNames(p.Roles),
			"created_at": p.CreatedAt.UTC().Format(time.RFC3339),
		}})
	}
	return finishFields(d, "id", "kind", "display_name", "description", "roles", "state", "created_at", "created_by", "disabled_at")
}

func tokenList(items []access.TokenInfo) *listDef {
	d := &listDef{
		filters: map[string][]string{"owner": opsEqIn, "owner_kind": opsEqIn, "state": opsEqIn},
		sorts:   []string{"created_at", "expires_at"}, defSort: []string{"created_at"},
	}
	for _, t := range items {
		d.rows = append(d.rows, row{id: t.ID, out: tokenView(t), vals: map[string]any{
			"owner": t.Owner, "owner_kind": string(t.OwnerKind), "state": string(t.State),
			"created_at": t.CreatedAt.UTC().Format(time.RFC3339), "expires_at": t.ExpiresAt.UTC().Format(time.RFC3339),
		}})
	}
	return finishFields(d, "id", "owner", "owner_kind", "roles", "description", "state", "created_at", "created_by",
		"expires_at", "last_used_at", "revoked_at", "revoked_by")
}

// finishFields fixes the closed list of selectable fields, which must not depend on whether the list has rows.
func finishFields(d *listDef, fields ...string) *listDef {
	d.outFields = append([]string(nil), fields...)
	sort.Strings(d.outFields)
	return d
}
