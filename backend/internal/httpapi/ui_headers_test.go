package httpapi_test

import (
	"strings"
	"testing"
)

// [REQ-318]
func TestUIContentSecurityPolicyNamesEachKindOfLoadAndAllowsNoOtherOrigin(t *testing.T) {
	h := uiServer(t, builtUI)
	for _, target := range []string{"/", "/assets/app-1.js", "/kg/main/types"} {
		csp := uiGet(h, "GET", target).Header.Get("Content-Security-Policy")
		for _, want := range []string{
			"default-src 'self'", "script-src 'self'", "connect-src 'self'", "worker-src 'self'",
			"object-src 'none'", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'self'",
		} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: the policy %q lacks %q", target, csp, want)
			}
		}
		for _, forbidden := range []string{"unsafe-eval", "http:", "https:", "*", "script-src 'self' 'unsafe-inline'"} {
			if strings.Contains(csp, forbidden) {
				t.Errorf("%s: the policy %q must not contain %q", target, csp, forbidden)
			}
		}
	}
}

// [REQ-318]
func TestUITurnsOffBrowserFeaturesItDoesNotUseAndKeepsItsOwnWindowGroup(t *testing.T) {
	h := uiServer(t, builtUI)
	for _, target := range []string{"/", "/assets/app-1.css"} {
		res := uiGet(h, "GET", target)
		policy := res.Header.Get("Permissions-Policy")
		for _, feature := range []string{"camera=()", "microphone=()", "geolocation=()"} {
			if !strings.Contains(policy, feature) {
				t.Errorf("%s: Permissions-Policy %q lacks %s", target, policy, feature)
			}
		}
		if got := res.Header.Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
			t.Errorf("%s: Cross-Origin-Opener-Policy is %q, want same-origin", target, got)
		}
		if got := res.Header.Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy is %q, want no-referrer", target, got)
		}
	}
}

// [REQ-318]
func TestUIPageIsNotStoredButBfcacheStaysPossible(t *testing.T) {
	res := uiGet(uiServer(t, builtUI), "GET", "/")
	cache := res.Header.Get("Cache-Control")
	if !strings.Contains(cache, "no-cache") || strings.Contains(cache, "no-store") {
		t.Fatalf("the page must be revalidated and may stay in the back/forward cache, got %q", cache)
	}
}
