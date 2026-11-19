package sourcecheck

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// scFake is a PostgreSQL server on the loopback that sends the bytes a scenario says. The scenario is the database name
// of the start packet. It knows the statements of pgsource just well enough to answer them. Nothing in it is a real
// server's data.
type scFake struct {
	t      *testing.T
	ln     net.Listener
	port   int
	mu     sync.Mutex
	open   []net.Conn
	simple []string // the simple queries a client sent (BEGIN, COMMIT...)
	cancel int      // cancel requests
	after  chan []byte
	big    []byte        // the message of the scenario bigrow, made by the test before it measures the heap
	silent chan struct{} // told when a scenario silent has the statement and says nothing
}

func scNewFake(t *testing.T) *scFake {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &scFake{t: t, ln: ln, port: ln.Addr().(*net.TCPAddr).Port, after: make(chan []byte, 8), silent: make(chan struct{}, 8)}
	t.Cleanup(func() {
		_ = ln.Close()
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, c := range f.open {
			_ = c.Close()
		}
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.open = append(f.open, c)
			f.mu.Unlock()
			go f.serve(c)
		}
	}()
	return f
}

func (f *scFake) cancels() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancel
}

func (f *scFake) queries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.simple...)
}

func scMsg(typ byte, body []byte) []byte {
	out := make([]byte, 5+len(body))
	out[0] = typ
	binary.BigEndian.PutUint32(out[1:], uint32(len(body)+4))
	copy(out[5:], body)
	return out
}

func scCstr(s string) []byte { return append([]byte(s), 0) }

const scTextOID = 25

// scRowDesc describes text columns.
func scRowDesc(cols ...string) []byte {
	b := binary.BigEndian.AppendUint16(nil, uint16(len(cols)))
	for _, c := range cols {
		b = append(b, scCstr(c)...)
		b = binary.BigEndian.AppendUint32(b, 0)
		b = binary.BigEndian.AppendUint16(b, 0)
		b = binary.BigEndian.AppendUint32(b, scTextOID)
		b = binary.BigEndian.AppendUint16(b, 0xffff)
		b = binary.BigEndian.AppendUint32(b, 0xffffffff)
		b = binary.BigEndian.AppendUint16(b, 0)
	}
	return scMsg('T', b)
}

func scDataRow(vals ...string) []byte {
	b := binary.BigEndian.AppendUint16(nil, uint16(len(vals)))
	for _, v := range vals {
		b = binary.BigEndian.AppendUint32(b, uint32(len(v)))
		b = append(b, v...)
	}
	return scMsg('D', b)
}

var scParam = regexp.MustCompile(`\$(\d+)`)

func scParamDesc(q string) []byte {
	n := 0
	for _, m := range scParam.FindAllStringSubmatch(q, -1) {
		if v, _ := strconv.Atoi(m[1]); v > n {
			n = v
		}
	}
	pd := binary.BigEndian.AppendUint16(nil, uint16(n))
	for i := 0; i < n; i++ {
		pd = binary.BigEndian.AppendUint32(pd, scTextOID)
	}
	return scMsg('t', pd)
}

const (
	scSSLRequest    = 80877103
	scCancelRequest = 80877102
)

// serve reads the first packet of a connection: a request for TLS (always refused, and what follows is kept), a cancel
// request (counted) or the start packet, whose database is the scenario.
func (f *scFake) serve(c net.Conn) {
	defer c.Close()
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return
	}
	code := binary.BigEndian.Uint32(hdr[4:])
	if code == scSSLRequest {
		_, _ = c.Write([]byte{'N'})
		rest, _ := io.ReadAll(c)
		f.after <- rest
		return
	}
	body := make([]byte, int(binary.BigEndian.Uint32(hdr[:4]))-8)
	if _, err := io.ReadFull(c, body); err != nil {
		return
	}
	if code == scCancelRequest {
		f.mu.Lock()
		f.cancel++
		f.mu.Unlock()
		return
	}
	params := map[string]string{}
	parts := strings.Split(string(body), "\x00")
	for i := 0; i+1 < len(parts); i += 2 {
		params[parts[i]] = parts[i+1]
	}
	s := &scSession{f: f, c: c, br: bufio.NewReader(c), scenario: params["database"], stmts: map[string]string{}}
	s.run()
}

// scSession is one connection after the start packet.
type scSession struct {
	f        *scFake
	c        net.Conn
	br       *bufio.Reader
	scenario string
	stmts    map[string]string // prepared statements by name
	bound    string
	pages    int
}

func (s *scSession) send(parts ...[]byte) {
	for _, p := range parts {
		_, _ = s.c.Write(p)
	}
}

func (s *scSession) read() (byte, []byte, bool) {
	h := make([]byte, 5)
	if _, err := io.ReadFull(s.br, h); err != nil {
		return 0, nil, false
	}
	n := int(binary.BigEndian.Uint32(h[1:])) - 4
	if n < 0 || n > 1<<20 {
		return 0, nil, false
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(s.br, body); err != nil {
		return 0, nil, false
	}
	return h[0], body, true
}

func (s *scSession) run() {
	s.send(scMsg('R', binary.BigEndian.AppendUint32(nil, 0)), scMsg('K', []byte{0, 0, 0, 42, 1, 2, 3, 4}), scMsg('Z', []byte{'I'}))
	for {
		typ, body, ok := s.read()
		if !ok || typ == 'X' {
			return
		}
		if !s.handle(typ, body) {
			return
		}
	}
}

func (s *scSession) handle(typ byte, body []byte) bool {
	switch typ {
	case 'Q':
		s.simpleQuery(string(strings.TrimRight(string(body), "\x00")))
	case 'P':
		p := strings.SplitN(string(body), "\x00", 3)
		s.stmts[p[0]] = p[1]
		s.send(scMsg('1', nil))
	case 'D':
		return s.describe(strings.TrimRight(string(body[1:]), "\x00"))
	case 'B':
		p := strings.SplitN(string(body), "\x00", 3)
		s.bound = s.stmts[p[1]]
		s.send(scMsg('2', nil))
	case 'E':
		return s.execute(s.bound)
	case 'C':
		s.send(scMsg('3', nil))
	case 'S':
		s.send(scMsg('Z', []byte{'T'}))
	}
	return true
}

func (s *scSession) simpleQuery(q string) {
	s.f.mu.Lock()
	s.f.simple = append(s.f.simple, q)
	s.f.mu.Unlock()
	tag, status := "BEGIN", byte('T')
	switch {
	case strings.TrimSpace(q) == ";": // the ping of the driver
		s.send(scMsg('I', nil), scMsg('Z', []byte{'I'}))
		return
	case strings.HasPrefix(q, "COMMIT"):
		tag, status = "COMMIT", 'I'
	case strings.HasPrefix(q, "ROLLBACK"):
		tag, status = "ROLLBACK", 'I'
	}
	s.send(scMsg('C', scCstr(tag)), scMsg('Z', []byte{status}))
}

// describe answers the description of a statement. The scenario neglen answers the page statement with a message whose
// length field is smaller than the field itself.
func (s *scSession) describe(name string) bool {
	q := s.stmts[name]
	if s.scenario == "neglen" && strings.Contains(q, "ORDER BY") {
		s.send([]byte{'D', 0, 0, 0, 2})
		return true
	}
	s.send(scParamDesc(q), s.rowDesc(q))
	return true
}

func (s *scSession) rowDesc(q string) []byte {
	switch {
	case strings.Contains(q, "set_config"):
		return scRowDesc("a", "b", "c")
	case strings.Contains(q, "pg_catalog.pg_type"):
		return scRowDesc("typname", "typtype", "ns", "base")
	case strings.Contains(q, "count(*)"):
		return scRowDesc("first", "all", "n")
	case strings.Contains(q, "LIMIT 0"):
		return scRowDesc("id")
	case s.scenario == "fewcols":
		return scRowDesc("id")
	}
	return scRowDesc("id", "__praxium_key_0")
}

// execute answers one Execute. The scenarios (the database name of the start packet):
//
//	neglen     a message whose length field is smaller than the field itself
//	shortmsg   a data row that says it has more columns than it has bytes
//	fewcols    rows with fewer columns than the statement has
//	nullidx    a count of the rows without a key that names a key column the statement does not have
//	nonebatch  a page that is full of one key, and no row for that key when its rows are asked for
//	silent     no answer to a statement of a page
//	bigrow     one row of 40 MiB
//	endless    rows without end
func (s *scSession) execute(q string) bool {
	switch {
	case strings.Contains(q, "set_config"):
		s.send(scDataRow("1", "1", "1"))
	case strings.Contains(q, "pg_catalog.pg_type"):
		s.send(scDataRow("text", "b", "t", "0"))
	case strings.Contains(q, "count(*)"):
		if s.scenario == "nullidx" {
			s.send(scDataRow("7", "f", "1"))
		}
	case strings.Contains(q, "LIMIT 0"):
	default:
		if !s.pageRows(q) {
			return false
		}
	}
	s.send(scMsg('C', scCstr("SELECT 0")))
	return true
}

// pageRows sends the rows of a statement of a page, by the scenario.
func (s *scSession) pageRows(q string) bool {
	switch s.scenario {
	case "fewcols":
		s.send(scDataRow("1"))
	case "shortmsg":
		s.send(scMsg('D', []byte{0, 5, 0, 0, 0, 9, 'a'}))
	case "nonebatch":
		if !strings.Contains(q, ") = (") {
			s.send(scDataRow("5", "5"), scDataRow("5", "5"))
		}
	case "silent":
		s.f.silent <- struct{}{}
		_, _ = io.Copy(io.Discard, s.c) // says nothing until the client closes the connection
		return false
	case "bigrow":
		s.send(s.f.big)
	case "endless":
		for i := 0; i < 1000; i++ {
			k := strconv.Itoa(1000000 + s.pages*1000 + i)
			s.send(scDataRow(k, k))
		}
		s.pages++
	}
	return true
}
