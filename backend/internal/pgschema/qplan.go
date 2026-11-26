package pgschema

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// objects are every view and function of the query schema of a grade, in the order they are made. The views read the tables of the
// storage schema and nothing in another query schema, so what a grade does not make for itself is the one of the grade below it.
func (g qGen) objects() []qObject {
	var out []qObject
	for _, t := range g.qm.Types {
		out = append(out, g.typeView(t))
	}
	for _, l := range g.qm.Links {
		out = append(out, g.linkView(l))
	}
	out = append(out, g.linkAll(), g.objectView(), g.attributeView(), g.identifierView(), g.versionView(), g.changeView(), g.valueChangeView())
	out = append(out, g.modelViews()...)
	for _, t := range g.qm.Types {
		out = append(out, g.atFunction(t))
	}
	return append(out, g.objectDetail())
}

// queryBuild is what the steps of the query schema of one KG are made from.
type queryBuild struct {
	reg    *model.Registry
	qm     qModel
	kgNo   int64
	token  string
	store  string
	grades []gradeView
}

// buildQuery makes the steps that give a KG its query schemas: the description of the model is put into its tables, and then each
// schema of the KG is dropped and made again with every view and function, and the roles get what they may read in it. The steps are
// the same whether the schemas are new or the model changed, so a model that was renamed has views with the new names and nothing of
// the old. The steps of one schema follow each other, from the step that drops it to the step of its rights, and they read only the
// storage schema, so the executor runs each schema in a transaction of its own (ADR-0126); the steps that fill the description come
// before the first schema and run with it. The roles themselves are not made here (Plan.Roles); a role belongs to the cluster and the
// executor makes it when it is missing.
func buildQuery(reg *model.Registry, sh shape, opts Options, search SearchSet) ([]Step, error) {
	if err := CheckInstallToken(opts.InstallToken); err != nil {
		return nil, err
	}
	qm, err := gatherModel(reg, sh, search)
	if err != nil {
		return nil, err
	}
	b := queryBuild{reg: reg, qm: qm, kgNo: opts.KGNo, token: opts.InstallToken, store: SchemaName(opts.KGNo)}
	for _, grade := range QueryGrades() {
		b.grades = append(b.grades, newGradeView(reg, grade))
	}
	steps := qm.fillSteps(b.store)
	for _, watermark := range []bool{false, true} {
		made, err := b.schemas(watermark)
		if err != nil {
			return nil, err
		}
		steps = append(steps, made...)
	}
	return steps, nil
}

// schemas makes the three schemas of a set, each from the step that drops it to the step of its rights. A grade makes the objects
// that are not the same as those the grade below it sees, so the schema of the highest grade has what is special to it and the search
// path finds the rest in the schemas below.
func (b queryBuild) schemas(watermark bool) ([]Step, error) {
	var steps []Step
	seen := map[string]string{}
	for i, g := range b.grades {
		schema := QuerySchemaName(b.kgNo, g.grade, watermark)
		steps = append(steps, Step{Phase: InTx, What: WhatSchema, Name: schema, SQL: "DROP SCHEMA IF EXISTS " + QuoteIdent(schema) + " CASCADE"},
			Step{Phase: InTx, What: WhatSchema, Name: schema, SQL: "CREATE SCHEMA " + QuoteIdent(schema)})
		gen := qGen{qm: b.qm, grade: g, store: b.store, watermark: watermark}
		for _, o := range gen.objects() {
			if strings.Contains(o.Body, bodyTag) {
				return nil, kernel.Errorf(kernel.CodeInternal, "the body of %s holds the quote of a function", o.Name)
			}
			key := o.key()
			if seen[o.What+" "+o.Name] == key {
				continue
			}
			seen[o.What+" "+o.Name] = key
			steps = append(steps, Step{Phase: InTx, What: o.What, Name: schema + "." + o.Name, SQL: o.statement(schema)})
		}
		steps = append(steps, b.grants(schema, b.grades[i:]))
	}
	return steps, nil
}

// grants give the roles of a grade and of the grades above it the right to read a schema of the grade: the schema, its views and its
// functions, and nothing else. So each role reads the schemas of its grade and of the grades below, in both sets. No role has a right
// on the storage schema, so what a role reads is what the views show it.
func (b queryBuild) grants(schema string, readers []gradeView) Step {
	var sql strings.Builder
	quoted := QuoteIdent(schema)
	for _, g := range readers {
		role := QuoteIdent(QueryRoleName(b.token, b.kgNo, g.grade))
		sql.WriteString("GRANT USAGE ON SCHEMA " + quoted + " TO " + role + ";\n")
		sql.WriteString("GRANT SELECT ON ALL TABLES IN SCHEMA " + quoted + " TO " + role + ";\n")
		sql.WriteString("GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA " + quoted + " TO " + role + ";\n")
	}
	return Step{Phase: InTx, What: WhatGrant, Name: schema, SQL: sql.String()}
}

// CreateRoleSQL is the statement that makes a role of the query schema: one that cannot log in and has no right but those the plan
// gives it. A role belongs to the cluster, so the executor makes it only when the cluster has none of the name.
func CreateRoleSQL(role string) string {
	return "CREATE ROLE " + QuoteIdent(role) + " NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS"
}

// MemberSQL lets the role that runs the executor switch to the role of the query (SET ROLE): without it a role that CREATEROLE made
// cannot be switched to. The executor does not take the rights of the role (NOINHERIT), but it is a member of every grade's role, and
// that is what SET ROLE and RESET ROLE look at: a statement that runs as the role of one grade can switch to the role of another
// grade, or back to the executor, with SET ROLE or RESET ROLE. So the separation of the grades does not stand on the roles alone: it
// stands on a query executor that lets no such statement through (SET, RESET, SET ROLE, more than one statement, DO, CREATE of
// anything, a schema named in the query that is not the current set of the grade; ADR-0113, REQ-666).
func MemberSQL(role string) string {
	return "GRANT " + QuoteIdent(role) + " TO CURRENT_USER WITH SET TRUE, INHERIT FALSE"
}

// DropRolesSQL are the statements that drop the roles of a KG, once its schemas are dropped.
func DropRolesSQL(token string, kgNo int64) []string {
	var out []string
	for _, r := range QueryRoleNames(token, kgNo) {
		out = append(out, "DROP ROLE IF EXISTS "+QuoteIdent(r))
	}
	return out
}
