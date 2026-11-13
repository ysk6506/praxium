package contract

import "sort"

// ProblemCode is one entry of the closed catalogue of error codes of the API
// (rest-api.md 3-9). A code is `<group>.<detail>`; once issued its meaning is
// never changed, and a new code is an addition inside the same major version.
type ProblemCode struct {
	Code    string `json:"code"`
	Group   string `json:"group"`
	Status  int    `json:"status"`
	Meaning string `json:"meaning"`
}

// ProblemGroups returns the nine groups of error codes in the order of the design.
func ProblemGroups() []string {
	return []string{
		"auth", "permission", "not_found", "invalid_input", "conflict",
		"rate_limited", "blocked", "stale", "server",
	}
}

// problemCodes is the catalogue. The first block is what rest-api.md and the
// resource documents name; the second block is what the server of the first
// alpha needs beyond them (ADR-0024 lists each addition).
var problemCodes = []ProblemCode{
	{"auth.token_missing", "auth", 401, "The request carries no bearer token."},
	{"auth.token_expired", "auth", 401, "The bearer token has expired; get a new one."},
	{"auth.token_invalid", "auth", 401, "The bearer token is not accepted."},
	{"permission.role_missing", "permission", 403, "The subject holds no role that may do this."},
	{"permission.not_in_scope", "permission", 403, "The subject holds the role but not for this resource."},
	{"permission.self_approval", "permission", 403, "A subject cannot approve what it changed itself."},
	{"permission.hidden_attribute", "permission", 403, "The request filters or groups by an attribute the subject may not read."},
	{"not_found.resource", "not_found", 404, "The resource does not exist or is not visible to the subject."},
	{"invalid_input.field", "invalid_input", 422, "One or more fields of the input are wrong; see errors."},
	{"invalid_input.field_format", "invalid_input", 422, "A field has the wrong format."},
	{"invalid_input.field_required", "invalid_input", 422, "A required field is missing."},
	{"invalid_input.filter_field_unknown", "invalid_input", 400, "The filter names a field that is not filterable on this resource."},
	{"invalid_input.filter_op_unknown", "invalid_input", 400, "The filter uses an operation that is unknown or not allowed on this field."},
	{"invalid_input.idempotency_key_required", "invalid_input", 400, "A create or action request needs an Idempotency-Key header."},
	{"invalid_input.precondition_required", "invalid_input", 428, "A change needs an If-Match header with the version it is based on."},
	{"invalid_input.history_with_filter", "invalid_input", 422, "A history request cannot be combined with a filter or grouping."},
	{"invalid_input.envelope_too_large", "invalid_input", 413, "The ingest envelope is larger than the limit."},
	{"invalid_input.param_unknown", "invalid_input", 400, "The request has a query parameter this operation does not define."},
	{"invalid_input.param_invalid", "invalid_input", 400, "A query parameter has a value this operation does not accept."},
	{"invalid_input.limit_invalid", "invalid_input", 400, "The limit is not a whole number from 1 to 1000."},
	{"invalid_input.cursor_invalid", "invalid_input", 400, "The cursor is not one this operation issued for this query."},
	{"invalid_input.sort_field_unknown", "invalid_input", 400, "The sort names a field that is not sortable on this resource."},
	{"invalid_input.fields_unknown", "invalid_input", 400, "The fields list names a field the resource does not have."},
	{"invalid_input.include_unknown", "invalid_input", 400, "The include list names something this operation cannot include."},
	{"invalid_input.body_too_large", "invalid_input", 413, "The request body is larger than the limit."},
	{"invalid_input.method_not_allowed", "invalid_input", 405, "The path exists but not for this method."},
	{"invalid_input.not_acceptable", "invalid_input", 406, "The server cannot answer in any media type the Accept header allows."},
	{"invalid_input.unsupported_media_type", "invalid_input", 415, "The request body is not in a media type this operation accepts."},
	{"conflict.version_mismatch", "conflict", 412, "The version in If-Match is not the current one; read again and merge."},
	{"conflict.idempotency_key_reused", "conflict", 409, "The Idempotency-Key was used with a different body."},
	{"conflict.request_in_progress", "conflict", 409, "A request with the same Idempotency-Key is still running."},
	{"conflict.generation_changed", "conflict", 409, "The generation behind a cursor, event id or min_seq has changed; read the state again."},
	{"conflict.state_not_allowed", "conflict", 409, "The current state of the resource does not allow this action."},
	{"rate_limited.requests", "rate_limited", 429, "Too many requests in the period."},
	{"rate_limited.concurrency", "rate_limited", 429, "Too many requests at the same time."},
	{"rate_limited.compute_budget", "rate_limited", 429, "The compute budget of the subject is used up."},
	{"blocked.proposal", "blocked", 409, "The proposal is blocked; see blockers."},
	{"stale.branch_behind_trunk", "stale", 409, "The branch is behind the trunk; align it first."},
	{"stale.since_too_old", "stale", 410, "The event id is older than the events kept; read the state again."},
	{"stale.projection_not_ready", "stale", 503, "The projection the answer needs is not built yet."},
	{"stale.min_seq_not_reached", "stale", 503, "The replica has not reached min_seq within the wait."},
	{"server.internal", "server", 500, "A bug or a broken dependency; retrying does not help."},
	{"server.unavailable", "server", 503, "The server cannot answer now; retry later."},
}

// ProblemCodes returns the whole catalogue sorted by group order and then code.
func ProblemCodes() []ProblemCode {
	out := append([]ProblemCode(nil), problemCodes...)
	rank := map[string]int{}
	for i, g := range ProblemGroups() {
		rank[g] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Group] != rank[out[j].Group] {
			return rank[out[i].Group] < rank[out[j].Group]
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// LookupProblem returns the entry of a code, or false for a code outside the catalogue.
func LookupProblem(code string) (ProblemCode, bool) {
	for _, p := range problemCodes {
		if p.Code == code {
			return p, true
		}
	}
	return ProblemCode{}, false
}

// ReasonCode is one closed reason code of allowed_actions (rest-api.md section 4).
type ReasonCode struct {
	Code    string `json:"code"`
	Meaning string `json:"meaning"`
}

// ReasonCodes returns the six reason codes in the order of the design.
func ReasonCodes() []ReasonCode {
	return []ReasonCode{
		{"forbidden.role", "The subject lacks the role the action needs."},
		{"forbidden.scope", "The subject has the role but the resource is outside its scope."},
		{"conflict.state", "The current state of the resource does not allow the action."},
		{"blocked.approval_required", "An approval is needed first."},
		{"blocked.limit", "A limit (open items or budget of the subject) is reached."},
		{"unavailable.degraded", "The system is degraded and the action is not available now."},
	}
}
