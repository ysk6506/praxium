package pgsource

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-489] [REQ-670]
func TestPgsConvertsEveryTypeToItsText(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TYPE mood AS ENUM ('sad','ok')`)
	r.exec(`CREATE DOMAIN pos AS int`)
	r.exec(`CREATE TABLE x (id int primary key, i2 int2, i8 int8, f4 float4, f8 float8, n numeric(30,10), nan numeric, t text, bp char(4), b bool, d date, ts timestamp, tz timestamptz, tm time, tmz timetz, j json, jb jsonb, ar int[], ts_arr text[], e mood, dm pos, u uuid, iv interval, inf timestamptz, ninf date, bc date)`)
	r.exec(`INSERT INTO x VALUES (1, 7, 9000000000000000000, 1.5, 0.1, 12345678901234567890.1234567890, 'NaN', 'héllo', 'ab', true, '2020-02-03', '2020-02-03 04:05:06.7', '2020-02-03 04:05:06.789+09', '04:05:06.5', '04:05:06+02', '{"a":{"b":[1,2.50]}}', '{"z":1,"a":12345678901234567890}', '{1,2,NULL}', '{"a,b","c"}', 'ok', 5, '11111111-2222-3333-4444-555555555555', '1 day', 'infinity', '-infinity', '0044-03-15 BC')`)
	r.exec(`INSERT INTO x (id) VALUES (2)`)
	cols := []string{"id", "i2", "i8", "f4", "f8", "n", "nan", "t", "bp", "b", "d", "ts", "tz", "tm", "tmz", "u", "iv", "inf", "ninf", "bc", "e", "dm",
		"ja=j.a.b.1", "jbz=jb.z", "jba=jb.a", "ar0=ar.0", "ar1=ar.1", "ar2=ar.2", "arlast=ts_arr.0", "ts1=ts_arr.1"}
	res := r.read(pgsDS("ds", pgsTable("x"), "id", cols...), ingest.PageOptions{})
	if res.err != nil {
		t.Fatal(res.err)
	}
	rows := res.rows()
	if len(rows) != 2 {
		t.Fatalf("rows %d", len(rows))
	}
	want := map[string]string{
		"id": "1", "i2": "7", "i8": "9000000000000000000", "f4": "1.5", "f8": "0.1",
		"n": "12345678901234567890.1234567890", "nan": "NaN", "t": "héllo", "bp": "ab  ", "b": "true",
		"d": "2020-02-03", "ts": "2020-02-03T04:05:06.7", "tz": "2020-02-02T19:05:06.789Z", "tm": "04:05:06.5", "tmz": "04:05:06+02:00",
		"u": "11111111-2222-3333-4444-555555555555", "iv": "1 day", "inf": "infinity", "ninf": "-infinity", "bc": "-0043-03-15",
		"e": "ok", "dm": "5",
		"ja": "2.50", "jbz": "1", "jba": "12345678901234567890", "ar0": "1", "ar1": "2", "arlast": "a,b", "ts1": "c",
	}
	for k, v := range want {
		if got := rows[0].Fields[k]; got != v {
			t.Errorf("%s: got %q want %q", k, got, v)
		}
	}
	if v, has := rows[0].Fields["ar2"]; has && v != "" {
		t.Errorf("a NULL inside an array is no value, got %q", v)
	}
	// A NULL is no value in any column.
	for k, v := range rows[1].Fields {
		if k != "id" && v != "" {
			t.Errorf("NULL column %s gave %q", k, v)
		}
	}
}

// [REQ-489] [REQ-670]
func TestPgsTextIsAlwaysValidUTF8(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE u (id int primary key, t text)`)
	r.exec(`INSERT INTO u VALUES (1, E'tab\there'), (2, '한국어 ✓')`)
	res := r.read(pgsDS("ds", pgsTable("u"), "id", "id", "t"), ingest.PageOptions{})
	if res.err != nil {
		t.Fatal(res.err)
	}
	if got := res.rows()[1].Fields["t"]; got != "한국어 ✓" || !strings.Contains(res.rows()[0].Fields["t"], "\t") {
		t.Fatalf("%q", got)
	}
}

func TestPgsConvertHelpers(t *testing.T) {
	if _, err := convertValue("BYTEA", []byte{1, 2}); err == nil {
		t.Fatal("binary was converted")
	}
	if _, err := convertValue("JSON", []byte("{")); err == nil {
		t.Fatal("broken json was converted")
	}
	if _, err := convertValue("X", struct{}{}); err == nil {
		t.Fatal("an unknown Go type was converted")
	}
	if v, _ := convertValue("TEXT", "a\xffb"); v != "a�b" {
		t.Fatalf("%q", v)
	}
	if v, _ := convertValue("", []byte("enum")); v != "enum" {
		t.Fatalf("%v", v)
	}
	if v, _ := convertValue("FLOAT8", 2.5); v != "2.5" {
		t.Fatalf("%v", v)
	}
	if v, _ := convertValue("BOOL", false); v != false {
		t.Fatalf("%v", v)
	}
}
