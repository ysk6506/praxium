package httpapi_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/contract"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// testToken is an obviously fake token for the development authenticator.
const testToken = "test-token-not-real-0001"

// fixedAuthenticator vouches for one token; the real authenticators are tested in their own packages.
type fixedAuthenticator struct {
	token string
	roles []access.Role
}

func (f fixedAuthenticator) Authenticate(token string) (access.Subject, error) {
	if token != f.token {
		return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "the token is not known")
	}
	return access.Subject{Kind: access.SubjectPerson, ID: "test-user", DisplayName: "Test User", Roles: f.roles}, nil
}

type harness struct {
	t       *testing.T
	server  *httpapi.Server
	handler http.Handler
	logs    *bytes.Buffer
}

// newHarness builds a server over the standard model with a development
// authenticator that holds the roles.
func newHarness(t *testing.T, roles ...access.Role) *harness {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	return newHarnessWith(t, reg, roles...)
}

func newHarnessWith(t *testing.T, reg *model.Registry, roles ...access.Role) *harness {
	t.Helper()
	if len(roles) == 0 {
		roles = []access.Role{access.RoleViewer}
	}
	authn := fixedAuthenticator{token: testToken, roles: roles}
	logs := &bytes.Buffer{}
	srv, err := httpapi.New(httpapi.Config{
		Registry: reg, Authenticator: authn,
		Clock: kernel.NewManualClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)), IDs: kernel.NewSequentialIDs(),
		Logger: slog.New(slog.NewJSONHandler(logs, nil)), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.MarkStarted()
	return &harness{t: t, server: srv, handler: srv.Handler(), logs: logs}
}

type reply struct {
	status  int
	header  http.Header
	body    []byte
	headers map[string]string
}

func (r reply) json(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("not JSON: %v: %s", err, r.body)
	}
	return out
}

func (r reply) code(t *testing.T) string {
	t.Helper()
	code, _ := r.json(t)["code"].(string)
	return code
}

// do sends a request. headers may carry Authorization; withToken adds the fake token.
func (h *harness) do(method, target string, withToken bool, headers ...string) reply {
	h.t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if withToken {
		req.Header.Set("Authorization", "Bearer "+testToken)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	flat := map[string]string{}
	for k, v := range rec.Header() {
		flat[strings.ToLower(k)] = v[0]
	}
	return reply{status: rec.Code, header: rec.Header(), body: rec.Body.Bytes(), headers: flat}
}

func (h *harness) get(target string) reply { return h.do(http.MethodGet, target, true) }

func contractValidator(t *testing.T) *contract.Validator {
	t.Helper()
	doc, err := contract.ParseDocument(contract.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	v, err := contract.NewValidator(doc, true)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
