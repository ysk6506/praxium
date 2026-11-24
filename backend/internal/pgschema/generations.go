package pgschema

// generatorAdditions are what a generator version added to the fixed tables of a storage schema: the steps that
// make them in a schema that an older version made. A version that changed a table that was already there, or
// took one away, has no entry, and a schema of an older version is not followed past it (Diff refuses it).
var generatorAdditions = map[int]func(schema string, old Manifest) ([]Step, error){
	3: func(schema string, _ Manifest) ([]Step, error) { return ingestSteps(schema), nil },
	4: func(schema string, _ Manifest) ([]Step, error) { return heldSteps(schema), nil },
	5: valueSteps,
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
