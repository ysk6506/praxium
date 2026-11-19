package pgschema

import (
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// WhatPartition is a step that attaches a table to the partitioned table it is a partition of.
const WhatPartition = "partition"

// rawPartitionWith are the storage parameters of the partition of a source in raw_record. A partitioned table
// has none of its own, so each partition is given them when it is made.
const rawPartitionWith = fillTypeTable + ", " + vacuumBig

func checkSourceNos(kgNo, srcNo int64) error {
	if kgNo < 1 || srcNo < 1 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the number of the KG and of the source must be 1 or more, not %d and %d", kgNo, srcNo)
	}
	return nil
}

// SourcePartitionSteps are the statements that put a source to a KG: for each table that has a partition per
// source, a table of its own that is made like it and then attached. Making the table first and attaching it
// takes a lock on the partitioned table that lets the other sources go on reading and writing, which making the
// partition in one statement does not. They run in one transaction, before the first run of the source (a run
// does no DDL), and they are the same on a second call: IF NOT EXISTS makes the table, and an attach of a table
// that is attached already is left out by the caller (the executor checks the partitions it finds).
func SourcePartitionSteps(kgNo, srcNo int64) ([]Step, error) {
	if err := checkSourceNos(kgNo, srcNo); err != nil {
		return nil, err
	}
	schema, n := SchemaName(kgNo), strconv.FormatInt(srcNo, 10)
	var steps []Step
	for _, parent := range SourceTables() {
		part := SourcePartition(parent, srcNo)
		with := ""
		if parent == TableRawRecord {
			with = " WITH (" + rawPartitionWith + ")"
		}
		steps = append(steps,
			Step{Phase: InTx, What: WhatTable, Name: part,
				SQL: "CREATE TABLE " + Qualified(schema, part) + " (LIKE " + Qualified(schema, parent) + " INCLUDING ALL)" + with},
			Step{Phase: InTx, What: WhatPartition, Name: part,
				SQL: "ALTER TABLE " + Qualified(schema, parent) + " ATTACH PARTITION " + Qualified(schema, part) + " FOR VALUES IN (" + n + ")"})
	}
	return steps, nil
}

// DropSourcePartitionSteps are the statements that take a source off a KG: its raw records and its staged rows go
// with it. The records the source's runs made (record_ref, record_wait, record_problem) are rows of tables that
// are not partitioned and are deleted by the caller, in chunks.
func DropSourcePartitionSteps(kgNo, srcNo int64) ([]Step, error) {
	if err := checkSourceNos(kgNo, srcNo); err != nil {
		return nil, err
	}
	schema := SchemaName(kgNo)
	var steps []Step
	for _, parent := range SourceTables() {
		part := SourcePartition(parent, srcNo)
		steps = append(steps, Step{Phase: InTx, What: WhatTable, Name: part, SQL: "DROP TABLE IF EXISTS " + Qualified(schema, part)})
	}
	return steps, nil
}

// TruncateReceiveSQL empties what a source was received into: the four staged tables of its partition. It is the
// start of a receiving that begins again from the first page, the end of a cleaning, and a discard.
func TruncateReceiveSQL(kgNo, srcNo int64) (string, error) {
	if err := checkSourceNos(kgNo, srcNo); err != nil {
		return "", err
	}
	schema := SchemaName(kgNo)
	names := make([]string, 0, len(ReceiveTables()))
	for _, t := range ReceiveTables() {
		names = append(names, Qualified(schema, SourcePartition(t, srcNo)))
	}
	return "TRUNCATE " + strings.Join(names, ", "), nil
}

// TruncateComputeSQL empties the tables of the computation of a KG and starts the numbers of the rows again.
func TruncateComputeSQL(kgNo int64) (string, error) {
	if err := checkSourceNos(kgNo, 1); err != nil {
		return "", err
	}
	schema := SchemaName(kgNo)
	names := make([]string, 0, len(ComputeTables()))
	for _, t := range ComputeTables() {
		names = append(names, Qualified(schema, t))
	}
	return "TRUNCATE " + strings.Join(names, ", ") + " RESTART IDENTITY", nil
}

// TableRef is the name of a table of a KG, with the schema, quoted, for a statement that reads or writes it.
func TableRef(kgNo int64, table string) string { return Qualified(SchemaName(kgNo), table) }
