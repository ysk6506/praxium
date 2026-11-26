package pgschema

import "strings"

// generatorAdditions are what a generator version added to the fixed tables of a storage schema: the steps that
// make them in a schema that an older version made. A version that changed a table that was already there, or
// took one away, has no entry, and a schema of an older version is not followed past it (Diff refuses it).
var generatorAdditions = map[int]func(schema string, old Manifest) ([]Step, error){
	3: func(schema string, _ Manifest) ([]Step, error) { return ingestSteps(schema), nil },
	4: func(schema string, _ Manifest) ([]Step, error) { return heldSteps(schema), nil },
	5: valueSteps,
	6: func(schema string, _ Manifest) ([]Step, error) { return rawDiffSteps(schema), nil },
	7: func(schema string, _ Manifest) ([]Step, error) { return identifySteps(schema), nil },
	8: func(schema string, _ Manifest) ([]Step, error) { return expandSteps(schema), nil },
}

// oldestFollowed is the oldest generator version whose schemas a newer version follows. Version 1 was made by a
// build that no release carries.
const oldestFollowed = 2

// fixedAdditions is the steps that bring a schema described by old up to the generator version to, which may not be
// past the one this build has. The same version needs none. A step may need what the schema has (the types of old), so
// the steps of a version are made from the manifest of the schema as it was.
func fixedAdditions(schema string, old Manifest, to int) ([]Step, error) {
	from := old.GeneratorVersion
	if from == to {
		return nil, nil
	}
	if from < oldestFollowed || from > to || to > GeneratorVersion {
		return nil, notAdditive("the generator changed from version %d to %d", from, to)
	}
	var steps []Step
	for v := from + 1; v <= to; v++ {
		add, ok := generatorAdditions[v]
		if !ok {
			return nil, notAdditive("the generator version %d changed what the version %d made", v, v-1)
		}
		made, err := add(schema, old)
		if err != nil {
			return nil, err
		}
		steps = append(steps, made...)
	}
	return steps, nil
}

// ingestSteps makes the tables of the ingestion at scale and their indexes (generator version 3).
func ingestSteps(schema string) []Step {
	var steps []Step
	for _, d := range ingestTableDefs() {
		steps = append(steps, Step{Phase: InTx, What: WhatTable, Name: d.Name, SQL: d.sql(schema)})
	}
	for _, d := range ingestIndexDefs() {
		steps = append(steps, Step{Phase: InTx, What: WhatIndex, Name: d.Name, SQL: d.sql(schema, false)})
	}
	return steps
}

// heldTables are the tables of the rows received that gained the column held in generator version 4.
func heldTables() []string { return []string{TableStageKey, TableStageEnd, TableStageClaim} }

// heldSteps adds the column held to the tables of the keys, the ends and the claims that were received (generator version 4):
// a schema of version 3 has the tables without it. A schema that was made by the steps of version 3 of this build has the
// column already, so the step adds it only when it is missing. The partitions that sources already have get it with the table.
func heldSteps(schema string) []Step {
	var steps []Step
	for _, t := range heldTables() {
		steps = append(steps, Step{Phase: InTx, What: WhatColumn, Name: t + "." + ColHeld,
			SQL: "ALTER TABLE " + Qualified(schema, t) + " ADD COLUMN IF NOT EXISTS " + ColHeld + " " + typeHeld})
	}
	return steps
}

// valueSteps is what version 5 adds to a schema of version 4 (ADR-0101): the name indexes of the tables of the types, built
// outside the transaction, and for each table of values one statement that moves the values that the older write kept in prov
// into their columns and takes them (and the old key of the time) out of prov. The statement leaves a row that has no entry with
// a value as it is, so a row that was moved is not moved again.
func valueSteps(schema string, old Manifest) ([]Step, error) {
	var steps []Step
	moved, err := moveValuesSteps(schema, old)
	if err != nil {
		return nil, err
	}
	steps = append(steps, moved...)
	for _, t := range old.Types {
		steps = append(steps, NameIndexSteps(schema, t.TypeNo)...)
	}
	return steps, nil
}

// NameIndexSteps are the steps that build the name indexes of the table of a type that has rows, outside a transaction
// and without blocking the writers; an index that is there is left as it is.
func NameIndexSteps(schema string, typeNo int) []Step {
	var steps []Step
	for _, d := range typeIndexDefs(typeNo) {
		steps = append(steps, Step{Phase: Concurrent, What: WhatIndex, Name: d.Name, SQL: d.sql(schema, true)})
	}
	return steps
}

// rawDiffSteps is what generator version 6 adds for the comparison of the received rows with the raw records (ADR-0102): a
// wait that says its time is up, the place of the row a record of the computation was read from, and a problem that may have no
// definition, and the index of the rows received by key. A schema made by this build has them already, so each step is made to be run twice, and a schema that has no such table (a KG that was not made with the ingestion tables) is left as it is.
func rawDiffSteps(schema string) []Step {
	alter := func(table, column, what string) Step {
		return Step{Phase: InTx, What: WhatColumn, Name: table + "." + column,
			SQL: "ALTER TABLE IF EXISTS " + Qualified(schema, table) + " " + what}
	}
	return []Step{
		alter(TableRecordWait, ColExpired, "ADD COLUMN IF NOT EXISTS "+ColExpired+" "+typeExpired),
		alter(TableCalcRecord, ColPage, "ADD COLUMN IF NOT EXISTS "+ColPage+" integer"),
		alter(TableCalcRecord, ColRowNo, "ADD COLUMN IF NOT EXISTS "+ColRowNo+" integer"),
		alter(TableRecordProblem, ColDefNo, "ALTER COLUMN "+ColDefNo+" DROP NOT NULL"),
		alter(TableCalcProblem, ColDefNo, "ALTER COLUMN "+ColDefNo+" DROP NOT NULL"),
		{Phase: InTx, What: WhatTable, Name: TableCalcOwner,
			SQL: "DO $$ BEGIN IF to_regclass('" + Qualified(schema, TableCalcRecord) + "') IS NOT NULL THEN " +
				strings.Replace(calcOwnerDef().sql(schema), "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1) + "; END IF; END $$"},
		{Phase: InTx, What: WhatIndex, Name: "stage_record_key",
			SQL: "DO $$ BEGIN IF to_regclass('" + Qualified(schema, TableStageRecord) + "') IS NOT NULL THEN CREATE INDEX IF NOT EXISTS " + QuoteIdent("stage_record_key") +
				" ON " + Qualified(schema, TableStageRecord) + " (" + stageRecordKeyIndex().Expr + "); END IF; END $$"},
	}
}

// identifySteps is what generator version 7 adds for the identification of the records of a run (ADR-0108): the column held of the
// keys, the ends and the claims of the computation, the feed of a bound record, the four working tables and the indexes. A schema made
// by this build has them already, so each step is made to be run twice, and a schema that has no such table is left as it is.
func identifySteps(schema string) []Step {
	alter := func(table, column, what string) Step {
		return Step{Phase: InTx, What: WhatColumn, Name: table + "." + column,
			SQL: "ALTER TABLE IF EXISTS " + Qualified(schema, table) + " ADD COLUMN IF NOT EXISTS " + column + " " + what}
	}
	steps := []Step{
		alter(TableCalcKey, ColHeld, typeHeld),
		alter(TableCalcEnd, ColHeld, typeHeld),
		alter(TableCalcClaim, ColHeld, typeHeld),
		alter(TableCalcBind, ColFeedNo, "integer NOT NULL DEFAULT 0"),
	}
	for _, d := range identifyTableDefs() {
		steps = append(steps, Step{Phase: InTx, What: WhatTable, Name: d.Name,
			SQL: strings.Replace(d.sql(schema), "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1)})
	}
	for _, d := range identifyIndexDefs() {
		steps = append(steps, Step{Phase: InTx, What: WhatIndex, Name: d.Name,
			SQL: "DO $$ BEGIN IF to_regclass('" + Qualified(schema, d.Table) + "') IS NOT NULL THEN " +
				createIfNotExists(d.sql(schema, false)) + "; END IF; END $$"})
	}
	return steps
}

// expandSteps is what generator version 8 adds for the expansion of the set of a run (ADR-0118): the table of the objects that are held to
// retire for good (an object that a feed that retires made is retired once nothing stands under it: the old flow's "once on, always on"),
// and the table of the keys the expansion has read. Both are tables made beside the others: no table that a reader may have open is
// altered. A schema made by this build has them already, so each step is made to be run twice.
func expandSteps(schema string) []Step {
	var steps []Step
	for _, d := range append([]tableDef{objectRetireDef()}, expandTableDefs()...) {
		steps = append(steps, Step{Phase: InTx, What: WhatTable, Name: d.Name,
			SQL: strings.Replace(d.sql(schema), "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1)})
	}
	return steps
}

// createIfNotExists is the statement of an index made to be run again.
func createIfNotExists(sql string) string {
	i := strings.Index(sql, "INDEX ")
	return sql[:i+len("INDEX ")] + "IF NOT EXISTS " + sql[i+len("INDEX "):]
}
