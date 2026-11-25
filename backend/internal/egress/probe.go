package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The probe (ADR-0105). When the check of a source fails because its certificate cannot be verified, the person who fixes
// the definition needs to see what the server presented, so that they can compare it with what the owner of the server
// gave them and pin it. The probe makes one more connection to the same host and port through the same dialer (so every
// check of the policy holds), runs the handshake only as far as the certificate, and ends it there: the settings of a
// probe fail the handshake the moment the chain is seen (probeConfig), so no byte of a request, of a password or of data
// is ever sent on it.

// probeTimeout bounds a probe, the dial and the handshake together.
const probeTimeout = 10 * time.Second

// The limits of the text of a summary. The words are the server's, so they are cut and cleaned before anyone is shown them.
const (
	summaryTextMax = 256
	summarySANMax  = 20
)

// CertSummary is what a server presented: its own certificate, in words that are safe to show. The words are the server's
// own, written by whoever made the certificate, so they are text and never markup: a screen shows them escaped, and gives
// them no meaning (they are cleaned of control and formatting characters here, but a text such as "<script>" is left as
// it is). A text that was cut (the subject, the issuer or a name) or a list of names that was cut is marked by Truncated, so
// that a long first part cannot hide what follows it without anyone seeing that something is missing.
type CertSummary struct {
	Subject, Issuer string
	// SANs are the names and addresses the certificate is for, at most 20.
	SANs []string
	// SANsMore is how many more names the certificate has than SANs holds.
	SANsMore  int
	NotBefore time.Time
	NotAfter  time.Time
	// SPKISHA256 is the pin of the certificate ("sha256/…"): what is written in tls.pins to accept it.
	SPKISHA256 string
	// Truncated says that a text was cut at its limit or that SANsMore is not zero: what is shown is not all of it.
	Truncated bool
}

// ProbeCert shows the certificate of the server that rawURL (an https address) names, as the client would reach it.
func (c *Client) ProbeCert(ctx context.Context, rawURL string) (*CertSummary, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil {
		return nil, refused("", "the address is not an https address with a host and no user information")
	}
	port, ok := parsePort(portOf(u))
	if !ok {
		return nil, refused("", "the port is not a number from 1 to 65535")
	}
	return c.d.probe(ctx, u.Hostname(), port, false)
}

// ProbePostgresCert shows the certificate of a PostgreSQL server: the connection begins as the protocol says, with a
// request to start TLS, and the handshake follows when the server agrees.
func (d *Dialer) ProbePostgresCert(ctx context.Context, host string, port int) (*CertSummary, error) {
	if port < 1 || port > 65535 {
		return nil, refused("", "the port is not a number from 1 to 65535")
	}
	return d.probe(ctx, host, uint16(port), true)
}

// postgresSSLRequest is the packet that asks a PostgreSQL server to start TLS: its length (8) and the code 80877103.
func postgresSSLRequest() []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[0:4], 8)
	binary.BigEndian.PutUint32(b[4:8], 80877103)
	return b
}

func (d *Dialer) probe(ctx context.Context, host string, port uint16, postgres bool) (*CertSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	name, ip, bad := NormalizeHost(host)
	if bad != "" {
		return nil, refused("", bad)
	}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(name, strconv.Itoa(int(port))))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if postgres {
		if err := askPostgresForTLS(conn); err != nil {
			return nil, err
		}
	}
	sni := name
	if ip.IsValid() {
		sni = ""
	}
	var rec presented
	hs := tls.Client(conn, probeConfig(sni, &rec))
	herr := hs.HandshakeContext(ctx)
	if len(rec.chain) == 0 {
		if herr == nil {
			herr = errNoPeerCertificate
		}
		return nil, Sanitize(herr)
	}
	return summarize(rec.chain[0]), nil
}

// askPostgresForTLS sends the request to start TLS and reads the one byte of the answer: S means the TLS handshake
// follows. Nothing else is sent.
func askPostgresForTLS(conn net.Conn) error {
	if _, err := conn.Write(postgresSSLRequest()); err != nil {
		return Sanitize(err)
	}
	var answer [1]byte
	if _, err := io.ReadFull(conn, answer[:]); err != nil {
		return Sanitize(err)
	}
	if answer[0] != 'S' {
		return &Error{Kind: KindTLS, Reason: "the server does not offer TLS"}
	}
	return nil
}

func summarize(cert *x509.Certificate) *CertSummary {
	sum := &CertSummary{NotBefore: cert.NotBefore.UTC(), NotAfter: cert.NotAfter.UTC(), SPKISHA256: PinOf(cert)}
	var cut bool
	sum.Subject, cut = cleanText(cert.Subject.String())
	sum.Truncated = cut
	sum.Issuer, cut = cleanText(cert.Issuer.String())
	sum.Truncated = sum.Truncated || cut
	var names []string
	names = append(names, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		if a, ok := netip.AddrFromSlice(ip); ok {
			names = append(names, a.Unmap().String())
		}
	}
	names = append(names, cert.EmailAddresses...)
	for _, u := range cert.URIs {
		names = append(names, u.String())
	}
	for i, n := range names {
		if i == summarySANMax {
			sum.SANsMore = len(names) - summarySANMax
			sum.Truncated = true
			break
		}
		text, cut := cleanText(n)
		sum.Truncated = sum.Truncated || cut
		sum.SANs = append(sum.SANs, text)
	}
	return sum
}

// cleanText makes a text of a certificate fit to be shown: bytes that are not UTF-8 are replaced, the control characters
// and the formatting characters of Unicode (the marks that turn the direction of the text, the zero-width ones) and the
// separators of lines are taken out, and what is left is cut at summaryTextMax bytes. The second result says that it was cut.
func cleanText(s string) (string, bool) {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > summaryTextMax {
			return b.String(), true
		}
		b.WriteRune(r)
	}
	return b.String(), false
}
