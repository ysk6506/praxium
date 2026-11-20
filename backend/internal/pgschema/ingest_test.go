package pgschema_test

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the tables of the ingestion at scale (ADR-0095). Their names start with Ingest, and the helpers
// with igt, so they never meet a name of another test file of this folder.

func igtPlan(t *testing.T) pgschema.Plan {
	t.Helper()
	plan, err := pgschema.Build(registry(t, smallDefs(t)), pgschema.Options{KGNo: 7, WithStats: true})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func igtHash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func igtHex(b []byte) string { return hex.EncodeToString(b) }

// [REQ-680]
func TestIngestTablesAreInThePlanInTheOrderTheNamesSay(t *testing.T) {
	plan := igtPlan(t)
	got := plan.Tables()
	start := slices.Index(got, pgschema.TableRawRecord)
	if start < 0 || !slices.Equal(got[start:start+len(pgschema.IngestTables())], pgschema.IngestTables()) {
		t.Fatalf("the tables of the plan are %v, and the ingestion tables %v are not in a row", got, pgschema.IngestTables())
	}
	for _, name := range pgschema.IngestTables() {
		sql := igtStep(plan, name)
		if !strings.Contains(sql, `CREATE TABLE "s_7"."`+name+`"`) {
			t.Errorf("%s is not made in the schema of the KG: %q", name, sql)
		}
		if strings.Contains(sql, "UNLOGGED") || strings.Contains(sql, "TEMP") {
			t.Errorf("%s is not a logged table: %s", name, sql)
		}
		if cols := pgschema.ColumnsOf(name); len(cols) == 0 || strings.Count(sql, "\n    ") < len(cols) {
			t.Errorf("%s: the columns %v are not those of %s", name, cols, sql)
		}
	}
	if pgschema.ColumnsOf("no_such_table") != nil {
		t.Error("a table that is not one of the ingestion has columns")
	}
}

func igtStep(plan pgschema.Plan, name string) string {
	for _, s := range plan.Steps {
		if s.What == pgschema.WhatTable && s.Name == name {
			return s.SQL
		}
	}
	return ""
}

// [REQ-680]
func TestIngestTablesThatHaveAPartitionPerSourceAreListPartitionedBySource(t *testing.T) {
	plan := igtPlan(t)
	for _, name := range pgschema.SourceTables() {
		if !strings.Contains(igtStep(plan, name), "PARTITION BY LIST (src_no)") {
			t.Errorf("%s is not partitioned by the source: %s", name, igtStep(plan, name))
		}
	}
	for _, name := range []string{pgschema.TableRecordRef, pgschema.TableRecordWait, pgschema.TableRecordProblem, pgschema.TableCalcRecord, pgschema.TableIngestDiff} {
		if strings.Contains(igtStep(plan, name), "PARTITION") {
			t.Errorf("%s is partitioned", name)
		}
	}
	raw := igtStep(plan, pgschema.TableRawRecord)
	if !strings.Contains(raw, "PRIMARY KEY (src_no, dataset_no, key) INCLUDE (raw_hash, fate)") {
		t.Errorf("the key of the raw records has no hash and fate in its index: %s", raw)
	}
}

// [REQ-680]
func TestIngestProblemTablesRefuseAKindThatIsNotInTheClosedList(t *testing.T) {
	plan := igtPlan(t)
	kinds := pgschema.ProblemKinds()
	seen := map[string]bool{}
	for _, k := range kinds {
		if k == "" || seen[k] {
			t.Fatalf("the list of kinds has %q twice or empty", k)
		}
		seen[k] = true
		for _, table := range []string{pgschema.TableRecordProblem, pgschema.TableCalcProblem} {
			if !strings.Contains(igtStep(plan, table), "'"+k+"'") {
				t.Errorf("%s does not name the kind %s", table, k)
			}
		}
	}
	if !strings.Contains(igtStep(plan, pgschema.TableRecordProblem), "field text NOT NULL") {
		t.Errorf("a problem does not say the field: %s", igtStep(plan, pgschema.TableRecordProblem))
	}
}

// [REQ-680]
func TestIngestIndexesOfTheKeptTablesAreInThePlan(t *testing.T) {
	plan := igtPlan(t)
	want := map[string]string{
		"record_ref_target": `CREATE INDEX "record_ref_target" ON "s_7"."record_ref" (target_no) WHERE role = 1`,
		"record_ref_link":   `CREATE INDEX "record_ref_link" ON "s_7"."record_ref" (target_no) WHERE role = 2`,
		"record_ref_lookup": `CREATE INDEX "record_ref_lookup" ON "s_7"."record_ref" (role, def_no, lookup_hash) WHERE lookup_hash IS NOT NULL`,
		"record_ref_open":   `CREATE INDEX "record_ref_open" ON "s_7"."record_ref" (src_no, dataset_no, key) WHERE state <> 0`,
		"ingest_diff_chunk": `CREATE INDEX "ingest_diff_chunk" ON "s_7"."ingest_diff" (chunk_no, ord)`,
	}
	for name, sql := range want {
		if g := stepSQL(plan, name); g != sql {
			t.Errorf("%s:\n got %s\nwant %s", name, g, sql)
		}
	}
}

// [REQ-680]
func TestIngestDiffFollowsTheStepFromTheGeneratorBeforeByAddingOnly(t *testing.T) {
	plan := igtPlan(t)
	next := plan.Manifest
	next.GeneratorVersion = 3
	old := next
	old.GeneratorVersion = 2
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for _, s := range delta.Steps {
		if s.Phase != pgschema.InTx {
			t.Errorf("a step of the generator runs outside the transaction: %s", s.SQL)
		}
		switch {
		case s.What == pgschema.WhatTable:
			tables = append(tables, s.Name)
		case s.What != pgschema.WhatIndex:
			t.Errorf("the step %s %s is not a table or an index", s.What, s.Name)
		}
		if !strings.HasPrefix(s.SQL, "CREATE ") {
			t.Errorf("the step does more than make: %s", s.SQL)
		}
	}
	if !slices.Equal(tables, pgschema.IngestTables()) {
		t.Errorf("the generator step makes %v, the ingestion tables are %v", tables, pgschema.IngestTables())
	}
	if delta.Result.GeneratorVersion != next.GeneratorVersion {
		t.Errorf("the manifest to keep has the generator %d", delta.Result.GeneratorVersion)
	}
	// every step is also in the plan of a new KG, with the same text.
	for _, s := range delta.Steps {
		if g := stepSQL(plan, s.Name); g != s.SQL {
			t.Errorf("%s differs between a new KG and an adjusted one:\n%s\n%s", s.Name, g, s.SQL)
		}
	}
}

// [REQ-680]
func TestIngestDiffRefusesAGeneratorItCannotFollowByAdding(t *testing.T) {
	next := igtPlan(t).Manifest
	for name, from := range map[string]int{"too old": 1, "newer": pgschema.GeneratorVersion + 1} {
		old := next
		old.GeneratorVersion = from
		if _, err := pgschema.Diff(old, next); err == nil || kernel.CodeOf(err) != pgschema.CodeNotAdditive {
			t.Errorf("%s: %v", name, err)
		}
	}
	// a schema of an older generator is followed only up to this build's generator.
	old, ahead := next, next
	old.GeneratorVersion, ahead.GeneratorVersion = 2, pgschema.GeneratorVersion+1
	if _, err := pgschema.Diff(old, ahead); err == nil || kernel.CodeOf(err) != pgschema.CodeNotAdditive {
		t.Errorf("a generator past this build: %v", err)
	}
}

// [REQ-680]
func TestIngestSourcePartitionsAreMadeThenAttachedAndDropped(t *testing.T) {
	steps, err := pgschema.SourcePartitionSteps(7, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2*len(pgschema.SourceTables()) {
		t.Fatalf("%d steps", len(steps))
	}
	if steps[0].SQL != `CREATE TABLE "s_7"."raw_record_p12" (LIKE "s_7"."raw_record" INCLUDING ALL) WITH (fillfactor = 85, `+
		`autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_insert_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.01)` {
		t.Errorf("the table of the raw records of the source: %s", steps[0].SQL)
	}
	if steps[1].SQL != `ALTER TABLE "s_7"."raw_record" ATTACH PARTITION "s_7"."raw_record_p12" FOR VALUES IN (12)` || steps[1].What != pgschema.WhatPartition {
		t.Errorf("the attach: %s", steps[1].SQL)
	}
	if steps[2].SQL != `CREATE TABLE "s_7"."stage_record_p12" (LIKE "s_7"."stage_record" INCLUDING ALL)` {
		t.Errorf("the table of the staged records of the source: %s", steps[2].SQL)
	}
	drop, err := pgschema.DropSourcePartitionSteps(7, 12)
	if err != nil || len(drop) != len(pgschema.SourceTables()) || drop[0].SQL != `DROP TABLE IF EXISTS "s_7"."raw_record_p12"` {
		t.Fatalf("%v %v", drop, err)
	}
	trunc, err := pgschema.TruncateReceiveSQL(7, 12)
	if err != nil || trunc != `TRUNCATE "s_7"."stage_record_p12", "s_7"."stage_key_p12", "s_7"."stage_end_p12", "s_7"."stage_claim_p12"` {
		t.Fatalf("%q %v", trunc, err)
	}
	for _, name := range []string{"raw_record_p12", "stage_end_p3", "stage_record_p100"} {
		if !pgschema.IsReservedName(name) {
			t.Errorf("%s is not known as the partition of a source", name)
		}
	}
	for _, name := range []string{"raw_record_p", "raw_record_px", "raw_record", "record_ref_p1", "t_1_p1"} {
		if name != "raw_record" && pgschema.IsReservedName(name) {
			t.Errorf("%s is taken for the partition of a source", name)
		}
	}
	if _, err := pgschema.DropSourcePartitionSteps(7, 0); err == nil {
		t.Error("a source of the number 0 is taken off")
	}
	if _, err := pgschema.SourcePartitionSteps(0, 1); err == nil {
		t.Error("a KG of the number 0 has partitions")
	}
	if _, err := pgschema.TruncateReceiveSQL(1, 0); err == nil {
		t.Error("a source of the number 0 is emptied")
	}
	if pgschema.SourcePartition(pgschema.TableStageKey, 3) != "stage_key_p3" {
		t.Error("the name of a partition")
	}
}

// [REQ-680]
func TestIngestComputeTablesAreEmptiedTogetherAndTheirNumbersStartAgain(t *testing.T) {
	sql, err := pgschema.TruncateComputeSQL(7)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sql, "TRUNCATE ") || !strings.HasSuffix(sql, " RESTART IDENTITY") {
		t.Errorf("%s", sql)
	}
	for _, name := range pgschema.ComputeTables() {
		if !strings.Contains(sql, `"s_7"."`+name+`"`) {
			t.Errorf("%s is not emptied", name)
		}
	}
	for _, name := range pgschema.ReceiveTables() {
		if strings.Contains(sql, `"`+name+`"`) {
			t.Errorf("%s of a source is emptied with the computation of the KG", name)
		}
	}
	if _, err := pgschema.TruncateComputeSQL(0); err == nil {
		t.Error("a KG of the number 0")
	}
	if pgschema.TableRef(7, "link") != `"s_7"."link"` {
		t.Error("the reference of a table")
	}
}

// [REQ-681]
func TestIngestDigestLinesHaveAFixedWidthAndFixedBytes(t *testing.T) {
	if pgschema.DiffLineSize != 76 || len(pgschema.DigestTag) != 16 {
		t.Fatalf("the width of a line is %d and of the tag %d", pgschema.DiffLineSize, len(pgschema.DigestTag))
	}
	line := pgschema.DiffLine{Kind: pgschema.DiffKindObject, TypeNo: 3, EntityNo: 258, Change: pgschema.ChangeKindAdded, NewHash: igtHash("a")}
	b, err := line.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := "0000030000000000000102" + "00" + strings.Repeat("00", 32) + "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb"
	if igtHex(b) != want {
		t.Fatalf("the bytes of a line are\n%s\nnot\n%s", igtHex(b), want)
	}
	for name, bad := range map[string]pgschema.DiffLine{
		"kind":   {Kind: 256},
		"change": {Change: -1},
		"type":   {TypeNo: -1},
		"hash":   {OldHash: []byte{1, 2, 3}},
	} {
		if _, err := bad.Encode(); err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// [REQ-681]
func TestIngestDigestOfTheSameLinesIsTheSameValueWhateverTheChunksAreCutAt(t *testing.T) {
	h1, h2 := igtHash("a"), igtHash("b")
	l1 := pgschema.DiffLine{Kind: 0, TypeNo: 3, EntityNo: 258, Change: 0, NewHash: h1}
	l2 := pgschema.DiffLine{Kind: 0, TypeNo: 3, EntityNo: 259, Change: 1, OldHash: h1, NewHash: h2}
	l3 := pgschema.DiffLine{Kind: 2, TypeNo: 12, EntityNo: 1 << 40, Change: 2, OldHash: h2}
	c1, err := pgschema.ChunkDigest([]pgschema.DiffLine{l1, l2})
	if err != nil || igtHex(c1[:]) != "673b84e8c22bce7d94c0365e304e42bd5fafef49032b767dc20d181153cd0e46" {
		t.Fatalf("the first chunk: %x %v", c1, err)
	}
	c2, _ := pgschema.ChunkDigest([]pgschema.DiffLine{l3})
	if igtHex(c2[:]) != "75dedcdc5a3ad5de9fbb6969e1fbf427c7fdeba72a9f38859ff0a2085dac7e11" {
		t.Fatalf("the second chunk: %x", c2)
	}
	var zero [32]byte
	ch1 := pgschema.ChainDigest(zero, c1)
	ch2 := pgschema.ChainDigest(ch1, c2)
	if igtHex(ch1[:]) != "979ddc56127c68ab6182dd6e1aac089aa42ab5296d09e1f47aa6ca4e37939e97" ||
		igtHex(ch2[:]) != "7fde885caee18026590bf53af8f52b83d6683dddc6e44b6bd907bbab9ae28378" {
		t.Fatalf("the chain: %x %x", ch1, ch2)
	}
	whole, err := pgschema.InputDigest([][]pgschema.DiffLine{{l1, l2}, {l3}})
	if err != nil || whole != ch2 {
		t.Fatalf("the digest of the input %x is not the chain %x (%v)", whole, ch2, err)
	}
	// the digest says where the chunks end and in what order the lines are.
	cut, _ := pgschema.InputDigest([][]pgschema.DiffLine{{l1}, {l2, l3}})
	swapped, _ := pgschema.InputDigest([][]pgschema.DiffLine{{l2, l1}, {l3}})
	if cut == whole || swapped == whole {
		t.Error("another cut or another order gives the same digest")
	}
	if _, err := pgschema.InputDigest([][]pgschema.DiffLine{{{Kind: 300}}}); err == nil {
		t.Error("a line out of its width is hashed")
	}
}

// [REQ-681]
func TestIngestKeyRowHashCoversTheTypeTheContextAndTheSignature(t *testing.T) {
	sig := make([]byte, 16)
	for i := range sig {
		sig[i] = byte(i + 1)
	}
	got, err := pgschema.KeyRowHash(7, 99, sig)
	if err != nil || igtHex(got[:]) != "1c7706758507e576fcc79dee695b452534009f316b655691cec24d1723c6b0b9" {
		t.Fatalf("%x %v", got, err)
	}
	if _, err := pgschema.KeyRowHash(7, 99, sig[:15]); err == nil {
		t.Error("a signature of 15 bytes")
	}
	other, _ := pgschema.KeyRowHash(7, 100, sig)
	if other == got {
		t.Error("the context is not in the hash")
	}
}

// [REQ-681]
func TestIngestDigestSQLSpellsTheSameLineAsTheGoCode(t *testing.T) {
	expr := pgschema.DiffLineExpr("d")
	for _, want := range []string{`"d".kind`, `"d".type_no`, `"d".entity_no`, `"d".change_kind`, `"d".old_hash`, `"d".new_hash`} {
		if !strings.Contains(expr, want) {
			t.Errorf("%s is not in %s", want, expr)
		}
	}
	if strings.Contains(pgschema.DiffLineExpr(""), `".`) {
		t.Error("an empty alias still has a prefix")
	}
	q := pgschema.ChunkDigestSQL(7)
	if !strings.Contains(q, `FROM "s_7"."ingest_diff" WHERE chunk_no = $1`) || !strings.Contains(q, "ORDER BY ord") {
		t.Errorf("the query of a chunk: %s", q)
	}
	if !strings.Contains(pgschema.KeyRowHashExpr("k"), `"k".sig_hash`) {
		t.Error("the expression of the hash of a key")
	}
}

// [REQ-682]
func TestIngestRunNamesAreAClosedSet(t *testing.T) {
	stages := pgschema.RunStages()
	if len(stages) != len(pgschema.RunHoldsKG())+len(pgschema.RunOver())+2 {
		t.Errorf("the stages %v are not the work that holds the gate, the work that does not (receiving, awaiting) and the ends", stages)
	}
	for _, s := range append(pgschema.RunHoldsKG(), pgschema.RunOver()...) {
		if !slices.Contains(stages, s) {
			t.Errorf("%s is not a stage", s)
		}
	}
	if slices.Contains(pgschema.RunHoldsKG(), pgschema.RunAwaitingConfirmation) {
		t.Error("a run that waits for a confirmation holds the gate of the KG")
	}
	if len(pgschema.EndClasses()) != 3 {
		t.Errorf("a run fails in three ways, not %v", pgschema.EndClasses())
	}
	for _, cols := range [][]string{pgschema.SyncRunColumns(), pgschema.SourceStateColumns(), pgschema.SourceDatasetColumns()} {
		seen := map[string]bool{}
		for _, c := range cols {
			if seen[c] {
				t.Errorf("the column %s is twice in %v", c, cols)
			}
			seen[c] = true
		}
	}
}

// [REQ-680]
func TestIngestHeldDiffFromGeneratorThreeAddsTheColumnHeldToTheThreeTablesOfTheRowsReceivedAndNothingElse(t *testing.T) {
	next := igtPlan(t).Manifest
	old := next
	old.GeneratorVersion = 3
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"stage_key.held":   `ALTER TABLE "s_7"."stage_key" ADD COLUMN IF NOT EXISTS held boolean NOT NULL DEFAULT false`,
		"stage_end.held":   `ALTER TABLE "s_7"."stage_end" ADD COLUMN IF NOT EXISTS held boolean NOT NULL DEFAULT false`,
		"stage_claim.held": `ALTER TABLE "s_7"."stage_claim" ADD COLUMN IF NOT EXISTS held boolean NOT NULL DEFAULT false`,
	}
	if len(delta.Steps) != len(want) {
		t.Fatalf("%d steps", len(delta.Steps))
	}
	for _, s := range delta.Steps {
		if s.Phase != pgschema.InTx || s.What != pgschema.WhatColumn || want[s.Name] != s.SQL {
			t.Errorf("unexpected step %+v", s)
		}
	}
	if delta.Result.GeneratorVersion != pgschema.GeneratorVersion {
		t.Errorf("the generator to keep is %d", delta.Result.GeneratorVersion)
	}
	for _, table := range []string{"stage_key", "stage_end", "stage_claim"} {
		if !strings.Contains(stepSQL(igtPlan(t), table), "held boolean NOT NULL DEFAULT false") {
			t.Errorf("a new KG has no column held in %s", table)
		}
	}
	if strings.Contains(stepSQL(igtPlan(t), "stage_record"), "held") {
		t.Error("the table of the records has a column held")
	}
}
