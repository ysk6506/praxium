package pgschema

// generatorAdditions are what a generator version added to the fixed tables of a storage schema: the steps that
// make them in a schema that an older version made. A version that changed a table that was already there, or
// took one away, has no entry, and a schema of an older version is not followed past it (Diff refuses it).
var generatorAdditions = map[int]func(schema string) []Step{
	3: ingestSteps,
	4: heldSteps,
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
