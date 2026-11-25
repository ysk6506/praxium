package pgsource

import (
	"embed"
	"regexp"
	"strings"
	"testing"
)

//go:embed *.go
var pgsSources embed.FS

// pgsCode is the lines of a non-test source file that are not comments, with the file's name.
func pgsCode(t *testing.T) map[string][]string {
	t.Helper()
	entries, err := pgsSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := pgsSources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			out[name] = append(out[name], line)
		}
	}
	if len(out) < 10 {
		t.Fatalf("only %d source files were read", len(out))
	}
	return out
}

// [REQ-487] [REQ-490] [REQ-659] [REQ-660]
func TestPgsTheCodeHasNoWayAroundTheDefence(t *testing.T) {
	code := pgsCode(t)
	only := map[string]string{ // text -> the one file that may have it
		".Reveal(":         "credential.go",
		".QueryContext(":   "session.go",
		".PrepareContext(": "session.go",
		".ExecContext(":    "",
		".Exec(":           "",
		".Query(":          "",
		".QueryRow":        "",
		".Prepare(":        "",
		"sql.OpenDB(":      "connect.go",
		".Begin(":          "",
		"BeginTx(":         "session.go",
	}
	forbidden := map[string]string{
		"log.":                  "a log from the adapter (the Observer is the way to tell)",
		"slog.":                 "a log from the adapter",
		"fmt.Print":             "a print from the adapter",
		"os.Std":                "a write to the standard streams",
		"os.Getenv":             "a read of the environment",
		"os.LookupEnv":          "a read of the environment",
		"os.Environ":            "a read of the environment",
		"sql.Open(":             "a connection that does not go through the dialer of egress",
		"pq.Open":               "a connection that does not go through the dialer of egress",
		"pq.NewConfig":          "a configuration read from the environment",
		"pq.NewConnector(":      "a configuration read from a text and the environment",
		"pq.DialOpen":           "a dial that is not the connector's",
		"InsecureSkipVerify":    "a switch that turns the verification of a certificate off",
		"VerifyPeerCertificate": "a hook that replaces the verification of a certificate",
		"VerifyConnection":      "a hook that replaces the verification of a certificate (egress sets it, for the pins)",
		"KeyLogWriter":          "a log of the keys of the connection",
		"EncryptedClientHello":  "a setting that makes the handshake skip the hook of the verification",
		"GetClientCertificate":  "a client certificate (a definition has none)",
		"RootCAs":               "a list of authorities of this package (egress makes the settings)",
		"ServerName":            "a change to the settings that egress made",
		"MinVersion":            "a change to the settings that egress made",
		"MaxVersion":            "a change to the settings that egress made",
		"Renegotiation":         "a change to the settings that egress made",
		"egress.TLSConfig":      "the settings of egress in the hand of this package (RegisterPostgresTLS keeps them out of it)",
		"pq.RegisterTLSConfig(": "a registration of settings that this package holds (the function is handed to egress.RegisterPostgresTLS, not called)",
		"net.Dial":              "a dial that does not go through egress",
		"net.Dialer":            "a dialer that is not egress's",
		"crypto/tls":            "a TLS configuration of this package (egress makes it)",
		"SSLModeRequire":        "a mode that does not verify",
		"SSLModePrefer":         "a mode that does not verify",
		"SSLModeVerifyCA":       "a mode that does not verify the name",
		"SSLModeAllow":          "a mode that does not verify",
	}
	// Write statements and settings, as words of a statement (a word of Go, such as `Update`, is no match).
	write := regexp.MustCompile(`"[^"]*\b(INSERT|UPDATE|DELETE|CREATE|DROP|TRUNCATE|ALTER|GRANT|COPY|LISTEN|NOTIFY|VACUUM|LOCK|CALL|DO|SET|RESET)\b[^"]*"`)
	for name, lines := range code {
		for _, line := range lines {
			for text, file := range only {
				if strings.Contains(line, text) && name != file {
					t.Errorf("%s: %q is used outside %q", name, strings.TrimSpace(line), file)
				}
			}
			for text, why := range forbidden {
				if pgsHasWord(line, text) {
					t.Errorf("%s: %q is %s", name, strings.TrimSpace(line), why)
				}
			}
			if m := write.FindString(line); m != "" && !strings.Contains(line, "set_config") {
				t.Errorf("%s: %q looks like a statement that writes or changes a setting", name, strings.TrimSpace(line))
			}
		}
	}
	// The two places that must exist, so that the scan does not pass because it looks at nothing.
	for _, c := range []struct{ file, text string }{{"credential.go", ".Reveal("}, {"session.go", ".PrepareContext("}, {"session.go", ".QueryContext("}, {"connect.go", "conn.Dialer("}, {"session.go", "ReadOnly: true"}, {"connect.go", "egress.RegisterPostgresTLS(pq.RegisterTLSConfig,"}} {
		found := false
		for _, line := range code[c.file] {
			found = found || strings.Contains(line, c.text)
		}
		if !found {
			t.Errorf("%s does not have %q", c.file, c.text)
		}
	}
}

// pgsHasWord is whether the text is in the line and not at the end of a longer name (catalog. is no log.).
func pgsHasWord(line, text string) bool {
	for from := 0; ; {
		i := strings.Index(line[from:], text)
		if i < 0 {
			return false
		}
		i += from
		if i == 0 || !(line[i-1] == '_' || line[i-1] >= '0' && line[i-1] <= '9' || line[i-1] >= 'a' && line[i-1] <= 'z' || line[i-1] >= 'A' && line[i-1] <= 'Z') {
			return true
		}
		from = i + 1
	}
}

// [REQ-679] Every function the statements of the adapter call is named with its schema, so that the account of the source
// cannot put a function of its own in the place of one by a search_path of its own.
func TestPgsStatementsNameTheSchemaOfTheirFunctions(t *testing.T) {
	bare := regexp.MustCompile(`(^|[^.\w"])(set_config|count|trim_scale|to_jsonb|pg_typeof|pg_type)\(`)
	for name, lines := range pgsCode(t) {
		for _, line := range lines {
			if bare.MatchString(line) {
				t.Fatalf("%s calls a function without its schema: %s", name, strings.TrimSpace(line))
			}
		}
	}
}
