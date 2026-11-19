package pgsource

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-670] A key of a domain nine steps deep is not followed to its base (the catalog walk stops at eight): the dataset is refused.
func TestPgfAKeyWithADomainNineStepsDeepIsRefused(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE DOMAIN pgf_d1 AS int`)
	for i := 2; i <= 9; i++ {
		r.exec(fmt.Sprintf(`CREATE DOMAIN pgf_d%d AS pgf_d%d`, i, i-1))
	}
	r.exec(`CREATE TABLE t (k pgf_d9, v int)`)
	r.exec(`INSERT INTO t VALUES (1, 1)`)
	rd, ds := r.one(pgsDS("ds", pgsTable("t"), "k", "k", "v"), "")
	if res := pgsRead(rd, ds, ingest.PageOptions{}); res.err == nil {
		t.Fatalf("a key of a domain nine steps deep was read")
	}
}

// [REQ-670] A row bigger than the byte budget of a transaction is refused as a response the adapter does not take.
func TestPgfARowLargerThanTheBudgetIsRefused(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key, v text)`)
	r.exec(`INSERT INTO t VALUES (1, repeat('x', 3145728))`)
	rd, ds := r.one(pgsDS("ds", pgsTable("t"), "k", "k", "v"), "")
	res := pgsRead(rd, ds, ingest.PageOptions{MaxBytes: 1024})
	pgsSrcErr(t, res.err, ingest.SrcResponseFormat)
}

// [REQ-670] A statement that fails on one of its rows is a failure of the check and of the read, not a short dataset.
func TestPgfAStatementThatFailsOnARowIsReportedByTheCheckAndTheRead(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t SELECT generate_series(1, 3)`)
	rd, ds := r.one(pgsDS("ds", pgsSQL("SELECT k, 1/(k-1) AS v FROM t"), "k", "k", "v"), "")
	res := rd.Check(pgsCtx, pgsSourceID, ds)
	if res.Kind == ingest.CheckOK || len(res.Steps) == 0 || res.Steps[len(res.Steps)-1].Name != stepFirstPage {
		t.Fatalf("the check did not stop at the first page: %+v", res)
	}
	if got := pgsRead(rd, ds, ingest.PageOptions{}); got.err == nil {
		t.Fatalf("the read of a failing statement gave no error")
	}
}

// [REQ-670] A preview of two pages of three rows is trimmed to the five rows it asks for.
func TestPgfAPreviewEndsAtItsRowLimitWithinAPage(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key, v int)`)
	r.exec(`INSERT INTO t SELECT i, i FROM generate_series(1, 10) i`)
	rd, ds := r.one(pgsOneDS(), "")
	pv, err := rd.Preview(pgsCtx, pgsSourceID, ds, ingest.PreviewOptions{MaxRows: 5, MaxPages: 2})
	if err != nil {
		t.Fatalf("%v", err)
	}
	n := 0
	for _, pg := range pv.Pages {
		n += len(pg.Rows)
	}
	if n != 5 {
		t.Fatalf("the preview has %d rows, want 5", n)
	}
}

// [REQ-670] A resume token whose key the column cannot read is a refused statement, not a page.
func TestPgfAResumeTokenWithAKeyTheColumnCannotReadIsRefused(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key, v int)`)
	r.exec(`INSERT INTO t SELECT i, i FROM generate_series(1, 10) i`)
	rd, ds := r.one(pgsOneDS(), "")
	pl, err := newPlan(ds)
	if err != nil {
		t.Fatalf("%v", err)
	}
	res := pgsRead(rd, ds, ingest.PageOptions{Resume: encodeToken(pl.fp, []string{"not a number"})})
	pgsSrcErr(t, res.err, ingest.SrcRequestRejected)
}

// [REQ-670] A key expression that fails on one row is a failure of the read at the count of the rows without a key.
func TestPgfAKeyExpressionThatFailsOnARowIsAFailureOfTheRead(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t SELECT generate_series(1, 3)`)
	rd, ds := r.one(pgsDS("ds", pgsSQL("SELECT 1/(k-1) AS k, k AS v FROM t"), "k", "k", "v"), "")
	if res := pgsRead(rd, ds, ingest.PageOptions{}); res.err == nil {
		t.Fatalf("a key that divides by zero on one row was read")
	}
}

// [REQ-670] A panic of the driver ends the read with the kind response_format, and the connection is not used again.
func TestPgfAPanicOfTheDriverEndsTheReadAndTheConnection(t *testing.T) {
	r := pgsNewRig(t)
	s := &session{tr: r.dialer.Tracker(egress.TrackerConfig{})}
	pgsSrcErr(t, s.guard(func() error { panic("bytes the source sent") }), ingest.SrcResponseFormat)
	if !s.dead {
		t.Fatalf("the session is not marked dead after a panic")
	}
	pgsSrcErr(t, s.guard(func() error { return nil }), ingest.SrcResponseFormat)
}

// [REQ-670] A connection that cannot start within the time of the connection is a timeout.
func TestPgfAConnectionThatCannotStartInItsTimeIsATimeout(t *testing.T) {
	r := pgsNewRig(t)
	s := &session{db: pgsOpen(t, pgsDSN(r, r.dbName)), st: settings{connect: -(connectSlack + time.Second)}}
	pgsSrcErr(t, s.connect(pgsCtx), ingest.SrcTimeout)
}

// [REQ-670] A read transaction that runs out of its time is a timeout, and the transaction does not start.
func TestPgfAReadTransactionThatRunsOutOfItsTimeIsATimeout(t *testing.T) {
	r := pgsNewRig(t)
	s := &session{db: pgsOpen(t, pgsDSN(r, r.dbName)), st: settings{statement: -time.Hour}, tr: r.dialer.Tracker(egress.TrackerConfig{})}
	conn, err := s.db.Conn(pgsCtx)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer func() { _ = conn.Close() }()
	s.conn = conn
	pgsSrcErr(t, s.readTx(pgsCtx, func(context.Context, *sql.Tx) error { return nil }), ingest.SrcTimeout)
}

// [REQ-670] A key that the statement does not give is a dataset that is refused.
func TestPgfAKeyThatTheStatementDoesNotGiveIsRefused(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	rd, ds := r.one(pgsDS("ds", pgsSQL("SELECT 1 AS v FROM t"), "k", "k", "v"), "")
	if res := pgsRead(rd, ds, ingest.PageOptions{}); res.err == nil {
		t.Fatalf("a dataset whose key the statement does not give was read")
	}
}

// [REQ-670] A read whose connection is already closed does not start a transaction, and says so.
func TestPgfAReadOnAClosedConnectionIsAnError(t *testing.T) {
	r := pgsNewRig(t)
	s := &session{db: pgsOpen(t, pgsDSN(r, r.dbName)), st: settings{statement: time.Second}, tr: r.dialer.Tracker(egress.TrackerConfig{})}
	conn, err := s.db.Conn(pgsCtx)
	if err != nil {
		t.Fatalf("%v", err)
	}
	_ = conn.Close()
	s.conn = conn
	if err := s.readTx(pgsCtx, func(context.Context, *sql.Tx) error { return nil }); err == nil {
		t.Fatalf("a read on a closed connection was not an error")
	}
}

// [REQ-670] A source that refuses the limits of the read (a negative statement time) ends the read with an error.
func TestPgfAReadWhoseLimitsTheSourceRefusesIsAnError(t *testing.T) {
	r := pgsNewRig(t)
	s := &session{db: pgsOpen(t, pgsDSN(r, r.dbName)), st: settings{statement: -time.Millisecond}, tr: r.dialer.Tracker(egress.TrackerConfig{})}
	conn, err := s.db.Conn(pgsCtx)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer func() { _ = conn.Close() }()
	s.conn = conn
	if err := s.readTx(pgsCtx, func(context.Context, *sql.Tx) error { return nil }); err == nil {
		t.Fatalf("a read with a limit the source refuses was not an error")
	}
}

// [REQ-670] When a read is cancelled and does not end, its connections are closed after the grace of the driver.
func TestPgfConnectionsAreClosedAfterTheGraceWhenTheReadIsCancelled(t *testing.T) {
	r := pgsNewRig(t)
	tr := r.dialer.Tracker(egress.TrackerConfig{Plaintext: true})
	c, err := tr.Dial("tcp", r.host+":"+strconv.Itoa(r.port))
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithCancel(pgsCtx)
	s := &session{tr: tr}
	stop := s.closeLater(ctx)
	defer stop()
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.Read(make([]byte, 1))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("a connection that the read left open gave data")
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("the connection was not closed after the grace")
	}
}

// [REQ-670] A key with more rows than a page holds ends the read as incomplete, with the reason of that key and not of a changed source.
func TestPgfAKeyWithMoreRowsThanAPageEndsTheReadWithItsReason(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int, v int)`)
	r.exec(`INSERT INTO t SELECT 1, i FROM generate_series(1, 12) i`)
	rd, ds := r.one(pgsDS("ds", pgsTable("t"), "k", "k", "v"), "")
	res := pgsRead(rd, ds, ingest.PageOptions{MaxRows: 5})
	if res.err != nil {
		t.Fatalf("%v", res.err)
	}
	if len(res.pages) == 0 {
		t.Fatalf("the read gave no page")
	}
	last := res.pages[len(res.pages)-1]
	if last.End.Complete || !strings.Contains(last.End.Reason, "more than 5 rows have one natural key") || strings.Contains(last.End.Reason, "changed") {
		t.Fatalf("the read of a key bigger than a page ended with %+v", last.End)
	}
}

// [REQ-670] A key whose rows go away between the page statement and the group statement of one transaction ends the
// read as changed, with no record. A volatile view that shows three rows the first three times it is asked does that.
func TestPgfARowsThatGoAwayInsideOneTransactionEndTheReadAsChanged(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE pgf_t (k text, v int)`)
	r.exec(`INSERT INTO pgf_t VALUES ('a', 1), ('a', 2), ('a', 3)`)
	r.exec(`CREATE FUNCTION pgf_tick() RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$ DECLARE n int := coalesce(nullif(current_setting('pgf.calls', true), ''), '0')::int + 1; BEGIN PERFORM pg_catalog.set_config('pgf.calls', n::text, true); RETURN n <= 3; END $$`)
	r.exec(`CREATE VIEW pgf_v AS SELECT k, v FROM pgf_t WHERE pgf_tick()`)
	rd, ds := r.one(pgsDS("ds", pgsTable("pgf_v"), "k", "k", "v"), "")
	res := pgsRead(rd, ds, ingest.PageOptions{MaxRows: 3})
	if res.err != nil {
		t.Fatalf("%v", res.err)
	}
	if n := len(res.rows()); n != 0 {
		t.Fatalf("the read gave %d records, want none", n)
	}
	last := res.pages[len(res.pages)-1]
	if last.End.Complete || !strings.HasPrefix(last.End.Reason, "the source changed while the dataset was read") {
		t.Fatalf("the read did not end as changed: %+v", last.End)
	}
}
