package httpsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The rig of the tests of the datasets that ask with the values of another dataset (ADR-0111). Every name here begins with hxe so that it
// never meets a name of the older tests of this folder; the values are made up.

// hxeReply is what the fake source answers.
type hxeReply struct {
	status int
	body   string
}

func hxeOK(body string) hxeReply { return hxeReply{status: 200, body: body} }

// hxeServer is the fake source: it advances the clock for every request, tells how many bodies were open when each request came,
// and makes a body that is read longer than the timeout fail as the client's time limit does (it covers the body, X5).
type hxeServer struct {
	handle  func(n int, r hsRequest) hxeReply
	clock   *kernel.ManualClock
	each    time.Duration
	timeout time.Duration
	seen    []hsRequest
	open    int
	openAt  []int
}

type hxeClient struct{ s *hxeServer }

// hxeBody fails to be read when more time than the limit has passed since the request began.
type hxeBody struct {
	r     io.Reader
	s     *hxeServer
	begun time.Time
}

func (b *hxeBody) Read(p []byte) (int, error) {
	if b.s.timeout > 0 && b.s.clock.Now().Sub(b.begun) > b.s.timeout {
		return 0, &egress.Error{Kind: egress.KindTimeout, Reason: "the time limit of the request passed while the body was read"}
	}
	return b.r.Read(p)
}

func (b *hxeBody) Close() error {
	b.s.open--
	return nil
}

func (c hxeClient) Do(_ context.Context, req egress.Request) (*egress.Response, error) {
	s := c.s
	body := ""
	if req.Body != nil {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
	}
	r := hsRequest{hsMethod: req.Method, hsURL: req.URL, hsHeader: req.Header, hsBody: body}
	s.seen = append(s.seen, r)
	s.openAt = append(s.openAt, s.open)
	if s.each > 0 {
		_, _ = s.clock.Advance(s.each)
	}
	rep := s.handle(len(s.seen), r)
	s.open++
	return &egress.Response{Status: rep.status, Header: map[string][]string{"Content-Type": {"application/json"}},
		Body: &hxeBody{r: strings.NewReader(rep.body), s: s, begun: s.clock.Now()}, Ended: true}, nil
}

func (c hxeClient) CloseIdleConnections() {}

// hxeRig is a reader on the fake source.
type hxeRig struct {
	t      *testing.T
	srv    *hxeServer
	def    ingest.Source
	reader *Reader
	clock  *kernel.ManualClock
	events []ingest.Event
}

// hxeDoc is a source with no authentication and the datasets given; conn is added to the connection.
func hxeDoc(conn string, datasets ...string) string {
	return `{"version":1,"id":"src_hosts","adapter":"http","connection":{"base_url":"https://api.example.test/v1"` + conn + `,"auth":{"kind":"none"}},"datasets":[` + strings.Join(datasets, ",") + `]}`
}

const (
	// hxeIDs is the list of ids the details are asked for: a helper, read only for its values.
	hxeIDs = `{"id":"ds_ids","collect":false,"request":{"path":"/vm"},"records_path":"items"}`
)

// hxeDetail is the dataset of the detail of each id, with the pieces of its parent block and of the dataset given.
func hxeDetail(parent, extra string) string {
	return `{"id":"ds_vm","parent":{"dataset":"ds_ids","values":{"id":"id"}` + parent + `},` + extra +
		`"request":{"path":"/vm/{parent.id}"},"records_path":"items","natural_key":["id"],"columns":[{"name":"id"},{"name":"name"}]}`
}

// hxeBatch is the dataset that asks for many ids in one POST.
func hxeBatch(parent string) string {
	return `{"id":"ds_vm","post_reads":true,"parent":{"dataset":"ds_ids","values":{"id":"id"},"batch":{"size":2,"match":"id"` + parent + `}},` +
		`"request":{"method":"POST","path":"/vm/query","body":{"ids":"{parent.id}"}},"records_path":"items","natural_key":["id"],"columns":[{"name":"id"},{"name":"name"}]}`
}

func hxeNew(t *testing.T, doc string, handle func(n int, r hsRequest) hxeReply, mut ...func(*Config)) *hxeRig {
	t.Helper()
	clock := kernel.NewManualClock(hsEpoch)
	g := &hxeRig{t: t, srv: &hxeServer{handle: handle, clock: clock}, clock: clock}
	g.def = hsParse(t, doc)
	cfg := Config{
		Source: g.def, Clock: clock, Rng: kernel.NewSeededRng(1),
		Connect: func(o egress.ClientOptions) (Doer, error) {
			g.srv.timeout = o.Timeout
			return hxeClient{s: g.srv}, nil
		},
		Sleep: func(_ context.Context, d time.Duration) error {
			_, err := clock.Advance(d)
			return err
		},
		Observer: ingest.ObserverFunc(func(e ingest.Event) { g.events = append(g.events, e) }),
	}
	for _, m := range mut {
		if m != nil {
			m(&cfg)
		}
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	g.reader = r
	return g
}

// dataset is the dataset with the id.
func (g *hxeRig) dataset(id string) ingest.Dataset {
	for _, d := range g.def.Datasets {
		if d.ID == id {
			return d
		}
	}
	g.t.Fatalf("no dataset %s", id)
	return ingest.Dataset{}
}

// read reads the dataset to its end.
func (g *hxeRig) read(id string) ([]ingest.Page, error) {
	var pages []ingest.Page
	for p, err := range g.reader.ReadPages(context.Background(), "src_hosts", g.dataset(id), ingest.PageOptions{}) {
		if err != nil {
			return pages, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// paths lists the escaped path and query of every request the source saw.
func (g *hxeRig) paths() []string {
	var out []string
	for _, r := range g.srv.seen {
		u, err := url.Parse(r.hsURL)
		if err != nil {
			out = append(out, "?"+r.hsURL)
			continue
		}
		p := u.EscapedPath()
		if u.RawQuery != "" {
			p += "?" + u.RawQuery
		}
		out = append(out, p)
	}
	return out
}

func hxeIDsOf(pages []ingest.Page) []string {
	var out []string
	for _, p := range pages {
		for _, r := range p.Rows {
			out = append(out, r.Fields["id"])
		}
	}
	return out
}

func hxeEnd(t *testing.T, pages []ingest.Page) ingest.Completeness {
	t.Helper()
	if len(pages) == 0 || !pages[len(pages)-1].Last {
		t.Fatalf("the read did not end with a last page: %d pages", len(pages))
	}
	return pages[len(pages)-1].End
}

// hxeList is the answer of the list of ids.
func hxeList(ids ...string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		text, _ := json.Marshal(id)
		parts[i] = `{"id":` + string(text) + `}`
	}
	return `{"items":[` + strings.Join(parts, ",") + `]}`
}

// hxeDetailBody is the answer of the detail of one id: an object under items.
func hxeDetailBody(id string) string {
	return fmt.Sprintf(`{"items":[{"id":%q,"name":"n-%s"}]}`, id, id)
}

// hxeVM is the fake source of a list of ids and a detail for each of them.
func hxeVM(ids ...string) func(n int, r hsRequest) hxeReply {
	return func(_ int, r hsRequest) hxeReply {
		p := r.hsPath()
		if p == "/v1/vm" {
			return hxeOK(hxeList(ids...))
		}
		return hxeOK(hxeDetailBody(strings.TrimPrefix(p, "/v1/vm/")))
	}
}

// hxeSeconds is n seconds.
func hxeSeconds(n int) time.Duration { return time.Duration(n) * time.Second }
