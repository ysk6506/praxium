package httpsource

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The rig of the tests of the answers that are JSON lines and of the records that have an array in them (ADR-0109). Every name
// here begins with hxd so that it never meets a name of the older tests of this folder; the values are made up.

// hxdReply is what the fake source answers: the body, whether the protocol marks its end, and where it breaks.
type hxdReply struct {
	status int
	header map[string][]string
	body   string
	ended  bool
	// cutAt breaks the body with a read error after that many bytes; 0 does not.
	cutAt int
	// cancel is called when the body is read up to cutAt, before it breaks.
	cancel func()
	// together gives the break with the last bytes of the part that is read (a read may return both), so that the break is known
	// before the reader has looked at those bytes.
	together bool
}

func hxdOK(body string, ended bool) hxdReply {
	return hxdReply{status: 200, header: map[string][]string{"Content-Type": {"application/x-ndjson"}}, body: body, ended: ended}
}

func hxdJSON(body string) hxdReply {
	return hxdReply{status: 200, header: map[string][]string{"Content-Type": {"application/json"}}, body: body, ended: true}
}

// hxdBroken is a body that has given its first part and breaks the way a connection that is cut does.
type hxdBroken struct{ cancel func() }

func (b hxdBroken) Read([]byte) (int, error) {
	if b.cancel != nil {
		b.cancel()
	}
	return 0, &egress.Error{Kind: egress.KindConnect, Reason: "the connection broke"}
}

// hxdTogether is a body that gives all it has and the break in one read.
type hxdTogether struct {
	data string
	done bool
}

func (b *hxdTogether) Read(p []byte) (int, error) {
	if b.done {
		return 0, &egress.Error{Kind: egress.KindConnect, Reason: "the connection broke"}
	}
	b.done = true
	return copy(p, b.data), &egress.Error{Kind: egress.KindConnect, Reason: "the connection broke"}
}

// hxdBody counts the bytes the reader took of the body, and whether it was closed.
type hxdBody struct {
	r      io.Reader
	taken  *int
	closed *int
}

func (b hxdBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	*b.taken += n
	return n, err
}

func (b hxdBody) Close() error {
	*b.closed++
	return nil
}

// hxdServer is the fake source and the client made for it.
type hxdServer struct {
	handle func(n int, r hsRequest) hxdReply
	seen   []hsRequest
	taken  int
	closed int
}

type hxdClient struct{ s *hxdServer }

func (c hxdClient) Do(_ context.Context, req egress.Request) (*egress.Response, error) {
	s := c.s
	r := hsRequest{hsMethod: req.Method, hsURL: req.URL, hsHeader: req.Header}
	s.seen = append(s.seen, r)
	rep := s.handle(len(s.seen), r)
	var body io.Reader = strings.NewReader(rep.body)
	if rep.cutAt > 0 && rep.together {
		body = &hxdTogether{data: rep.body[:rep.cutAt]}
	} else if rep.cutAt > 0 {
		body = io.MultiReader(strings.NewReader(rep.body[:rep.cutAt]), hxdBroken{cancel: rep.cancel})
	}
	return &egress.Response{Status: rep.status, Header: rep.header, Body: hxdBody{r: body, taken: &s.taken, closed: &s.closed}, Ended: rep.ended}, nil
}

func (c hxdClient) CloseIdleConnections() {}

// hxdRig is a reader on the fake source.
type hxdRig struct {
	t      *testing.T
	srv    *hxdServer
	def    ingest.Source
	reader *Reader
	events []ingest.Event
}

// hxdDoc is the definition of a source with no authentication and the datasets given; conn is added to the connection.
func hxdDoc(conn, datasets string) string {
	return `{"version":1,"id":"src_export","adapter":"http","connection":{"base_url":"https://api.example.test/v1"` + conn + `,"auth":{"kind":"none"}},"datasets":[` + datasets + `]}`
}

const (
	// hxdLines is a dataset whose answer is JSON lines, a record each.
	hxdLines = `{"id":"ds_a","request":{"path":"/export"},"response_format":"json_lines","natural_key":["id"],"columns":[{"name":"id"},{"name":"name"}]}`
)

func hxdNew(t *testing.T, doc string, handle func(n int, r hsRequest) hxdReply) *hxdRig {
	t.Helper()
	g := &hxdRig{t: t, srv: &hxdServer{handle: handle}}
	g.def = hsParse(t, doc)
	clock := kernel.NewManualClock(hsEpoch)
	r, err := New(Config{
		Source: g.def, Connect: func(egress.ClientOptions) (Doer, error) { return hxdClient{s: g.srv}, nil },
		Clock: clock, Rng: kernel.NewSeededRng(1),
		Sleep: func(_ context.Context, d time.Duration) error {
			_, err := clock.Advance(d)
			return err
		},
		Observer: ingest.ObserverFunc(func(e ingest.Event) { g.events = append(g.events, e) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	g.reader = r
	return g
}

// hxdRead collects the pages of the first dataset.
func (g *hxdRig) hxdRead(ctx context.Context, opts ingest.PageOptions) ([]ingest.Page, error) {
	var pages []ingest.Page
	for p, err := range g.reader.ReadPages(ctx, "src_export", g.def.Datasets[0], opts) {
		if err != nil {
			return pages, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// hxdKeys lists the id of every row of the pages, with the host when there is one.
func hxdKeys(pages []ingest.Page) []string {
	var out []string
	for _, p := range pages {
		for _, r := range p.Rows {
			if h := r.Fields["host"]; h != "" {
				out = append(out, h+"/"+r.Fields["id"])
			} else {
				out = append(out, r.Fields["id"])
			}
		}
	}
	return out
}

func hxdEnd(t *testing.T, pages []ingest.Page) ingest.Completeness {
	t.Helper()
	if len(pages) == 0 || !pages[len(pages)-1].Last {
		t.Fatalf("the read did not end with a last page: %d pages", len(pages))
	}
	return pages[len(pages)-1].End
}

func hxdLine(i int) string { return fmt.Sprintf(`{"id":"r%03d","name":"n%d"}`, i, i) }

// hxdHostsDS is a dataset of hosts whose interfaces are its records, read by offset two hosts at a time; pageExtra goes into the
// pagination and extra among the fields of the dataset.
func hxdHostsDS(pageExtra, extra string) string {
	return `{"id":"ds_a","request":{"path":"/hosts"},"records_path":"result[*].interfaces","pagination":{"kind":"offset","offset_param":"offset","size_param":"limit","size":2` + pageExtra + `},` + extra +
		`"natural_key":["host","id"],"columns":[{"name":"host","paths":["^.hostid"]},{"name":"id"},{"name":"kind"}]}`
}

// hxdHost is a host of the fake source: its id and its interfaces, or the text its interfaces are given as when that is not the
// array of them (omit leaves the field out).
type hxdHost struct {
	id   string
	ifs  []string
	raw  string
	omit bool
}

func (h hxdHost) hxdJSON() string {
	var fields string
	switch {
	case h.omit:
	case h.raw != "":
		fields = `,"interfaces":` + h.raw
	default:
		parts := make([]string, len(h.ifs))
		for i, id := range h.ifs {
			parts[i] = `{"id":"` + id + `","kind":"k"}`
		}
		fields = `,"interfaces":[` + strings.Join(parts, ",") + `]`
	}
	return `{"hostid":"` + h.id + `"` + fields + `}`
}

// hxdHostsBody is the answer for the offset and the limit of a request: that part of the hosts, and how many there are.
func hxdHostsBody(hosts []hxdHost, r hsRequest) string {
	q := r.hsQuery()
	var offset, limit int
	fmt.Sscan(q.Get("offset"), &offset)
	fmt.Sscan(q.Get("limit"), &limit)
	end := min(offset+limit, len(hosts))
	offset = min(offset, end)
	parts := make([]string, 0, end-offset)
	for _, h := range hosts[offset:end] {
		parts = append(parts, h.hxdJSON())
	}
	return fmt.Sprintf(`{"total":%d,"result":[%s]}`, len(hosts), strings.Join(parts, ","))
}

// hxdFive is five hosts with ten interfaces in all, one of the hosts with none.
func hxdFive() []hxdHost {
	return []hxdHost{
		{id: "h1", ifs: []string{"i1", "i2", "i3"}}, {id: "h2", ifs: []string{}}, {id: "h3", ifs: []string{"i4", "i5"}},
		{id: "h4", ifs: []string{"i6"}}, {id: "h5", ifs: []string{"i7", "i8", "i9", "i10"}},
	}
}

func hxdHostsServer(hosts []hxdHost) func(int, hsRequest) hxdReply {
	return func(_ int, r hsRequest) hxdReply { return hxdJSON(hxdHostsBody(hosts, r)) }
}

// hxdOffsets lists the offsets the source was asked for.
func hxdOffsets(g *hxdRig) []string {
	var out []string
	for _, r := range g.srv.seen {
		out = append(out, r.hsQuery().Get("offset"))
	}
	return out
}
