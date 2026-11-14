package identity

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var idtEpoch = time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)

// idtRig is a service over the in-memory store with a clock that moves when told, a counting random source and a
// log kept in a buffer.
type idtRig struct {
	t     *testing.T
	svc   *Service
	store *idtCountingStore
	clock *kernel.ManualClock
	logs  *idtLog
	ctx   context.Context
}

// idtLog is a buffer that is safe to write from several goroutines.
type idtLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *idtLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *idtLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// idtCountingStore counts the writes by kind and can be made to fail.
type idtCountingStore struct {
	*MemoryStore
	mu     sync.Mutex
	puts   map[string]int
	broken error
}

func (s *idtCountingStore) Get(ctx context.Context, kind, id string) ([]byte, int64, error) {
	if err := s.failure(); err != nil {
		return nil, 0, err
	}
	return s.MemoryStore.Get(ctx, kind, id)
}

func (s *idtCountingStore) Put(ctx context.Context, kind, id string, data []byte, expect int64) (int64, error) {
	if err := s.failure(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.puts[kind]++
	s.mu.Unlock()
	return s.MemoryStore.Put(ctx, kind, id, data, expect)
}

func (s *idtCountingStore) Scan(ctx context.Context, kind string, fn func(string, []byte, int64) error) error {
	if err := s.failure(); err != nil {
		return err
	}
	return s.MemoryStore.Scan(ctx, kind, fn)
}

func (s *idtCountingStore) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.broken
}

func (s *idtCountingStore) writes(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts[kind]
}

func (s *idtCountingStore) breakWith(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broken = err
}

// idtRandom is a random source for tests: every call gives the next number as hex, so IDs and secrets differ.
func idtRandom() func(n int) string {
	var mu sync.Mutex
	next := 0
	return func(n int) string {
		mu.Lock()
		defer mu.Unlock()
		next++
		return fmt.Sprintf("%0*x", 2*n, next+0xa0000)
	}
}

func idtNewRig(t *testing.T) *idtRig {
	t.Helper()
	store := &idtCountingStore{MemoryStore: NewMemoryStore(), puts: map[string]int{}}
	clock := kernel.NewManualClock(idtEpoch)
	logs := &idtLog{}
	svc, err := New(Config{Store: store, Clock: clock, Random: idtRandom(), Log: slog.New(slog.NewJSONHandler(logs, nil))})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return &idtRig{t: t, svc: svc, store: store, clock: clock, logs: logs, ctx: context.Background()}
}

var idtAdmin = access.Subject{Kind: access.SubjectPerson, ID: "root", Roles: []access.Role{access.RoleAdmin}}

func (r *idtRig) person(name string, roles ...access.Role) access.Principal {
	r.t.Helper()
	p, err := r.svc.CreatePrincipal(r.ctx, idtAdmin, access.PrincipalRequest{Kind: access.SubjectPerson, Name: name, DisplayName: name, Roles: roles})
	if err != nil {
		r.t.Fatalf("create %s: %v", name, err)
	}
	return p
}

func (r *idtRig) token(owner string, roles ...access.Role) access.IssuedToken {
	r.t.Helper()
	tok, err := r.svc.IssueToken(r.ctx, idtAdmin, access.TokenRequest{Owner: owner, Roles: roles})
	if err != nil {
		r.t.Fatalf("issue a token for %s: %v", owner, err)
	}
	return tok
}

func idtWantCode(t *testing.T, what string, err error, want kernel.Code) {
	t.Helper()
	if err == nil || kernel.CodeOf(err) != want {
		t.Fatalf("%s: error %v, want code %s", what, err, want)
	}
}

func (r *idtRig) advance(d time.Duration) {
	r.t.Helper()
	if _, err := r.clock.Advance(d); err != nil {
		r.t.Fatalf("advance the clock: %v", err)
	}
}
