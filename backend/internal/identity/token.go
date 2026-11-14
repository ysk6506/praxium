package identity

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// The shape of an access token: "pxm_" and 16 lower-case hexadecimal characters that identify the token (the
// token ID), an underscore and 64 hexadecimal characters of secret (256 bits from the kernel's entropy). Anyone may
// see the ID; only the holder knows the secret.
const (
	// TokenPrefix starts every token, so a token in a file or a log is recognised for what it is.
	TokenPrefix = "pxm_"
	idBytes     = 8
	secretBytes = 32
	idHexLen    = 2 * idBytes
	secretLen   = 2 * secretBytes
	// hashDomain separates this hash from any other use of SHA-256 over the same bytes.
	hashDomain = "praxium.token.v1"
)

// constantTimeEqual compares a stored hash with the one computed from a presented secret. It is a variable so a
// test can see that the check goes through it, and that it is the comparison of the standard library.
var constantTimeEqual = subtle.ConstantTimeCompare

// newTokenParts makes the ID and the secret of a new token from the random source: n bytes as 2n hex characters.
func newTokenParts(random func(n int) string) (id, secret string, ok bool) {
	idPart, secret := random(idBytes), random(secretBytes)
	if len(idPart) != idHexLen || len(secret) != secretLen || !lowerHex(idPart) || !lowerHex(secret) {
		return "", "", false
	}
	return TokenPrefix + idPart, secret, true
}

// formatToken is the whole token: the text the holder presents.
func formatToken(id, secret string) string { return id + "_" + secret }

// parseToken splits a presented token. A text of another shape is not a token and costs no look into the store.
func parseToken(raw string) (id, secret string, ok bool) {
	rest, found := strings.CutPrefix(raw, TokenPrefix)
	if !found || len(rest) != idHexLen+1+secretLen || rest[idHexLen] != '_' {
		return "", "", false
	}
	idPart, secret := rest[:idHexLen], rest[idHexLen+1:]
	if !lowerHex(idPart) || !lowerHex(secret) {
		return "", "", false
	}
	return TokenPrefix + idPart, secret, true
}

// hashSecret is the hex SHA-256 of the secret, bound to the token ID. The secret is 256 bits of randomness, so a
// fast hash is enough: there is nothing to guess that a slow hash would make more expensive (ADR-0044).
func hashSecret(id, secret string) string {
	sum := sha256.Sum256([]byte(hashDomain + "\x00" + id + "\x00" + secret))
	return hex.EncodeToString(sum[:])
}

// secretMatches reports whether the secret is the one whose hash is stored, comparing in constant time.
func secretMatches(id, secret, storedHash string) bool {
	return constantTimeEqual([]byte(hashSecret(id, secret)), []byte(storedHash)) == 1
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
