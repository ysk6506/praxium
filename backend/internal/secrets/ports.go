package secrets

import "context"

// The ports are given out as values of their own, each with only the methods of its port. A Describer that is
// asserted to an Opener or a Writer fails, so a role that was given the port to describe cannot reach the others by
// a type assertion. (Handing out the *Service itself would let it.)

type writerPort struct{ s *Service }

func (p writerPort) Create(ctx context.Context, in CreateInput) (Meta, error) {
	return p.s.Create(ctx, in)
}

func (p writerPort) AddVersion(ctx context.Context, id string, f Fields) (Meta, error) {
	return p.s.AddVersion(ctx, id, f)
}

func (p writerPort) RetireVersion(ctx context.Context, id string, version int) (Meta, error) {
	return p.s.RetireVersion(ctx, id, version)
}

func (p writerPort) Delete(ctx context.Context, id string) error { return p.s.Delete(ctx, id) }

func (p writerPort) SetScope(ctx context.Context, id string, ids []string) (Meta, error) {
	return p.s.SetScope(ctx, id, ids)
}

func (p writerPort) SetAllowedHosts(ctx context.Context, id string, hosts []Address) (Meta, error) {
	return p.s.SetAllowedHosts(ctx, id, hosts)
}

type openerPort struct{ s *Service }

func (p openerPort) Open(ctx context.Context, id string, use Use) (*Opened, error) {
	return p.s.Open(ctx, id, use)
}

type describerPort struct{ s *Service }

func (p describerPort) List(ctx context.Context) ([]Meta, error) { return p.s.List(ctx) }

func (p describerPort) Get(ctx context.Context, id string) (Meta, error) { return p.s.Get(ctx, id) }

// Writer is the port that changes secrets, as a value with that port's methods only.
func (s *Service) Writer() Writer { return writerPort{s: s} }

// Opener is the port that decrypts, as a value with that port's method only.
func (s *Service) Opener() Opener { return openerPort{s: s} }

// Describer is the port that reads what is not secret, as a value with that port's methods only.
func (s *Service) Describer() Describer { return describerPort{s: s} }
