package httpapi_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ysk6506/praxium/backend/internal/httpapi"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func uiServer(t *testing.T, files fstest.MapFS) http.Handler {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	cfg := httpapi.Config{
		Registry: reg,
		Clock:    kernel.NewManualClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)), IDs: kernel.NewSequentialIDs(),
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), Version: "test",
	}
	if files != nil {
		cfg.UI = files
	}
	srv, err := httpapi.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

func uiGet(h http.Handler, method, target string) *http.Response {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec.Result()
}

func uiBody(t *testing.T, res *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var builtUI = fstest.MapFS{
	"index.html":       {Data: []byte("<html>page</html>")},
	"assets/app-1.js":  {Data: []byte("console.log(1)")},
	"assets/app-1.css": {Data: []byte("a{}")},
	"placeholder.html": {Data: []byte("<html>placeholder</html>")},
	"favicon.svg":      {Data: []byte("<svg/>")},
}

// [REQ-275]
func TestUIServesThePageAndItsFiles(t *testing.T) {
	h := uiServer(t, builtUI)
	page := uiGet(h, "GET", "/")
	if page.StatusCode != 200 || uiBody(t, page) != "<html>page</html>" {
		t.Fatalf("/ must be the built page, got %d", page.StatusCode)
	}
	if got := page.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("the page must be HTML, got %q", got)
	}
	if got := page.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("the page must be revalidated, got %q", got)
	}
	asset := uiGet(h, "GET", "/assets/app-1.js")
	if asset.StatusCode != 200 || uiBody(t, asset) != "console.log(1)" {
		t.Fatalf("an asset must be served as it is, got %d", asset.StatusCode)
	}
	if got := asset.Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("a hashed asset may be cached for good, got %q", got)
	}
	if got := uiGet(h, "GET", "/favicon.svg").Header.Get("Cache-Control"); got != "" {
		t.Fatalf("a file outside assets/ gets no long cache, got %q", got)
	}
	if got := uiGet(h, "GET", "/index.html"); uiBody(t, got) != "<html>page</html>" {
		t.Fatal("/index.html must be the page too")
	}
}

// [REQ-275]
func TestUIPagesCannotBeFramedOrLoadOutsideTheirOrigin(t *testing.T) {
	h := uiServer(t, builtUI)
	for _, target := range []string{"/", "/assets/app-1.js"} {
		res := uiGet(h, "GET", target)
		if !strings.Contains(res.Header.Get("Content-Security-Policy"), "default-src 'self'") {
			t.Fatalf("%s must carry a content security policy", target)
		}
		if res.Header.Get("X-Frame-Options") != "DENY" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s must not be framed or sniffed", target)
		}
	}
}

// [REQ-275]
func TestUIAnswersAnAddressOfThePagesRouterWithThePage(t *testing.T) {
	h := uiServer(t, builtUI)
	if got := uiBody(t, uiGet(h, "GET", "/kg/main/types")); got != "<html>page</html>" {
		t.Fatalf("an address without a file extension is the page's, got %q", got)
	}
	if res := uiGet(h, "GET", "/assets/missing.js"); res.StatusCode != 404 {
		t.Fatalf("a missing file is a 404, got %d", res.StatusCode)
	}
	if res := uiGet(h, "GET", "/placeholder.html"); res.StatusCode != 404 {
		t.Fatalf("the placeholder is served only as the page, never by name, got %d", res.StatusCode)
	}
}

// [REQ-275]
func TestUIHeadAnswersWithoutABodyAndOtherMethodsAreNotThePages(t *testing.T) {
	h := uiServer(t, builtUI)
	head := uiGet(h, "HEAD", "/")
	if head.StatusCode != 200 || uiBody(t, head) != "" {
		t.Fatalf("HEAD / must answer 200 without a body, got %d", head.StatusCode)
	}
	if res := uiGet(h, "POST", "/"); res.StatusCode != 404 {
		t.Fatalf("POST / is not the page, got %d", res.StatusCode)
	}
}

// [REQ-275]
func TestUILeavesTheAPIToTheAPI(t *testing.T) {
	h := uiServer(t, builtUI)
	if res := uiGet(h, "GET", "/v1/health/live"); res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), "json") {
		t.Fatalf("the API must answer under /v1, got %d", res.StatusCode)
	}
	res := uiGet(h, "GET", "/v1/no-such-route")
	if res.StatusCode != 404 || !strings.Contains(res.Header.Get("Content-Type"), "problem+json") {
		t.Fatalf("an unknown API path is a problem, never the page, got %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if res := uiGet(h, "GET", "/v1"); res.StatusCode != 404 {
		t.Fatalf("/v1 is not the page, got %d", res.StatusCode)
	}
}

// [REQ-275]
func TestUIWithoutABuiltPageAnswersWithThePlaceholder(t *testing.T) {
	h := uiServer(t, fstest.MapFS{"placeholder.html": {Data: []byte("<html>placeholder</html>")}})
	if got := uiBody(t, uiGet(h, "GET", "/")); got != "<html>placeholder</html>" {
		t.Fatalf("a build without the UI must say so, got %q", got)
	}
	empty := uiServer(t, fstest.MapFS{})
	if res := uiGet(empty, "GET", "/"); res.StatusCode != 404 {
		t.Fatalf("nothing to serve is a 404, got %d", res.StatusCode)
	}
}

// [REQ-275]
func TestWithoutUIConfiguredTheServerIsTheAPIOnly(t *testing.T) {
	h := uiServer(t, nil)
	if res := uiGet(h, "GET", "/"); res.StatusCode != 404 || !strings.Contains(res.Header.Get("Content-Type"), "problem+json") {
		t.Fatalf("without a UI, / is an unknown route, got %d", res.StatusCode)
	}
}
