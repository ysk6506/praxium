package pgsource

import (
	"context"
	"errors"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-655] [REQ-670]
func TestPgsAKeyGroupThatFillsABatchIsReadWhole(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE g (k int, n int)`)
	r.exec(`INSERT INTO g SELECT 1, i FROM generate_series(1,5) i`)
	r.exec(`INSERT INTO g SELECT 2, i FROM generate_series(1,3) i`)
	res := r.read(pgsDS("ds", pgsTable("g"), "k", "k", "n"), ingest.PageOptions{MaxRows: 5})
	if res.err != nil || len(res.rows()) != 8 {
		t.Fatalf("%v %d", res.err, len(res.rows()))
	}
	if !res.pages[len(res.pages)-1].End.Complete {
		t.Fatal("not complete")
	}
}

// [REQ-490] [REQ-670]
func TestPgsStopsBetweenPagesWhenTheContextEnds(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t SELECT generate_series(1,30)`)
	rd, ds := r.one(pgsOneDS(), "")
	ctx, cancel := context.WithCancel(pgsCtx)
	defer cancel()
	var got error
	pages := 0
	for _, err := range rd.ReadPages(ctx, pgsSourceID, ds, ingest.PageOptions{MaxRows: 5}) {
		if err != nil {
			got = err
			break
		}
		pages++
		cancel()
	}
	if !errors.Is(got, context.Canceled) || pages == 0 {
		t.Fatalf("%v after %d pages", got, pages)
	}
}

// [REQ-659] [REQ-660] [REQ-670]
func TestPgsReadsWithoutObserverOrClock(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t VALUES (1)`)
	src := r.parse(r.sourceDoc("", pgsOneDS()))
	rd, err := New(Config{Source: src, Secrets: r.svc.Opener(), Dialer: r.dialer})
	if err != nil {
		t.Fatal(err)
	}
	if res := pgsRead(rd, src.Datasets[0], ingest.PageOptions{}); res.err != nil || len(res.rows()) != 1 {
		t.Fatalf("%v", res.err)
	}
}

// [REQ-489] [REQ-670]
func TestPgsAnUnreadableDatasetIsRefusedByEveryEntrance(t *testing.T) {
	r := pgsNewRig(t)
	rd, _ := r.one(pgsOneDS(), "")
	bad := ingest.Dataset{ID: "d", NaturalKey: []string{"k"}, Columns: []ingest.Column{{Name: "k"}}}
	pgsSrcErr(t, pgsRead(rd, bad, ingest.PageOptions{}).err, ingest.SrcRequestRejected)
	res := rd.Check(pgsCtx, pgsSourceID, bad)
	if res.Kind != ingest.CheckConfig || res.Steps[len(res.Steps)-1].Name != stepQuery {
		t.Fatalf("%+v", res)
	}
	_, err := rd.Preview(pgsCtx, pgsSourceID, bad, ingest.PreviewOptions{})
	pgsSrcErr(t, err, ingest.SrcRequestRejected)
	res = rd.Check(pgsCtx, "src_other", bad)
	if res.Kind != ingest.CheckProduct {
		t.Fatalf("%+v", res)
	}
}

// [REQ-487] [REQ-670]
func TestPgsASecretWithoutAPasswordIsNotUsed(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	// A password secret may not have an empty value; if the store takes one the read still refuses to connect without it.
	if _, err := r.svc.Create(pgsCtx, secrets.CreateInput{ID: "sec_empty", Kind: secrets.KindPassword,
		Fields: secrets.NewFields(map[string]string{"username": r.user, "password": ""}),
		Scope:  []string{pgsSourceID}, AllowedHosts: []secrets.Address{{Host: r.host, Port: r.port}}}); err != nil {
		if kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Fatal(err)
		}
		return
	}
	r.secretID = "sec_empty"
	pgsSrcErr(t, r.read(pgsOneDS(), ingest.PageOptions{}).err, ingest.SrcSecretUnusable)
}

// [REQ-655] [REQ-670]
func TestPgsEmptyKeysAreSkippedWhenTheDatasetSaysSo(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE e (k text, v text)`)
	r.exec(`INSERT INTO e VALUES ('a','1'), ('','total'), ('b','2')`)
	doc := `{"id":"ds","table":"e","natural_key":["k"],"ignore_rows_without_key":true,"columns":[{"name":"k"},{"name":"v"}]}`
	rd, src := r.reader(r.sourceDoc("", doc))
	res := pgsRead(rd, src.Datasets[0], ingest.PageOptions{})
	if res.err != nil || len(res.rows()) != 2 {
		t.Fatalf("%v %d", res.err, len(res.rows()))
	}
	skipped := 0
	for _, pg := range res.pages {
		skipped += pg.Skipped[ingest.SkipNoKey]
	}
	if skipped != 1 {
		t.Fatalf("skipped %d", skipped)
	}
}

// [REQ-655] [REQ-670]
func TestPgsStopsWhenTheCallerLeavesTheLoopInTheNullKeyRows(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE n (k int, v int)`)
	r.exec(`INSERT INTO n SELECT NULL, i FROM generate_series(1,20) i`)
	rd, ds := r.one(pgsDS("ds", pgsTable("n"), "k", "k", "v"), "")
	pages := 0
	for _, err := range rd.ReadPages(pgsCtx, pgsSourceID, ds, ingest.PageOptions{MaxRows: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		pages++
		break
	}
	if pages != 1 {
		t.Fatalf("%d", pages)
	}
}

// [REQ-490] A key with more rows than a page holds makes the first page of a check end the dataset: it is told as [REQ-670]
// passing trouble, with the reason.
func TestPgsCheckTellsAnIncompleteFirstPage(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE big (k int, n int)`)
	r.exec(`INSERT INTO big SELECT 1, i FROM generate_series(1,10001) i`)
	rd, ds := r.one(pgsDS("ds", pgsTable("big"), "k", "k", "n"), "")
	res := rd.Check(pgsCtx, pgsSourceID, ds)
	if res.Kind != ingest.CheckTransient || res.Steps[len(res.Steps)-1].Name != stepFirstPage {
		t.Fatalf("%+v", res)
	}
}
