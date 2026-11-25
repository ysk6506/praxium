package egress

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strings"
)

// Certificate pinning (ADR-0105). A source whose certificate cannot be verified (a self-signed one, a name that does not
// match) may name the public keys of the certificates it is allowed to present. The check is then not the chain, the name
// or the dates, but whether the SHA-256 of the public key of the certificate the server sent (its SubjectPublicKeyInfo)
// is one of the pins. The server has to prove it owns the key in the handshake, so a copy of the certificate is no use to
// anyone else.
//
// This file holds the one place in the whole module where the verification of a certificate is turned off:
// pinnedTLSConfig. It is made only from a pinSet, and a pinSet is made only by parsePins, which refuses an empty list, so
// there is no way to reach it without pins. The scan test of the package checks all of this on the syntax tree.

// pinPrefix starts every pin: the hash and the base64 of the hash (the form of the SPKI pins of HTTP public key pinning).
const pinPrefix = "sha256/"

// MaxPins is how many pins a connection may name (several, so that a key can be replaced without a gap).
const MaxPins = 8

// pinSet is a list of public key hashes that has at least one. parsePins is the only way to make one.
type pinSet struct {
	sums [][sha256.Size]byte
}

// PinMismatchError is the error a handshake gives when the server's certificate is not one of the pins. It holds what the
// server presented, which is public, so that a person who is shown it can compare it with the one they were given.
type PinMismatchError struct {
	// Presented is the pin of the certificate the server sent, in the form of the pins ("sha256/…").
	Presented string
}

// Error implements error. The words are fixed.
func (e *PinMismatchError) Error() string {
	return "the certificate of the server is not one of the pinned ones"
}

var (
	errNoPeerCertificate = errors.New("egress: the server sent no certificate")
	errProbeDone         = errors.New("egress: the probe has seen the certificate and stops before any data is sent")
)

// PinOf is the pin of a certificate: the SHA-256 of its SubjectPublicKeyInfo, written as "sha256/" and base64.
func PinOf(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return pinPrefix + base64.StdEncoding.EncodeToString(sum[:])
}

// ParsePin checks the text of a pin and returns it as written in the canonical form. The error says only what is wrong
// with the form, never the text.
func ParsePin(text string) (string, error) {
	sum, err := parseOnePin(text)
	if err != nil {
		return "", err
	}
	return pinPrefix + base64.StdEncoding.EncodeToString(sum[:]), nil
}

func parseOnePin(text string) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte
	rest, ok := strings.CutPrefix(text, pinPrefix)
	if !ok {
		return out, &Error{Kind: KindTLS, Reason: "a pin has to begin with sha256/"}
	}
	raw, err := base64.StdEncoding.DecodeString(rest)
	if err != nil || len(raw) != sha256.Size {
		return out, &Error{Kind: KindTLS, Reason: "a pin has to be the base64 of 32 bytes after sha256/"}
	}
	copy(out[:], raw)
	return out, nil
}

// parsePins makes a pinSet from the pins of a connection. An empty list, more than MaxPins and a pin that is not in the
// form are refused; a pin written twice counts once.
func parsePins(pins []string) (pinSet, error) {
	var set pinSet
	if len(pins) == 0 {
		return set, &Error{Kind: KindTLS, Reason: "there is no pin; a connection is pinned with at least one"}
	}
	if len(pins) > MaxPins {
		return set, &Error{Kind: KindTLS, Reason: "there are more pins than a connection may have"}
	}
	seen := map[[sha256.Size]byte]bool{}
	for _, text := range pins {
		sum, err := parseOnePin(text)
		if err != nil {
			return pinSet{}, err
		}
		if !seen[sum] {
			seen[sum] = true
			set.sums = append(set.sums, sum)
		}
	}
	return set, nil
}

// verify is the check of a pinned connection: the first certificate of the chain, which is the server's own, has a public
// key whose hash is a pin. A pinSet that holds no pin accepts nothing.
func (p pinSet) verify(chain []*x509.Certificate) error {
	sum := sha256.Sum256(chain[0].RawSubjectPublicKeyInfo)
	for _, want := range p.sums {
		if subtle.ConstantTimeCompare(sum[:], want[:]) == 1 {
			return nil
		}
	}
	return &PinMismatchError{Presented: pinPrefix + base64.StdEncoding.EncodeToString(sum[:])}
}

// pinnedConfig is the TLS settings of a connection with pins.
func pinnedConfig(pins []string) (*tls.Config, error) {
	set, err := parsePins(pins)
	if err != nil {
		return nil, err
	}
	return pinnedTLSConfig("", set.verify), nil
}

// presented is the chain a server sent in a probe.
type presented struct {
	chain []*x509.Certificate
}

// probeConfig is the TLS settings of a probe: it writes down the chain the server sends and then fails the handshake, so
// that nothing is sent after the hello and the answer of the server.
func probeConfig(serverName string, rec *presented) *tls.Config {
	return pinnedTLSConfig(serverName, func(chain []*x509.Certificate) error {
		rec.chain = chain
		return errProbeDone
	})
}

// pinnedTLSConfig is the one place of the module that does not verify the chain, the name and the dates of the
// certificate of a server, and says by what the certificate is judged in its place: verify, which is given the chain the
// server sent. The version is TLS 1.2 or later. VerifyConnection is called by the handshake whatever InsecureSkipVerify
// says, in a full handshake and when a session is resumed.
func pinnedTLSConfig(serverName string, verify func(chain []*x509.Certificate) error) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName,
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errNoPeerCertificate
			}
			return verify(cs.PeerCertificates)
		},
	}
}
