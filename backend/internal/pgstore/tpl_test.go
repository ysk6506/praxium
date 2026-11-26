package pgstore

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the templates of KGs that the tests of other units copy (ADR-0120). Every helper they add starts with tpl, so that
// none of them meets a name of another test file of this folder.

func tplKG(t *testing.T, n int) graph.KG {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	id, err := kernel.ParseID[graph.KGKind](fmt.Sprintf("kg-%016x", 0x7e00+n))
	if err != nil {
		t.Fatal(err)
	}
	return graph.KG{ID: id, Name: fmt.Sprintf("tpl%d", n), Role: graph.RoleTest, DefinitionSetHash: reg.Hash(), CreatedAt: time.Unix(2000, 0)}
}

// tplSecond makes a second KG on a database, as a test's own step on top of the template would.
func tplSecond(t *testing.T) func(dsn string) error {
	return func(dsn string) error {
		models, err := StandardModels(false)
		if err != nil {
			return err
		}
		return makeTestKG(dsn, models, tplKG(t, 2))
	}
}

// [REQ-270]
func TestTplACopyOfAKGTemplateIsTheDatabaseThatOpenAndCreateKGMakeTheSlowWay(t *testing.T) {
	pgconn.TestInParallel(t)
	ctx := context.Background()
	kg := tplKG(t, 1)
	for _, c := range []struct {
		stats   bool
		thenKey string
		then    func(string) error
	}{{true, "", nil}, {false, "a second KG", tplSecond(t)}} {
		models, err := testKGModels(c.stats)
		if err != nil {
			t.Fatal(err)
		}
		fresh := NewTestDatabase(t)
		if err := makeTestKG(fresh, models, kg); err != nil {
			t.Fatal(err)
		}
		if c.then != nil {
			if err := c.then(fresh); err != nil {
				t.Fatal(err)
			}
		}
		copied, made := NewTestDatabaseWithKG(t, c.stats, kg, c.thenKey, c.then)
		want, err := pgconn.DumpTestDatabase(ctx, fresh)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgconn.DumpTestDatabase(ctx, copied)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("stats %v: the copy differs from the database made the slow way:\n%s", c.stats, tplDiffLines(want, got))
		}
		joined := strings.Join(want, "\n")
		if !strings.Contains(joined, "row public.kg_catalog") || !strings.Contains(joined, "planner statistics rows 0") ||
			strings.Contains(joined, "statistics object ") != c.stats || strings.Contains(joined, "\nview ") {
			t.Errorf("stats %v: the dump does not show the KG, the empty statistics and the statistics objects: %d lines", c.stats, len(want))
		}
		st, err := Open(ctx, Config{DSN: copied, Models: models})
		if err != nil {
			t.Fatal(err)
		}
		read, err := st.GetKG(ctx, kg.ID.String())
		_ = st.Close()
		if err != nil {
			t.Fatal(err)
		}
		if read.ID != made.ID || read.Name != made.Name || read.Role != made.Role || read.State != made.State || read.Generation != made.Generation ||
			!read.CreatedAt.Equal(made.CreatedAt) || read.DefinitionSetHash != made.DefinitionSetHash {
			t.Errorf("the KG given back %+v is not the one in the copy %+v", made, read)
		}
	}
}

// Two copies of one template each make a KG of the model of a server, with its query schema and its roles, and delete it: the
// roles are named by the token of the installation, which every copy has of its own (ADR-0113).
//
// [REQ-270]
func TestTplTwoCopiesOfATemplateMakeAndDeleteKGsWithAQuerySchemaWithoutMeetingTheirRoles(t *testing.T) {
	pgconn.TestInParallel(t)
	ctx := context.Background()
	server, err := StandardModels(true)
	if err != nil {
		t.Fatal(err)
	}
	var stores []*Store
	for range 2 {
		dsn, _ := NewTestDatabaseWithKG(t, false, tplKG(t, 1), "", nil)
		st, err := Open(ctx, Config{DSN: dsn, Models: server})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		stores = append(stores, st)
	}
	for i, st := range stores {
		kg, err := st.CreateKG(ctx, tplKG(t, 9))
		if err != nil {
			t.Fatalf("copy %d: %v", i, err)
		}
		defer func() {
			if err := st.DeleteKG(ctx, kg.ID); err != nil {
				t.Errorf("copy %d: the KG with a query schema cannot be deleted: %v", i, err)
			}
		}()
	}
}

// [REQ-270]
func TestTplTheKeyOfAKGTemplateChangesWithEverythingThatDecidesWhatTheTemplateHolds(t *testing.T) {
	std, err := testKGModels(true)
	if err != nil {
		t.Fatal(err)
	}
	light, err := testKGModels(false)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	noSearch := func(kernel.Hash) (SchemaModel, error) {
		return SchemaModel{Registry: reg, WithStats: true, noQuery: true}, nil
	}
	kg := tplKG(t, 1)
	key := func(models ModelProvider, kg graph.KG, then string) string {
		t.Helper()
		k, err := testKGKey(models, kg, then)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	base := key(std, kg, "then")
	if base != key(std, tplKG(t, 1), "then") {
		t.Fatal("the same KG, model and step give two keys")
	}
	if !strings.HasPrefix(base, pgconn.TestMigrationsKey()) || !strings.Contains(base, fmt.Sprintf("GeneratorVersion:%d ", pgschema.GeneratorVersion)) ||
		!strings.Contains(base, reg.Hash().String()) {
		t.Errorf("the key leaves out the migrations, the generator version or the model: %.300s", base)
	}
	other := func(change func(*graph.KG)) graph.KG {
		k := kg
		change(&k)
		return k
	}
	base2, err := kernel.ParseID[graph.KGKind]("kg-00000000000000ff")
	if err != nil {
		t.Fatal(err)
	}
	for name, k := range map[string]string{
		"statistics objects": key(light, kg, "then"),
		"search set":         key(noSearch, kg, "then"),
		"step":               key(std, kg, "then and more"),
		"id":                 key(std, tplKG(t, 3), "then"),
		"name":               key(std, other(func(k *graph.KG) { k.Name = "other" }), "then"),
		"role":               key(std, other(func(k *graph.KG) { k.Role = graph.RoleProduction }), "then"),
		"state":              key(std, other(func(k *graph.KG) { k.State = graph.StateBuilding }), "then"),
		"base":               key(std, other(func(k *graph.KG) { k.BaseKGID = base2 }), "then"),
		"time":               key(std, other(func(k *graph.KG) { k.CreatedAt = k.CreatedAt.Add(time.Microsecond) }), "then"),
	} {
		if k == base {
			t.Errorf("a template of another %s has the same key", name)
		}
	}
	if _, err := testKGKey(func(kernel.Hash) (SchemaModel, error) {
		return SchemaModel{}, kernel.Errorf(kernel.CodeNotFound, "no model")
	}, kg, ""); err == nil {
		t.Error("a key without a model")
	}
	// a KG of another definition set is not made from the standard model: there is no key, as CreateKG would refuse it
	if _, err := testKGKey(std, other(func(k *graph.KG) { k.DefinitionSetHash = kernel.HashBytes([]byte("other")) }), ""); err == nil {
		t.Error("a key for a KG of another definition set")
	}
	// the model of a server makes a query schema, whose roles belong to the cluster: it has no template
	server, err := StandardModels(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testKGKey(server, kg, ""); err == nil || !strings.Contains(err.Error(), "no query schema") {
		t.Errorf("a key for a model that makes a query schema: %v", err)
	}
	if !strings.Contains(base, "model: statistics objects true, query schema false") ||
		!strings.Contains(key(light, kg, "then"), "model: statistics objects false, query schema false") {
		t.Error("the key does not name the model")
	}
}

// [REQ-270]
func TestTplAKGTemplateThatCannotBeMadeFailsTheTestAndIsNotMadeAnotherWay(t *testing.T) {
	bad := tplKG(t, 4)
	bad.Name = ""
	f := &failRecorder{}
	if dsn, _ := NewTestDatabaseWithKG(f, false, bad, "", nil); dsn != "" || !f.failed || !strings.Contains(f.message, "cannot make the template database") ||
		!strings.Contains(f.message, "a KG needs an id, a name and a role") {
		t.Fatalf("a KG that CreateKG refuses: %q failed=%v %q", dsn, f.failed, f.message)
	}
	f = &failRecorder{}
	other := tplKG(t, 8)
	other.DefinitionSetHash = kernel.HashBytes([]byte("another model"))
	if dsn, _ := NewTestDatabaseWithKG(f, false, other, "", nil); dsn != "" || !f.failed || !strings.Contains(f.message, "not the model of the template") {
		t.Fatalf("a KG of another model: %q failed=%v %q", dsn, f.failed, f.message)
	}
	f = &failRecorder{}
	if dsn, _ := NewTestDatabaseWithKG(f, false, tplKG(t, 5), "a step that fails", func(string) error {
		return kernel.Errorf(kernel.CodeConflict, "the step failed")
	}); dsn != "" || !f.failed || !strings.Contains(f.message, "the step failed") {
		t.Fatalf("a step that fails: %q failed=%v %q", dsn, f.failed, f.message)
	}
	models, err := StandardModels(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := makeTestKG("host=/nonexistent-socket-dir user=u sslmode=disable connect_timeout=2", models, tplKG(t, 6)); err == nil {
		t.Error("a KG was made on a server that cannot be reached")
	}
	t.Setenv(TestDSNEnv, "")
	f = &failRecorder{}
	if dsn, _ := NewTestDatabaseWithKG(f, false, tplKG(t, 7), "", nil); dsn != "" || !f.failed || !strings.Contains(f.message, "with-postgres.sh") {
		t.Fatalf("without the server: %q failed=%v %q", dsn, f.failed, f.message)
	}
}

// tplDiffLines is the lines that are in one dump and not in the other.
func tplDiffLines(want, got []string) string {
	in := map[string]int{}
	for _, l := range want {
		in[l]++
	}
	for _, l := range got {
		in[l]--
	}
	var out []string
	for _, l := range append(append([]string(nil), want...), got...) {
		switch n := in[l]; {
		case n > 0:
			out = append(out, "- "+l)
		case n < 0:
			out = append(out, "+ "+l)
		}
		in[l] = 0
	}
	return strings.Join(out, "\n")
}
