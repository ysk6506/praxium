package httpsource

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

func hsSecretTexts() []string {
	return []string{hsTokenValue, hsPassValue, hsMintedValue, base64.StdEncoding.EncodeToString([]byte(hsUserValue + ":" + hsPassValue))}
}

func hsClean(t *testing.T, where, text string) {
	t.Helper()
	for _, s := range hsSecretTexts() {
		if strings.Contains(text, s) {
			t.Errorf("%s holds a secret value", where)
		}
	}
}

// hsEcho is a source that does its best to give a secret back: in its error bodies and headers, in its records, and in the
// token it hands out.
func hsEcho(n int, r hsRequest) hsReply {
	auth := r.hsFirst("Authorization") + r.hsFirst("X-Api-Key") + r.hsBody
	if strings.HasSuffix(r.hsPath(), "/auth/token") {
		return hsJSON(`{"token":"` + hsMintedValue + `"}`)
	}
	switch n % 4 {
	case 0:
		return hsReply{hsStatus: 500, hsHeader: map[string][]string{"X-Echo": {auth}}, hsBody: `{"error":"` + auth + `"}`}
	case 1:
		return hsJSON(`{"items":[{"serial":"s-1","api_token":"` + hsMintedValue + `","rack":"` + hsPassValue + `"},{"serial":"s-2","rack":"` + hsTokenValue + `"}]}`)
	case 2:
		return hsReply{hsStatus: 403, hsBody: auth}
	}
	return hsJSON(`not json ` + auth)
}

// [REQ-665]
func TestNoSecretValueIsInAnyErrorEventResultOrPrintOfTheReader(t *testing.T) {
	for _, auth := range []string{hsBearer, hsBasic, hsHeader, hsTokReq} {
		for i := 0; i < 6; i++ {
			g := hsNew(t, hsDoc(auth, "", hsPageKind(2, 1, false), ""), func(n int, r hsRequest) hsReply { return hsEcho(n+i, r) })
			pages, err := g.hsRead(ingest.PageOptions{})
			if err != nil {
				for _, verb := range []string{"%v", "%+v", "%#v", "%q", "%s"} {
					hsClean(t, "an error printed with "+verb, fmt.Sprintf(verb, err))
				}
			}
			for _, p := range pages {
				hsClean(t, "a token", string(p.Token))
			}
			check := g.hsReader.Check(hsCtx, hsSource, g.hsDef.Datasets[0])
			data, _ := json.Marshal(check)
			hsClean(t, "a check", string(data))
			prev, _ := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{})
			data, _ = json.Marshal(prev)
			hsClean(t, "a preview", string(data))
			data, _ = json.Marshal(g.hsEvents)
			hsClean(t, "the events", string(data))
			for _, verb := range []string{"%v", "%+v", "%#v"} {
				hsClean(t, "the reader printed with "+verb, fmt.Sprintf(verb, g.hsReader))
				hsClean(t, "the settings printed with "+verb, fmt.Sprintf(verb, g.hsReader.set))
			}
		}
	}
}

// [REQ-665]
func TestTheStateOfARunAndTheTokenItHoldsPrintAsNothing(t *testing.T) {
	g := hsNew(t, hsDoc(hsTokReq, "", "", ""), func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			return hsJSON(`{"token":"` + hsMintedValue + `"}`)
		}
		return hsOne(n, r)
	})
	rn, err := g.hsReader.begin(hsCtx, hsSource, g.hsDef.Datasets[0], "run")
	if err != nil {
		t.Fatal(err)
	}
	defer rn.close()
	if _, err := rn.data(hsCtx, g.hsDef.Datasets[0], rn.pager.first()); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		hsClean(t, "the run printed with "+verb, fmt.Sprintf(verb, rn.signer))
		hsClean(t, "the token printed with "+verb, fmt.Sprintf(verb, rn.signer.token))
	}
	data, _ := json.Marshal(rn.signer.token)
	hsClean(t, "the token as JSON", string(data))
	text, _ := rn.signer.token.MarshalText()
	hsClean(t, "the token as text", string(text))
	if rn.signer.token.text() != hsMintedValue {
		t.Fatal("the token is not held")
	}
	rn.signer.drop()
	if rn.signer.token.text() != "" || (secretText{}).text() != "" {
		t.Fatal("a dropped token is still held")
	}
}

// [REQ-665]
func TestTheWordsOfAnErrorAreMaskedOfTheValuesOfTheOpenSecretsAsASecondWall(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", "", ""), hsOne)
	rn, err := g.hsReader.begin(hsCtx, hsSource, g.hsDef.Datasets[0], "run")
	if err != nil {
		t.Fatal(err)
	}
	defer rn.close()
	se := rn.fail(ingest.SrcInternal, "a failure that says "+hsTokenValue, nil)
	if strings.Contains(se.Message, hsTokenValue) || !strings.Contains(se.Message, ingest.MaskedValue) {
		t.Fatalf("the message: %s", se.Message)
	}
}

// [REQ-665]
func TestEventsAreToldWhatHappenedAndHaveNoFieldForAValue(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", hsPageKind(2, 1, true), ""), func(n int, r hsRequest) hsReply {
		if n == 1 {
			return hsStatus(503)
		}
		return hsAPI(ingest.PagePage, 3, 2, 1)(n, r)
	})
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	kinds := map[ingest.EventKind]int{}
	for _, e := range g.hsEvents {
		kinds[e.Kind]++
		if e.Source != hsSource || e.Dataset != "ds_servers" || e.Host != "api.example.test" || e.Port != 443 {
			t.Fatalf("where: %+v", e)
		}
		if strings.Contains(e.Path, "?") || strings.Contains(e.Path, "status") {
			t.Fatalf("a path with a query: %s", e.Path)
		}
	}
	if kinds[ingest.EventRequest] != 3 || kinds[ingest.EventResponse] != 3 || kinds[ingest.EventRetry] != 1 || kinds[ingest.EventPage] != 1 {
		t.Fatalf("events: %v", kinds)
	}
	g = hsNew(t, hsDoc(hsBearer, "", "", ""), func(_ int, _ hsRequest) hsReply { return hsStatus(404) })
	_, _ = g.hsRead(ingest.PageOptions{})
	last := g.hsEvents[len(g.hsEvents)-1]
	if last.Kind != ingest.EventFailed || last.Class != ingest.CheckConfig || last.ErrorKind != ingest.SrcRequestRejected {
		t.Fatalf("the last event: %+v", last)
	}
	g = hsNew(t, hsDoc("", "", `{"kind":"page","page_param":"page","size_param":"pageSize","size":2,"first_page":1,"max_pages":1}`, ""), hsAPI(ingest.PagePage, 9, 2, 1))
	_, _ = g.hsRead(ingest.PageOptions{})
	if e := g.hsEvents[len(g.hsEvents)-2]; e.Kind != ingest.EventIncomplete || !strings.Contains(e.Reason, "max_pages") {
		t.Fatalf("incomplete: %+v", e)
	}
}

// [REQ-665]
func TestAReaderWithNoObserverTellsNobody(t *testing.T) {
	g := hsNew(t, hsDoc("", "", "", ""), hsOne)
	cfg := g.hsReader.cfg
	cfg.Observer = nil
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range r.ReadPages(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PageOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(g.hsEvents) != 0 {
		t.Fatal("an observer that was not given was told")
	}
}

//go:embed *.go
var hsSources embed.FS

func hsProduction(t *testing.T) map[string]string {
	t.Helper()
	entries, err := hsSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := hsSources.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var code []string
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "//") {
				code = append(code, line)
			}
		}
		out[e.Name()] = strings.Join(code, "\n")
	}
	if len(out) < 10 {
		t.Fatalf("only %d source files were read", len(out))
	}
	return out
}

// [REQ-665]
func TestTheValuesOfASecretAreTakenOutInOneFileAndNothingIsLoggedOrPrinted(t *testing.T) {
	forbidden := map[string]string{
		"log.":            "a log",
		"slog.":           "a log",
		"fmt.Print":       "a print",
		"fmt.Fprint":      "a print",
		"os.Std":          "a standard stream",
		"time.Now":        "the wall clock",
		"math/rand":       "randomness of the system",
		"InsecureSkip":    "a way not to verify a certificate",
		"http.Default":    "a client that is not egress's",
		"Accept-Encoding": "a request for compression",
	}
	revealed := 0
	for name, code := range hsProduction(t) {
		if strings.Contains(code, ".Reveal(") {
			revealed++
			if name != "auth.go" {
				t.Errorf("%s takes a value out of a secret", name)
			}
		}
		for word, why := range forbidden {
			if strings.Contains(code, word) {
				t.Errorf("%s has %q: %s", name, word, why)
			}
		}
	}
	if revealed != 1 {
		t.Fatalf("the values are taken out in %d files", revealed)
	}
}

// [REQ-662]
func TestTheRequestIsMadeInOnePlaceWithTheTwoMethodsAndNoOtherNetworkCodeIsImported(t *testing.T) {
	made, imports := 0, map[string]bool{}
	for name, code := range hsProduction(t) {
		if strings.Contains(code, "egress.Request{") {
			made++
			if name != "request.go" {
				t.Errorf("%s makes a request", name)
			}
		}
		for _, verb := range []string{`"GET"`, `"POST"`, `"PUT"`, `"DELETE"`, `"PATCH"`, `"HEAD"`} {
			if strings.Contains(code, verb) {
				t.Errorf("%s writes a method as %s", name, verb)
			}
		}
		for _, line := range strings.Split(code, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), `"`) && strings.Contains(line, "/") || strings.TrimSpace(line) == `"net"` {
				imports[strings.Trim(strings.TrimSpace(line), `"`)] = true
			}
		}
	}
	if made != 1 {
		t.Fatalf("requests are made in %d files", made)
	}
	for _, banned := range []string{"net", "net/http", "crypto/tls", "net/http/httputil", "net/textproto"} {
		if imports[banned] {
			t.Errorf("the package imports %s", banned)
		}
	}
	if !imports["github.com/ysk6506/praxium/backend/internal/egress"] {
		t.Fatal("the package does not use adapter.egress")
	}
	_ = egress.KindPolicy
}
