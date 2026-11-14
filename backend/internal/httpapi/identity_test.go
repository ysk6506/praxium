package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func idhData(t *testing.T, r reply) map[string]any {
	t.Helper()
	data, _ := r.json(t)["data"].(map[string]any)
	if data == nil {
		t.Fatalf("no data in %s", r.body)
	}
	return data
}

// idhFits checks an answer against the contract operation it belongs to.
func idhFits(t *testing.T, method, template string, r reply) {
	t.Helper()
	if err := contractValidator(t).ValidateResponse(method, template, r.status, r.header.Get("Content-Type"), r.headers, r.body); err != nil {
		t.Fatalf("%s %s %d: %v: %s", method, template, r.status, err, r.body)
	}
}

// [REQ-313]
func TestAnAdministratorManagesUsersAndTheAnswersFitTheContract(t *testing.T) {
	r := idhNewRig(t, false)
	key := []string{"Idempotency-Key", "idh-key-000001"}
	created := r.send("POST", "/v1/users", idhAdminToken, `{"name":"grace","display_name":"Grace","roles":["viewer"]}`, key...)
	if created.status != http.StatusCreated {
		t.Fatalf("create: %d %s", created.status, created.body)
	}
	idhFits(t, "POST", "/v1/users", created)
	if got := idhData(t, created)["id"]; got != "grace" {
		t.Fatalf("id = %v", got)
	}
	replay := r.send("POST", "/v1/users", idhAdminToken, `{"name":"grace","display_name":"Grace","roles":["viewer"]}`, key...)
	if replay.status != http.StatusCreated || replay.header.Get("Idempotent-Replayed") != "true" || string(replay.body) != string(created.body) {
		t.Fatalf("the same key and body must replay the answer: %d %s", replay.status, replay.body)
	}
	other := r.send("POST", "/v1/users", idhAdminToken, `{"name":"heidi","roles":["viewer"]}`, key...)
	if other.status != http.StatusConflict || other.code(t) != "conflict.idempotency_key_reused" {
		t.Fatalf("the same key with another body: %d %s", other.status, other.body)
	}
	idhFits(t, "POST", "/v1/users", other)
	taken := r.send("POST", "/v1/users", idhAdminToken, `{"name":"grace","roles":["viewer"]}`, "Idempotency-Key", "idh-key-000002")
	if taken.status != http.StatusConflict || taken.code(t) != "conflict.state_not_allowed" {
		t.Fatalf("a name in use: %d %s", taken.status, taken.body)
	}
	list := r.send("GET", "/v1/users?sort=-name&limit=2", idhAdminToken, "")
	if list.status != 200 {
		t.Fatalf("list: %d %s", list.status, list.body)
	}
	idhFits(t, "GET", "/v1/users", list)
	disabled := r.send("POST", "/v1/users/grace:disable", idhAdminToken, "", "Idempotency-Key", "idh-key-000003")
	if disabled.status != 200 || idhData(t, disabled)["state"] != "disabled" {
		t.Fatalf("disable: %d %s", disabled.status, disabled.body)
	}
	idhFits(t, "POST", "/v1/users/{user_id}:disable", disabled)
	account := r.send("POST", "/v1/system-accounts", idhAdminToken, `{"name":"ingest-a","roles":["ingester"]}`, "Idempotency-Key", "idh-key-000004")
	idhFits(t, "POST", "/v1/system-accounts", account)
	accounts := r.send("GET", "/v1/system-accounts", idhAdminToken, "")
	idhFits(t, "GET", "/v1/system-accounts", accounts)
	gone := r.send("POST", "/v1/system-accounts/nobody:disable", idhAdminToken, "", "Idempotency-Key", "idh-key-000005")
	if gone.status != http.StatusNotFound {
		t.Fatalf("an account that does not exist: %d", gone.status)
	}
}

// [REQ-313]
func TestCreatingAndActingCallsNeedAWellFormedIdempotencyKey(t *testing.T) {
	r := idhNewRig(t, false)
	for _, key := range []string{"", "short", strings.Repeat("k", 129)} {
		var headers []string
		if key != "" {
			headers = []string{"Idempotency-Key", key}
		}
		got := r.send("POST", "/v1/users", idhAdminToken, `{"name":"x1","roles":["viewer"]}`, headers...)
		if got.status != 400 || got.code(t) != "invalid_input.idempotency_key_required" {
			t.Errorf("key %q: %d %s", key, got.status, got.body)
		}
	}
}

// [REQ-308]
func TestANewTokenIsInOneAnswerOnlyAndItsKeyCannotBeUsedAgain(t *testing.T) {
	r := idhNewRig(t, false)
	key := []string{"Idempotency-Key", "idh-key-100001"}
	body := `{"owner":"ada","description":"read only","roles":["viewer"],"expires_in_days":30}`
	first := r.send("POST", "/v1/tokens", idhAdminToken, body, key...)
	if first.status != http.StatusCreated {
		t.Fatalf("issue: %d %s", first.status, first.body)
	}
	idhFits(t, "POST", "/v1/tokens", first)
	secret, _ := idhData(t, first)["token"].(string)
	if !strings.HasPrefix(secret, "pxm_") {
		t.Fatalf("the answer must carry the token: %s", first.body)
	}
	if first.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("an answer with a secret must not be cached: %q", first.header.Get("Cache-Control"))
	}
	again := r.send("POST", "/v1/tokens", idhAdminToken, body, key...)
	if again.status != http.StatusConflict || strings.Contains(string(again.body), secret) {
		t.Fatalf("the key of an issue must not replay the secret: %d %s", again.status, again.body)
	}
	list := r.send("GET", "/v1/tokens", idhAdminToken, "")
	idhFits(t, "GET", "/v1/tokens", list)
	if strings.Contains(string(list.body), secret) || strings.Contains(string(list.body), "idh-new-secret") {
		t.Fatalf("a list must not hold a secret: %s", list.body)
	}
	if strings.Contains(r.logs.String(), secret) || strings.Contains(r.logs.String(), "idh-new-secret") {
		t.Fatalf("the log holds a secret: %s", r.logs.String())
	}
}

// [REQ-309]
func TestAnOwnerSeesAndRevokesOnlyItsOwnTokensAndARevokedTokenIsRefusedAtOnce(t *testing.T) {
	r := idhNewRig(t, false)
	own := r.send("GET", "/v1/tokens", idhReaderToken, "")
	idhFits(t, "GET", "/v1/tokens", own)
	if strings.Contains(string(own.body), "adm0000000000001") {
		t.Fatalf("a reader must not see the tokens of others: %s", own.body)
	}
	if got := r.send("POST", "/v1/tokens", idhReaderToken, `{"owner":"ada"}`, "Idempotency-Key", "idh-key-200001"); got.status != 403 {
		t.Fatalf("a reader makes no tokens: %d", got.status)
	}
	if got := r.send("POST", "/v1/tokens/adm0000000000001:revoke", idhReaderToken, "", "Idempotency-Key", "idh-key-200002"); got.status != 404 {
		t.Fatalf("the token of another looks absent: %d %s", got.status, got.body)
	}
	revoked := r.send("POST", "/v1/tokens/red0000000000001:revoke", idhReaderToken, "", "Idempotency-Key", "idh-key-200003")
	if revoked.status != 200 || idhData(t, revoked)["state"] != "revoked" {
		t.Fatalf("revoke own: %d %s", revoked.status, revoked.body)
	}
	idhFits(t, "POST", "/v1/tokens/{token_id}:revoke", revoked)
	if got := r.send("GET", "/v1/tokens/current", idhReaderToken, ""); got.status != 401 {
		t.Fatalf("a revoked token: %d", got.status)
	}
	if got := r.send("POST", "/v1/tokens/not-a-verb", idhAdminToken, ""); got.status != 404 {
		t.Fatalf("a path without the verb: %d", got.status)
	}
}

// [REQ-313]
func TestTheCurrentTokenSaysWhoIsCalling(t *testing.T) {
	r := idhNewRig(t, false)
	got := r.send("GET", "/v1/tokens/current", idhReaderToken, "")
	if got.status != 200 {
		t.Fatalf("%d %s", got.status, got.body)
	}
	idhFits(t, "GET", "/v1/tokens/current", got)
	data := idhData(t, got)
	if data["subject"].(map[string]any)["id"] != "ada" || data["token"].(map[string]any)["id"] != "red0000000000001" {
		t.Fatalf("%s", got.body)
	}
}

// [REQ-314]
func TestARefusedTokenIsLoggedWithoutItAndAFailingCallerIsBlocked(t *testing.T) {
	r := idhNewRig(t, false)
	bad := "pxm_0000000000000000_idh-wrong-secret-not-real"
	for i := 0; i < 2; i++ {
		if got := r.send("GET", "/v1/roles", bad, ""); got.status != 401 {
			t.Fatalf("a wrong token: %d", got.status)
		}
	}
	blocked := r.send("GET", "/v1/tokens", idhAdminToken, "")
	if blocked.status != 429 || blocked.header.Get("Retry-After") != "43" {
		t.Fatalf("a blocked caller: %d, Retry-After %q: %s", blocked.status, blocked.header.Get("Retry-After"), blocked.body)
	}
	idhFits(t, "GET", "/v1/tokens", blocked)
	logs := r.logs.String()
	if !strings.Contains(logs, `"event":"auth_failed"`) || !strings.Contains(logs, `"event":"auth_blocked"`) || !strings.Contains(logs, "203.0.113.9") {
		t.Fatalf("the refusals must be in the audit log with the address: %s", logs)
	}
	for _, secret := range []string{bad, "idh-wrong-secret-not-real", idhAdminToken} {
		if strings.Contains(logs, secret) || strings.Contains(string(blocked.body), secret) {
			t.Fatalf("a token is in the log or the answer: %s", secret)
		}
	}
	if got := r.send("GET", "/v1/health/live", "", ""); got.status != 200 {
		t.Fatalf("the probes stay open to a blocked caller: %d", got.status)
	}
}

// [REQ-314]
func TestAMissingTokenIsNotCountedAndASuccessIsReported(t *testing.T) {
	r := idhNewRig(t, false)
	for i := 0; i < 5; i++ {
		r.send("GET", "/v1/roles", "", "")
	}
	if len(r.limiter.fails) != 0 {
		t.Fatalf("a missing token counted: %v", r.limiter.fails)
	}
	r.send("GET", "/v1/roles", idhAdminToken, "")
	if r.limiter.passes != 1 {
		t.Fatalf("passes = %d", r.limiter.passes)
	}
}

// [REQ-314]
func TestBehindAProxyTheLastForwardedAddressIsTheCaller(t *testing.T) {
	r := idhNewRig(t, true)
	r.send("GET", "/v1/roles", "pxm_0000000000000000_idh-wrong-secret-not-real", "", "X-Forwarded-For", "198.51.100.1, 198.51.100.77")
	if r.limiter.fails["198.51.100.77"] != 1 || len(r.limiter.fails) != 1 {
		t.Fatalf("the key must be the last address: %v", r.limiter.fails)
	}
	direct := idhNewRig(t, false)
	direct.send("GET", "/v1/roles", "pxm_0000000000000000_idh-wrong-secret-not-real", "", "X-Forwarded-For", "198.51.100.1")
	if direct.limiter.fails["203.0.113.9"] != 1 {
		t.Fatalf("without a proxy the header is not believed: %v", direct.limiter.fails)
	}
}

// [REQ-312]
func TestAnAuthenticatorThatCannotAnswerIsAnOutageNotARefusal(t *testing.T) {
	r := idhNewRig(t, false)
	r.dir.fail = kernel.Errorf(kernel.CodeInternal, "the store is down: secret-detail-not-real")
	got := r.send("GET", "/v1/roles", idhAdminToken, "")
	if got.status != 503 || got.code(t) != "server.unavailable" {
		t.Fatalf("%d %s", got.status, got.body)
	}
	if len(r.limiter.fails) != 0 {
		t.Fatal("an outage must not count against the caller")
	}
	if strings.Contains(string(got.body), "secret-detail") {
		t.Fatal("the answer shows the store's error")
	}
}

// [REQ-313]
func TestWithoutADirectoryTheManagementRoutesAnswerNotFound(t *testing.T) {
	h := newHarness(t, access.RoleAdmin)
	if got := h.get("/v1/users"); got.status != http.StatusNotFound {
		t.Fatalf("%d %s", got.status, got.body)
	}
	current := h.get("/v1/tokens/current")
	if current.status != 200 || idhData(t, current)["token"] != nil {
		t.Fatalf("%d %s", current.status, current.body)
	}
}

// [REQ-313]
func TestBadRequestsToTheManagementRoutesAreRefusedWithAProblem(t *testing.T) {
	r := idhNewRig(t, false)
	n := 0
	send := func(method, target, token, body string) reply {
		n++
		return r.send(method, target, token, body, "Idempotency-Key", "idh-bad-key-"+string(rune('a'+n)))
	}
	cases := []struct {
		name, method, target, body string
		status                     int
	}{
		{"an unknown role", "POST", "/v1/users", `{"name":"x1","roles":["emperor"]}`, 422},
		{"an unknown field", "POST", "/v1/users", `{"name":"x1","roles":["viewer"],"password":"no"}`, 422},
		{"a name the rules refuse", "POST", "/v1/users", `{"roles":["viewer"]}`, 422},
		{"an unknown role for a token", "POST", "/v1/tokens", `{"owner":"ada","roles":["emperor"]}`, 422},
		{"a lifetime out of range", "POST", "/v1/tokens", `{"owner":"ada","expires_in_days":-1}`, 422},
		{"an owner that does not exist", "POST", "/v1/tokens", `{"owner":"nobody"}`, 404},
		{"a token that does not exist", "POST", "/v1/tokens/zzzz000000000000:revoke", ``, 404},
		{"a disable without its verb", "POST", "/v1/users/ada:enable", ``, 404},
		{"a disable of an empty id", "POST", "/v1/users/:disable", ``, 404},
	}
	for _, c := range cases {
		got := send(c.method, c.target, idhAdminToken, c.body)
		if got.status != c.status {
			t.Errorf("%s: %d, want %d: %s", c.name, got.status, c.status, got.body)
		}
	}
	if got := r.send("POST", "/v1/users", idhAdminToken, `{"name":`, "Idempotency-Key", "idh-bad-json-1"); got.status != 422 {
		t.Errorf("a body that is not JSON: %d", got.status)
	}
}

// [REQ-312]
func TestAFailingDirectoryIsAnOutageThatShowsNothingOfTheStore(t *testing.T) {
	r := idhNewRig(t, false)
	r.dir.broken = kernel.Errorf(kernel.CodeInternal, "the store is down: detail-not-real")
	for _, c := range [][]string{
		{"GET", "/v1/users", ""}, {"GET", "/v1/tokens", ""},
		{"POST", "/v1/users", `{"name":"x1","roles":["viewer"]}`}, {"POST", "/v1/tokens", `{"owner":"ada"}`},
	} {
		got := r.send(c[0], c[1], idhAdminToken, c[2], "Idempotency-Key", "idh-down-key-1")
		if got.status != 503 || strings.Contains(string(got.body), "detail-not-real") {
			t.Errorf("%s %s: %d %s", c[0], c[1], got.status, got.body)
		}
	}
}

// [REQ-313]
func TestACallWhoseFirstTryIsStillRunningIsRefusedNotRepeated(t *testing.T) {
	r := idhNewRig(t, false)
	r.dir.broken = kernel.Errorf(kernel.CodeInvalidValue, "a rule")
	first := r.send("POST", "/v1/users", idhAdminToken, `{"name":"x1","roles":["viewer"]}`, "Idempotency-Key", "idh-retry-key-1")
	if first.status != 422 {
		t.Fatalf("%d", first.status)
	}
	// A failed call gives its key back: the retry with the same key runs again.
	r.dir.broken = nil
	again := r.send("POST", "/v1/users", idhAdminToken, `{"name":"x1","roles":["viewer"]}`, "Idempotency-Key", "idh-retry-key-1")
	if again.status != http.StatusCreated {
		t.Fatalf("the retry of a failed call: %d %s", again.status, again.body)
	}
}
