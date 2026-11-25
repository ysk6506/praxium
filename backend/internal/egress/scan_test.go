package egress

import (
	"embed"
	"strings"
	"testing"
)

//go:embed *.go
var scanSources embed.FS

// [REQ-661]
func TestTheCodeHasNoWayAroundTheDefence(t *testing.T) {
	forbidden := map[string]string{
		"VerifyPeerCertificate": "a hook that replaces the verification of a certificate",
		"KeyLogWriter":          "a log of the keys of every connection",
		"EncryptedClientHello":  "a setting that makes the handshake skip the hook of the verification",
		"ProxyFromEnvironment":  "a proxy read from the environment",
		"ProxyURL":              "a proxy",
		"\"syscall\"":           "the syscall package (the connect hook of net.Dialer)",
		".Control":              "the connect hook of net.Dialer",
		"ControlContext":        "the connect hook of net.Dialer",
		"LookupHost":            "a lookup that drops the order and the family of the answer",
		"LookupIP":              "a lookup that drops the family of the answer",
		"DialTLS":               "a dial of the transport that skips the dialer of the package",
		"http.DefaultClient":    "a client that is not this package's",
		"http.DefaultTransport": "a transport that is not this package's",
		"slog.":                 "a log from the package",
		"log.Print":             "a log from the package",
		"fmt.Print":             "a print from the package",
	}
	// The two words that turn the verification of a certificate off and put a check in its place are in one file, pin.go, and
	// there once each (ADR-0105). Where in that file, and in what shape, is checked on the syntax tree by
	// TestHxbTheOnlyWayToSkipTheVerificationIsTheConfigOfThePins.
	pinOnly := map[string]string{
		"InsecureSkipVerify": "a switch that turns the verification of a certificate off",
		"VerifyConnection":   "a hook that replaces the verification of a certificate",
	}
	seenInPin := map[string]int{}
	entries, err := scanSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := scanSources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for word, why := range forbidden {
				if strings.Contains(line, word) {
					t.Errorf("%s: %q is %s", name, strings.TrimSpace(line), why)
				}
			}
			for word, why := range pinOnly {
				if !strings.Contains(line, word) {
					continue
				}
				if name != "pin.go" {
					t.Errorf("%s: %q is %s, and only pin.go may have it", name, strings.TrimSpace(line), why)
				}
				seenInPin[word]++
			}
		}
	}
	for word := range pinOnly {
		if seenInPin[word] != 1 {
			t.Errorf("%s is in %d lines of code of pin.go, and has to be in exactly one", word, seenInPin[word])
		}
	}
	if checked < 6 {
		t.Errorf("only %d source files were read", checked)
	}
}

// [REQ-661]
func TestTheDialerConnectsToTheLiteralAddressItJudged(t *testing.T) {
	data, err := scanSources.ReadFile("dialer.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, ".connect(") != 1 || !strings.Contains(text, "d.connect(attempt, network, netip.AddrPortFrom(a, port).String())") {
		t.Error("the one connect of the dialer is not given the literal address of the answer")
	}
	if strings.Count(text, ".LookupNetIP(") != 1 {
		t.Error("the dialer resolves in more than one place")
	}
}
