package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// [REQ-554]
func TestThePoolFlagsReachTheStoreAndTheReadStringHasItsOwnFile(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.env.ReadFile = os.ReadFile
	dsn := pgstore.NewTestDatabase(t)
	o, err := parseServe([]string{"-store", "postgres", "-store-dsn-file", dsnFile(t, dsn), "-store-read-dsn-file", dsnFile(t, dsn+" application_name=reader"),
		"-store-write-conns", "3", "-store-read-conns", "2", "-store-query-conns", "1", "-store-new-conns-per-second", "50"})
	if err != nil {
		t.Fatal(err)
	}
	if o.storeWriteConns != 3 || o.storeReadConns != 2 || o.storeQueryConns != 1 || o.storeNewConns != 50 || o.storeReadDSNFile == "" {
		t.Fatalf("the flags: %+v", o)
	}
	st, closeStore, err := openStore(context.Background(), o, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListKGs(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = closeStore()
	o.storeReadDSNFile = filepathMissing(t)
	if _, _, err := openStore(context.Background(), o, d); kernel.CodeOf(err) != kernel.CodeInvalidValue || !strings.Contains(err.Error(), "read DSN file") {
		t.Fatalf("a read DSN file that is not there: %v", err)
	}
	for _, store := range []string{storeMemory, storeFile} {
		_, _, err := openStore(context.Background(), serveOptions{store: store, storeDataDir: t.TempDir(), storeReadDSNFile: "x"}, d)
		if kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("-store-read-dsn-file with -store %s: %v", store, err)
		}
	}
	var a adminOptions
	if fs := newAdminFlags("bootstrap", &a); fs.Lookup("store-read-dsn-file") == nil || fs.Lookup("store-write-conns") == nil {
		t.Error("the admin commands lack the pool flags")
	}
}

func filepathMissing(t *testing.T) string { return t.TempDir() + "/missing" }

// [REQ-472]
func TestTheReadConnectionStringIsJudgedForTLSToo(t *testing.T) {
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.env.ReadFile = os.ReadFile
	local := dsnFile(t, "host=/tmp sslmode=disable")
	remote := dsnFile(t, "host=db.example.test sslmode=require")
	verified := dsnFile(t, "host=db.example.test sslmode=verify-full")
	o := serveOptions{store: storePostgres, storeDSNFile: local, storeReadDSNFile: remote}
	st, ok, err := dbTLSState(o, d)
	if err != nil || !ok || st.Acceptable() {
		t.Fatalf("a remote read server without a check: %+v %v %v", st, ok, err)
	}
	o.storeReadDSNFile = verified
	if st, ok, err = dbTLSState(o, d); err != nil || !ok || !st.Acceptable() {
		t.Fatalf("a verified read server: %+v %v %v", st, ok, err)
	}
	o.storeDSNFile, o.storeReadDSNFile = remote, local
	if st, ok, err = dbTLSState(o, d); err != nil || !ok || st.Acceptable() {
		t.Fatalf("a weak write link stays weak: %+v %v %v", st, ok, err)
	}
	o.storeReadDSNFile = filepathMissing(t)
	if _, ok, err = dbTLSState(o, d); err != nil || !ok {
		t.Fatalf("a read file that cannot be read: %v %v", ok, err)
	}
	o.storeReadDSNFile = dsnFile(t, "postgres://u:p@%zz/db")
	if _, ok, err = dbTLSState(o, d); err == nil || ok {
		t.Fatalf("an unreadable read string: %v %v", ok, err)
	}
}
