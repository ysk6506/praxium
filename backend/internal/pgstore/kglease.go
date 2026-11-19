package pgstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// resolve is for a write that moves the head. When its COMMIT got no answer (pgconn.CodeOutcomeUnknown) the write may
// or may not be in, so the head is read again from the primary server: a head that is the one the write set means it
// was committed, and the error is dropped; any other head means it was not, and the caller gets an error that says
// so and may try again (the applier tells a repeat by the head and the apply id). When the head cannot be read
// either, the answer stays "unknown". Every other error passes unchanged.
func (s *StoreV2) resolve(ctx context.Context, a *kgAccess, want graph.Head, err error) error {
	if kernel.CodeOf(err) != pgconn.CodeOutcomeUnknown {
		return err
	}
	var cur kgHeadRow
	rerr := a.sh.pool.TxOn(ctx, pgconn.WritePool, RoleApplier, true, func(tx *sql.Tx) error {
		var e error
		cur, e = loadKGHeadRaw(ctx, tx, a, false)
		return e
	})
	if rerr != nil {
		return err
	}
	if sameHead(cur.head, want) {
		return nil
	}
	return kernel.Wrap(pgconn.CodeConnectionLost, err, "the head of the KG does not show this write, so it was not committed; try it again")
}

// leaseUntil is the SQL of the new end of a lease: the database's time plus the ttl in seconds, or the caller's
// end as it is.
func (s *StoreV2) leaseUntil(index string) string {
	if s.clock == LeaseClockCaller {
		return index + "::timestamptz"
	}
	return "now() + " + index + "::double precision * interval '1 second'"
}

// untilArg is the argument that goes with leaseUntil.
func (s *StoreV2) untilArg(now time.Time, ttl time.Duration) any {
	if s.clock == LeaseClockCaller {
		return pgTime(now.Add(ttl))
	}
	return ttl.Seconds()
}

// AcquireLease implements graph.Writer: it takes the lease of a KG and raises the attempt. It refuses while
// another owner's lease is alive and an engine older than the one that applied (or the one that holds the lease).
func (s *StoreV2) AcquireLease(ctx context.Context, req graph.LeaseRequest) (graph.Lease, error) {
	a, err := s.access(ctx, req.KG)
	if err != nil {
		return graph.Lease{}, err
	}
	var out graph.Lease
	err = a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		head, err := loadKGHead(ctx, tx, a, false)
		if err != nil {
			return err
		}
		cur, err := s.loadLease(ctx, tx, a, lockExclusive, req.Now)
		if err != nil {
			return err
		}
		if err := mayAcquireV2(cur, head.head.AppliedSemanticVersion, req); err != nil {
			return err
		}
		var until time.Time
		err = tx.QueryRowContext(ctx, a.sub(`UPDATE @ SET lease_owner = $1, lease_until = `+s.leaseUntil("$2")+`, attempt = $3,
			semantic_version = $4 WHERE id = 1 RETURNING lease_until`, pgschema.TableLease), req.Owner, s.untilArg(req.Now, req.TTL), cur.attempt+1, req.Semantic).Scan(&until)
		if err != nil {
			return dbErr(err, "take the lease")
		}
		out = graph.Lease{KG: req.KG, Owner: req.Owner, Until: until.UTC(), Attempt: cur.attempt + 1, Semantic: req.Semantic}
		return nil
	})
	return out, err
}

// mayAcquireV2 is the rule of AcquireLease.
func mayAcquireV2(cur kgLease, applied int, req graph.LeaseRequest) error {
	if req.Owner == "" || req.TTL <= 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "a lease needs an owner and a ttl")
	}
	if cur.alive && cur.owner != req.Owner {
		return kernel.Errorf(graph.CodeLeaseHeld, "KG %s is leased by %s until %s", req.KG, cur.owner, cur.until.Format(time.RFC3339))
	}
	if req.Semantic < applied || (cur.alive && req.Semantic < cur.semantic) {
		return kernel.Errorf(graph.CodeSemanticTooLow, "engine %d is older than the applied %d", req.Semantic, applied)
	}
	return nil
}

// RenewLease implements graph.Writer: it moves lease_until of the lease the caller still holds.
func (s *StoreV2) RenewLease(ctx context.Context, l graph.Lease, ttl time.Duration, now time.Time) (graph.Lease, error) {
	a, err := s.access(ctx, l.KG)
	if err != nil {
		return graph.Lease{}, err
	}
	var out graph.Lease
	err = a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		cur, err := s.loadLease(ctx, tx, a, lockExclusive, now)
		if err != nil {
			return err
		}
		head, err := loadKGHead(ctx, tx, a, false)
		if err != nil {
			return err
		}
		if err := cur.holds(l); err != nil {
			return err
		}
		if l.Semantic < head.head.AppliedSemanticVersion {
			return kernel.Errorf(graph.CodeSemanticTooLow, "engine %d is below the applied %d", l.Semantic, head.head.AppliedSemanticVersion)
		}
		var until time.Time
		err = tx.QueryRowContext(ctx, a.sub(`UPDATE @ SET lease_until = `+s.leaseUntil("$1")+` WHERE id = 1 RETURNING lease_until`, pgschema.TableLease),
			s.untilArg(now, ttl)).Scan(&until)
		if err != nil {
			return dbErr(err, "renew the lease")
		}
		out = graph.Lease{KG: l.KG, Owner: cur.owner, Until: until.UTC(), Attempt: cur.attempt, Semantic: cur.semantic}
		return nil
	})
	return out, err
}

// ReleaseLease implements graph.Writer: it gives the lease up; one that is already lost is not an error.
func (s *StoreV2) ReleaseLease(ctx context.Context, l graph.Lease) error {
	a, err := s.access(ctx, l.KG)
	if err != nil {
		return err
	}
	return a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		cur, err := s.loadLease(ctx, tx, a, lockExclusive, time.Time{})
		if err != nil {
			return err
		}
		if !cur.held || cur.owner != l.Owner || cur.attempt != l.Attempt {
			return nil
		}
		_, err = tx.ExecContext(ctx, a.sub(`UPDATE @ SET lease_owner = NULL, lease_until = NULL WHERE id = 1`, pgschema.TableLease))
		return dbErr(err, "release the lease")
	})
}

// Lease implements graph.Reader: the stored lease, also when it has run out, and false when nobody holds it.
func (s *StoreV2) Lease(ctx context.Context, kg graph.KGID) (graph.Lease, bool, error) {
	a, err := s.access(ctx, kg)
	if err != nil {
		return graph.Lease{}, false, err
	}
	var cur kgLease
	err = a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		var err error
		cur, err = s.loadLease(ctx, tx, a, lockNone, time.Time{})
		return err
	})
	if err != nil || !cur.held {
		return graph.Lease{}, false, err
	}
	return graph.Lease{KG: kg, Owner: cur.owner, Until: cur.until, Attempt: cur.attempt, Semantic: cur.semantic}, true, nil
}
