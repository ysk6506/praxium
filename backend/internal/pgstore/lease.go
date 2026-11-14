package pgstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// AcquireLease implements graph.Writer: it takes the lease of a KG and raises the attempt. It refuses while
// another owner's lease is alive and an engine older than the one that applied (or the one that holds the lease).
func (s *Store) AcquireLease(ctx context.Context, req graph.LeaseRequest) (graph.Lease, error) {
	sh, err := s.rt.locate(req.KG)
	if err != nil {
		return graph.Lease{}, err
	}
	var out graph.Lease
	err = sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		cur, err := loadLease(ctx, tx, req.KG, lockExclusive)
		if err != nil {
			return err
		}
		head, err := loadHead(ctx, tx, req.KG, false)
		if err != nil {
			return err
		}
		if err := mayAcquire(cur, head.head.AppliedSemanticVersion, req); err != nil {
			return err
		}
		out = graph.Lease{KG: req.KG, Owner: req.Owner, Until: pgTime(req.Now.Add(req.TTL)), Attempt: cur.attempt + 1, Semantic: req.Semantic}
		_, err = tx.ExecContext(ctx, `UPDATE kg_apply_lease SET lease_owner = $2, lease_until = $3, attempt = $4, semantic_version = $5 WHERE kg_id = $1`,
			req.KG.String(), out.Owner, out.Until, out.Attempt, out.Semantic)
		return dbErr(err, "take the lease")
	})
	return out, err
}

// mayAcquire is the rule of AcquireLease.
func mayAcquire(cur leaseRow, applied int, req graph.LeaseRequest) error {
	if req.Owner == "" || req.TTL <= 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "a lease needs an owner and a ttl")
	}
	alive := cur.held && req.Now.Before(cur.until)
	if alive && cur.owner != req.Owner {
		return kernel.Errorf(graph.CodeLeaseHeld, "KG %s is leased by %s until %s", req.KG, cur.owner, cur.until.Format(time.RFC3339))
	}
	if req.Semantic < applied || (alive && req.Semantic < cur.semantic) {
		return kernel.Errorf(graph.CodeSemanticTooLow, "engine %d is older than the applied %d", req.Semantic, applied)
	}
	return nil
}

// RenewLease implements graph.Writer: it moves lease_until of the lease the caller still holds.
func (s *Store) RenewLease(ctx context.Context, l graph.Lease, ttl time.Duration, now time.Time) (graph.Lease, error) {
	sh, err := s.rt.locate(l.KG)
	if err != nil {
		return graph.Lease{}, err
	}
	var out graph.Lease
	err = sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		cur, err := loadLease(ctx, tx, l.KG, lockExclusive)
		if err != nil {
			return err
		}
		head, err := loadHead(ctx, tx, l.KG, false)
		if err != nil {
			return err
		}
		if err := cur.holds(l, now); err != nil {
			return err
		}
		if l.Semantic < head.head.AppliedSemanticVersion {
			return kernel.Errorf(graph.CodeSemanticTooLow, "engine %d is below the applied %d", l.Semantic, head.head.AppliedSemanticVersion)
		}
		out = graph.Lease{KG: l.KG, Owner: cur.owner, Until: pgTime(now.Add(ttl)), Attempt: cur.attempt, Semantic: cur.semantic}
		_, err = tx.ExecContext(ctx, `UPDATE kg_apply_lease SET lease_until = $2 WHERE kg_id = $1`, l.KG.String(), out.Until)
		return dbErr(err, "renew the lease")
	})
	return out, err
}

// ReleaseLease implements graph.Writer: it gives the lease up; one that is already lost is not an error.
func (s *Store) ReleaseLease(ctx context.Context, l graph.Lease) error {
	sh, err := s.rt.locate(l.KG)
	if err != nil {
		return err
	}
	return sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		cur, err := loadLease(ctx, tx, l.KG, lockExclusive)
		if err != nil {
			return err
		}
		if !cur.held || cur.owner != l.Owner || cur.attempt != l.Attempt {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE kg_apply_lease SET lease_owner = NULL, lease_until = NULL WHERE kg_id = $1`, l.KG.String())
		return dbErr(err, "release the lease")
	})
}

// Lease implements graph.Reader: the stored lease, also when it has run out, and false when nobody holds it.
func (s *Store) Lease(ctx context.Context, kg graph.KGID) (graph.Lease, bool, error) {
	sh, err := s.rt.locate(kg)
	if err != nil {
		return graph.Lease{}, false, err
	}
	var cur leaseRow
	err = sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		var err error
		cur, err = loadLease(ctx, tx, kg, lockNone)
		return err
	})
	if err != nil || !cur.held {
		return graph.Lease{}, false, err
	}
	return graph.Lease{KG: kg, Owner: cur.owner, Until: cur.until, Attempt: cur.attempt, Semantic: cur.semantic}, true, nil
}
