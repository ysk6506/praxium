package pgstore

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// More tests of the catalog, the read points and the refusals of the new storage. Their names start with v2e.

func TestV2eTheTestStoreMakesKGsAndTheCatalogFindsThemByIdNameAndMain(t *testing.T) {
	st, ctx := NewTestStoreV2(t), context.Background()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	clock := kernel.NewManualClock(time.Unix(2000, 0))
	prodID, err := kernel.NewID[graph.KGKind](ids, "kg")
	noErr(t, "id", err)
	prod, err := st.CreateKG(ctx, graph.KG{ID: prodID, Name: "prod", Role: graph.RoleProduction, DefinitionSetHash: fx.Reg.Hash(), CreatedAt: clock.Now()})
	noErr(t, "production", err)
	cand := graph.KG{Name: "cand", Role: graph.RoleCandidate, BaseKGID: prod.ID, DefinitionSetHash: fx.Reg.Hash(), CreatedAt: clock.Now()}
	cand.ID, err = kernel.NewID[graph.KGKind](ids, "kg")
	noErr(t, "id", err)
	cand, err = st.CreateKG(ctx, cand)
	noErr(t, "candidate", err)
	for _, ref := range []string{"main", "prod", prod.ID.String()} {
		got, err := st.GetKG(ctx, ref)
		if err != nil || got.ID != prod.ID || got.State != graph.StateBuilding {
			t.Fatalf("%s: %+v %v", ref, got, err)
		}
	}
	got, err := st.GetKG(ctx, "cand")
	if err != nil || got.BaseKGID != prod.ID || got.Role != graph.RoleCandidate {
		t.Fatalf("candidate %+v %v", got, err)
	}
	list, err := st.ListKGs(ctx)
	if err != nil || len(list) != 2 || list[0].ID != prod.ID {
		t.Fatalf("list %v %v", list, err)
	}
	_, err = st.GetKG(ctx, "nothing")
	wantCode(t, "unknown", err, graph.CodeKGNotFound)
	other := cand
	other.ID, other.Name = prod.ID, "again"
	_, err = st.CreateKG(ctx, other)
	wantCode(t, "id taken", err, kernel.CodeConflict)
	other.ID, other.DefinitionSetHash = cand.ID, kernel.Hash{9}
	_, err = st.CreateKG(ctx, other)
	wantCode(t, "another definition set", err, kernel.CodeInvalidValue)
	noErr(t, "delete", st.DeleteKG(ctx, cand.ID))
	if list, _ = st.ListKGs(ctx); len(list) != 1 {
		t.Fatalf("list after the delete: %v", list)
	}
	_, err = OpenV2(ctx, Config{DSN: "x"}, V2Options{})
	wantCode(t, "no models", err, kernel.CodeInvalidValue)
}

func TestV2eTheCatalogRefusesDamagedRowsAndAnUnknownModel(t *testing.T) {
	w := v2rWorldOf(t)
	ctx := context.Background()
	models := func(kernel.Hash) (SchemaModel, error) {
		return SchemaModel{}, kernel.Errorf(kernel.CodeNotFound, "no model")
	}
	fresh, err := OpenV2(ctx, Config{DSN: w.dsn}, V2Options{Models: models})
	noErr(t, "open", err)
	defer fresh.Close()
	_, err = fresh.Head(ctx, w.kg.ID)
	wantCode(t, "a model the server does not hold", err, kernel.CodeNotFound)
	_, err = fresh.CreateKG(ctx, graph.KG{ID: w.kg.ID, Name: "x", Role: graph.RoleTest})
	wantCode(t, "create with no model", err, kernel.CodeNotFound)

	std, err := StandardModels(false)
	noErr(t, "models", err)
	damage := func(set string) *StoreV2 {
		v2rExec(t, w.admin, `UPDATE kg_catalog SET `+set)
		st, err := OpenV2(ctx, Config{DSN: w.dsn}, V2Options{Models: std})
		noErr(t, "open", err)
		t.Cleanup(func() { _ = st.Close() })
		return st
	}
	_, err = damage(`base_kg_id = 'not an id'`).Head(ctx, w.kg.ID)
	if err == nil {
		t.Fatal("a base KG that is no id")
	}
	v2rExec(t, w.admin, `UPDATE kg_catalog SET base_kg_id = ''`)
	_, err = damage(`definition_set_hash = '\x01'`).Head(ctx, w.kg.ID)
	if err == nil {
		t.Fatal("a definition hash of the wrong length")
	}
	v2rExec(t, w.admin, `UPDATE kg_catalog SET definition_set_hash = $1`, w.kg.DefinitionSetHash[:])
	_, err = damage(`kg_id = 'not an id'`).ListKGs(ctx)
	if err == nil {
		t.Fatal("a KG id that is no id")
	}
}

func TestV2eReadPointsAndVersions(t *testing.T) {
	w := v2rWorldOf(t)
	w.v2rSeed(t)
	ctx := context.Background()
	v2rExec(t, w.admin, `INSERT INTO `+w.a.t(pgschema.TableVersion)+` (version_no, version_id, sealed_seq, generation, prev_version_id, definition_set_hash, engine_version,
		hash_rule_version, input_digest, graph_hash, row_count, sealed_at, seal_kind) VALUES (1,'ver_one',3,1,'',$1,1,1,$1,$1,3,now(),'sync')`, make([]byte, 32))
	v2rExec(t, w.admin, `INSERT INTO `+w.a.t(pgschema.TableVersionHashPart)+` (version_no, entity_kind, type_id, partial_sum, row_count) VALUES (1,'object','t',$1,3)`, make([]byte, 32))
	vs, err := w.st.Versions(ctx, w.kg.ID)
	if err != nil || len(vs) != 1 || vs[0].SealedSeq != 3 || len(vs[0].Parts) != 1 || vs[0].Prev.String() != "" {
		t.Fatalf("versions %+v %v", vs, err)
	}
	v2rExec(t, w.admin, `INSERT INTO `+w.a.t(pgschema.TableVersion)+` (version_no, version_id, sealed_seq, generation, prev_version_id, definition_set_hash, engine_version,
		hash_rule_version, input_digest, graph_hash, row_count, sealed_at, seal_kind) VALUES (2,'ver_two',4,1,'ver_one',$1,1,1,$1,$1,2,now(),'sync')`, make([]byte, 32))
	if vs, err = w.st.Versions(ctx, w.kg.ID); err != nil || len(vs) != 2 || vs[1].Prev != vs[0].ID {
		t.Fatalf("two versions %+v %v", vs, err)
	}
	sn := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadVersion, Version: vs[0].ID})
	if sn.Info().Seq != 3 || sn.Info().Version != vs[0].ID || sn.Info().Volatile != graph.VolatileOmitted {
		t.Fatalf("version read %+v", sn.Info())
	}
	applied := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadApplied})
	if applied.Info().Seq != 4 || applied.Info().Volatile != graph.VolatileLatest {
		t.Fatalf("applied read %+v", applied.Info())
	}
	missing, _ := kernel.ParseID[graph.VersionKind]("ver_nothing")
	_, err = w.st.Snapshot(ctx, graph.ReadPoint{KG: w.kg.ID, Kind: graph.ReadVersion, Version: missing})
	wantCode(t, "no such version", err, graph.CodeKGNotFound)
	v2rExec(t, w.admin, `UPDATE `+w.a.t(pgschema.TableVersion)+` SET prev_version_id = 'not an id' WHERE version_no = 2`)
	if _, err = w.st.Versions(ctx, w.kg.ID); err == nil {
		t.Fatal("a version that points at no id")
	}
	v2rExec(t, w.admin, `UPDATE `+w.a.t(pgschema.TableVersion)+` SET prev_version_id = '', graph_hash = '\x01' WHERE version_no = 2`)
	if _, err = w.st.Versions(ctx, w.kg.ID); err == nil {
		t.Fatal("a graph hash of the wrong length")
	}
	v2rExec(t, w.admin, `UPDATE `+w.a.t(pgschema.TableVersion)+` SET version_id = 'x y' WHERE version_no = 2`)
	if _, err = w.st.Versions(ctx, w.kg.ID); err == nil {
		t.Fatal("a version with no id")
	}
}

func TestV2eAServerThatHoldsAnotherModelRefusesTheKG(t *testing.T) {
	w := v2rWorldOf(t)
	ctx := context.Background()
	std, err := StandardModels(false)
	noErr(t, "models", err)
	other := func(h kernel.Hash) (SchemaModel, error) {
		m, err := std(h)
		m.Search = pgschema.NewSearchSet()
		return m, err
	}
	st, err := OpenV2(ctx, Config{DSN: w.dsn}, V2Options{Models: other})
	noErr(t, "open", err)
	defer st.Close()
	_, err = st.Snapshot(ctx, graph.ReadPoint{KG: w.kg.ID, Kind: graph.ReadLatest})
	wantCode(t, "snapshot", err, CodeSchemaMismatch)
	_, err = st.Snapshot(ctx, graph.ReadPoint{KG: graph.KGID{}, Kind: graph.ReadLatest})
	wantCode(t, "no KG", err, graph.CodeKGNotFound)
}

func TestV2eALeaseAndAHeadThatAreDamagedFail(t *testing.T) {
	w := v2rWorldOf(t)
	w.v2rSeed(t)
	ctx, kg := context.Background(), w.kg.ID
	l, err := v2cLease(t, w.st, kg, "a", time.Time{}, time.Hour, 1)
	noErr(t, "acquire", err)
	ops := v2dOps{
		"acquire": func() error { _, err := v2cLease(t, w.st, kg, "a", time.Time{}, time.Hour, 1); return err },
		"renew":   func() error { _, err := w.st.RenewLease(ctx, l, time.Hour, time.Time{}); return err },
		"release": func() error { return w.st.ReleaseLease(ctx, l) },
		"head":    func() error { _, err := w.st.Head(ctx, kg); return err },
		"read": func() error {
			_, _, err := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest}).Object(ctx, w.srv)
			return err
		},
	}
	v2dDamage(t, w, pgschema.TableLease, ops, "acquire", "renew", "release")
	v2dDamage(t, w, pgschema.TableHead, ops, "acquire", "renew", "head")
	for _, set := range []string{`hash_sum = '\x01'`, `hash_parts = '{"a":1}'`, `input_digest = '\x01'`, `rollback = '"x"'`} {
		v2rExec(t, w.admin, `UPDATE `+w.a.t(pgschema.TableHead)+` SET `+set)
		if err := ops["head"](); err == nil {
			t.Errorf("a head with %s: no error", set)
		}
		v2rExec(t, w.admin, `UPDATE `+w.a.t(pgschema.TableHead)+` SET hash_sum = $1, hash_parts = '[]', input_digest = $1, rollback = NULL`, make([]byte, 32))
	}
	v2rExec(t, w.admin, `UPDATE `+w.a.t(pgschema.TableHead)+` SET work = 'restoring'`)
	if _, err := w.st.GetKG(ctx, "v2r"); err != nil {
		t.Fatalf("a KG that is being restored is in the catalog: %v", err)
	}
}
