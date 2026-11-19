package pgsource

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-489] [REQ-655] [REQ-670]
func TestPgsReadsATableInThePagesOfItsKey(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE servers (serial int PRIMARY KEY, name text, note text)`)
	r.exec(`INSERT INTO servers SELECT i, 'host-'||i, NULL FROM generate_series(1,25) i`)
	res := r.read(pgsDS("ds_servers", pgsTable("servers"), "serial", "serial", "name", "note"), ingest.PageOptions{MaxRows: 10})
	if res.err != nil {
		t.Fatal(res.err)
	}
	if len(res.pages) != 3 || len(res.rows()) != 25 {
		t.Fatalf("pages %d rows %d", len(res.pages), len(res.rows()))
	}
	for i, pg := range res.pages {
		if pg.Last != (i == 2) || (i < 2 && len(pg.Token) == 0) {
			t.Fatalf("page %d: last %v token %d", i, pg.Last, len(pg.Token))
		}
	}
	last := res.pages[2]
	if !last.End.Complete {
		t.Fatalf("not complete: %+v", last.End)
	}
	first := res.rows()[0]
	if first.Key != "1" || first.Fields["name"] != "host-1" {
		t.Fatalf("first row %+v", first)
	}
	if _, has := first.Fields["note"]; has && first.Fields["note"] != "" {
		t.Fatalf("a NULL is no value: %+v", first.Fields)
	}
}

// [REQ-489] [REQ-490] [REQ-670]
func TestPgsReadsAStatement(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE placement (vm_id int, host_id int, retired_at timestamptz)`)
	r.exec(`INSERT INTO placement SELECT i, i%3, CASE WHEN i%2=0 THEN now() END FROM generate_series(1,20) i`)
	for _, stmt := range []string{
		`SELECT vm_id, host_id FROM placement WHERE retired_at IS NULL`,
		"SELECT vm_id, host_id FROM placement WHERE retired_at IS NULL;",
		"WITH p AS (SELECT * FROM placement WHERE retired_at IS NULL) SELECT vm_id, host_id FROM p -- trailing comment",
	} {
		res := r.read(pgsDS("ds_vm", pgsSQL(stmt), "vm_id", "vm_id", "host_id"), ingest.PageOptions{MaxRows: 4})
		if res.err != nil {
			t.Fatalf("%q: %v", stmt, res.err)
		}
		want := []string{"1", "3", "5", "7", "9", "11", "13", "15", "17", "19"}
		if !reflect.DeepEqual(res.keys(), want) {
			t.Fatalf("%q: keys %v", stmt, res.keys())
		}
	}
}

// [REQ-655] [REQ-670]
func TestPgsResumesFromAMiddleToken(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int PRIMARY KEY, v text)`)
	r.exec(`INSERT INTO t SELECT i, 'v'||i FROM generate_series(1,30) i`)
	ds := pgsDS("ds", pgsTable("t"), "k", "k", "v")
	rd, d := r.one(ds, "")
	all := pgsRead(rd, d, ingest.PageOptions{MaxRows: 7})
	if all.err != nil || len(all.pages) < 3 {
		t.Fatalf("%v %d", all.err, len(all.pages))
	}
	mid := all.pages[1]
	rest := pgsRead(rd, d, ingest.PageOptions{MaxRows: 7, Resume: mid.Token})
	if rest.err != nil {
		t.Fatal(rest.err)
	}
	var want []string
	for _, pg := range all.pages[2:] {
		for _, row := range pg.Rows {
			want = append(want, row.Key+row.Fields["v"])
		}
	}
	var got []string
	for _, row := range rest.rows() {
		got = append(got, row.Key+row.Fields["v"])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	// A token that is not ours, or damaged, is ignored: the read starts at the first page.
	for _, tok := range [][]byte{[]byte("nonsense"), []byte(`{"v":1,"fp":"00","k":["3"]}`), []byte(`{"v":9}`)} {
		again := pgsRead(rd, d, ingest.PageOptions{MaxRows: 7, Resume: tok})
		if again.err != nil || len(again.rows()) != 30 {
			t.Fatalf("token %q: %v %d", tok, again.err, len(again.rows()))
		}
	}
	// A token of another dataset (other fingerprint) is ignored too.
	rd2, d2 := r.one(pgsDS("ds", pgsTable("t"), "v", "v", "k"), "")
	other := pgsRead(rd2, d2, ingest.PageOptions{MaxRows: 7, Resume: mid.Token})
	if other.err != nil || len(other.rows()) != 30 {
		t.Fatalf("foreign token: %v %d", other.err, len(other.rows()))
	}
}

// [REQ-655] [REQ-670]
func TestPgsKeepsRowsOfOneKeyOnOnePage(t *testing.T) {
	r := pgsNewRig(t)
	// Not a primary key: the user names a natural key that repeats.
	r.exec(`CREATE TABLE d (k int, n int)`)
	r.exec(`INSERT INTO d SELECT i/3, i FROM generate_series(0,29) i`)
	rd, ds := r.one(pgsDS("ds", pgsTable("d"), "k", "k", "n"), "")
	res := pgsRead(rd, ds, ingest.PageOptions{MaxRows: 4})
	if res.err != nil {
		t.Fatal(res.err)
	}
	seen := map[string]int{}
	for i, pg := range res.pages {
		for _, row := range pg.Rows {
			seen[row.Key]++
			_ = i
		}
	}
	// The row builder reports a repeated key as a duplicate: the three rows of a key are one record's concern of the
	// service; here every row is delivered and a key never ends one page and starts the next.
	pageOf := map[string]int{}
	for i, pg := range res.pages {
		for _, row := range pg.Rows {
			if p, ok := pageOf[row.Key]; ok && p != i {
				t.Fatalf("key %s is on pages %d and %d", row.Key, p, i)
			}
			pageOf[row.Key] = i
		}
	}
	total := 0
	for _, n := range seen {
		total += n
	}
	if total != 30 {
		t.Fatalf("rows %d", total)
	}
	// A key with more rows than the dataset may have is an incomplete read, never a silent cut.
	res = pgsRead(rd, ds, ingest.PageOptions{MaxRows: 2, MaxDatasetRows: 2})
	if res.err != nil {
		t.Fatal(res.err)
	}
	if last := res.pages[len(res.pages)-1]; last.End.Complete {
		t.Fatalf("a read cut at the row limit is complete: %+v", last.End)
	}
}

// [REQ-655] [REQ-670]
func TestPgsKeyTypesRoundTripThroughTokens(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TYPE mood AS ENUM ('sad','ok','happy')`)
	r.exec(`CREATE TABLE kt (i2 int2, i8 int8, num numeric, txt text, vc varchar(20), bp char(6), u uuid, d date, ts timestamp, tz timestamptz, tm time, b bool, e mood, k2a int, k2b text)`)
	r.exec(`INSERT INTO kt
	  SELECT i, i::int8*1000000000, (i::numeric/7), 'Ä'||lpad(i::text,3,'0'), 'v'||(100-i), 'c'||i, ('00000000-0000-0000-0000-'||lpad(i::text,12,'0'))::uuid,
	         date '2020-01-01'+i, timestamp '2020-01-01 10:00:00.123456'+i*interval '1 hour', timestamptz '2020-01-01 00:00:00+09'+i*interval '1 day',
	         time '01:00'+i*interval '1 minute', i%2=0, (ARRAY['sad','ok','happy'])[1+i%3]::mood, i/4, 'g'||i%4
	  FROM generate_series(1,13) i`)
	cols := []string{"i2", "i8", "num", "txt", "vc", "bp", "u", "d", "ts", "tz", "tm", "b", "e", "k2a", "k2b"}
	for _, key := range []string{"i2", "i8", "num", "txt", "vc", "bp", "u", "d", "ts", "tz", "tm", "k2a,k2b"} {
		rd, ds := r.one(pgsDS("ds", pgsTable("kt"), key, cols...), "")
		all := pgsRead(rd, ds, ingest.PageOptions{MaxRows: 3})
		if all.err != nil {
			t.Fatalf("key %s: %v", key, all.err)
		}
		if len(all.rows()) != 13 || len(all.pages) < 3 {
			t.Fatalf("key %s: rows %d pages %d", key, len(all.rows()), len(all.pages))
		}
		// Every token gives the same rest as the read that made it.
		for i := 0; i < len(all.pages)-1; i++ {
			rest := pgsRead(rd, ds, ingest.PageOptions{MaxRows: 3, Resume: all.pages[i].Token})
			if rest.err != nil {
				t.Fatalf("key %s page %d: %v", key, i, rest.err)
			}
			var want []string
			for _, pg := range all.pages[i+1:] {
				for _, row := range pg.Rows {
					want = append(want, row.Key)
				}
			}
			if !reflect.DeepEqual(rest.keys(), want) {
				t.Fatalf("key %s page %d: %v vs %v", key, i, rest.keys(), want)
			}
		}
	}
	// An enum key is allowed too.
	rd, ds := r.one(pgsDS("ds", pgsTable("kt"), "e,i2", "e", "i2"), "")
	if res := pgsRead(rd, ds, ingest.PageOptions{MaxRows: 5}); res.err != nil || len(res.rows()) != 13 {
		t.Fatalf("enum key: %v", res.err)
	}
}

// [REQ-489] [REQ-655] [REQ-670]
func TestPgsRefusesAKeyThatCannotBeOrderedFaithfully(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE bad (f float8, b bytea, j jsonb, a int[], id int)`)
	for _, key := range []string{"f", "b", "j", "a"} {
		res := r.read(pgsDS("ds", pgsTable("bad"), key, key, "id"), ingest.PageOptions{})
		if res.err == nil {
			t.Fatalf("key %s was accepted", key)
		}
		pgsSrcErr(t, res.err, ingest.SrcRequestRejected)
	}
	// A column of bytea in the dataset is a definition error.
	res := r.read(pgsDS("ds", pgsTable("bad"), "id", "id", "b"), ingest.PageOptions{})
	pgsSrcErr(t, res.err, ingest.SrcRequestRejected)
}

// [REQ-655] [REQ-670]
func TestPgsRowsWithoutAKeyAreReportedNotRead(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE n (a int, b int, v text)`)
	r.exec(`INSERT INTO n VALUES (1,1,'x'),(2,NULL,'y'),(NULL,NULL,'z'),(NULL,NULL,'w'),(3,3,'q')`)
	res := r.read(pgsDS("ds", pgsTable("n"), "a,b", "a", "b", "v"), ingest.PageOptions{})
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !reflect.DeepEqual(res.keys(), []string{"1" + pgsKeySep + "1", "3" + pgsKeySep + "3"}) {
		t.Fatalf("keys %q", res.keys())
	}
	var bad int
	for _, pg := range res.pages {
		bad += len(pg.Unreadable)
	}
	if bad == 0 {
		t.Fatalf("rows without a key are not reported")
	}
	// With ignore_rows_without_key the rows whose keys are all empty are only counted.
	doc := `{"id":"ds","table":"n","natural_key":["a","b"],"ignore_rows_without_key":true,"columns":[{"name":"a"},{"name":"b"},{"name":"v"}]}`
	rd, src := r.reader(r.sourceDoc("", doc))
	res = pgsRead(rd, src.Datasets[0], ingest.PageOptions{})
	if res.err != nil {
		t.Fatal(res.err)
	}
	skipped := 0
	for _, pg := range res.pages {
		skipped += pg.Skipped[ingest.SkipNoKey]
	}
	if skipped != 2 {
		t.Fatalf("skipped %d", skipped)
	}
	_ = fmt.Sprint
}
