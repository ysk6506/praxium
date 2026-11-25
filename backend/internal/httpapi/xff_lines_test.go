package httpapi_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// [REQ-314]
// Behind a proxy the caller's own line of X-Forwarded-For and the line the proxy adds are two lines of one header. The key is
// the last item of all of them, the one the proxy wrote, and not the first line a caller could have written.
func TestXffRESTTwoLinesTheKeyIsTheLastItemOfTheProxyLine(t *testing.T) {
	r := idhNewRig(t, true)
	req := httptest.NewRequest("GET", "/v1/roles", strings.NewReader(""))
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("Authorization", "Bearer pxm_0000000000000000_idh-wrong-secret-not-real")
	req.Header.Add("X-Forwarded-For", "198.51.100.1")
	req.Header.Add("X-Forwarded-For", "198.51.100.77")
	r.handler.ServeHTTP(httptest.NewRecorder(), req)
	if r.limiter.fails["198.51.100.77"] != 1 || len(r.limiter.fails) != 1 {
		t.Fatalf("the key must be the last item of the proxy line: %v", r.limiter.fails)
	}
}
