package pgstore

import (
	"context"
	"database/sql"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The query schema of a KG (ADR-0113): the views, the functions and the roles a user of SQL reads. The plan makes the views and the
// functions; the roles belong to the cluster, so the executor makes them, once, when the cluster has none of the name.

// readInstallToken reads the token of the installation, which the names of the roles carry (migration 0008).
func readInstallToken(ctx context.Context, tx *sql.Tx) (string, error) {
	var token string
	if err := tx.QueryRowContext(ctx, `SELECT token FROM install_token WHERE id = 1`).Scan(&token); err != nil {
		return "", dbErr(err, "read the token of the installation")
	}
	if err := pgschema.CheckInstallToken(token); err != nil {
		return "", err
	}
	return token, nil
}

// runQuerySteps gives a KG its query schema: the roles first (a plan without a query schema has none), then the steps of the plan,
// which drop the schemas and make them again whole. It runs in the transaction of the change, so a failure leaves the KG as it was.
func runQuerySteps(ctx context.Context, tx *sql.Tx, p pgschema.Plan) error {
	if len(p.Query) == 0 {
		return nil
	}
	if err := ensureQueryRoles(ctx, tx, p.Roles); err != nil {
		return err
	}
	return runSteps(ctx, tx, p.Query, pgschema.InTx)
}

// ensureQueryRoles makes the roles that the cluster does not have, and lets the role of the executor switch to each of them. A role
// that CREATEROLE makes can be switched to only by a member that has the right to, and the creator has it by default for neither
// SET nor INHERIT, so the executor says so (ADR-0113). The grant of a membership that is there is the same statement again.
func ensureQueryRoles(ctx context.Context, tx *sql.Tx, roles []string) error {
	for _, role := range roles {
		var have bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, role).Scan(&have); err != nil {
			return dbErr(err, "look for a role of the query schema")
		}
		if !have {
			if _, err := tx.ExecContext(ctx, pgschema.CreateRoleSQL(role)); err != nil {
				return dbErr(err, "make a role of the query schema (the role of the executor needs the right to make roles)")
			}
		}
		if _, err := tx.ExecContext(ctx, pgschema.MemberSQL(role)); err != nil {
			return dbErr(err, "let the executor switch to a role of the query schema")
		}
	}
	return nil
}

// dropQuerySchema drops the query schemas of a KG and then its roles. The roles have rights on the schemas only, which the drop of the
// schemas takes away with them.
func dropQuerySchema(ctx context.Context, tx *sql.Tx, kgNo int64) error {
	token, err := readInstallToken(ctx, tx)
	if err != nil {
		return err
	}
	for _, name := range pgschema.QuerySchemaNames(kgNo) {
		if _, err := tx.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+pgschema.QuoteIdent(name)+` CASCADE`); err != nil {
			return dbErr(err, "drop a query schema")
		}
	}
	for _, stmt := range pgschema.DropRolesSQL(token, kgNo) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return dbErr(err, "drop a role of the query schema")
		}
	}
	return nil
}
