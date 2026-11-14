package definitions

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Config is what a Service needs. The time and the new IDs come from here, so the same inputs give the same result.
type Config struct {
	Store  Store
	Clock  kernel.Clock
	IDs    kernel.IDGenerator
	Limits Limits
}

// Service is the definitions: it reads and writes through the Store and keeps the rules above it. It holds no state of
// its own, so a second Service on the same store, or on a store opened again, sees the same definitions.
type Service struct {
	store  Store
	clock  kernel.Clock
	ids    kernel.IDGenerator
	limits Limits
}

// New makes a Service.
func New(cfg Config) (*Service, error) {
	if cfg.Store == nil || cfg.Clock == nil || cfg.IDs == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the definitions need a store, a clock and an ID generator")
	}
	return &Service{store: cfg.Store, clock: cfg.Clock, ids: cfg.IDs, limits: cfg.Limits.withDefaults()}, nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// freshID asks the generator for an ID of the prefix that is not taken, so IDs from a generator that started again do
// not meet the ones that are stored.
func (s *Service) freshID(prefix string, taken func(string) (bool, error)) (string, error) {
	for i := 0; i < 1000; i++ {
		id := s.ids.Next(prefix)
		if _, err := kernel.ParseID[struct{}](id); err != nil {
			return "", err
		}
		used, err := taken(id)
		if err != nil {
			return "", err
		}
		if !used {
			return id, nil
		}
	}
	return "", kernel.Errorf(kernel.CodeInternal, "no free ID with the prefix %q after 1000 tries", prefix)
}

func (s *Service) freshItemID(ctx context.Context) (string, error) {
	return s.freshID("item", func(id string) (bool, error) {
		found, err := s.store.Items(ctx, []string{id})
		return len(found) > 0, err
	})
}

func (s *Service) checkActor(a Actor) error {
	if a.ID == "" {
		return kernel.Errorf(kernel.CodeInvalidValue, "who acts must be said")
	}
	return overLimit("max_name_len", s.limits.MaxNameLen, len(a.ID)+len(a.OnBehalfOf))
}

func (s *Service) checkText(what string, text string, max int, limit string, required bool) error {
	if required && strings.TrimSpace(text) == "" {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s must not be empty", what)
	}
	return overLimit(limit, max, len(text))
}

func (s *Service) checkMessage(m string) error {
	return s.checkText("a message", m, s.limits.MaxMessageLen, "max_message_len", false)
}

func entryMap(set *Set) map[string]SetEntry {
	out := map[string]SetEntry{}
	if set == nil {
		return out
	}
	for _, e := range set.Entries {
		out[e.ItemID] = e
	}
	return out
}

func sortedEntries(m map[string]SetEntry) []SetEntry {
	out := make([]SetEntry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID < out[j].ItemID })
	return out
}

// loadHead returns the set the main line is at and its number; a main line without a set is nil and 0.
func (s *Service) loadHead(ctx context.Context) (*Set, int64, error) {
	no, err := s.store.Head(ctx)
	if err != nil || no == 0 {
		return nil, 0, err
	}
	set, err := s.store.Set(ctx, no)
	if err != nil {
		return nil, 0, err
	}
	return &set, no, nil
}

// loadSetOrEmpty returns a set by number, or nil for 0, which stands for a base without items.
func (s *Service) loadSetOrEmpty(ctx context.Context, no int64) (*Set, error) {
	if no == 0 {
		return nil, nil
	}
	set, err := s.store.Set(ctx, no)
	if err != nil {
		return nil, err
	}
	return &set, nil
}

// resolve gives the items of the entries with their content.
func (s *Service) resolve(ctx context.Context, entries []SetEntry) ([]ResolvedItem, error) {
	ids := make([]string, len(entries))
	refs := make([]RevRef, len(entries))
	for i, e := range entries {
		ids[i] = e.ItemID
		refs[i] = RevRef{ItemID: e.ItemID, RevNo: e.RevNo}
	}
	items, err := s.store.Items(ctx, ids)
	if err != nil {
		return nil, err
	}
	revs, err := s.store.Revisions(ctx, refs)
	if err != nil {
		return nil, err
	}
	out := make([]ResolvedItem, len(entries))
	for i, r := range revs {
		it := items[r.ItemID]
		out[i] = ResolvedItem{ItemID: r.ItemID, Kind: it.Kind, Name: it.Name, RevNo: r.RevNo,
			ContentHash: r.ContentHash, SchemaVersion: r.SchemaVersion, Body: r.Body}
	}
	return out, nil
}

func notFound(what, id string) error {
	return kernel.Errorf(kernel.CodeNotFound, "there is no %s %q", what, id)
}
