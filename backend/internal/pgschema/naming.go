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
)

// FixedTables lists the fixed tables in the order they are created.
func FixedTables() []string {
	return []string{
		TableObject, TableObjectState, TableLink, TableObjectHist, TableLinkHist, TableObjectKey,
		TableChangeBatch, TableChangeEntry, TableLightChange, TableObjectIdentifier, TableIdentityEvent, TableIngestOrder,
	}
}

// The names that the plan keeps for the tables of an apply that works from staged rows. The tables are made
// by a later change; the manifest knows these names, so a check of the schema does not take them for strangers.
const (
	ReservedPrefixStage = "stage_"
	ReservedPrefixCalc  = "calc_"
	ReservedIngestDiff  = "ingest_diff"
)

// IsReservedName reports whether a table name is one that a later change will make for the staged apply.
func IsReservedName(name string) bool {
	return strings.HasPrefix(name, ReservedPrefixStage) || strings.HasPrefix(name, ReservedPrefixCalc) || name == ReservedIngestDiff
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
