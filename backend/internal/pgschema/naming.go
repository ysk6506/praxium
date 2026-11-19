package pgschema

import (
	"strconv"
	"strings"
)

// MaxIdentBytes is the longest name PostgreSQL keeps (NAMEDATALEN minus one).
const MaxIdentBytes = 63

// The fixed tables of a storage schema, besides the tables of the types.
const (
	TableObject           = "object"
	TableObjectState      = "object_state"
	TableLink             = "link"
	TableObjectHist       = "object_hist"
	TableLinkHist         = "link_hist"
	TableObjectKey        = "object_key"
	TableChangeBatch      = "change_batch"
	TableChangeEntry      = "change_entry"
	TableLightChange      = "light_change"
	TableObjectIdentifier = "object_identifier"
	TableIdentityEvent    = "identity_event"
	TableIngestOrder      = "ingest_order"
	// The control tables of a KG (ADR-0089): the head, the lease, the generations and the sealed versions. They
	// are in the storage schema, so a KG stands on its own and a transaction of the applier never leaves it.
	TableHead            = "head"
	TableLease           = "lease"
	TableGenerationEvent = "generation_event"
	TableVersion         = "version"
	TableVersionHashPart = "version_hash_part"
)

// FixedTables lists the fixed tables in the order they are created.
func FixedTables() []string {
	base := []string{
		TableObject, TableObjectState, TableLink, TableObjectHist, TableLinkHist, TableObjectKey,
		TableChangeBatch, TableChangeEntry, TableLightChange, TableObjectIdentifier, TableIdentityEvent, TableIngestOrder,
		TableHead, TableLease, TableGenerationEvent, TableVersion, TableVersionHashPart,
	}
	return append(base, IngestTables()...)
}

// The names that the plan keeps for the tables of an apply that works from staged rows (the tables themselves are
// in ingest_names.go). A name of this prefix that is not in the plan is a table of a later version, and the
// manifest knows it, so a check of the schema does not take it for a stranger.
const (
	ReservedPrefixStage = "stage_"
	ReservedPrefixCalc  = "calc_"
	ReservedIngestDiff  = "ingest_diff"
	// ReservedPrefixPartition is the prefix of the partitions of the tables that only grow (the history and the
	// change records), which a later change may cut by change number (ADR-0089 decision 6). Nothing makes them yet.
	ReservedPrefixPartition = "part_"
)

// IngestTables lists the tables of the ingestion at scale in the order they are created: the permanent ones, the
// ones a source is received into, and the ones of a computation.
func IngestTables() []string {
	out := []string{TableRawRecord, TableRecordWait, TableRecordRef, TableRecordProblem}
	out = append(out, ReceiveTables()...)
	return append(out, ComputeTables()...)
}

// IsReservedName reports whether a table name is one that a later change will make: for the staged apply or for
// a partition of a table that only grows.
func IsReservedName(name string) bool {
	return isSourcePartition(name) || strings.HasPrefix(name, ReservedPrefixStage) || strings.HasPrefix(name, ReservedPrefixCalc) ||
		strings.HasPrefix(name, ReservedPrefixPartition) || name == ReservedIngestDiff
}

// isSourcePartition reports whether a name is the partition of a source of one of the tables that have one.
func isSourcePartition(name string) bool {
	for _, t := range SourceTables() {
		if rest, ok := strings.CutPrefix(name, t+"_p"); ok && rest != "" && strings.Trim(rest, "0123456789") == "" {
			return true
		}
	}
	return false
}

// SchemaName is the storage schema of a KG.
func SchemaName(kgNo int64) string { return "s_" + strconv.FormatInt(kgNo, 10) }

// TypeTable is the table of the objects of a type.
func TypeTable(typeNo int) string { return "t_" + strconv.Itoa(typeNo) }

// StateTable is the table of the volatile values that only some types have.
func StateTable(typeNo int) string { return "s_" + strconv.Itoa(typeNo) }

// StateColumn is the column that holds the volatile value of an attribute.
func StateColumn(attrNo int) string { return "a" + strconv.Itoa(attrNo) }

// StateColumnAt is the column that holds the time the source saw the volatile value.
func StateColumnAt(attrNo int) string { return "a" + strconv.Itoa(attrNo) + "_at" }

// SearchIndex is the name of the index on the value of an attribute in the table of a type.
func SearchIndex(typeNo, attrNo int) string { return TypeTable(typeNo) + "_a" + strconv.Itoa(attrNo) }

// StatsName is the name of a statistics object of a type; first is the lowest attribute number in it.
func StatsName(typeNo, first int) string { return TypeTable(typeNo) + "_st_" + strconv.Itoa(first) }

// QuoteIdent quotes a name. Every name the plan writes goes through it, and the names are built from numbers
// and fixed words, so nothing from outside ever reaches a statement.
func QuoteIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// Qualified is schema.name with both quoted.
func Qualified(schema, name string) string { return QuoteIdent(schema) + "." + QuoteIdent(name) }
