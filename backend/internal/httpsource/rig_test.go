package httpsource

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// Every value here is made up. A failing test never prints a secret: it says which comparison failed.
const (
	hsTokenValue  = "tok-not-a-real-secret-0001"
	hsPassValue   = "pw-not-a-real-secret-0002"
	hsUserValue   = "demo-user"
	hsMintedValue = "minted-not-a-real-secret-0003"
	hsSource      = "src_inventory"
)

var hsCtx = context.Background()

// hsRequest is a request as the fake source saw it.
type hsRequest struct {
	hsMethod string
	hsURL    string
	hsHeader map[string][]string
	hsBody   string
}

func (r hsRequest) hsQuery() url.Values {
	u, err := url.Parse(r.hsURL)
	if err != nil {
		return url.Values{}
	}
	return u.Query()
}

func (r hsRequest) hsPath() string {
	u, err := url.Parse(r.hsURL)
	if err != nil {
		return ""
	}
	return u.Path
}

func (r hsRequest) hsFirst(name string) string {
	for k, v := range r.hsHeader {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// hsReply is what the fake source answers.
type hsReply struct {
	hsStatus int
	hsHeader map[string][]string
	hsBody   string
	hsErr    error
	// hsReadErr breaks the body after its first half, as a connection does that is cut.
	hsReadErr error
}

func hsJSON(body string) hsReply {
	return hsReply{hsStatus: 200, hsHeader: map[string][]string{"Content-Type": {"application/json"}}, hsBody: body}
}

func hsStatus(code int) hsReply { return hsReply{hsStatus: code, hsBody: "{}"} }

type hsBody struct {
	hsR      io.Reader
	hsClosed *int
}

func (b hsBody) Read(p []byte) (int, error) { return b.hsR.Read(p) }
func (b hsBody) Close() error {
	*b.hsClosed++
	return nil
}

// hsServer is the fake source and the client made for it: it hands every request to a function and keeps what it saw.
type hsServer struct {
	hsHandle   func(n int, r hsRequest) hsReply
	hsSeen     []hsRequest
	hsMade     int
	hsClients  int
	hsShut     int
	hsBodies   int
	hsOpened   int
	hsCAs      []string
	hsTimeouts []time.Duration
}

type hsClient struct{ hsS *hsServer }

func (c hsClient) Do(ctx context.Context, req egress.Request) (*egress.Response, error) {
	s := c.hsS
	if err := ctx.Err(); err != nil {
		return nil, &egress.Error{Kind: egress.KindCanceled, Reason: "the request was cancelled"}
	}
	body := ""
	if req.Body != nil {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
	}
	r := hsRequest{hsMethod: req.Method, hsURL: req.URL, hsHeader: req.Header, hsBody: body}
	s.hsSeen = append(s.hsSeen, r)
	rep := s.hsHandle(len(s.hsSeen), r)
	if rep.hsErr != nil {
		return nil, rep.hsErr
	}
	s.hsOpened++
	var rbody io.Reader = strings.NewReader(rep.hsBody)
	if rep.hsReadErr != nil {
		rbody = io.MultiReader(strings.NewReader(rep.hsBody[:len(rep.hsBody)/2]), hsBroken{hsErr: rep.hsReadErr})
	}
	return &egress.Response{Status: rep.hsStatus, Header: rep.hsHeader, Body: hsBody{hsR: rbody, hsClosed: &s.hsBodies}}, nil
}

func (c hsClient) CloseIdleConnections() { c.hsS.hsShut++ }

func (s *hsServer) hsConnect(opts egress.ClientOptions) (Doer, error) {
	s.hsClients++
	s.hsCAs = append(s.hsCAs, opts.CAPEM)
	s.hsTimeouts = append(s.hsTimeouts, opts.Timeout)
	return hsClient{hsS: s}, nil
}

// hsRig is a reader on a fake source with a secret store, a clock that moves when the reader waits, and an observer.
type hsRig struct {
	hsT      *testing.T
	hsSrv    *hsServer
	hsClock  *kernel.ManualClock
	hsSleeps []time.Duration
	hsEvents []ingest.Event
	hsSvc    *secrets.Service
	hsUses   []secrets.Use
	hsDef    ingest.Source
	hsReader *Reader
}

// hsOpener remembers what the secrets were opened for.
type hsOpener struct{ hsRig *hsRig }

func (o hsOpener) Open(ctx context.Context, id string, use secrets.Use) (*secrets.Opened, error) {
	o.hsRig.hsUses = append(o.hsRig.hsUses, use)
	return o.hsRig.hsSvc.Open(ctx, id, use)
}

var hsEpoch = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

// hsParse reads a definition of a source the way a service does.
func hsParse(t *testing.T, doc string) ingest.Source {
	t.Helper()
	def, err := ingest.ParseAdapterSource(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("the definition is not valid: %v", err)
	}
	return def
}

func hsNew(t *testing.T, doc string, handle func(n int, r hsRequest) hsReply) *hsRig {
	t.Helper()
	g := &hsRig{hsT: t, hsSrv: &hsServer{hsHandle: handle}, hsClock: kernel.NewManualClock(hsEpoch)}
	g.hsDef = hsParse(t, doc)
	g.hsStore(t)
	cfg := Config{
		Source: g.hsDef, Secrets: hsOpener{hsRig: g}, Connect: g.hsSrv.hsConnect, Clock: g.hsClock, Rng: kernel.NewSeededRng(1),
		Sleep: func(ctx context.Context, d time.Duration) error {
			g.hsSleeps = append(g.hsSleeps, d)
			_, err := g.hsClock.Advance(d)
			return err
		},
		Observer: ingest.ObserverFunc(func(e ingest.Event) { g.hsEvents = append(g.hsEvents, e) }),
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	g.hsReader = r
	return g
}

// hsStore makes the secret service with the two secrets the definitions refer to, allowed for this source and address.
func (g *hsRig) hsStore(t *testing.T) {
	t.Helper()
	svc, err := secrets.New(secrets.Config{Store: secrets.NewMemoryStore(), Keys: secrets.StaticKey(secrets.GenerateMasterKey(kernel.NewSeededBytes(9))), Clock: g.hsClock, Bytes: kernel.NewSeededBytes(3)})
	if err != nil {
		t.Fatal(err)
	}
	g.hsSvc = svc
	scope := []string{hsSource}
	allowed := []secrets.Address{{Host: "api.example.test", Port: 443}}
	for _, in := range []secrets.CreateInput{
		{ID: "sec_token", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": hsTokenValue}), Scope: scope, AllowedHosts: allowed},
		{ID: "sec_pw", Kind: secrets.KindPassword, Fields: secrets.NewFields(map[string]string{"username": hsUserValue, "password": hsPassValue}), Scope: scope, AllowedHosts: allowed},
		{ID: "sec_other", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": hsTokenValue}), Scope: []string{"src_elsewhere"}, AllowedHosts: allowed},
	} {
		if _, err := svc.Create(hsCtx, in); err != nil {
			t.Fatal(err)
		}
	}
}

// hsRead collects the pages of the first dataset.
func (g *hsRig) hsRead(opts ingest.PageOptions) ([]ingest.Page, error) {
	return g.hsReadDS(g.hsDef.Datasets[0], opts)
}

func (g *hsRig) hsReadDS(ds ingest.Dataset, opts ingest.PageOptions) ([]ingest.Page, error) {
	var pages []ingest.Page
	for p, err := range g.hsReader.ReadPages(hsCtx, hsSource, ds, opts) {
		if err != nil {
			return pages, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// hsSerials lists the serial of every row of the pages, in order.
func hsSerials(pages []ingest.Page) []string {
	var out []string
	for _, p := range pages {
		for _, r := range p.Rows {
			out = append(out, r.Fields["serial"])
		}
	}
	return out
}

func hsWant(from, to int) []string {
	var out []string
	for i := from; i < to; i++ {
		out = append(out, hsSerial(i))
	}
	return out
}

func hsSerial(i int) string { return fmt.Sprintf("s-%04d", i) }

func hsRecord(i int) string {
	return fmt.Sprintf(`{"serial":%q,"location":{"rack":"r%d"}}`, hsSerial(i), i%4)
}

func hsRecords(from, to int) string {
	parts := make([]string, 0, to-from)
	for i := from; i < to; i++ {
		parts = append(parts, hsRecord(i))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// hsLast is the last page of a sequence.
func hsLast(t *testing.T, pages []ingest.Page) ingest.Page {
	t.Helper()
	if len(pages) == 0 || !pages[len(pages)-1].Last {
		t.Fatalf("the sequence did not end with a last page: %d pages", len(pages))
	}
	return pages[len(pages)-1]
}

// hsAPI is a fake source of total records that gives them size at a time by the pagination of its kind.
func hsAPI(kind string, total, size, firstPage int) func(int, hsRequest) hsReply {
	return func(_ int, r hsRequest) hsReply {
		q := r.hsQuery()
		start := 0
		switch kind {
		case ingest.PagePage:
			p, _ := strconv.Atoi(q.Get("page"))
			start = (p - firstPage) * size
		case ingest.PageOffset:
			start, _ = strconv.Atoi(q.Get("offset"))
		case ingest.PageCursor:
			start = hsCursorStart(q.Get("cursor"), r.hsBody)
		default:
			start, _ = strconv.Atoi(q.Get("after"))
		}
		end := min(start+size, total)
		start = min(start, end)
		next := ""
		if end < total {
			next = strconv.Itoa(end)
		}
		body := fmt.Sprintf(`{"items":%s,"pageInfo":{"totalCount":%d},"meta":{"next":%q},"next":%q}`, hsRecords(start, end), total, hsCursorOf(next), hsNextURL(next))
		rep := hsJSON(body)
		if next != "" && kind == ingest.PageLinkHeader {
			rep.hsHeader["Link"] = []string{`<https://api.example.test/other>; rel="prev", </api/v2/servers?after=` + next + `>; rel="next"`}
		}
		return rep
	}
}

func hsCursorOf(next string) string {
	if next == "" {
		return ""
	}
	return "c" + next
}

func hsNextURL(next string) string {
	if next == "" {
		return ""
	}
	return "/api/v2/servers?after=" + next
}

func hsCursorStart(fromQuery, body string) int {
	c := fromQuery
	if c == "" && body != "" {
		var m map[string]any
		if json.Unmarshal([]byte(body), &m) == nil {
			c, _ = m["cursor"].(string)
		}
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(c, "c"))
	return n
}

// hsDoc makes the definition of a source with one dataset.
func hsDoc(auth, request, pagination, extra string) string {
	if auth == "" {
		auth = `{"kind":"none"}`
	}
	if request == "" {
		request = `{"path":"/servers","query":{"status":"active"}}`
	}
	if pagination != "" {
		pagination = `,"pagination":` + pagination
	}
	return `{"version":1,"id":"` + hsSource + `","adapter":"http","connection":{"base_url":"https://api.example.test/api/v2","requests_per_second":1000` +
		extra + `,"auth":` + auth + `},"datasets":[{"id":"ds_servers","request":` + request + `,"records_path":"items"` + pagination +
		`,"natural_key":["serial"],"columns":[{"name":"serial"},{"name":"rack","paths":["location.rack"]}]}]}`
}

func hsPageKind(size, first int, total bool) string {
	t := ""
	if total {
		t = `,"total_path":"pageInfo.totalCount"`
	}
	return fmt.Sprintf(`{"kind":"page","page_param":"page","size_param":"pageSize","size":%d,"first_page":%d%s}`, size, first, t)
}

const (
	hsBearer = `{"kind":"bearer","credential":{"secret_id":"sec_token"}}`
	hsBasic  = `{"kind":"basic","credential":{"secret_id":"sec_pw"}}`
	hsHeader = `{"kind":"header","credential":{"secret_id":"sec_token"},"header":"X-Api-Key","prefix":"Key "}`
	hsTokReq = `{"kind":"token_request","credential":{"secret_id":"sec_pw"},"request":{"path":"/auth/token","format":"json","body":{"username":"{{credential.username}}","password":"{{credential.password}}"}},"token_path":"token"}`
)

func hsUnknownAuthority() error { return x509.UnknownAuthorityError{} }

func cancelled() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(hsCtx)
	cancel()
	return ctx, cancel
}

var errNobody = io.ErrUnexpectedEOF

type hsBroken struct{ hsErr error }

func (b hsBroken) Read([]byte) (int, error) { return 0, b.hsErr }
