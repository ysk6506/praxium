package pgstore

import (
	"strings"
	"testing"
)

// [REQ-472]
func TestInspectTLSJudgesTheLinkOfAConnectionString(t *testing.T) {
	for _, tt := range []struct {
		name, dsn  string
		remote, ok bool
	}{
		{"remote, no sslmode (driver default require)", "postgres://u:p@db.example.net:5432/praxium", true, false},
		{"remote, disable", "postgres://u:p@db.example.net/praxium?sslmode=disable", true, false},
		{"remote, require", "postgres://u:p@db.example.net/praxium?sslmode=require", true, false},
		{"remote, prefer", "host=db.example.net user=u dbname=praxium sslmode=prefer", true, false},
		{"remote, verify-full URL", "postgres://u:p@db.example.net/praxium?sslmode=verify-full", true, true},
		{"remote, verify-ca key=value", "host=db.example.net user=u dbname=praxium sslmode=verify-ca", true, true},
		{"loopback name, no sslmode", "host=localhost user=u dbname=praxium", false, true},
		{"loopback v4, disable", "postgres://u:p@127.0.0.1:5432/praxium?sslmode=disable", false, true},
		{"loopback v6", "postgres://u:p@[::1]:5432/praxium?sslmode=disable", false, true},
		{"unix socket", "host=/var/run/postgresql user=u dbname=praxium", false, true},
		{"no host at all", "user=u dbname=praxium", false, true},
		{"two hosts, one remote", "host=localhost,db.example.net user=u sslmode=require", true, false},
		{"two hosts, one remote, verified", "host=localhost,db.example.net user=u sslmode=verify-full", true, true},
		{"two hosts, both local", "host=localhost,127.0.0.1 user=u sslmode=disable", false, true},
		{"host address decides", "host=db.example.net hostaddr=127.0.0.1 user=u sslmode=disable", false, true},
		{"host address remote", "host=localhost hostaddr=192.0.2.10 user=u sslmode=disable", true, false},
	} {
		got, err := InspectTLS(tt.dsn)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got.Remote != tt.remote || got.Acceptable() != tt.ok {
			t.Errorf("%s: remote=%v acceptable=%v, want remote=%v acceptable=%v", tt.name, got.Remote, got.Acceptable(), tt.remote, tt.ok)
		}
	}
}

// [REQ-472]
func TestInspectTLSReportsTheModeAndHidesTheString(t *testing.T) {
	got, err := InspectTLS("postgres://u:p@db.example.net/praxium?sslmode=require")
	if err != nil || got.SSLMode != "require" || got.Verified {
		t.Fatalf("%+v %v", got, err)
	}
	_, err = InspectTLS("postgres://u:secret-pass@db.example.net/praxium?sslmode=nonsense")
	if err == nil || strings.Contains(err.Error(), "secret-pass") {
		t.Fatalf("a bad string: %v", err)
	}
}
