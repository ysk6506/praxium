package pgstore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the query schema of the standard model (ADR-0113). Their names start with Qsc.

// [REQ-574]
func TestQscTheQuerySchemaOfTheStandardModelHasAViewForEveryTypeAndEveryLinkTypeAndTheTimeToBuildItIsKnown(t *testing.T) {
	st, admin := sxOpen(t)
	m := sxStandard(t, false)
	start := qscClock(t, admin)
	rec, err := st.CreateStorageSchema(context.Background(), sxKG(t, 1), m)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("a KG of the standard model with its query schema is made in %v", qscClock(t, admin).Sub(start))
	open := pgschema.QuerySchemaName(rec.KGNo, model.SensitivityOpen, false)
	has := func(name string) bool {
		return sxInt(t, admin, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind = 'v'`, open, name) == 1
	}
	for _, v := range []string{"object", "link", "attribute", "identifier", "model_type", "model_attribute", "model_link_type", "model_link_rule"} {
		if !has(v) {
			t.Errorf("no view %s", v)
		}
	}
	for _, ot := range m.Registry.ObjectTypes() {
		if name := pgschema.TypeViewName(ot.Name); !has(name) {
			t.Errorf("no view for the type %s (%s)", ot.Name, name)
		}
	}
	for _, lt := range m.Registry.LinkTypes() {
		if name := pgschema.LinkViewName(lt.Name); !has(name) {
			t.Errorf("no view for the link type %s (%s)", lt.Name, name)
		}
	}
	for view, want := range map[string]int{"model_type": len(m.Registry.ObjectTypes()), "model_link_type": len(m.Registry.LinkTypes())} {
		if n := sxInt(t, admin, `SELECT count(*) FROM `+pgschema.QuoteIdent(open)+`.`+view); n != want {
			t.Errorf("%s has %d rows, the model has %d", view, n, want)
		}
	}
	for _, view := range []string{"model_attribute", "model_link_rule"} {
		if sxInt(t, admin, `SELECT count(*) FROM `+pgschema.QuoteIdent(open)+`.`+view) == 0 {
			t.Errorf("%s is empty", view)
		}
	}
}

// [REQ-561] [REQ-562]
func TestQscTheTimeToBuildAllTheViewsAgainIsMeasuredByKindOfStatement(t *testing.T) {
	_, admin := sxOpen(t)
	ctx := context.Background()
	m := sxStandard(t, false)
	m.token = sxText(t, admin, `SELECT token FROM install_token`)
	plan, err := buildPlan(m, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Query) == 0 || len(plan.Roles) != 3 {
		t.Fatalf("the plan has %d steps of the query schema and %d roles", len(plan.Query), len(plan.Roles))
	}
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, s := range plan.Steps {
		if _, err := tx.ExecContext(ctx, s.SQL); err != nil {
			t.Fatalf("%s %s: %v", s.What, s.Name, err)
		}
	}
	if err := ensureQueryRoles(ctx, tx, plan.Roles); err != nil {
		t.Fatal(err)
	}
	byKind := map[string]time.Duration{}
	start := qscClock(t, tx)
	for _, s := range plan.Query {
		t0 := qscClock(t, tx)
		if _, err := tx.ExecContext(ctx, s.SQL); err != nil {
			t.Fatalf("%s %s: %v", s.What, s.Name, err)
		}
		byKind[s.What] += qscClock(t, tx).Sub(t0)
	}
	t.Logf("all the views of the standard model are built in %v (%d statements); by kind: %v", qscClock(t, tx).Sub(start), len(plan.Query), byKind)
	var views int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname LIKE 'q\_5\_%' AND c.relkind = 'v'`).Scan(&views); err != nil {
		t.Fatal(err)
	}
	start = qscClock(t, tx)
	for _, s := range plan.Query {
		if _, err := tx.ExecContext(ctx, s.SQL); err != nil {
			t.Fatalf("again %s %s: %v", s.What, s.Name, err)
		}
	}
	t.Logf("and built again over the ones that are there in %v", qscClock(t, tx).Sub(start))
	var after int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname LIKE 'q\_5\_%' AND c.relkind = 'v'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != views {
		t.Errorf("building the query schema twice left %d views, the first time %d", after, views)
	}
}

// qscClock is the clock of the database server (clock_timestamp advances inside a transaction), so a test measures a time without
// reading the clock of the process.
func qscClock(t *testing.T, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}) time.Time {
	t.Helper()
	var now time.Time
	if err := q.QueryRowContext(context.Background(), `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	return now
}
