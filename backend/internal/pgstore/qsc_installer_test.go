package pgstore

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The test of the installer that only has the right to make roles (ADR-0113). Its name starts with Qsc.

// [REQ-573]
func TestQscAnInstallerThatMayMakeRolesButIsNoSuperuserMakesTheQuerySchemaAndCanSwitchToItsRoles(t *testing.T) {
	dsn := NewTestDatabase(t)
	root, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	installer := "qsc_inst_" + sxText(t, root, `SELECT substr(md5(random()::text), 1, 10)`)
	sxRun(t, root, `CREATE ROLE `+installer+` LOGIN CREATEROLE NOSUPERUSER`)
	sxRun(t, root, `ALTER DATABASE `+pgschema.QuoteIdent(sxText(t, root, `SELECT current_database()`))+` OWNER TO `+installer)
	own := strings.Replace(dsn, "user=praxium", "user="+installer, 1)
	if own == dsn {
		t.Fatalf("the DSN of the test server has no user to replace")
	}
	st, err := Open(context.Background(), Config{DSN: own})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rec, err := st.CreateStorageSchema(context.Background(), sxKG(t, 1), qscModel(t))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("postgres", own)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	token := sxText(t, conn, `SELECT token FROM install_token`)
	for _, grade := range qscGrades {
		tx, err := conn.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		role := pgschema.QuoteIdent(pgschema.QueryRoleName(token, rec.KGNo, grade))
		if _, err := tx.Exec(`SET LOCAL ROLE ` + role); err != nil {
			t.Errorf("the installer cannot switch to the role of %s: %v", grade, err)
		}
		_ = tx.Rollback()
	}
	if err := st.DropStorageSchema(context.Background(), sxKG(t, 1)); err != nil {
		t.Fatalf("the installer cannot drop what it made: %v", err)
	}
	if n := sxInt(t, root, `SELECT count(*) FROM pg_roles WHERE rolname LIKE 'pq\_`+token+`\_%'`); n != 0 {
		t.Errorf("%d roles are left", n)
	}
}
