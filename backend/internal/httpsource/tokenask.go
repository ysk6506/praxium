package httpsource

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// Asking for the token (auth.kind token_request): where the request goes, when the token is asked for again, and what is
// read from the answer. The values of the secret are not taken out here (auth.go does that); the request is made in
// request.go, as every request is.

// maxTokenBytes is the longest a token may be. It goes into a header of every request of the read, so a token of a size that
// a header cannot have, or that a source gives to make every request heavy, is refused (ADR-0106).
const maxTokenBytes = 16 << 10

// maxTokenRefreshes is how many times one read asks for a token again because the source refused the one it had (401). The
// token request is a request to a server that other services use too, and a source that refuses a token it has just given
// would otherwise be asked for tokens as often as it asks for data.
const maxTokenRefreshes = 3

// The lifetime a token is held for, whatever the source says (a lifetime that is shorter is asked for again in a hot loop, one
// that is longer is a token the read holds for days), and how much earlier than its end it is asked for again.
const (
	shortestLife = 30 * time.Second
	longestLife  = 24 * time.Hour
	lifeMargin   = 30 * time.Second
)

// lifeDigits is what a lifetime is when the source gives it as a text: digits, and a fraction.
var lifeDigits = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// ensure gets a token for a token_request authentication that has none, or whose token holds no longer.
func (s *signer) ensure(ctx context.Context, rn *run) error {
	s.dropIfDue(rn.r.cfg.Clock.Now())
	if !s.needsToken() {
		return nil
	}
	p := rn.tokenPlan(s.basicRequest())
	rn.tokenRequests++
	rep, err := rn.exchange(ctx, p)
	if err != nil {
		return err
	}
	data, err := rn.smallBody(ctx, p, rep, tokenAnswerMax)
	if err != nil {
		return err
	}
	tok, life, err := s.tokenOf(rn, data)
	if err != nil {
		return err
	}
	s.token = holdText(tok)
	if life > 0 {
		s.expires = rn.r.cfg.Clock.Now().Add(life)
	} else if s.auth.ExpiresInPath != "" {
		rn.noteLifeUnread()
	}
	s.addMasked(tok, s.sendPrefix()+tok)
	for _, h := range s.auth.Headers {
		if src, ok := ingest.ParseHeaderFrom(h.From); ok && src.Token {
			s.addMasked(ingest.UnescapeBraces(h.Prefix) + tok)
		}
	}
	rn.prog.tokenAnswered = true
	if !rn.crossToken {
		rn.prog.authenticated = true
	}
	return nil
}

// lifeUnread is the kind of the event that says the answer of the token request gave no lifetime at expires_in_path, though the
// definition names one: the token is asked for again only when the source refuses it. The event has no value of the answer.
const lifeUnread ingest.EventKind = "token_life_unread"

// noteLifeUnread tells that the lifetime could not be read, once in a read.
func (rn *run) noteLifeUnread() {
	if rn.lifeNoted {
		return
	}
	rn.lifeNoted = true
	rn.emit(ingest.Event{Kind: lifeUnread, Reason: "the answer of the token request has no lifetime at expires_in_path that is a number of seconds; the token is asked for again only when the source refuses it"})
}

// tokenPlan is the request that asks for the token: a POST to the address of the token request, with the body of the
// definition (or none, for a Basic request that has none).
func (rn *run) tokenPlan(basic bool) plan {
	tr := rn.signer.auth.Request
	u, host, port := rn.tokenURL()
	p := plan{method: ingest.MethodPost, url: &u, host: host, port: port, token: true, basic: basic, form: tr.Format == "form", tmpl: tr.Body}
	if p.tmpl == nil && !(p.basic && tr.Format == "") {
		p.tmpl = map[string]any{}
	}
	return p
}

// tokenURL is the address the token is asked for at, and the host and the port it goes to: the address of the token host
// when that is another one (the host the secret was opened for), and otherwise the API's own origin, with the path the
// definition gives (auth.request.path under the base address, or the path of auth.request.url).
func (rn *run) tokenURL() (url.URL, string, int) {
	switch {
	case rn.crossToken:
		return *rn.tokenAt, rn.tokenHost, rn.tokenPort
	case rn.set.token.url:
		u := *rn.origin
		u.Path, u.RawPath = rn.set.token.path, rn.set.token.rawPath
		return u, rn.host, rn.port
	}
	u := *rn.origin
	u.Path += rn.signer.auth.Request.Path
	return u, rn.host, rn.port
}

// tokenOf reads the token and how long it holds from the answer. The token is at token_path, or the whole body is it as a
// JSON text (token_from body, which is not an object, a number or an empty text). A token with a line break or a control
// character is refused: a header could not hold it, and nothing is sent with it. A token that is longer than maxTokenBytes
// is refused too. The lifetime is zero when the definition names none or the answer has no number there.
func (s *signer) tokenOf(rn *run, data []byte) (string, time.Duration, error) {
	doc, err := decodeJSON(data)
	if err != nil {
		return "", 0, rn.fail(ingest.SrcResponseFormat, "the answer to the token request is not JSON", nil)
	}
	tok, ok := "", false
	if s.auth.TokenFrom == ingest.TokenFromBody {
		tok, ok = doc.(string)
		if !ok || tok == "" {
			return "", 0, rn.fail(ingest.SrcResponseFormat, "the answer to the token request is not a JSON text with a token in it (token_from is body)", nil)
		}
	} else {
		path, perr := ingest.ParsePath(s.auth.TokenPath)
		if perr == nil {
			tok, ok = path.Pick(doc)
		}
		if !ok {
			return "", 0, rn.fail(ingest.SrcResponseFormat, "the answer to the token request has no token at token_path", nil)
		}
	}
	switch {
	case len(tok) > maxTokenBytes:
		return "", 0, rn.fail(ingest.SrcResponseFormat, "the token the source gave is longer than "+strconv.Itoa(maxTokenBytes)+" bytes", nil)
	case !headerSafe(tok):
		return "", 0, rn.fail(ingest.SrcResponseFormat, "the token the source gave has a line break or a control character, which a header cannot hold", nil)
	}
	return tok, lifeOf(doc, s.auth.ExpiresInPath), nil
}

// lifeOf is how long a token is held, from the number of seconds the answer gives at the path (a number, or a text of digits,
// as some servers write it): the seconds are kept between shortestLife and longestLife, and the token is asked for again
// lifeMargin or a tenth of it before its end, whichever is less. It is zero when no path is named, nothing is there, or what is
// there is no number; the token is then asked for again only when the source refuses it.
func lifeOf(doc any, text string) time.Duration {
	if text == "" {
		return 0
	}
	path, err := ingest.ParsePath(text)
	if err != nil {
		return 0
	}
	at, ok := path.Lookup(doc)
	if !ok {
		return 0
	}
	var secs float64
	switch x := at.(type) {
	case json.Number:
		secs, err = x.Float64()
	case string:
		if !lifeDigits.MatchString(x) {
			return 0
		}
		secs, err = strconv.ParseFloat(x, 64)
	default:
		return 0
	}
	// A number out of the range of a float is as long as the longest life (or as short as the shortest) and not none: the
	// parser gives the infinity it is nearest to together with an error of range.
	if (err != nil && !errors.Is(err, strconv.ErrRange)) || math.IsNaN(secs) {
		return 0
	}
	secs = math.Min(math.Max(secs, shortestLife.Seconds()), longestLife.Seconds())
	life := time.Duration(secs * float64(time.Second))
	return life - min(life/10, lifeMargin)
}

// noSecretForTheAPI refuses what the definition check does not let in, once more where a request is made: when the token is
// asked for at another host than the API, a value of the secret goes to the token host only, and a request of the API has the
// token and nothing of the secret. Nothing is sent.
func (s *signer) noSecretForTheAPI() error {
	if !s.cross {
		return nil
	}
	return noSecretInHeaders(s.auth)
}

func noSecretInHeaders(a ingest.Auth) error {
	for _, e := range a.Headers {
		if src, ok := ingest.ParseHeaderFrom(e.From); ok && !src.Token {
			return unusable("a header of the authentication has a value of the secret, and the token is asked for at another host than the API; the secret goes to that host only, and nothing was sent")
		}
	}
	return nil
}

// noSecretInTheRequestsOfTheAPI is the same refusal as noSecretForTheAPI and valueIn make, made before the first request of the
// read: the headers of the authentication and the body of the dataset are looked at, so that the token request is not sent for a
// read that would be refused at its first request of the API. It refuses nothing when the token is not asked for at another host.
func noSecretInTheRequestsOfTheAPI(set settings, ds ingest.Dataset) error {
	if !set.token.cross {
		return nil
	}
	if err := noSecretInHeaders(set.auth); err != nil {
		return err
	}
	if ds.Request == nil {
		return nil
	}
	for _, p := range ingest.PlaceholdersOf(ds.Request.Body) {
		if !p.Token {
			return unusable("a body of a request of the API has a placeholder of the secret, and the token is asked for at another host; the secret goes to that host only, and nothing was sent")
		}
	}
	return nil
}
