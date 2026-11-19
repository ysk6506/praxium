package egress

import (
	"bufio"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// ctRaw answers one request with the given bytes written first, then does what end says with the connection.
func ctRaw(t *testing.T, reply string, end func(*net.TCPConn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		tc := c.(*net.TCPConn)
		r := bufio.NewReader(tc)
		for {
			line, err := r.ReadString('\n')
			if err != nil || line == "\r\n" {
				break
			}
		}
		_, _ = io.WriteString(tc, reply)
		end(tc)
	}()
	return "http://" + ln.Addr().String() + "/"
}

// [REQ-661]
func TestABodyThatBreaksMidwayLeaksNoAddress(t *testing.T) {
	target := ctRaw(t, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nabc", func(c *net.TCPConn) {
		_ = c.SetLinger(0)
		_ = c.Close()
	})
	c, _ := ctClient(t, nil, Policy{}, ClientOptions{})
	resp, err := ctGet(t, c, target)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err == nil || string(body) != "abc" {
		t.Fatalf("body %q, err %v", body, err)
	}
	var e *Error
	if !errors.As(err, &e) || strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), "->") {
		t.Errorf("the read error is not sanitized: %v", err)
	}
	if err := resp.Body.Close(); err != nil && !errors.As(err, &e) {
		t.Errorf("close: %v", err)
	}
}

// [REQ-661]
func TestABodyThatEndsTooEarlyIsAnEgressErrorToo(t *testing.T) {
	target := ctRaw(t, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nabc", func(c *net.TCPConn) { _ = c.Close() })
	c, _ := ctClient(t, nil, Policy{}, ClientOptions{})
	resp, err := ctGet(t, c, target)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(resp.Body)
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindConnect {
		t.Errorf("err %v", err)
	}
	_ = resp.Body.Close()
}

// [REQ-661]
func TestAResponseHeaderOverTheLimitIsRefused(t *testing.T) {
	big := strings.Repeat("a", maxHeaderBytes+1024)
	target := ctRaw(t, "HTTP/1.1 200 OK\r\nX-Big: "+big+"\r\nContent-Length: 0\r\n\r\n", func(c *net.TCPConn) { _ = c.Close() })
	c, _ := ctClient(t, nil, Policy{}, ClientOptions{})
	_, err := ctGet(t, c, target)
	var e *Error
	if !errors.As(err, &e) || strings.Contains(err.Error(), "aaaa") {
		t.Errorf("err %v", err)
	}
}

// [REQ-661]
func TestARebindingNameIsJudgedAgainOnTheNextConnection(t *testing.T) {
	cert, caPEM := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	port, _ := ctServe(t, cert, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	c, res := ctClient(t, nil, Policy{}, ClientOptions{CAPEM: caPEM})
	res.answers = map[string][][]netip.Addr{"api.example.test": {dtAddrs(t, "127.0.0.1"), dtAddrs(t, "169.254.169.254")}}
	req := Request{URL: "https://api.example.test:" + port + "/", Header: map[string][]string{"Connection": {"close"}}}
	resp, err := c.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	_, err = c.Do(t.Context(), req)
	if e := dtKind(t, err); e.Kind != KindPolicy {
		t.Errorf("second: %v", err)
	}
	if res.calls["api.example.test"] != 2 {
		t.Errorf("lookups %d", res.calls["api.example.test"])
	}
}

// [REQ-661]
func TestAServerThatSpeaksOnlyTLS11IsRefused(t *testing.T) {
	cert, caPEM := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11}
	srv.Config.ErrorLog = nil
	srv.StartTLS()
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	c, _ := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, ClientOptions{CAPEM: caPEM})
	_, err := ctGet(t, c, "https://api.example.test:"+port+"/")
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err %v", err)
	}
	if e.Kind == "" || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("err %v", err)
	}
}
