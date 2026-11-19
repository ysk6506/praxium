package pgstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-565]
func TestTheStorageSchemaOfAKGHasTheTablesOfThePlanAndNoIndexThatWasNotMarked(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	m := sxStandard(t, true)
	kg := sxKG(t, 1)
	rec, err := st.CreateStorageSchema(ctx, kg, m)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(m, rec.KGNo)
	if err != nil {
		t.Fatal(err)
	}
	if got := sxTables(t, admin, rec.Schema()); got != len(plan.Tables()) {
		t.Fatalf("%d tables, the plan has %d", got, len(plan.Tables()))
	}
	if sxInvalid(t, admin, rec.Schema()) != 0 || rec.State != "ok" {
		t.Fatalf("invalid indexes or a state that is not ok: %q", rec.State)
	}
	want, _ := plan.Manifest.Hash()
	got, err := rec.Manifest.Hash()
	if err != nil || got != want {
		t.Fatalf("the stored manifest has the hash %v, the plan %v (%v)", got, want, err)
	}
	if rec.KGNo != 1 || rec.Schema() != "s_1" || rec.RegistryHash != m.Registry.Hash().String() {
		t.Fatalf("the record is %+v", rec)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_statistic_ext e JOIN pg_namespace n ON n.oid = e.stxnamespace WHERE n.nspname = $1`, rec.Schema()); n == 0 || n != statsOf(plan) {
		t.Fatalf("%d statistics objects, the plan has %d", n, statsOf(plan))
	}
	marked := map[string]bool{}
	for _, e := range m.Search {
		marked[pgschema.SearchIndex(e.TypeNo, e.AttrNo)] = true
	}
	found := 0
	rows, err := admin.QueryContext(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind = 'i' AND c.relname ~ '^t_[0-9]+_a[0-9]+$'`, rec.Schema())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !marked[name] {
			t.Errorf("the index %s was not marked", name)
		}
		found++
	}
	if found != len(marked) {
		t.Fatalf("%d indexes of the search set, %d marked", found, len(marked))
	}
	for _, name := range []string{"object_state_a4", "object_state_a7"} {
		if n := sxInt(t, admin, `SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname = $2`, rec.Schema(), name); n != 1 {
			t.Errorf("the index %s of the common state is missing", name)
		}
	}
	read, err := st.StorageSchema(ctx, kg)
	if err != nil || read.KGNo != rec.KGNo || read.AppliedAt.IsZero() {
		t.Fatalf("read back %+v: %v", read, err)
	}
}

func statsOf(p pgschema.Plan) int {
	n := 0
	for _, s := range p.Steps {
		if s.What == pgschema.WhatStatistics {
			n++
		}
	}
	return n
}

func TestCreatingAStorageSchemaTwiceOrAtTheSameTimeLeavesOneSchema(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	m := sxSmall(t, sxSearch(1, 10, 1, 12))
	kg := sxKG(t, 7)
	var wg sync.WaitGroup
	recs := make([]SchemaRecord, 4)
	errs := make([]error, 4)
	for i := range recs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recs[i], errs[i] = st.CreateStorageSchema(ctx, kg, m)
		}()
	}
	wg.Wait()
	for i := range recs {
		if errs[i] != nil {
			t.Fatalf("run %d: %v", i, errs[i])
		}
		if recs[i].KGNo != recs[0].KGNo || !recs[i].AppliedAt.Equal(recs[0].AppliedAt) {
			t.Fatalf("run %d got %+v, run 0 %+v", i, recs[i], recs[0])
		}
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM kg_schema_state`); n != 1 {
		t.Fatalf("%d records", n)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname ~ '^s_[0-9]+$'`); n != 1 {
		t.Fatalf("%d storage schemas", n)
	}
	if last := sxInt(t, admin, `SELECT last_value FROM kg_no_seq`); last != 1 {
		t.Fatalf("the sequence is at %d: a run that found the schema took a number", last)
	}
	before := sxSnapshot(t, admin, recs[0].Schema())
	again, err := st.CreateStorageSchema(ctx, kg, m)
	if err != nil || sxSnapshot(t, admin, recs[0].Schema()) != before || again.KGNo != recs[0].KGNo {
		t.Fatalf("a third call changed something: %v", err)
	}
	_, err = st.CreateStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10, 1, 12), "type"))
	if kernel.CodeOf(err) != kernel.CodeConflict || !strings.Contains(err.Error(), "was not made from") {
		t.Fatalf("another model for a schema that exists: %v", err)
	}
	if sxSnapshot(t, admin, recs[0].Schema()) != before {
		t.Fatal("the refusal changed the database")
	}
}

func TestAStorageSchemaThatFailsHalfWayLeavesNoSchemaAndNoRecord(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	m := sxSmall(t, sxSearch(1, 10))
	whole, err := buildPlan(m, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []int{1, len(whole.Steps) / 2, len(whole.Steps) - 1} {
		broken := func(m SchemaModel, no int64) (pgschema.Plan, error) {
			p, err := buildPlan(m, no)
			p.Steps[at].SQL = "CREATE TABLE (("
			return p, err
		}
		kg := sxKG(t, 100+at)
		if _, err := st.createStorageSchema(ctx, kg, m, broken); err == nil {
			t.Fatalf("a broken step %d went through", at)
		}
		if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname ~ '^s_[0-9]+$'`); n != 0 {
			t.Fatalf("step %d: %d storage schemas are left", at, n)
		}
		if n := sxInt(t, admin, `SELECT count(*) FROM kg_schema_state`); n != 0 {
			t.Fatalf("step %d: %d records are left", at, n)
		}
	}
	failing := func(SchemaModel, int64) (pgschema.Plan, error) {
		return pgschema.Plan{}, kernel.Errorf(kernel.CodeInternal, "no plan")
	}
	if _, err := st.createStorageSchema(ctx, sxKG(t, 5), m, failing); err == nil {
		t.Fatal("a plan that failed made a schema")
	}
	late := func(m SchemaModel, no int64) (pgschema.Plan, error) {
		p, err := buildPlan(m, no)
		p.Steps[0].Phase = pgschema.Concurrent
		return p, err
	}
	if _, err := st.createStorageSchema(ctx, sxKG(t, 6), m, late); err == nil {
		t.Fatal("a step of the wrong phase ran")
	}
	if _, err := st.CreateStorageSchema(ctx, sxKG(t, 8), m); err != nil {
		t.Fatalf("a good one after the broken ones: %v", err)
	}
}

func TestCreatingRefusesWhatItCannotMakeASchemaFrom(t *testing.T) {
	ctx := context.Background()
	st, _ := sxOpen(t)
	var none graph.KGID
	if _, err := st.CreateStorageSchema(ctx, none, sxSmall(t, nil)); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("no id: %v", err)
	}
	if _, err := st.CreateStorageSchema(ctx, sxKG(t, 1), SchemaModel{}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("no registry: %v", err)
	}
	if _, err := st.CreateStorageSchema(ctx, sxKG(t, 1), sxSmall(t, sxSearch(1, 14))); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("a list in the search set: %v", err)
	}
	if _, err := st.StorageSchema(ctx, sxKG(t, 1)); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("a KG without a schema: %v", err)
	}
	if _, err := st.AdjustStorageSchema(ctx, sxKG(t, 1), sxSmall(t, nil)); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("adjusting a KG without a schema: %v", err)
	}
	if _, err := st.FinishStorageStats(ctx, sxKG(t, 1), sxSmall(t, nil)); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("statistics of a KG without a schema: %v", err)
	}
	if _, err := st.AdjustStorageSchema(ctx, sxKG(t, 1), SchemaModel{}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("adjusting with no registry: %v", err)
	}
}

// [REQ-565]
func TestAMoreCompleteModelIsFollowedByAdditionsAndTheTablesThatHadRowsAreNotRewritten(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	kg := sxKG(t, 3)
	rec, err := st.CreateStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10)))
	if err != nil {
		t.Fatal(err)
	}
	schema := rec.Schema()
	for i := 1; i <= 50; i++ {
		sxRow(t, admin, schema, "t_1", i, fmt.Sprintf(`{"10":{"v":"SN-%d"},"11":{"v":%d},"12":{"v":"2030-01-01T00:00:%02dZ"}}`, i, i, i%60))
	}
	files := func() string {
		return sxText(t, admin, `SELECT string_agg(relname || relfilenode, ',' ORDER BY relname) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND relname IN ('t_1', 's_1', 'object', 'link')`, schema)
	}
	before := files()
	next := sxSmall(t, sxSearch(1, 10, 1, 11, 1, 12), "type", "serverstate", "rackstate")
	got, err := st.AdjustStorageSchema(ctx, kg, next)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "ok" {
		t.Fatalf("state %q", got.State)
	}
	for _, name := range []string{"t_3", "s_2"} {
		if sxInt(t, admin, `SELECT count(*) FROM pg_tables WHERE schemaname = $1 AND tablename = $2`, schema, name) != 1 {
			t.Errorf("the table %s was not made", name)
		}
	}
	if sxInt(t, admin, `SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = 's_1' AND column_name = 'a15'`, schema) != 1 {
		t.Error("the column of the new volatile attribute was not added")
	}
	for _, name := range []string{"t_1_a10", "t_1_a11", "t_1_a12"} {
		if sxInt(t, admin, `SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname = $2`, schema, name) != 1 {
			t.Errorf("the index %s is missing", name)
		}
	}
	if sxInvalid(t, admin, schema) != 0 {
		t.Error("an index is invalid")
	}
	if files() != before {
		t.Fatalf("a table was rewritten: %s, then %s", before, files())
	}
	if err := st.CheckStorageSchema(ctx, kg, next); err != nil {
		t.Fatalf("the schema does not match the model it was adjusted to: %v", err)
	}
	fresh, err := buildPlan(next, rec.KGNo)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := fresh.Manifest.Hash()
	if have, _ := got.Manifest.Hash(); have != want {
		t.Fatal("an adjusted schema has another manifest than a new one")
	}
	same := sxSnapshot(t, admin, schema)
	if _, err := st.AdjustStorageSchema(ctx, kg, next); err != nil || sxSnapshot(t, admin, schema) != same {
		t.Fatalf("adjusting to the same model changed something: %v", err)
	}
	dropped, err := st.AdjustStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10, 1, 12), "type", "serverstate", "rackstate"))
	if err != nil || dropped.State != "ok" {
		t.Fatalf("taking an attribute out of the search set: %v", err)
	}
	if sxInt(t, admin, `SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname = 't_1_a11'`, schema) != 0 {
		t.Error("the index of an attribute that is not searched any more is still there")
	}
}

func TestAChangeThatIsNotAnAdditionIsRefusedAndTheDatabaseIsAsItWas(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	kg := sxKG(t, 4)
	rec, err := st.CreateStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10)))
	if err != nil {
		t.Fatal(err)
	}
	before := sxSnapshot(t, admin, rec.Schema())
	for name, m := range map[string]SchemaModel{
		"a value kind changed": sxSmall(t, sxSearch(1, 10), "kind"),
		"a type is gone":       sxSmall(t, sxSearch(1, 10), "notype"),
	} {
		_, err := st.AdjustStorageSchema(ctx, kg, m)
		if kernel.CodeOf(err) != pgschema.CodeNotAdditive {
			t.Errorf("%s: %v", name, err)
		}
		if sxSnapshot(t, admin, rec.Schema()) != before {
			t.Errorf("%s: the database changed", name)
		}
	}
	if err := st.CheckStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10))); err != nil {
		t.Fatal(err)
	}
}

func TestAnIndexThatCannotBeBuiltLeavesTheStateIndexPendingAndTheNextRunFinishesIt(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	kg := sxKG(t, 9)
	rec, err := st.CreateStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10)))
	if err != nil {
		t.Fatal(err)
	}
	schema := rec.Schema()
	sxRow(t, admin, schema, "t_1", 1, `{"10":{"v":"a"},"11":{"v":"not a number"}}`)
	more := sxSmall(t, sxSearch(1, 10, 1, 11))
	if _, err := st.AdjustStorageSchema(ctx, kg, more); err == nil {
		t.Fatal("an index on text that is not a number was built")
	}
	if state := sxText(t, admin, `SELECT state FROM kg_schema_state`); state != "index_pending" {
		t.Fatalf("the state is %q", state)
	}
	if sxInvalid(t, admin, schema) != 1 {
		t.Fatal("the half built index is not there")
	}
	sxRun(t, admin, fmt.Sprintf(`UPDATE %s SET doc = '{"10":{"v":"a"},"11":{"v":3}}'`, pgschema.Qualified(schema, "t_1")))
	done, err := st.AdjustStorageSchema(ctx, kg, more)
	if err != nil || done.State != "ok" {
		t.Fatalf("the next run: %q, %v", done.State, err)
	}
	if sxInvalid(t, admin, schema) != 0 || sxText(t, admin, `SELECT state FROM kg_schema_state`) != "ok" || sxText(t, admin, `SELECT COALESCE(online_since::text, '')  FROM kg_schema_state`) != "" {
		t.Fatal("the schema is not clean")
	}
}

func TestARunThatStoppedWhileBuildingLeavesInvalidAndStrayIndexesThatTheNextRunCleansUp(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	kg := sxKG(t, 11)
	m := sxSmall(t, sxSearch(1, 10, 1, 12))
	rec, err := st.CreateStorageSchema(ctx, kg, m)
	if err != nil {
		t.Fatal(err)
	}
	schema := rec.Schema()
	table := pgschema.Qualified(schema, "t_1")
	sxRun(t, admin, fmt.Sprintf(`UPDATE pg_index SET indisvalid = false WHERE indexrelid = '%s'::regclass`, pgschema.Qualified(schema, "t_1_a10")))
	sxRun(t, admin, fmt.Sprintf(`DROP INDEX %s`, pgschema.Qualified(schema, "t_1_a12")))
	sxRun(t, admin, fmt.Sprintf(`CREATE INDEX t_1_a11 ON %s ((doc->'11'->>'v'))`, table))
	sxRun(t, admin, `UPDATE kg_schema_state SET state = 'building', online_since = now()`)
	if _, err := st.AdjustStorageSchema(ctx, kg, m); kernel.CodeOf(err) != kernel.CodeConflict || !strings.Contains(err.Error(), "another run") {
		t.Fatalf("a run that is building: %v", err)
	}
	sxRun(t, admin, `UPDATE kg_schema_state SET online_since = now() - interval '2 hours'`)
	got, err := st.AdjustStorageSchema(ctx, kg, m)
	if err != nil || got.State != "ok" {
		t.Fatalf("taking over a run that said nothing for two hours: %q, %v", got.State, err)
	}
	if sxInvalid(t, admin, schema) != 0 {
		t.Fatal("an invalid index is left")
	}
	for name, want := range map[string]int{"t_1_a10": 1, "t_1_a12": 1, "t_1_a11": 0} {
		if n := sxInt(t, admin, `SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname = $2`, schema, name); n != want {
			t.Errorf("%s: %d, wanted %d", name, n, want)
		}
	}
}

func TestStatisticsThatWereLeftForLaterAreMadeOnceAndAnAdjustmentWaitsForThem(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	kg := sxKG(t, 12)
	m := sxSmall(t, sxSearch(1, 10))
	m.WithStats = false
	rec, err := st.CreateStorageSchema(ctx, kg, m)
	if err != nil || rec.State != "stats_pending" {
		t.Fatalf("%q, %v", rec.State, err)
	}
	count := func() int {
		return sxInt(t, admin, `SELECT count(*) FROM pg_statistic_ext e JOIN pg_namespace n ON n.oid = e.stxnamespace WHERE n.nspname = $1`, rec.Schema())
	}
	if count() != 0 {
		t.Fatal("statistics were made")
	}
	if _, err := st.AdjustStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10), "type")); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Fatalf("adjusting before the statistics: %v", err)
	}
	other := sxSmall(t, sxSearch(1, 10), "type")
	if _, err := st.FinishStorageStats(ctx, kg, other); kernel.CodeOf(err) != CodeSchemaMismatch {
		t.Fatalf("statistics for another model: %v", err)
	}
	done, err := st.FinishStorageStats(ctx, kg, m)
	if err != nil || done.State != "ok" || count() == 0 {
		t.Fatalf("%q, %d, %v", done.State, count(), err)
	}
	n := count()
	if again, err := st.FinishStorageStats(ctx, kg, m); err != nil || again.State != "ok" || count() != n {
		t.Fatalf("a second time: %v", err)
	}
}

func TestAServerWhoseModelIsNotTheOneTheSchemaWasMadeFromIsTold(t *testing.T) {
	ctx := context.Background()
	st, _ := sxOpen(t)
	kg := sxKG(t, 13)
	m := sxSmall(t, sxSearch(1, 10))
	rec, err := st.CreateStorageSchema(ctx, kg, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSchemaMatch(rec, m); err != nil {
		t.Fatalf("the model it was made from: %v", err)
	}
	for name, other := range map[string]SchemaModel{
		"another model":      sxSmall(t, sxSearch(1, 10), "type"),
		"another search set": sxSmall(t, sxSearch(1, 10, 1, 12)),
	} {
		err := st.CheckStorageSchema(ctx, kg, other)
		if kernel.CodeOf(err) != CodeSchemaMismatch {
			t.Errorf("%s: %v", name, err)
		}
	}
	err = CheckSchemaMatch(SchemaRecord{KG: kg, GeneratorVersion: pgschema.GeneratorVersion + 1, RegistryHash: rec.RegistryHash, NumberingHash: rec.NumberingHash, SearchHash: rec.SearchHash}, m)
	if kernel.CodeOf(err) != CodeSchemaMismatch || !strings.Contains(err.Error(), "generator version") {
		t.Fatalf("another generator: %v", err)
	}
	if err := CheckSchemaMatch(rec, SchemaModel{}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("no registry: %v", err)
	}
	if err := st.CheckStorageSchema(ctx, sxKG(t, 99), m); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("no schema: %v", err)
	}
}

func TestDroppingAStorageSchemaRemovesItAndItsNumberIsNotGivenAgain(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	m := sxSmall(t, sxSearch(1, 10))
	first, err := st.CreateStorageSchema(ctx, sxKG(t, 20), m)
	if err != nil {
		t.Fatal(err)
	}
	keep, err := st.CreateStorageSchema(ctx, sxKG(t, 21), m)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DropStorageSchema(ctx, sxKG(t, 20)); err != nil {
		t.Fatal(err)
	}
	if sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, first.Schema()) != 0 ||
		sxInt(t, admin, `SELECT count(*) FROM kg_schema_state WHERE kg_id = $1`, sxKG(t, 20).String()) != 0 {
		t.Fatal("the schema or its record is still there")
	}
	if sxTables(t, admin, keep.Schema()) == 0 {
		t.Fatal("the other KG lost its tables")
	}
	if err := st.DropStorageSchema(ctx, sxKG(t, 20)); err != nil {
		t.Fatalf("dropping what is gone: %v", err)
	}
	again, err := st.CreateStorageSchema(ctx, sxKG(t, 20), m)
	if err != nil || again.KGNo <= keep.KGNo || again.KGNo == first.KGNo {
		t.Fatalf("the number %d was given again (first %d, other %d): %v", again.KGNo, first.KGNo, keep.KGNo, err)
	}
}

func TestTheIndexOfTheSearchSetIsTheOneTheQueryPlannerChooses(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	kg := sxKG(t, 30)
	rec, err := st.CreateStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10, 1, 11, 1, 12)))
	if err != nil {
		t.Fatal(err)
	}
	table := pgschema.Qualified(rec.Schema(), "t_1")
	sxRun(t, admin, fmt.Sprintf(`INSERT INTO %s (obj_no, ref, name, min_grade, doc, content_hash, row_hash, seq_from)
		SELECT g, 'ref-' || g, 'n', 0,
		  jsonb_build_object('10', jsonb_build_object('v', 'SN-' || g), '11', jsonb_build_object('v', g),
		    '12', jsonb_build_object('v', to_char(timestamp '2030-01-01' + g * interval '1 second', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))),
		  '\x00', '\x00', 1 FROM generate_series(1, 30000) g`, table))
	sxRun(t, admin, `ANALYZE `+table)
	cpus, _ := pgschema.Expr("doc", 11, "integer")
	booted, _ := pgschema.Expr("doc", 12, "time")
	serial, _ := pgschema.Expr("doc", 10, "string")
	for index, where := range map[string]string{
		"t_1_a10": serial + ` = 'SN-777'`,
		"t_1_a11": cpus + ` = 777`,
		"t_1_a12": booted + ` = '2030-01-01T00:12:57Z'::timestamptz`,
	} {
		plan := sxText(t, admin, `EXPLAIN (FORMAT TEXT) SELECT obj_no FROM `+table+` WHERE `+where)
		if !strings.Contains(plan, index) {
			t.Errorf("the plan of %s does not use %s:\n%s", where, index, plan)
		}
	}
}
