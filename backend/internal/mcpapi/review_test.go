package mcpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// [REQ-505] [REQ-515] The check of Origin and Host comes before the token: a wrong Origin or Host with no token at all
// is 403 and not 401, so a page of another origin learns nothing about the token.
func TestOriginAndHostAreCheckedBeforeTheToken(t *testing.T) {
	rg := newRig(t, func(c *Config) { c.CheckHost = true })
	noToken := goodCall(t).withHeader("Authorization", "")
	if r := rg.do(noToken.withHeader("Origin", "https://evil.example")); r.status != http.StatusForbidden {
		t.Errorf("a wrong Origin without a token: %d; want 403", r.status)
	}
	if r := rg.do(noToken.withHeaderHost("evil.example")); r.status != http.StatusForbidden {
		t.Errorf("a wrong Host without a token: %d; want 403", r.status)
	}
	if r := rg.do(noToken); r.status != http.StatusUnauthorized {
		t.Errorf("a good origin without a token: %d; want 401", r.status)
	}
}

// [REQ-505] An Origin header given twice is refused, even when both are on the list.
func TestOriginGivenTwiceIsRefused(t *testing.T) {
	rg := newRig(t, func(c *Config) { c.AllowedOrigins = []string{"https://console.example"} })
	c := goodCall(t)
	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(c.body))
	req.Host = "127.0.0.1:8080"
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	req.Header.Add("Origin", "https://console.example")
	w := httptest.NewRecorder()
	rg.h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("one allowed Origin: %d", w.Code)
	}
	req.Header.Add("Origin", "https://console.example")
	w = httptest.NewRecorder()
	rg.h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("two Origin headers: %d; want 403", w.Code)
	}
}

// [REQ-505] With the Host check off (a server behind a declared proxy) any Host is taken, and the Origin check and the
// token stay.
func TestWithoutTheHostCheckTheOriginAndTheTokenStay(t *testing.T) {
	rg := newRig(t)
	if r := rg.do(goodCall(t).withHeaderHost("kg.example.com")); r.status != http.StatusOK {
		t.Errorf("the outer Host: %d %s", r.status, r.body)
	}
	if r := rg.do(goodCall(t).withHeaderHost("kg.example.com").withHeader("Origin", "https://evil.example")); r.status != http.StatusForbidden {
		t.Errorf("an Origin that is not on the list: %d; want 403", r.status)
	}
	if r := rg.do(goodCall(t).withHeaderHost("kg.example.com").withHeader("Authorization", "Bearer nope")); r.status != http.StatusUnauthorized {
		t.Errorf("a wrong token: %d; want 401", r.status)
	}
}
