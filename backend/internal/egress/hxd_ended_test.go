package egress

import (
	"io"
	"net"
	"testing"
)

// The tests of the mark of the end of a body (Response.Ended, ADR-0109): a reader of lines takes a body that ends without one as a
// body that may have been cut. Every name here begins with hxd so that it never meets a name of the older tests of this folder.

// [REQ-901]
// A length, the last chunk and a body of an HTTP/1.1 answer that the transport unpacks are marks of an end; a body that goes on
// until the connection closes has none.
func TestHxdTheEndOfABodyIsMarkedByALengthOrTheLastChunkAndNotByAClosedConnection(t *testing.T) {
	for _, c := range []struct {
		name  string
		reply string
		want  bool
	}{
		{"a length", "HTTP/1.1 200 OK\r\nContent-Length: 3\r\n\r\nabc", true},
		{"the last chunk", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabc\r\n0\r\n\r\n", true},
		{"nothing but the closing of the connection", "HTTP/1.0 200 OK\r\n\r\nabc", false},
		{"HTTP/1.1 with nothing but the closing", "HTTP/1.1 200 OK\r\nConnection: close\r\n\r\nabc", false},
	} {
		target := ctRaw(t, c.reply, func(tc *net.TCPConn) { _ = tc.Close() })
		cl, _ := ctClient(t, nil, Policy{}, ClientOptions{})
		resp, err := ctGet(t, cl, target)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != "abc" || resp.Ended != c.want {
			t.Errorf("%s: body %q, Ended %v, want %v", c.name, body, resp.Ended, c.want)
		}
	}
}

// [REQ-901]
// A body that the server sends as one chunked answer is marked, and one that is cut before its last chunk breaks the read, so
// that the reader never takes a cut body for a whole one.
func TestHxdABodyCutBeforeItsLastChunkBreaksTheRead(t *testing.T) {
	target := ctRaw(t, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabc\r\n", func(tc *net.TCPConn) { _ = tc.Close() })
	cl, _ := ctClient(t, nil, Policy{}, ClientOptions{})
	resp, err := ctGet(t, cl, target)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Ended {
		t.Errorf("a chunked body is marked")
	}
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Errorf("a body cut before its last chunk was read to a clean end")
	}
	_ = resp.Body.Close()
}
