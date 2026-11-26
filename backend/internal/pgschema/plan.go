package pgschema

import (
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// GeneratorVersion is the version of the rules that turn a model into tables. A change of a rule (a column,
// a setting, an index) is a new version; a schema made by an older version is not followed by a newer one
// without a decision of its own. Version 3 adds the tables of the ingestion at scale (ADR-0095): Diff follows
// the step from 2 to 3 by making them. Version 4 adds the column held to the tables of the keys, ends and claims that were
// received (ADR-0098): Diff follows the step from 3 to 4 by adding it (generations.go). Version 5 adds the name indexes of the
// tables of the types and moves the volatile values out of prov into their columns (ADR-0101): Diff follows the step from 4 to 5.
// Version 6 adds, for the comparison of the received rows with the raw records (ADR-0102), the column expired of the waits, the
// place of the row (page, row_no) of a record of the computation, a problem that has no definition, the table that says which run
// the computation is of and the index of the rows received by key: Diff follows the step from 5 to 6.
// Version 7 adds, for the identification of the records of a run (ADR-0108), the column held to the keys, ends and claims of the
// computation, the feed of a bound record, five working tables and the indexes they and the rows received are read by: Diff follows the
// step from 6 to 7.
// Version 8 adds, for the new computation of the objects (ADR-0115), the index by which the references of the computation are read from an
// object: Diff follows the step from 7 to 8.
const GeneratorVersion = 8

// Phase says where a step may run.
type Phase int

// The two phases. InTx steps run in the one transaction that holds the schema lock. Concurrent steps run
// outside it, one at a time: an index on a table that already has rows is built or dropped without blocking
// the writers, and that cannot be done inside a transaction.
const (
	InTx Phase = iota
	Concurrent
)

// What a step makes or changes.
const (
	WhatSchema     = "schema"
	WhatTable      = "table"
	WhatColumn     = "column"
	WhatIndex      = "index"
	WhatStatistics = "statistics"
)

// Step is one statement of a plan.
type Step struct {
	Phase Phase
	What  string
	Name  string
	SQL   string
}

// Options are what a plan is made for.
type Options struct {
	// KGNo is the number of the KG, from the sequence; the schema is s_<KGNo>.
	KGNo int64
	// Search is the set of attributes that get an index. Nothing is indexed that is not in it.
	Search SearchSet
	// WithStats puts the statistics objects in the steps. Without it they are in Plan.Deferred and the
	// manifest says the statistics are pending, so they can be made after the first load.
	WithStats bool
}

// Plan is what Build returns: the statements in the order they run, and the manifest to keep when they have.
type Plan struct {
	Steps    []Step
	Deferred []Step
	Manifest Manifest
}

// Tables lists the names of the tables the plan makes, in order.
func (p Plan) Tables() []string {
	var out []string
	for _, s := range p.Steps {
		if s.What == WhatTable {
			out = append(out, s.Name)
		}
	}
	return out
}

// Build plans the storage schema of one KG from the registry. It never fails on a valid model; it fails on
// a bad option, a search entry that names an attribute the type does not have, or a number out of range.
func Build(reg *model.Registry, opts Options) (Plan, error) {
	if opts.KGNo < 1 {
		return Plan{}, kernel.Errorf(kernel.CodeInvalidValue, "the number of the KG must be 1 or more, not %d", opts.KGNo)
	}
	sh, err := collect(reg)
	if err != nil {
		return Plan{}, err
	}
	search := NewSearchSet(opts.Search...)
	if err := checkSearch(sh, search); err != nil {
		return Plan{}, err
	}
	schema := SchemaName(opts.KGNo)
	steps := []Step{{Phase: InTx, What: WhatSchema, Name: schema, SQL: "CREATE SCHEMA " + QuoteIdent(schema)}}
	fixed, err := fixedTableDefs(sh.Common)
	if err != nil {
		return Plan{}, err
	}
	for _, d := range fixed {
		steps = append(steps, Step{Phase: InTx, What: WhatTable, Name: d.Name, SQL: d.sql(schema)})
	}
	for _, d := range fixedIndexDefs(sh.Common) {
		steps = append(steps, Step{Phase: InTx, What: WhatIndex, Name: d.Name, SQL: d.sql(schema, false)})
	}
	var stats []Step
	for _, ts := range sh.Types {
		made, err := newTypeSteps(schema, ts, search)
		if err != nil {
			return Plan{}, err
		}
		steps = append(steps, made.tables...)
		steps = append(steps, made.indexes...)
		stats = append(stats, made.stats...)
	}
	plan := Plan{Steps: steps}
	if opts.WithStats {
		plan.Steps = append(plan.Steps, stats...)
	} else {
		plan.Deferred = stats
	}
	plan.Manifest, err = newManifest(reg, sh, opts.KGNo, search)
	if err != nil {
		return Plan{}, err
	}
	if len(plan.Deferred) > 0 {
		plan.Manifest.State = StateStatsPending
	}
	return plan, nil
}

// typeSteps are the steps that make the tables of a new type: its tables, its indexes and its statistics.
type typeSteps struct {
	tables, indexes, stats []Step
}

// newTypeSteps plans a type that has no tables yet. All of it runs in the transaction: a table that was
// made a moment ago is empty, so its indexes are plain ones.
func newTypeSteps(schema string, ts typeShape, search SearchSet) (typeSteps, error) {
	var out typeSteps
	main := typeTableDef(ts.TypeNo)
	out.tables = append(out.tables, Step{Phase: InTx, What: WhatTable, Name: main.Name, SQL: main.sql(schema)})
	if len(ts.State) > 0 {
		def, err := typeStateTableDef(ts.TypeNo, ts.State)
		if err != nil {
			return out, err
		}
		out.tables = append(out.tables, Step{Phase: InTx, What: WhatTable, Name: def.Name, SQL: def.sql(schema)})
	}
	for _, d := range typeIndexDefs(ts.TypeNo) {
		out.indexes = append(out.indexes, Step{Phase: InTx, What: WhatIndex, Name: d.Name, SQL: d.sql(schema, false)})
	}
	for _, attrNo := range searchedOf(search, ts.TypeNo) {
		col, _ := findColumn(ts.Docs, attrNo)
		def, err := searchIndexDef(ts.TypeNo, col)
		if err != nil {
			return out, err
		}
		out.indexes = append(out.indexes, Step{Phase: InTx, What: WhatIndex, Name: def.Name, SQL: def.sql(schema, false)})
	}
	for _, group := range statsGroups(ts.Docs) {
		step, err := statsStep(schema, ts.TypeNo, group)
		if err != nil {
			return out, err
		}
		out.stats = append(out.stats, step)
	}
	return out, nil
}

// searchedOf is the attribute numbers of a type in the (ordered) search set.
func searchedOf(search SearchSet, typeNo int) []int {
	var out []int
	for _, e := range search {
		if e.TypeNo == typeNo {
			out = append(out, e.AttrNo)
		}
	}
	return out
}

// checkSearch refuses an entry of the search set that the model cannot serve.
func checkSearch(sh shape, search SearchSet) error {
	types := map[int]typeShape{}
	for _, ts := range sh.Types {
		types[ts.TypeNo] = ts
	}
	for _, e := range search {
		ts, ok := types[e.TypeNo]
		if !ok {
			return kernel.Errorf(kernel.CodeInvalidValue, "the search set names the type %d, which the model does not have", e.TypeNo)
		}
		col, ok := findColumn(ts.Docs, e.AttrNo)
		if !ok {
			return kernel.Errorf(kernel.CodeInvalidValue, "the search set names the attribute %d of the type %d, which is not in its document (a volatile attribute is not searched this way)", e.AttrNo, e.TypeNo)
		}
		if !Indexable(col.Kind) {
			return kernel.Errorf(kernel.CodeInvalidValue, "the attribute %d of the type %d has a value of the kind %q, which has no index", e.AttrNo, e.TypeNo, col.Kind)
		}
	}
	return nil
}
