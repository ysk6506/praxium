package definitions

import (
	"context"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ApplyInput is a direct apply: changes to the main line, made at once.
type ApplyInput struct {
	// ExpectHead is the number of the set the caller read as the head (0 when there is none); the apply is refused if
	// the head has moved.
	ExpectHead int64
	// Origin is change (the default), import or bootstrap.
	Origin Origin
	// Baseline is the hash of what stands outside the items; empty keeps the one the head has.
	Baseline string
	Changes  []Change
	// Counters are the next values of counters of the ledger, raised in the same write. A value must not be below the
	// stored one.
	Counters map[string]int64
	Actor    Actor
	Message  string
}

// ApplyResult is what an apply made. When Changed is false nothing was written, and Set is the head as it was (the
// zero Set if there was none).
type ApplyResult struct {
	Set     Set
	Changed bool
}

// Apply makes the changes in the set after the head: the new items, the revisions of the content that is new, the set,
// the ledger and the head, in one atomic write. Content equal to what the head holds is not a change; when nothing is
// left to change, nothing is written and the result says so.
func (s *Service) Apply(ctx context.Context, in ApplyInput) (ApplyResult, error) {
	if err := s.checkApply(in); err != nil {
		return ApplyResult{}, err
	}
	head, no, err := s.loadHead(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	if no != in.ExpectHead {
		return ApplyResult{}, kernel.Errorf(kernel.CodeConflict, "the head is D-%d, not the D-%d that was expected", no, in.ExpectHead)
	}
	return s.applyOn(ctx, head, in, "")
}

func (s *Service) checkApply(in ApplyInput) error {
	if err := s.checkActor(in.Actor); err != nil {
		return err
	}
	if err := s.checkMessage(in.Message); err != nil {
		return err
	}
	switch in.Origin {
	case "", OriginChange, OriginImport, OriginBootstrap:
		return nil
	}
	return kernel.Errorf(kernel.CodeInvalidValue, "a direct apply has the origin change, import or bootstrap, not %q", in.Origin)
}

// applyOn plans the changes on the head and commits them with the ledger and with any extra writes of a caller that
// goes on from here (a merge).
func (s *Service) applyOn(ctx context.Context, head *Set, in ApplyInput, proposalID string, extra ...func(*Write, planned)) (ApplyResult, error) {
	changes, err := s.prepare(ctx, in.Changes)
	if err != nil {
		return ApplyResult{}, err
	}
	baseline := in.Baseline
	if baseline == "" && head != nil {
		baseline = head.Baseline
	}
	origin := in.Origin
	if origin == "" {
		origin = OriginChange
	}
	p, err := s.plan(ctx, planInput{head: head, changes: changes, baseline: baseline, origin: origin,
		proposalID: proposalID, actor: in.Actor, message: in.Message})
	if err != nil {
		return ApplyResult{}, err
	}
	if p.unchanged {
		return ApplyResult{Set: headOrZero(head)}, nil
	}
	if p.write.Counters, err = s.counterWrites(ctx, in.Counters); err != nil {
		return ApplyResult{}, err
	}
	for _, f := range extra {
		f(&p.write, p)
	}
	if err := s.store.Commit(ctx, p.write); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Set: p.set, Changed: true}, nil
}

func headOrZero(head *Set) Set {
	if head == nil {
		return Set{}
	}
	return *head
}

// counterWrites turns the next values the caller gave into writes that expect the values read now.
func (s *Service) counterWrites(ctx context.Context, next map[string]int64) ([]CounterWrite, error) {
	names := make([]string, 0, len(next))
	for name := range next {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]CounterWrite, 0, len(names))
	for _, name := range names {
		have, err := s.store.Counter(ctx, name)
		if err != nil {
			return nil, err
		}
		if next[name] < 1 || next[name] < have {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the counter %q is %d and cannot be set to %d", name, have, next[name])
		}
		out = append(out, CounterWrite{Name: name, Expect: have, Value: next[name]})
	}
	return out, nil
}

// NextNumber returns the next number of a counter of the ledger: the stored value, or start when the counter is not
// there yet. Giving it back in the Counters of a write (after taking what you need) is what raises the counter.
func (s *Service) NextNumber(ctx context.Context, name string, start int64) (int64, error) {
	if name == "" || start < 1 {
		return 0, kernel.Errorf(kernel.CodeInvalidValue, "a counter needs a name and a start from 1")
	}
	have, err := s.store.Counter(ctx, name)
	if err != nil {
		return 0, err
	}
	if have < start {
		return start, nil
	}
	return have, nil
}
