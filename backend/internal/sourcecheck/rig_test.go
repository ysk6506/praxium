package sourcecheck

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgsource"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

var (
	scCtx   = context.Background()
	scEpoch = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
)

// scLoopback resolves every name to the loopback and counts the lookups: a dial of a name is a lookup, so the count says
// how many connections went through the policy of the dialer.
type scLoopback struct{ lookups atomic.Int64 }

func (r *scLoopback) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.lookups.Add(1)
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

// scSource is the connection of a source of these tests.
type scSource struct {
	host      string // a name the resolver of the test answers with the loopback
	port      int
	database  string
	user      string
	pass      string
	sslmode   string // "" is the default, verify-full
	caPEM     string
	statement int // seconds; 0 is the default
}

// scResult is what a read gave.
type scResult struct {
	pages []ingest.Page
	err   error
}

func (r scResult) rows() int {
	n := 0
	for _, p := range r.pages {
		n += len(p.Rows)
	}
	return n
}

// scReader makes the reader of a source with one dataset: the statement sql, with the key column id.
func scReader(t *testing.T, s scSource, stmt string, res *scLoopback) (*pgsource.Reader, ingest.Dataset) {
	t.Helper()
	svc, err := secrets.New(secrets.Config{
		Store: secrets.NewMemoryStore(), Keys: secrets.StaticKey(secrets.GenerateMasterKey(kernel.NewSeededBytes(7))),
		Clock: kernel.NewManualClock(scEpoch), Bytes: kernel.NewSeededBytes(8),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(scCtx, secrets.CreateInput{
		ID: "sec_sc", Kind: secrets.KindPassword,
		Fields: secrets.NewFields(map[string]string{"username": s.user, "password": s.pass}),
		Scope:  []string{"src_sc"}, AllowedHosts: []secrets.Address{{Host: s.host, Port: s.port}},
	}); err != nil {
		t.Fatalf("cannot create the secret: %v", err)
	}
	d, err := egress.New(egress.Config{Resolver: res})
	if err != nil {
		t.Fatal(err)
	}
	extra := ""
	if s.sslmode != "" {
		extra += fmt.Sprintf(`,"sslmode":%q`, s.sslmode)
	}
	if s.caPEM != "" {
		extra += fmt.Sprintf(`,"ca_pem":%q`, s.caPEM)
	}
	if s.statement != 0 {
		extra += fmt.Sprintf(`,"statement_timeout_seconds":%d`, s.statement)
	}
	doc := fmt.Sprintf(`{"version":1,"id":"src_sc","adapter":"postgres","connection":{"host":%q,"port":%d,"database":%q,"credential":{"secret_id":"sec_sc"}%s},"datasets":[{"id":"d","sql":%q,"natural_key":["id"],"columns":[{"name":"id"}]}]}`,
		s.host, s.port, s.database, extra, stmt)
	src, err := ingest.ParseAdapterSource(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("the test definition does not parse: %v", err)
	}
	rd, err := pgsource.New(pgsource.Config{Source: src, Secrets: svc.Opener(), Dialer: d})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rd, src.Datasets[0]
}

func scRun(ctx context.Context, rd *pgsource.Reader, ds ingest.Dataset, opts ingest.PageOptions) scResult {
	var out scResult
	for pg, err := range rd.ReadPages(ctx, "src_sc", ds, opts) {
		if err != nil {
			out.err = err
			return out
		}
		out.pages = append(out.pages, pg)
	}
	return out
}

func scSrcErr(t *testing.T, err error, kind string) {
	t.Helper()
	se, ok := ingest.SourceErrorOf(err)
	if !ok {
		t.Fatalf("want a SourceError of kind %s, got %v", kind, err)
	}
	if se.Kind != kind {
		t.Fatalf("want kind %s, got %s (%s)", kind, se.Kind, se.Message)
	}
}

// scFakeSource is a source whose server is the fake, on a plaintext connection (the fake has no TLS).
func scFakeSource(f *scFake, scenario string) scSource {
	return scSource{host: "scfake.test", port: f.port, database: scenario, user: "u", pass: "p", sslmode: ingest.SSLDisable}
}
