package pgschema

// generatorAdditions are what a generator version added to the fixed tables of a storage schema: the steps that
// make them in a schema that an older version made. A version that changed a table that was already there, or
// took one away, has no entry, and a schema of an older version is not followed past it (Diff refuses it).
var generatorAdditions = map[int]func(schema string) []Step{
	3: ingestSteps,
	4: heldSteps,
	5: noSteps, // the version of another change (ADR-0101) that is not here yet; it adds nothing to what this build has
	6: rawDiffSteps,
}

// oldestFollowed is the oldest generator version whose schemas a newer version follows. Version 1 was made by a
// build that no release carries.
const oldestFollowed = 2

// fixedAdditions is the steps that bring the fixed tables of a schema of the generator version from up to the
// version to, which may not be past the one this build has. The same version needs none.
func fixedAdditions(schema string, from, to int) ([]Step, error) {
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
		steps = append(steps, add(schema)...)
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

// noSteps is a generator version that added nothing to the fixed tables.
func noSteps(string) []Step { return nil }

// rawDiffSteps is what generator version 6 adds for the comparison of the received rows with the raw records (ADR-0102): a
// wait that says its time is up, the place of the row a record of the computation was read from, and a problem that may have no
// definition, and the index of the rows received by key. A schema made by this build has them already, so each step is made to be run twice.
func rawDiffSteps(schema string) []Step {
	alter := func(table, column, what string) Step {
		return Step{Phase: InTx, What: WhatColumn, Name: table + "." + column,
			SQL: "ALTER TABLE " + Qualified(schema, table) + " " + what}
	}
	return []Step{
		alter(TableRecordWait, ColExpired, "ADD COLUMN IF NOT EXISTS "+ColExpired+" "+typeExpired),
		alter(TableCalcRecord, ColPage, "ADD COLUMN IF NOT EXISTS "+ColPage+" integer"),
		alter(TableCalcRecord, ColRowNo, "ADD COLUMN IF NOT EXISTS "+ColRowNo+" integer"),
		alter(TableRecordProblem, ColDefNo, "ALTER COLUMN "+ColDefNo+" DROP NOT NULL"),
		alter(TableCalcProblem, ColDefNo, "ALTER COLUMN "+ColDefNo+" DROP NOT NULL"),
		{Phase: InTx, What: WhatIndex, Name: "stage_record_key",
			SQL: "CREATE INDEX IF NOT EXISTS " + QuoteIdent("stage_record_key") + " ON " + Qualified(schema, TableStageRecord) + " (" + stageRecordKeyIndex().Expr + ")"},
	}
}
