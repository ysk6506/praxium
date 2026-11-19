package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// dtResolver answers from a script: the nth lookup of a name gets the nth answer (the last one repeats).
type dtResolver struct {
	mu      sync.Mutex
	answers map[string][][]netip.Addr
	err     error
	block   bool
	calls   map[string]int
}

func (r *dtResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	if r.calls == nil {
		r.calls = map[string]int{}
	}
	n := r.calls[host]
	r.calls[host]++
	r.mu.Unlock()
	if r.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if r.err != nil {
		return nil, r.err
	}
	list := r.answers[host]
	if len(list) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return list[min(n, len(list)-1)], nil
}

func (r *dtResolver) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	sum := 0
	for _, n := range r.calls {
		sum += n
	}
	return sum
}

func dtAddrs(t *testing.T, texts ...string) []netip.Addr {
	t.Helper()
	out := make([]netip.Addr, 0, len(texts))
	for _, s := range texts {
		out = append(out, pAddr(t, s))
	}
	return out
}

type dtDial struct{ network, address string }

// dtDialer is a Dialer whose connections go to a recorder instead of the network.
func dtDialer(t *testing.T, cfg Config, failing map[string]bool) (*Dialer, *[]dtDial) {
	t.Helper()
	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var mu sync.Mutex
	var seen []dtDial
	d.connect = func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		seen = append(seen, dtDial{network, address})
		mu.Unlock()
		if failing[address] {
			return nil, errors.New("dial tcp " + address + ": connect: connection refused: secret-detail")
		}
		client, server := net.Pipe()
		t.Cleanup(func() { _ = server.Close() })
		return client, nil
	}
	return d, &seen
}

func dtKind(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not an *Error", err)
	}
	return e
}

// [REQ-661]
func TestDialerConnectsToTheAddressItJudgedAndResolvesOnce(t *testing.T) {
	// the second answer of the name is the metadata service: a rebinding name
	res := &dtResolver{answers: map[string][][]netip.Addr{
		"rebind.example.test": {dtAddrs(t, "93.184.216.34"), dtAddrs(t, "169.254.169.254")},
	}}
	d, seen := dtDialer(t, Config{Resolver: res}, nil)
	conn, err := d.DialContext(context.Background(), "tcp", "rebind.example.test:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()
	if res.total() != 1 {
		t.Errorf("the name was resolved %d times", res.total())
	}
	if len(*seen) != 1 || (*seen)[0] != (dtDial{"tcp4", "93.184.216.34:443"}) {
		t.Errorf("connected to %v, want the judged literal address only", *seen)
	}
	// the next dial resolves again and now meets the refused answer
	if _, err := d.DialContext(context.Background(), "tcp", "rebind.example.test:443"); dtKind(t, err).Kind != KindPolicy {
		t.Errorf("second dial: %v", err)
	}
	if len(*seen) != 1 {
		t.Errorf("a refused answer was dialed: %v", *seen)
	}
}

// [REQ-661]
func TestOneRefusedAddressRefusesTheWholeAnswer(t *testing.T) {
	orders := [][]string{
		{"93.184.216.34", "169.254.169.254"},
		{"169.254.169.254", "93.184.216.34"},
		{"93.184.216.34", "2606:4700::1111", "::ffff:169.254.169.254"},
		{"93.184.216.34", "fe80::1"},
		{"93.184.216.34", "224.0.0.1"},
		{"93.184.216.34", "0.0.0.0"},
	}
	for _, order := range orders {
		res := &dtResolver{answers: map[string][][]netip.Addr{"mixed.example.test": {dtAddrs(t, order...)}}}
		d, seen := dtDialer(t, Config{Resolver: res}, nil)
		_, err := d.DialContext(context.Background(), "tcp", "mixed.example.test:443")
		e := dtKind(t, err)
		if e.Kind != KindPolicy || e.Temporary() {
			t.Errorf("%v: error %v", order, err)
		}
		if len(*seen) != 0 {
			t.Errorf("%v: dialed %v", order, *seen)
		}
		for _, leak := range []string{"169.254", "fe80", "224.0", "0.0.0.0", "93.184"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%v: the message %q holds %q", order, err.Error(), leak)
			}
		}
		if e.Host != "mixed.example.test" {
			t.Errorf("host of the error = %q", e.Host)
		}
	}
}

// [REQ-661]
func TestIPLiteralsAreJudgedWithoutResolving(t *testing.T) {
	res := &dtResolver{}
	d, seen := dtDialer(t, Config{Resolver: res}, nil)
	for _, ok := range []string{"10.0.0.5:5432", "[::1]:5432", "[2606:4700::1111]:443", "127.0.0.1:8080", "[::ffff:10.0.0.7]:80"} {
		conn, err := d.DialContext(context.Background(), "tcp", ok)
		if err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
		_ = conn.Close()
	}
	if len(*seen) != 5 || res.total() != 0 {
		t.Errorf("dialed %v, resolved %d times", *seen, res.total())
	}
	if (*seen)[4] != (dtDial{"tcp4", "10.0.0.7:80"}) || (*seen)[1].network != "tcp6" {
		t.Errorf("families: %v", *seen)
	}
	before := len(*seen)
	for _, bad := range []string{
		"169.254.169.254:80", "[::ffff:169.254.169.254]:80", "[fd00:ec2::254]:80", "0.0.0.0:80", "[::]:80", "224.0.0.1:80",
		"127.1:80", "2130706433:80", "0x7f.0.0.1:80", "0177.0.0.1:80", "[fe80::1%25eth0]:80", "[::ffff:a9fe:a9fe]:80",
	} {
		if _, err := d.DialContext(context.Background(), "tcp", bad); dtKind(t, err).Kind != KindPolicy {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(*seen) != before || res.total() != 0 {
		t.Errorf("a refused literal reached the network or the resolver: %v", *seen)
	}
}

// [REQ-661]
func TestAddressesThatAreNotHostAndPortAreRefused(t *testing.T) {
	d, seen := dtDialer(t, Config{Resolver: &dtResolver{}}, nil)
	for _, bad := range []string{
		"", "example.test", ":443", "example.test:", "example.test:0", "example.test:http", "example.test:65536", "example.test:-1",
		"a b.test:443", "user@example.test:443", "example.test/x:443", "[::1:80",
	} {
		if _, err := d.DialContext(context.Background(), "tcp", bad); dtKind(t, err).Kind != KindPolicy {
			t.Errorf("%q: %v", bad, err)
		}
	}
	for _, network := range []string{"udp", "unix", "ip4:icmp", "tcp5", ""} {
		if _, err := d.DialContext(context.Background(), network, "10.0.0.1:80"); dtKind(t, err).Kind != KindPolicy {
			t.Errorf("network %q: %v", network, err)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("dialed %v", *seen)
	}
}

// [REQ-661]
func TestHostAndPortListsKeepASourceOnItsOwnAddress(t *testing.T) {
	res := &dtResolver{answers: map[string][][]netip.Addr{
		"api.example.test":   {dtAddrs(t, "93.184.216.34")},
		"other.example.test": {dtAddrs(t, "93.184.216.35")},
	}}
	d, seen := dtDialer(t, Config{Resolver: res, Policy: Policy{Hosts: []string{"API.example.test"}, Ports: []uint16{443}}}, nil)
	conn, err := d.DialContext(context.Background(), "tcp", "api.example.test.:443")
	if err != nil {
		t.Fatalf("listed host: %v", err)
	}
	_ = conn.Close()
	for _, bad := range []string{"other.example.test:443", "api.example.test:8443", "93.184.216.34:443"} {
		if _, err := d.DialContext(context.Background(), "tcp", bad); dtKind(t, err).Kind != KindPolicy {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(*seen) != 1 || res.total() != 1 {
		t.Errorf("dialed %v, resolved %d times", *seen, res.total())
	}
}

// [REQ-661]
func TestDialerKeepsToTheFamilyAskedFor(t *testing.T) {
	res := &dtResolver{answers: map[string][][]netip.Addr{
		"dual.example.test": {dtAddrs(t, "2606:4700::1111", "93.184.216.34", "::ffff:93.184.216.36")},
		"six.example.test":  {dtAddrs(t, "2606:4700::1111")},
	}}
	d, seen := dtDialer(t, Config{Resolver: res}, map[string]bool{"[2606:4700::1111]:443": true})
	conn, err := d.DialContext(context.Background(), "tcp", "dual.example.test:443")
	if err != nil {
		t.Fatalf("fallback to the next address: %v", err)
	}
	_ = conn.Close()
	if len(*seen) != 2 || (*seen)[0].network != "tcp6" || (*seen)[1] != (dtDial{"tcp4", "93.184.216.34:443"}) {
		t.Errorf("attempts: %v", *seen)
	}
	*seen = nil
	if conn, err = d.DialContext(context.Background(), "tcp4", "dual.example.test:443"); err != nil {
		t.Fatalf("tcp4: %v", err)
	}
	_ = conn.Close()
	if len(*seen) != 1 || (*seen)[0].network != "tcp4" {
		t.Errorf("tcp4 attempts: %v", *seen)
	}
	if _, err = d.DialContext(context.Background(), "tcp4", "six.example.test:443"); dtKind(t, err).Kind != KindResolve {
		t.Errorf("tcp4 with only an IPv6 answer: %v", err)
	}
}

// [REQ-661]
func TestEveryAddressFailingIsAConnectErrorThatHoldsNoDetail(t *testing.T) {
	var all []string
	failing := map[string]bool{}
	for i := 1; i <= 10; i++ {
		a := fmt.Sprintf("93.184.216.%d", i)
		all = append(all, a)
		failing[a+":443"] = true
	}
	res := &dtResolver{answers: map[string][][]netip.Addr{"down.example.test": {dtAddrs(t, all...)}}}
	d, seen := dtDialer(t, Config{Resolver: res}, failing)
	_, err := d.DialContext(context.Background(), "tcp", "down.example.test:443")
	e := dtKind(t, err)
	if e.Kind != KindConnect || !e.Temporary() || e.Timeout() {
		t.Errorf("error %v", err)
	}
	if strings.Contains(err.Error(), "secret-detail") || strings.Contains(err.Error(), "93.184") {
		t.Errorf("message %q holds a detail", err.Error())
	}
	if len(*seen) != maxAttempts {
		t.Errorf("%d attempts, want %d", len(*seen), maxAttempts)
	}
}

// [REQ-661]
func TestResolveFailuresAreResolveErrors(t *testing.T) {
	cases := []struct {
		name      string
		res       *dtResolver
		temporary bool
	}{
		{"not found", &dtResolver{}, false},
		{"temporary", &dtResolver{err: &net.DNSError{Err: "server misbehaving", IsTemporary: true}}, true},
		{"other", &dtResolver{err: errors.New("resolver exploded: secret-detail")}, false},
		{"empty answer", &dtResolver{answers: map[string][][]netip.Addr{"empty.example.test": {{}}}}, false},
	}
	for _, c := range cases {
		d, seen := dtDialer(t, Config{Resolver: c.res}, nil)
		_, err := d.DialContext(context.Background(), "tcp", "empty.example.test:443")
		e := dtKind(t, err)
		if e.Kind != KindResolve || e.Temporary() != c.temporary || len(*seen) != 0 {
			t.Errorf("%s: %v (temporary %v)", c.name, err, e.Temporary())
		}
		if strings.Contains(err.Error(), "secret-detail") {
			t.Errorf("%s: %q", c.name, err.Error())
		}
	}
}

// [REQ-661]
func TestTimeLimitAndCancellationEndTheDial(t *testing.T) {
	res := &dtResolver{block: true}
	d, _ := dtDialer(t, Config{Resolver: res, DialTimeout: 20 * time.Millisecond}, nil)
	_, err := d.DialContext(context.Background(), "tcp", "slow.example.test:443")
	e := dtKind(t, err)
	if e.Kind != KindTimeout || !e.Timeout() || !e.Temporary() || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline: %v", err)
	}
	if _, err = d.DialTimeout("tcp", "slow.example.test:443", 5*time.Millisecond); dtKind(t, err).Kind != KindTimeout {
		t.Errorf("DialTimeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = d.DialContext(ctx, "tcp", "slow.example.test:443")
	e = dtKind(t, err)
	if e.Kind != KindCanceled || e.Temporary() || !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cancel: %v", err)
	}
}

// [REQ-661]
func TestCancellationDuringTheConnectIsNotAFailureOfTheSource(t *testing.T) {
	res := &dtResolver{answers: map[string][][]netip.Addr{"c.example.test": {dtAddrs(t, "93.184.216.34", "93.184.216.35")}}}
	d, err := New(Config{Resolver: res})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	d.connect = func(ctx context.Context, network, address string) (net.Conn, error) {
		attempts++
		cancel()
		return nil, errors.New("interrupted")
	}
	_, err = d.DialContext(ctx, "tcp", "c.example.test:443")
	if e := dtKind(t, err); e.Kind != KindCanceled || attempts != 1 {
		t.Errorf("%v after %d attempts", err, attempts)
	}
}

// [REQ-661]
func TestDialerConnectsToARealLoopbackListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan error, 3)
	go func() {
		for i := 0; i < 3; i++ {
			c, err := ln.Accept()
			accepted <- err
			if c != nil {
				_ = c.Close()
			}
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	res := &dtResolver{answers: map[string][][]netip.Addr{"local.example.test": {dtAddrs(t, "127.0.0.1")}}}
	d, err := New(Config{Resolver: res})
	if err != nil {
		t.Fatal(err)
	}
	for i, dial := range []func() (net.Conn, error){
		func() (net.Conn, error) { return d.Dial("tcp", "local.example.test:"+port) },
		func() (net.Conn, error) { return d.DialTimeout("tcp4", "local.example.test:"+port, time.Second) },
		func() (net.Conn, error) { return d.DialContext(context.Background(), "tcp", "127.0.0.1:"+port) },
	} {
		conn, err := dial()
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		if got := conn.RemoteAddr().String(); got != ln.Addr().String() {
			t.Errorf("dial %d connected to %s", i, got)
		}
		_ = conn.Close()
		select {
		case err := <-accepted:
			if err != nil {
				t.Fatalf("dial %d: the listener failed: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("dial %d: the listener saw no connection", i)
		}
	}
	// the same listener is refused when the policy says the server itself listens there
	self := netip.MustParseAddrPort(ln.Addr().String())
	guarded, err := New(Config{Resolver: res, Policy: Policy{Self: []netip.AddrPort{self}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = guarded.Dial("tcp", "local.example.test:"+port); dtKind(t, err).Kind != KindPolicy {
		t.Errorf("own address: %v", err)
	}
}

// [REQ-661]
func TestNewFillsTheDefaults(t *testing.T) {
	d, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if d.resolver != Resolver(net.DefaultResolver) || d.timeout != defaultDialTimeout || d.locals == nil || d.connect == nil {
		t.Errorf("defaults not filled: %+v", d)
	}
	addrs, err := systemAddrs()
	if err != nil {
		t.Fatalf("systemAddrs: %v", err)
	}
	for _, a := range addrs {
		if !a.IsValid() {
			t.Errorf("invalid address %v", a)
		}
	}
}

// [REQ-661]
func TestSelfEntryWithAnOpenAddressUsesTheInterfacesOfTheConfig(t *testing.T) {
	res := &dtResolver{}
	cfg := Config{
		Resolver:   res,
		Policy:     Policy{Self: []netip.AddrPort{netip.MustParseAddrPort("0.0.0.0:8080")}},
		Interfaces: func() ([]netip.Addr, error) { return dtAddrs(t, "10.0.0.5"), nil },
	}
	d, seen := dtDialer(t, cfg, nil)
	for _, bad := range []string{"10.0.0.5:8080", "127.0.0.1:8080", "[::1]:8080"} {
		if _, err := d.DialContext(context.Background(), "tcp", bad); dtKind(t, err).Kind != KindPolicy {
			t.Errorf("%s: %v", bad, err)
		}
	}
	conn, err := d.DialContext(context.Background(), "tcp", "10.0.0.5:5432")
	if err != nil {
		t.Fatalf("another port: %v", err)
	}
	_ = conn.Close()
	if len(*seen) != 1 {
		t.Errorf("dialed %v", *seen)
	}
}
