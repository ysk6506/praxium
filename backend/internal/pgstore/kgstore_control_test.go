package pgstore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// The tests of the control rows of the new storage: the lease, the head, the guard of the writes and the codecs.
// Their names start with v2c.

func v2cLease(t *testing.T, st *Store, kg graph.KGID, owner string, now time.Time, ttl time.Duration, semantic int) (graph.Lease, error) {
	t.Helper()
	return st.AcquireLease(context.Background(), graph.LeaseRequest{KG: kg, Owner: owner, Now: now, TTL: ttl, Semantic: semantic})
}

func TestV2cLeaseIsTakenRenewedAndReleasedByTheDatabaseClock(t *testing.T) {
	w := v2rWorldOf(t)
	ctx, kg := context.Background(), w.kg.ID
	if _, ok, err := w.st.Lease(ctx, kg); ok || err != nil {
		t.Fatalf("a lease nobody holds: %v %v", ok, err)
	}
	_, err := v2cLease(t, w.st, kg, "", time.Time{}, time.Minute, 1)
	wantCode(t, "no owner", err, kernel.CodeInvalidValue)
	l, err := v2cLease(t, w.st, kg, "a", time.Time{}, time.Hour, 3)
	noErr(t, "acquire", err)
	if l.Attempt != 1 || l.Owner != "a" || !l.Until.After(time.Unix(1000, 0)) {
		t.Fatalf("lease %+v", l)
	}
	_, err = v2cLease(t, w.st, kg, "b", time.Time{}, time.Hour, 3)
	wantCode(t, "held by another", err, graph.CodeLeaseHeld)
	_, err = v2cLease(t, w.st, kg, "a", time.Time{}, time.Hour, 2)
	wantCode(t, "an older engine than the lease's", err, graph.CodeSemanticTooLow)
	again, err := v2cLease(t, w.st, kg, "a", time.Time{}, time.Hour, 3)
	noErr(t, "again", err)
	if again.Attempt != 2 {
		t.Fatalf("attempt %d", again.Attempt)
	}
	renewed, err := w.st.RenewLease(ctx, again, 2*time.Hour, time.Time{})
	noErr(t, "renew", err)
	if !renewed.Until.After(again.Until) {
		t.Fatal("the lease was not moved")
	}
	_, err = w.st.RenewLease(ctx, graph.Lease{KG: kg, Owner: "a", Attempt: 1, Semantic: 3}, time.Hour, time.Time{})
	wantCode(t, "an old attempt", err, graph.CodeLeaseLost)
	v2rExec(t, w.admin, `UPDATE `+w.a.t("head")+` SET applied_semantic_version = 5`)
	_, err = w.st.RenewLease(ctx, again, time.Hour, time.Time{})
	wantCode(t, "renewing under the applied engine", err, graph.CodeSemanticTooLow)
	noErr(t, "release by a stranger", w.st.ReleaseLease(ctx, graph.Lease{KG: kg, Owner: "b", Attempt: again.Attempt}))
	if _, ok, _ := w.st.Lease(ctx, kg); !ok {
		t.Fatal("a stranger released the lease")
	}
	noErr(t, "release", w.st.ReleaseLease(ctx, again))
	if _, ok, _ := w.st.Lease(ctx, kg); ok {
		t.Fatal("the lease is still held")
	}
	_, err = v2cLease(t, w.st, kg, "b", time.Time{}, time.Hour, 4)
	wantCode(t, "an engine older than the applied one", err, graph.CodeSemanticTooLow)
	ghost, _ := kernel.ParseID[graph.KGKind]("kg-00000000000000000000000000000001")
	_, err = w.st.RenewLease(ctx, graph.Lease{KG: ghost}, time.Hour, time.Time{})
	wantCode(t, "renew on a missing KG", err, graph.CodeKGNotFound)
	wantCode(t, "release on a missing KG", w.st.ReleaseLease(ctx, graph.Lease{KG: ghost}), graph.CodeKGNotFound)
}

func TestV2cLeaseRunsOutByTheClockOfTheCallerWhenTheStoreIsAskedTo(t *testing.T) {
	w := v2rWorldWith(t, LeaseClockCaller)
	kg := w.kg.ID
	start := time.Unix(5000, 0)
	l, err := v2cLease(t, w.st, kg, "a", start, time.Minute, 1)
	noErr(t, "acquire", err)
	if !l.Until.Equal(start.Add(time.Minute)) {
		t.Fatalf("until %v", l.Until)
	}
	_, err = v2cLease(t, w.st, kg, "b", start.Add(30*time.Second), time.Minute, 1)
	wantCode(t, "alive", err, graph.CodeLeaseHeld)
	taken, err := v2cLease(t, w.st, kg, "b", start.Add(2*time.Minute), time.Minute, 1)
	noErr(t, "run out", err)
	if taken.Owner != "b" || taken.Attempt != 2 {
		t.Fatalf("lease %+v", taken)
	}
	_, err = w.st.RenewLease(context.Background(), l, time.Minute, start.Add(2*time.Minute))
	wantCode(t, "the lease of the first owner is lost", err, graph.CodeLeaseLost)
}

func TestV2cGuardChecksInOrderAndTheHeadRoundTrips(t *testing.T) {
	w := v2rWorldOf(t)
	ctx, kg := context.Background(), w.kg.ID
	l, err := v2cLease(t, w.st, kg, "a", time.Time{}, time.Hour, 2)
	noErr(t, "acquire", err)
	p := planned{kg: kg, generation: 1, expected: 0, lease: l}
	guard := func() error {
		return w.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
			_, err := w.st.kgGuard(ctx, tx, w.a, p)
			return err
		})
	}
	noErr(t, "a write that is in order", guard())
	set := func(q string) { v2rExec(t, w.admin, `UPDATE `+w.a.t("head")+` SET `+q) }
	p.expected = 3
	wantCode(t, "head moved", guard(), graph.CodeHeadMoved)
	p.expected = 0
	p.generation = 2
	wantCode(t, "generation", guard(), graph.CodeGenerationChanged)
	p.generation = 1
	set(`applied_semantic_version = 9`)
	wantCode(t, "engine", guard(), graph.CodeSemanticTooLow)
	set(`applied_semantic_version = 0`)
	p.lease.Attempt = 7
	wantCode(t, "lease", guard(), graph.CodeLeaseLost)
	p.lease = l
	set(`work = 'abandoning'`)
	wantCode(t, "abandoning", guard(), graph.CodeApplyInProgress)
	set(`work = 'restoring'`)
	wantCode(t, "restoring", guard(), CodeKGRestoring)
	set(`work = ''`)

	want := graph.Head{KG: kg, AppliedSeq: 4, ReadableThrough: 3, IngestSeq: 2, ApplyID: "x", NextChunkNo: 1, ChunkCount: 2, LastApplyID: "y",
		AppliedSemanticVersion: 2, InputDigest: kernel.Hash{1}}
	want.Hash.Rows = 5
	err = w.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		if err := saveKGHead(ctx, tx, w.a, want); err != nil {
			return err
		}
		cur, err := loadKGHead(ctx, tx, w.a, true)
		if err != nil {
			return err
		}
		if !sameHead(cur.head, want) || cur.head.Hash.Rows != 5 || cur.head.InputDigest != want.InputDigest {
			t.Errorf("head %+v", cur.head)
		}
		raw, err := headDocOf(want)
		if err != nil {
			return err
		}
		back, err := headOfDoc(kg, raw)
		if err != nil {
			return err
		}
		if back.AppliedSeq != 4 || back.ReadableThrough != 3 || back.IngestSeq != 2 || back.LastApplyID != "y" || back.AppliedSemanticVersion != 2 || back.Hash.Rows != 5 {
			t.Errorf("doc %+v", back)
		}
		return nil
	})
	noErr(t, "save", err)
	_, err = headOfDoc(kg, []byte(`{"s":"zz"}`))
	if err == nil {
		t.Fatal("a damaged head doc is read")
	}
	_, err = headOfDoc(kg, []byte(`not json`))
	if err == nil {
		t.Fatal("a head doc that is not JSON is read")
	}
}

func TestV2cResolveDecidesAnUnknownCommitByTheHead(t *testing.T) {
	w := v2rWorldOf(t)
	ctx := context.Background()
	other := kernel.Errorf(kernel.CodeConflict, "other")
	if got := w.st.resolve(ctx, w.a, graph.Head{}, other); got != other {
		t.Fatalf("an error that is not unknown changed to %v", got)
	}
	unknown := kernel.Errorf(pgconn.CodeOutcomeUnknown, "no answer")
	if err := w.st.resolve(ctx, w.a, graph.Head{KG: w.kg.ID}, unknown); err != nil {
		t.Fatalf("the head is the one of the write, the write is in: %v", err)
	}
	err := w.st.resolve(ctx, w.a, graph.Head{KG: w.kg.ID, AppliedSeq: 9}, unknown)
	wantCode(t, "the head shows another write", err, pgconn.CodeConnectionLost)
	v2rExec(t, w.admin, `DROP SCHEMA `+w.a.rec.Schema()+` CASCADE`)
	if err := w.st.resolve(ctx, w.a, graph.Head{}, unknown); kernel.CodeOf(err) != pgconn.CodeOutcomeUnknown {
		t.Fatalf("a head that cannot be read leaves the answer unknown: %v", err)
	}
}

func TestV2cCodecsOfGradesAndKeys(t *testing.T) {
	for i, g := range model.Grades() {
		n, err := gradeNo(g)
		if err != nil || int(n) != i {
			t.Fatalf("grade %s: %d %v", g, n, err)
		}
		if back, err := gradeOf(n); err != nil || back != g {
			t.Fatalf("number %d: %s %v", n, back, err)
		}
	}
	_, err := gradeNo("nothing")
	wantCode(t, "a grade that is not one", err, kernel.CodeInvalidValue)
	_, err = gradeOf(99)
	wantCode(t, "a number that is not one", err, kernel.CodeInternal)
	site, _ := kernel.ParseID[graph.ObjectKind]("obj_00000000000000000000000000000001")
	other, _ := kernel.ParseID[graph.ObjectKind]("obj_00000000000000000000000000000002")
	if got, err := keyJSON(nil); got != nil || err != nil {
		t.Fatalf("no key: %v %v", got, err)
	}
	key := []graph.KeyPart{{Kind: graph.PartContext, Ref: "c", Objects: []graph.ObjectID{site}}, {Kind: graph.PartProperty, Ref: "p", Value: "v"}}
	raw, err := keyJSON(key)
	if err != nil || raw == nil {
		t.Fatalf("key: %v %v", raw, err)
	}
	ctxRef, hash, sig, ok, err := keySignature(key)
	if err != nil || !ok || ctxRef != site.String() || len(hash) != 16 || sig == "" {
		t.Fatalf("signature: %v %v %v %v", ctxRef, ok, sig, err)
	}
	_, hash2, _, _, _ := keySignature([]graph.KeyPart{{Kind: graph.PartContext, Ref: "c", Objects: []graph.ObjectID{other}}, key[1]})
	if string(hash) != string(hash2) {
		t.Fatal("the hash of the key depends on the context object, which is the index's own column")
	}
	for name, k := range map[string][]graph.KeyPart{
		"none":                nil,
		"a set":               {{Kind: graph.PartSet, Ref: "s", Objects: []graph.ObjectID{site, other}}},
		"a context of two":    {{Kind: graph.PartContext, Ref: "c", Objects: []graph.ObjectID{site, other}}},
		"a set with the rest": {key[1], {Kind: graph.PartSet, Ref: "s"}},
	} {
		if _, _, _, ok, err := keySignature(k); ok || err != nil {
			t.Fatalf("%s has a place in the index: %v %v", name, ok, err)
		}
	}
}
