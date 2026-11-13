package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func fixedDeps() deps {
	return deps{
		clock: kernel.NewManualClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)),
		ids:   kernel.NewSequentialIDs(),
	}
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		name string
		want Role
		ok   bool
	}{
		{"api", RoleAPI, true},
		{"worker", RoleWorker, true},
		{"query", RoleQuery, true},
		{"collector", RoleCollector, true},
		{"", "", false},
		{"API", "", false},
		{"server", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRole(tt.name)
			if (err == nil) != tt.ok || got != tt.want {
				t.Fatalf("ParseRole(%q) = %q, %v", tt.name, got, err)
			}
			if !tt.ok && kernel.CodeOf(err) != kernel.CodeInvalidValue {
				t.Fatalf("code = %q", kernel.CodeOf(err))
			}
		})
	}
}

func TestRunStartsEveryRole(t *testing.T) {
	for _, role := range []Role{RoleAPI, RoleWorker, RoleQuery, RoleCollector} {
		t.Run(string(role), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{string(role)}, &stdout, &stderr, fixedDeps()); code != exitOK {
				t.Fatalf("exit code = %d, stderr %q", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want nothing", stdout.String())
			}
			lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
			if len(lines) != 1 {
				t.Fatalf("%d log lines, want exactly one: %q", len(lines), stderr.String())
			}
			var entry map[string]any
			if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
				t.Fatalf("log line is not JSON: %v", err)
			}
			want := map[string]any{
				"level":    "INFO",
				"msg":      "praxium started",
				"role":     string(role),
				"version":  version,
				"instance": "inst-000001",
				"at":       "2030-01-02T03:04:05Z",
			}
			if len(entry) != len(want) {
				t.Fatalf("log entry = %v, want %v", entry, want)
			}
			for key, value := range want {
				if entry[key] != value {
					t.Errorf("log field %q = %v, want %v", key, entry[key], value)
				}
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	for _, flag := range []string{"--version", "-version"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{flag}, &stdout, &stderr, fixedDeps()); code != exitOK {
			t.Fatalf("%s: exit code = %d", flag, code)
		}
		if stdout.String() != "praxium "+version+"\n" || stderr.Len() != 0 {
			t.Fatalf("%s: stdout = %q, stderr = %q", flag, stdout.String(), stderr.String())
		}
	}
}

func TestRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", nil, "usage:"},
		{"too many arguments", []string{"api", "worker"}, "usage:"},
		{"unknown role", []string{"server"}, `unknown role "server"`},
		{"version with a role", []string{"--version", "api"}, "usage:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr, fixedDeps()); code != exitUsage {
				t.Fatalf("exit code = %d, want %d", code, exitUsage)
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stdout = %q, stderr = %q, want %q on stderr", stdout.String(), stderr.String(), tt.want)
			}
		})
	}
}
