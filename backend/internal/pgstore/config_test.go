package pgstore

import (
	"strings"
	"testing"
	"time"
)

// [REQ-270]
func TestThePollIntervalOfTheStoreHasItsDefaultAndTheRestGoesToTheConnectionBase(t *testing.T) {
	if got := (Config{}).poll(); got != 50*time.Millisecond {
		t.Errorf("default poll interval %s", got)
	}
	if got := (Config{PollInterval: -time.Second}).poll(); got != 50*time.Millisecond {
		t.Errorf("a negative poll interval was kept: %s", got)
	}
	if got := (Config{PollInterval: time.Minute}).poll(); got != time.Minute {
		t.Errorf("a given poll interval was changed: %s", got)
	}
	cfg := Config{DSN: "postgres://user:hunter2@host/db", Limits: map[Role]Limits{RoleAPIRead: {Statement: time.Hour}}, MaxOpenConns: 7, MaxIdleConns: 3, ConnMaxLifetime: time.Minute, PollInterval: time.Second}
	c := cfg.conn()
	if c.DSN != cfg.DSN || c.MaxOpenConns != 7 || c.MaxIdleConns != 3 || c.ConnMaxLifetime != time.Minute || c.Limits[RoleAPIRead].Statement != time.Hour {
		t.Errorf("the connection base got %+v", c)
	}
	if text := cfg.String(); strings.Contains(text, "hunter2") || !strings.Contains(text, "PollInterval: 1s") {
		t.Errorf("the text of the Config: %q", text)
	}
}
