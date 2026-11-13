package graphcheck

import (
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
)

func leaseCases() []Case {
	return []Case{
		{"lease_is_exclusive_until_it_expires_or_is_released", caseLeaseExclusive},
		{"lease_refuses_a_lower_semantic_version", caseLeaseSemantic},
		{"lease_renewal_keeps_a_long_apply_alive", caseLeaseRenew},
		{"write_after_the_lease_expired_is_refused", caseLeaseExpired},
	}
}

func caseLeaseExclusive(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	other := e.applier(graph.Options{Semantic: 1})
	_, err := other.Acquire(e.ctx, e.kg.ID)
	wantCode(tb, "a second owner", err, graph.CodeLeaseHeld)
	_, err = e.clock.Advance(31 * time.Second)
	fatalIf(tb, err, "advance")
	taken, err := other.Acquire(e.ctx, e.kg.ID)
	fatalIf(tb, err, "takeover after expiry")
	check(tb, taken.Attempt > e.lease.Attempt, "attempt did not go up: %d then %d", e.lease.Attempt, taken.Attempt)
	_, err = e.a.Apply(e.ctx, e.kg.ID, e.lease, e.fx.World(1, 1, 1))
	wantCode(tb, "the old holder writes", err, graph.CodeLeaseLost)
	_, err = other.Apply(e.ctx, e.kg.ID, taken, e.fx.World(1, 1, 1))
	fatalIf(tb, err, "the new holder writes")
	fatalIf(tb, other.Release(e.ctx, taken), "release")
	_, held, err := e.store.Lease(e.ctx, e.kg.ID)
	fatalIf(tb, err, "lease")
	check(tb, !held, "the lease is still held after the release")
	_, err = e.a.Acquire(e.ctx, e.kg.ID)
	fatalIf(tb, err, "acquire after release")
}

func caseLeaseSemantic(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{Semantic: 2})
	e.apply(e.fx.World(1, 1, 1))
	check(tb, e.head().AppliedSemanticVersion == 2, "applied semantic version %d", e.head().AppliedSemanticVersion)
	fatalIf(tb, e.a.Release(e.ctx, e.lease), "release")
	older := e.applier(graph.Options{Semantic: 1})
	_, err := older.Acquire(e.ctx, e.kg.ID)
	wantCode(tb, "an older engine", err, graph.CodeSemanticTooLow)
	newer := e.applier(graph.Options{Semantic: 3})
	lease, err := newer.Acquire(e.ctx, e.kg.ID)
	fatalIf(tb, err, "a newer engine")
	_, err = newer.Apply(e.ctx, e.kg.ID, lease, graph.Batch{Volatile: []graph.VolatileWrite{e.fx.Power(e.fx.ServerID("SN-000000"), "off", "2030-01-01T00:02:00Z")}})
	fatalIf(tb, err, "apply by the newer engine")
	check(tb, e.head().AppliedSemanticVersion == 3, "applied semantic version %d after the newer engine", e.head().AppliedSemanticVersion)
	fatalIf(tb, newer.Release(e.ctx, lease), "release")
	_, err = e.a.Acquire(e.ctx, e.kg.ID)
	wantCode(tb, "back to the older engine", err, graph.CodeSemanticTooLow)
}

func caseLeaseRenew(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	_, err := e.clock.Advance(25 * time.Second)
	fatalIf(tb, err, "advance")
	renewed, err := e.store.RenewLease(e.ctx, e.lease, 30*time.Second, e.clock.Now())
	fatalIf(tb, err, "renew")
	check(tb, renewed.Until.After(e.lease.Until) && renewed.Attempt == e.lease.Attempt, "renewed %+v", renewed)
	_, err = e.clock.Advance(20 * time.Second)
	fatalIf(tb, err, "advance")
	e.apply(e.fx.World(1, 1, 1))
	stranger := e.lease
	stranger.Owner = "someone-else"
	_, err = e.store.RenewLease(e.ctx, stranger, time.Minute, e.clock.Now())
	wantCode(tb, "renewal by a stranger", err, graph.CodeLeaseLost)
}

func caseLeaseExpired(tb TB, f Factory) {
	e := newEnv(tb, f, graph.Options{})
	e.apply(e.fx.World(1, 1, 1))
	before := e.head()
	_, err := e.clock.Advance(time.Minute)
	fatalIf(tb, err, "advance")
	_, err = e.a.Apply(e.ctx, e.kg.ID, e.lease, e.fx.World(1, 1, 2))
	wantCode(tb, "a write after expiry", err, graph.CodeLeaseLost)
	check(tb, e.head().AppliedSeq == before.AppliedSeq, "the head moved")
	_, err = e.a.Seal(e.ctx, e.kg.ID, e.lease, graph.SealSync)
	wantCode(tb, "a seal after expiry", err, graph.CodeLeaseLost)
}
