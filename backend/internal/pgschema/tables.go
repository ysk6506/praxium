package pgschema

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The settings that every table gets. Types and the tables that grow with the rows get a fill factor only
// where rows are updated in place; the cleaning (autovacuum) is told to start long before a fifth of a table
// is dead, which at thirty million rows would be six million. The values are a first guess and are checked
// against a measurement of a large KG.
const (
	fillTypeTable  = "fillfactor = 85"
	fillStateTable = "fillfactor = 70"
	vacuumBig      = "autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_insert_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.01"
	vacuumState    = "autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000, autovacuum_analyze_scale_factor = 0.02"
)

// The codes of the small enumerations that are kept as numbers in the change records, where a row is many.
const (
	EntityKindObject int16 = 0
	EntityKindLink   int16 = 1

	ChangeKindAdded   int16 = 0
	ChangeKindChanged int16 = 1
	ChangeKindRetired int16 = 2
)

// tableDef is one CREATE TABLE.
type tableDef struct {
	Name string
	Cols []string
	With string
	// PartitionBy is the column a table is cut into partitions by, one list partition for each value. A
	// partitioned table has no storage parameters of its own; its partitions are given them when they are made.
	PartitionBy string
}

func (d tableDef) sql(schema string) string {
	var b strings.Builder
	b.WriteString("CREATE TABLE " + Qualified(schema, d.Name) + " (\n    ")
	b.WriteString(strings.Join(d.Cols, ",\n    "))
	b.WriteString("\n)")
	if d.PartitionBy != "" {
		b.WriteString(" PARTITION BY LIST (" + d.PartitionBy + ")")
	}
	if d.With != "" {
		b.WriteString(" WITH (" + d.With + ")")
	}
	return b.String()
}

// indexDef is one CREATE INDEX on a table of the schema.
type indexDef struct {
	Name   string
	Table  string
	Expr   string // the key: columns or expressions, as they go between the parentheses
	Unique bool
	Where  string
	// Method is the kind of index when it is not a B-tree (brin).
	Method string
	// Include are the columns the index carries so that a read of them needs no table (INCLUDE).
	Include string
}

func (d indexDef) sql(schema string, concurrently bool) string {
	var b strings.Builder
	b.WriteString("CREATE ")
	if d.Unique {
		b.WriteString("UNIQUE ")
	}
	b.WriteString("INDEX ")
	if concurrently {
		b.WriteString("CONCURRENTLY IF NOT EXISTS ")
	}
	b.WriteString(QuoteIdent(d.Name) + " ON " + Qualified(schema, d.Table))
	if d.Method != "" {
		b.WriteString(" USING " + d.Method)
	}
	b.WriteString(" (" + d.Expr + ")")
	if d.Include != "" {
		b.WriteString(" INCLUDE (" + d.Include + ")")
	}
	if d.Where != "" {
		b.WriteString(" WHERE " + d.Where)
	}
	return b.String()
}

// valueColumns are the columns of a table of volatile values for these attributes: the value and the time
// the source saw it.
func valueColumns(cols []Column) ([]string, error) {
	var out []string
	for _, c := range cols {
		typ, err := ColumnType(c.Kind)
		if err != nil {
			return nil, err
		}
		out = append(out, StateColumn(c.AttrNo)+" "+typ, StateColumnAt(c.AttrNo)+" timestamptz")
	}
	return out, nil
}

const provColumn = "prov jsonb NOT NULL DEFAULT '{}'"

// stateTableDef is the table of the common state of every object. It has no history and is updated in
// place, so it has a low fill factor.
func stateTableDef(common []Column) (tableDef, error) {
	cols, err := valueColumns(common)
	if err != nil {
		return tableDef{}, err
	}
	all := append([]string{"obj_no bigint NOT NULL PRIMARY KEY", "type_no smallint NOT NULL"}, cols...)
	return tableDef{Name: TableObjectState, Cols: append(all, provColumn), With: fillStateTable + ", " + vacuumState}, nil
}

// typeStateTableDef is the table of the volatile values that only the type has.
func typeStateTableDef(typeNo int, state []Column) (tableDef, error) {
	cols, err := valueColumns(state)
	if err != nil {
		return tableDef{}, err
	}
	all := append([]string{"obj_no bigint NOT NULL PRIMARY KEY"}, cols...)
	return tableDef{Name: StateTable(typeNo), Cols: append(all, provColumn), With: fillStateTable + ", " + vacuumState}, nil
}

// typeTableDef is the table of the objects of a type. The document is the truth: a key per attribute number
// with the value and what says how far it can be trusted. name and min_grade repeat what is in it, so a plain
// listing does not open the document.
func typeTableDef(typeNo int) tableDef {
	return tableDef{Name: TypeTable(typeNo), With: fillTypeTable + ", " + vacuumBig, Cols: []string{
		"obj_no bigint NOT NULL PRIMARY KEY",
		`ref text COLLATE "C" NOT NULL`,
		"name text NOT NULL",
		"min_grade smallint NOT NULL",
		"doc jsonb NOT NULL",
		"source_refs jsonb NOT NULL DEFAULT '[]'",
		"content_hash bytea NOT NULL",
		"row_hash bytea NOT NULL",
		"seq_from bigint NOT NULL",
	}}
}

// fixedTableDefs are the tables every storage schema has. The tables that grow with the number of rows (the
// change records, the identity records) are here and not in the shared schema, so dropping a KG is dropping a
// schema and one KG's cleaning never meets another's rows (ADR-0080).
func fixedTableDefs(common []Column) ([]tableDef, error) {
	state, err := stateTableDef(common)
	if err != nil {
		return nil, err
	}
	defs := append(append(registryTableDefs(state), recordTableDefs()...), controlTableDefs()...)
	return append(defs, ingestTableDefs()...), nil
}

// registryTableDefs are the registry, the state, the links, the history and the identification index.
func registryTableDefs(state tableDef) []tableDef {
	linkCols := linkColumns()
	return []tableDef{
		{Name: TableObject, With: vacuumBig, Cols: []string{
			"obj_no bigint NOT NULL PRIMARY KEY",
			`ref text COLLATE "C" NOT NULL`,
			"type_no smallint NOT NULL",
			"name text NOT NULL",
			"min_grade smallint NOT NULL",
			"seq_from bigint NOT NULL",
			"retired_seq bigint",
			"first_key jsonb",
		}},
		state,
		{Name: TableLink, With: vacuumBig, Cols: append(append([]string(nil), linkCols...), "PRIMARY KEY (link_no)")},
		{Name: TableObjectHist, With: vacuumBig, Cols: []string{
			"obj_no bigint NOT NULL",
			"type_no smallint NOT NULL",
			"name text NOT NULL",
			"min_grade smallint NOT NULL",
			"doc jsonb NOT NULL",
			"source_refs jsonb NOT NULL DEFAULT '[]'",
			"content_hash bytea NOT NULL",
			"row_hash bytea NOT NULL",
			"seq_from bigint NOT NULL",
			"seq_to bigint NOT NULL",
			"PRIMARY KEY (seq_to, obj_no, seq_from)",
		}},
		{Name: TableLinkHist, With: vacuumBig, Cols: append(append([]string(nil), linkCols...), "seq_to bigint NOT NULL", "PRIMARY KEY (seq_to, link_no, seq_from)")},
		{Name: TableObjectKey, With: vacuumBig, Cols: []string{
			"type_no smallint NOT NULL",
			"ctx_no bigint NOT NULL DEFAULT 0",
			"sig_hash bytea NOT NULL",
			"obj_no bigint NOT NULL",
			"bound_seq bigint NOT NULL",
			"sig text",
			"PRIMARY KEY (type_no, ctx_no, sig_hash)",
		}},
	}
}

// recordTableDefs are the change records, the identity records and the order of the ingest.
func recordTableDefs() []tableDef {
	return []tableDef{
		{Name: TableChangeBatch, With: vacuumBig, Cols: []string{
			"change_seq bigint NOT NULL PRIMARY KEY",
			"kind text NOT NULL",
			"source_id text NOT NULL",
			"apply_id text NOT NULL",
			"chunk_no integer NOT NULL",
			"chunk_count integer NOT NULL",
			"ingest_seq bigint NOT NULL",
			"attempt bigint NOT NULL",
			"applied_at timestamptz NOT NULL",
			"objects integer NOT NULL",
			"links integer NOT NULL",
		}},
		{Name: TableChangeEntry, With: vacuumBig, Cols: []string{
			"change_seq bigint NOT NULL",
			"entity_kind smallint NOT NULL",
			"entity_no bigint NOT NULL",
			"change_kind smallint NOT NULL",
			"old_hash bytea",
			"new_hash bytea",
			"ord integer NOT NULL",
			"PRIMARY KEY (change_seq, entity_kind, entity_no)",
		}},
		{Name: TableLightChange, With: vacuumBig, Cols: []string{
			"change_seq bigint NOT NULL",
			"obj_no bigint NOT NULL",
			"attr_no integer NOT NULL",
			"value_mark text NOT NULL",
			"PRIMARY KEY (change_seq, obj_no, attr_no)",
		}},
		{Name: TableObjectIdentifier, With: vacuumBig, Cols: []string{
			"obj_no bigint NOT NULL",
			"scheme text NOT NULL",
			"value text NOT NULL",
			"scope text NOT NULL",
			"seq_from bigint NOT NULL",
			"seq_to bigint",
			"PRIMARY KEY (obj_no, scheme, value, scope, seq_from)",
		}},
		{Name: TableIdentityEvent, With: vacuumBig, Cols: []string{
			"change_seq bigint NOT NULL",
			"ord integer NOT NULL",
			"event_kind text NOT NULL",
			"retired_obj_no bigint NOT NULL",
			"into_obj_no bigint NOT NULL",
			"link_ref_map jsonb NOT NULL DEFAULT '[]'",
			"PRIMARY KEY (change_seq, ord)",
		}},
		{Name: TableIngestOrder, With: vacuumBig, Cols: []string{
			"ingest_seq bigint NOT NULL PRIMARY KEY",
			"event_kind text NOT NULL",
			"ref text NOT NULL",
			"digest bytea NOT NULL",
			"change_seq bigint NOT NULL",
		}},
	}
}

// fillControlTable leaves room in a page for the rows that are written at every chunk (the head, the lease),
// so an update stays in the page.
const fillControlTable = "fillfactor = 50"

// controlTableDefs are the tables of the control rows of a KG: the head (the change number, the generation,
// the read watermark, the state of an open apply, the hash state, the progress of an abandon or a restore, the
// floor of the history), the lease, the generations and the sealed versions with their partial sums. Each of
// the first two has one row.
func controlTableDefs() []tableDef {
	return []tableDef{
		{Name: TableHead, With: fillControlTable, Cols: []string{
			"id smallint NOT NULL PRIMARY KEY CHECK (id = 1)",
			"state text NOT NULL DEFAULT 'building' CHECK (state IN ('building', 'serving'))",
			"generation integer NOT NULL DEFAULT 1 CHECK (generation >= 1)",
			"applied_seq bigint NOT NULL DEFAULT 0",
			"readable_through bigint NOT NULL DEFAULT 0",
			"applied_through bigint NOT NULL DEFAULT 0",
			"hash_sum bytea NOT NULL",
			"hash_rows bigint NOT NULL DEFAULT 0",
			"hash_parts jsonb NOT NULL DEFAULT '[]'",
			"input_digest bytea NOT NULL",
			"apply_id text NOT NULL DEFAULT ''",
			"next_chunk_no integer NOT NULL DEFAULT 0",
			"chunk_count integer NOT NULL DEFAULT 0",
			"rollback jsonb",
			"last_apply_id text NOT NULL DEFAULT ''",
			"applied_semantic_version integer NOT NULL DEFAULT 0",
			"work text NOT NULL DEFAULT '' CHECK (work IN ('', 'abandoning', 'restoring'))",
			"work_seq bigint NOT NULL DEFAULT 0",
			"work_floor bigint NOT NULL DEFAULT 0",
			"work_head jsonb",
			"history_floor bigint NOT NULL DEFAULT 0",
		}},
		{Name: TableLease, With: fillControlTable, Cols: []string{
			"id smallint NOT NULL PRIMARY KEY CHECK (id = 1)",
			"lease_owner text",
			"lease_until timestamptz",
			"attempt bigint NOT NULL DEFAULT 0",
			"semantic_version integer NOT NULL DEFAULT 0",
		}},
		{Name: TableGenerationEvent, Cols: []string{
			"generation integer NOT NULL PRIMARY KEY",
			"restored_to_seq bigint NOT NULL",
			"at timestamptz NOT NULL",
		}},
		{Name: TableVersion, Cols: []string{
			"version_no integer NOT NULL PRIMARY KEY",
			`version_id text COLLATE "C" NOT NULL`,
			"sealed_seq bigint NOT NULL",
			"generation integer NOT NULL",
			"prev_version_id text NOT NULL",
			"definition_set_hash bytea NOT NULL",
			"engine_version integer NOT NULL",
			"hash_rule_version integer NOT NULL",
			"input_digest bytea NOT NULL",
			"graph_hash bytea NOT NULL",
			"row_count bigint NOT NULL",
			"sealed_at timestamptz NOT NULL",
			"seal_kind text NOT NULL",
		}},
		{Name: TableVersionHashPart, Cols: []string{
			"version_no integer NOT NULL",
			"entity_kind text NOT NULL",
			"type_id text NOT NULL",
			"partial_sum bytea NOT NULL",
			"row_count bigint NOT NULL",
			"PRIMARY KEY (version_no, entity_kind, type_id)",
		}},
	}
}

// fixedIndexDefs are the indexes of the fixed tables, besides the keys. Every one has a reader: the link
// has the two that a walk needs and one to find a link by its id, and no more (a walk is answered from the
// indexes alone when the cleaning keeps up).
func fixedIndexDefs(common []Column) []indexDef {
	defs := []indexDef{
		{Name: "object_ref", Table: TableObject, Expr: "ref", Unique: true},
		{Name: "object_alive_ref", Table: TableObject, Expr: "ref", Where: "retired_seq IS NULL"},
	}
	for _, c := range common {
		defs = append(defs, indexDef{Name: TableObjectState + "_" + StateColumn(c.AttrNo), Table: TableObjectState, Expr: StateColumn(c.AttrNo)})
	}
	defs = append(defs, ingestIndexDefs()...)
	return append(defs,
		indexDef{Name: "link_ref", Table: TableLink, Expr: "ref", Unique: true},
		indexDef{Name: "link_src", Table: TableLink, Expr: "src, type_no, dst"},
		indexDef{Name: "link_dst", Table: TableLink, Expr: "dst, type_no, src"},
		indexDef{Name: "object_hist_obj", Table: TableObjectHist, Expr: "obj_no, seq_from"},
		indexDef{Name: "object_hist_type_seq_to", Table: TableObjectHist, Expr: "type_no, seq_to"},
		indexDef{Name: "link_hist_link", Table: TableLinkHist, Expr: "link_no, seq_from"},
		indexDef{Name: "link_hist_src", Table: TableLinkHist, Expr: "src, type_no, dst"},
		indexDef{Name: "link_hist_dst", Table: TableLinkHist, Expr: "dst, type_no, src"},
		indexDef{Name: "link_hist_type_seq_to", Table: TableLinkHist, Expr: "type_no, seq_to"},
		indexDef{Name: "link_hist_ref", Table: TableLinkHist, Expr: "ref, seq_from"},
		indexDef{Name: "object_key_obj", Table: TableObjectKey, Expr: "obj_no"},
		indexDef{Name: "object_key_bound", Table: TableObjectKey, Expr: "bound_seq"},
		indexDef{Name: "object_identifier_value", Table: TableObjectIdentifier, Expr: "scheme, value"},
		indexDef{Name: "object_identifier_from", Table: TableObjectIdentifier, Expr: "seq_from"},
		indexDef{Name: "object_identifier_to", Table: TableObjectIdentifier, Expr: "seq_to", Where: "seq_to IS NOT NULL"},
		indexDef{Name: "ingest_order_change", Table: TableIngestOrder, Expr: "change_seq"},
		indexDef{Name: "version_id", Table: TableVersion, Expr: "version_id", Unique: true},
	)
}

// typeIndexDefs are the indexes every table of a type has besides its key: one for the exact name (name = $1, and the
// order of the names) and one for the start of the name with upper and lower case taken as one (lower(name) LIKE
// 'ab%', also lower(name) = 'ab'), which needs the pattern operators because the collation of the database is not
// C. The reference is not here: the registry (object) finds an object by its reference and says its type, so a table
// of a type is read by number (ADR-0101).
func typeIndexDefs(typeNo int) []indexDef {
	return []indexDef{
		{Name: NameIndex(typeNo), Table: TypeTable(typeNo), Expr: "name"},
		{Name: NameIndexFold(typeNo), Table: TypeTable(typeNo), Expr: "lower(name) text_pattern_ops"},
	}
}

// searchIndexDef is the index of one searched attribute of a type.
func searchIndexDef(typeNo int, col Column) (indexDef, error) {
	if !Indexable(col.Kind) {
		return indexDef{}, kernel.Errorf(kernel.CodeInvalidValue, "the attribute %d has a value of the kind %q, which has no index", col.AttrNo, col.Kind)
	}
	expr, err := Expr("doc", col.AttrNo, col.Kind)
	if err != nil {
		return indexDef{}, err
	}
	return indexDef{Name: SearchIndex(typeNo, col.AttrNo), Table: TypeTable(typeNo), Expr: expr}, nil
}

func linkColumns() []string {
	return []string{
		"link_no bigint NOT NULL",
		`ref text COLLATE "C" NOT NULL`,
		"type_no smallint NOT NULL",
		"src bigint NOT NULL",
		"dst bigint NOT NULL",
		"discriminator text NOT NULL DEFAULT ''",
		"grade text NOT NULL",
		"basis text NOT NULL",
		"doc jsonb NOT NULL DEFAULT '{}'",
		"source_refs jsonb NOT NULL DEFAULT '[]'",
		"content_hash bytea NOT NULL",
		"row_hash bytea NOT NULL",
		"seq_from bigint NOT NULL",
	}
}
