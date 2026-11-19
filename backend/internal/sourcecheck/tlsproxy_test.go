package sourcecheck

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// scNoRand is the source of "randomness" of the certificates of these tests: a certificate and its key are made from a
// fixed seed, so the same test makes the same bytes, and nothing here reads the system's randomness. (An Ed25519
// signature takes none.)
type scNoRand struct{}

func (scNoRand) Read(p []byte) (int, error) { return len(p), nil }

// scCert is a certificate of an authority that signs itself, for the names, made from a fixed seed, valid until 2126. The
// test trusts it as the CA of the source, so it is both: the CA and the certificate of the server.
func scCert(t *testing.T, seed byte, dns []string, loopbackIP bool) (tls.Certificate, string) {
	t.Helper()
	key := ed25519.NewKeyFromSeed([]byte(string(make([]byte, 31)) + string([]byte{seed})))
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(int64(seed)),
		Subject:               pkix.Name{CommonName: "praxium test " + strconv.Itoa(int(seed))},
		NotBefore:             time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2126, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dns,
	}
	if loopbackIP {
		tmpl.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)}
	}
	der, err := x509.CreateCertificate(scNoRand{}, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// scTLSProxy listens on the loopback, answers the request for TLS with S, ends TLS with the certificate, and talks
// plaintext to the real test server behind it. It gives its port.
func scTLSProxy(t *testing.T, cert tls.Certificate, backHost string, backPort int) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go scProxyOne(c, cfg, net.JoinHostPort(backHost, strconv.Itoa(backPort)))
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func scProxyOne(c net.Conn, cfg *tls.Config, back string) {
	defer c.Close()
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(c, hdr); err != nil || binary.BigEndian.Uint32(hdr[4:]) != scSSLRequest {
		return
	}
	if _, err := c.Write([]byte{'S'}); err != nil {
		return
	}
	srv := tls.Server(c, cfg)
	defer srv.Close()
	if err := srv.Handshake(); err != nil {
		return
	}
	real, err := net.Dial("tcp", back)
	if err != nil {
		return
	}
	defer real.Close()
	go func() {
		_, _ = io.Copy(real, srv)
		_ = real.Close()
	}()
	_, _ = io.Copy(srv, real)
}

func scTestServer(t *testing.T) (host string, port int, user, pass string) {
	t.Helper()
	get := func(name string) string {
		v, ok := kernel.LookupEnv("PRAXIUM_TEST_PG_TCP_" + name)
		if !ok || v == "" {
			t.Fatalf("PRAXIUM_TEST_PG_TCP_%s is not set: run the tests through scripts/with-postgres.sh", name)
		}
		return v
	}
	p, err := strconv.Atoi(get("PORT"))
	if err != nil {
		t.Fatal("PRAXIUM_TEST_PG_TCP_PORT is not a number")
	}
	return get("HOST"), p, get("USER"), get("PASSWORD")
}
