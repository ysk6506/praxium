package memstore

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// AppliedChanges implements graph.Feed.
func (s *Store) AppliedChanges(_ context.Context, kg graph.KGID, after int64, limit int) ([]graph.AppliedChange, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.state(kg)
	if err != nil {
		return nil, err
	}
	var out []graph.AppliedChange
	for _, b := range st.batches {
		if b.Seq <= after || b.Seq > st.head.ReadableThrough || len(out) >= limit {
			continue
		}
		out = append(out, graph.AppliedChange{Batch: b, Entries: append([]graph.ChangeEntry(nil), st.entries[b.Seq]...)})
	}
	return out, nil
}

// LightChanges implements graph.Feed.
func (s *Store) LightChanges(_ context.Context, kg graph.KGID, after int64, limit int) ([]graph.LightChange, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.state(kg)
	if err != nil {
		return nil, err
	}
	var out []graph.LightChange
	for _, l := range st.light {
		if l.Seq > after && l.Seq <= st.head.ReadableThrough && len(out) < limit {
			out = append(out, l)
		}
	}
	return out, nil
}

// OrderEvents implements graph.Feed.
func (s *Store) OrderEvents(_ context.Context, kg graph.KGID, after int64, limit int) ([]graph.OrderEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.state(kg)
	if err != nil {
		return nil, err
	}
	var out []graph.OrderEvent
	for _, e := range st.order {
		if e.IngestSeq > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

// WaitReadable implements graph.Feed: it returns when readable_through
// reaches minSeq, and gives up with CodeWaitTimeout after waitTimeout.
func (s *Store) WaitReadable(ctx context.Context, kg graph.KGID, minSeq int64, waitTimeout time.Duration) (int64, error) {
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()
	for {
		s.mu.RLock()
		st, err := s.state(kg)
		if err != nil {
			s.mu.RUnlock()
			return 0, err
		}
		readable, changed := st.head.ReadableThrough, s.changed
		s.mu.RUnlock()
		if readable >= minSeq {
			return readable, nil
		}
		select {
		case <-changed:
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return readable, kernel.Wrap(kernel.CodeInternal, ctx.Err(), "wait for readable_through was cancelled")
			}
			return readable, kernel.Errorf(graph.CodeWaitTimeout, "readable_through is %d, wanted %d", readable, minSeq)
		}
	}
}
