package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func dbtlsOptions(mode string, allow bool) serveOptions {
	return serveOptions{addr: "127.0.0.1:8080", mode: mode, store: storePostgres, storeDSNFile: "/dsn", allowUnverifiedDBTLS: allow}
}

// [REQ-472] [REQ-473]
func TestProductionRefusesAnUnverifiedLinkToARemoteDatabase(t *testing.T) {
	for _, tt := range []struct {
		name, dsn string
		allow     bool
		refused   bool
	}{
		{"remote, no sslmode", "postgres://u:p@db.example.net/praxium", false, true},
		{"remote, disable", "host=db.example.net user=u sslmode=disable", false, true},
		{"remote, require", "postgres://u:p@db.example.net/praxium?sslmode=require", false, true},
		{"remote, verify-full", "postgres://u:p@db.example.net/praxium?sslmode=verify-full", false, false},
		{"remote, verify-ca", "host=db.example.net user=u sslmode=verify-ca", false, false},
		{"loopback", "postgres://u:p@127.0.0.1/praxium?sslmode=disable", false, false},
		{"socket", "host=/var/run/postgresql user=u", false, false},
		{"remote, allowed", "postgres://u:p@db.example.net/praxium?sslmode=disable", true, false},
	} {
		d, _, stop := apiDeps(map[string]string{"/dsn": tt.dsn + "\n"}, nil)
		err := checkMode(dbtlsOptions("production", tt.allow), d)
		stop()
		if (err != nil) != tt.refused {
			t.Errorf("%s: err = %v, refused want %v", tt.name, err, tt.refused)
		}
		if err != nil && (!strings.Contains(err.Error(), "-allow-unverified-db-tls") || !strings.Contains(err.Error(), "verify-full") || strings.Contains(err.Error(), "u:p")) {
			t.Errorf("%s: the message does not name both ways out, or shows the string: %v", tt.name, err)
		}
	}
}

// [REQ-472]
func TestTheDatabaseLinkIsNotJudgedInDevelopmentOrForOtherStores(t *testing.T) {
	d, _, stop := apiDeps(map[string]string{"/dsn": "postgres://u:p@db.example.net/praxium?sslmode=disable"}, nil)
	defer stop()
	if err := checkMode(dbtlsOptions("development", false), d); err != nil {
		t.Errorf("development: %v", err)
	}
	o := dbtlsOptions("production", false)
	o.store = storeFile
	if err := checkDBTLS(o, d); err != nil {
		t.Errorf("file store: %v", err)
	}
}

// [REQ-473]
func TestAllowingAnUnverifiedLinkWritesAWarningToTheStartLog(t *testing.T) {
	d, _, stop := apiDeps(map[string]string{"/dsn": "postgres://u:p@db.example.net/praxium?sslmode=require"}, nil)
	defer stop()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	warnDBTLS(dbtlsOptions("production", false), d, log)
	warnDBTLS(dbtlsOptions("development", true), d, log)
	if buf.Len() != 0 {
		t.Fatalf("a warning without permission or outside production: %q", buf.String())
	}
	warnDBTLS(dbtlsOptions("production", true), d, log)
	if !strings.Contains(buf.String(), "level=WARN") || !strings.Contains(buf.String(), "sslmode=require") || strings.Contains(buf.String(), "u:p") {
		t.Fatalf("the warning: %q", buf.String())
	}
}
