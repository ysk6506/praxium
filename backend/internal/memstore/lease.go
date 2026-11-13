package memstore

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// AcquireLease implements graph.Writer.
func (s *Store) AcquireLease(_ context.Context, req graph.LeaseRequest) (graph.Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.state(req.KG)
	if err != nil {
		return graph.Lease{}, err
	}
	if req.Owner == "" || req.TTL <= 0 {
		return graph.Lease{}, kernel.Errorf(kernel.CodeInvalidValue, "a lease needs an owner and a ttl")
	}
	alive := st.lease != nil && req.Now.Before(st.lease.Until)
	if alive && st.lease.Owner != req.Owner {
		return graph.Lease{}, kernel.Errorf(graph.CodeLeaseHeld, "KG %s is leased by %s until %s", req.KG, st.lease.Owner, st.lease.Until.Format(time.RFC3339))
	}
	if req.Semantic < st.head.AppliedSemanticVersion || (alive && req.Semantic < st.lease.Semantic) {
		return graph.Lease{}, kernel.Errorf(graph.CodeSemanticTooLow, "engine %d is older than the applied %d", req.Semantic, st.head.AppliedSemanticVersion)
	}
	st.attempt++
	l := graph.Lease{KG: req.KG, Owner: req.Owner, Until: req.Now.Add(req.TTL), Attempt: st.attempt, Semantic: req.Semantic}
	st.lease = &l
	return l, nil
}

// RenewLease implements graph.Writer.
func (s *Store) RenewLease(_ context.Context, l graph.Lease, ttl time.Duration, now time.Time) (graph.Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.state(l.KG)
	if err != nil {
		return graph.Lease{}, err
	}
	if err := st.held(l, now); err != nil {
		return graph.Lease{}, err
	}
	st.lease.Until = now.Add(ttl)
	return *st.lease, nil
}

// ReleaseLease implements graph.Writer.
func (s *Store) ReleaseLease(_ context.Context, l graph.Lease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.state(l.KG)
	if err != nil {
		return err
	}
	if st.lease != nil && st.lease.Owner == l.Owner && st.lease.Attempt == l.Attempt {
		st.lease = nil
	}
	return nil
}
