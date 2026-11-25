package mcpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// xffKeyLimiter records the key of each failed token and blocks nothing.
type xffKeyLimiter struct{ keys []string }

func (l *xffKeyLimiter) Blocked(string) (time.Duration, bool) { return 0, false }
func (l *xffKeyLimiter) Failed(key string)                    { l.keys = append(l.keys, key) }

// [REQ-314]
// Behind a proxy the caller's own line of X-Forwarded-For and the line the proxy adds are two lines of one header. The key is
// the last item of all of them, the one the proxy wrote, and not the first line a caller could have written.
func TestXffMCPTwoLinesTheKeyIsTheLastItemOfTheProxyLine(t *testing.T) {
	lim := &xffKeyLimiter{}
	rg := newRig(t, func(c *Config) { c.TrustProxy = true; c.Gate.Limiter = lim })
	c := goodCall(t).withHeader("Authorization", "Bearer nope")
	r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(c.body))
	r.Host = "127.0.0.1:8080"
	r.RemoteAddr = "192.0.2.10:4000"
	for k, v := range c.header {
		r.Header.Set(k, v)
	}
	r.Header.Add("X-Forwarded-For", "198.51.100.1")
	r.Header.Add("X-Forwarded-For", "198.51.100.77")
	rg.h.ServeHTTP(httptest.NewRecorder(), r)
	if len(lim.keys) != 1 || lim.keys[0] != "198.51.100.77" {
		t.Fatalf("the failure is counted for %v, want the last item of the proxy line 198.51.100.77", lim.keys)
	}
}
