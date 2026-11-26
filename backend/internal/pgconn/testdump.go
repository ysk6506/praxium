package pgconn

import (
	"context"
	"database/sql"
	"sort"
)

// notSystem is the condition on a column of schema names that leaves out the schemas of the system.
func notSystem(col string) string {
	return col + ` NOT IN ('pg_catalog', 'information_schema') AND ` + col + ` NOT LIKE 'pg\_toast%' AND ` + col + ` NOT LIKE 'pg\_temp%'`
}

// testDumpRows gives one line: the statement that reads every row of every ordinary table as JSON, with a time within a day
// of the server's clock written as recent.
var testDumpRows = `WITH t AS (
	SELECT c.oid, quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS name FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	WHERE c.relkind = 'r' AND ` + notSystem("n.nspname") + `),
e AS (
	SELECT t.name, 'to_jsonb(t)' || coalesce(string_agg(format(
		' || jsonb_build_object(%L, CASE WHEN t.%I IS NOT NULL AND abs(extract(epoch FROM now() - t.%I::timestamptz)) < 86400 THEN to_jsonb(''recent''::text) ELSE to_jsonb(t.%I) END)',
		a.attname, a.attname, a.attname, a.attname), '' ORDER BY a.attnum) FILTER (WHERE a.attname IS NOT NULL), '') AS expr
	FROM t LEFT JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum > 0 AND NOT a.attisdropped AND a.atttypid IN ('timestamptz'::regtype, 'timestamp'::regtype)
	GROUP BY t.name)
SELECT coalesce(string_agg(format('SELECT %L || '' '' || (%s)::text FROM %s t', 'row ' || e.name, e.expr, e.name), ' UNION ALL ' ORDER BY e.name),
	'SELECT NULL::text WHERE false') FROM e`

// testDumpParts are the statements of DumpTestDatabase, each giving one text a line.
var testDumpParts = []string{
	`SELECT 'relation ' || n.nspname || '.' || c.relname || ' kind ' || c.relkind::text || ' pages ' || c.relpages || ' tuples ' || c.reltuples ||
		' visible ' || c.relallvisible || ' persistence ' || c.relpersistence::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE ` + notSystem("n.nspname"),
	`SELECT 'column ' || table_schema || '.' || table_name || '.' || column_name || ' ' || ordinal_position || ' ' || data_type || ' ' || is_nullable || ' ' ||
		coalesce(column_default, '') FROM information_schema.columns WHERE ` + notSystem("table_schema"),
	`SELECT 'index ' || schemaname || '.' || indexname || ' ' || indexdef FROM pg_indexes WHERE ` + notSystem("schemaname"),
	`SELECT 'constraint ' || n.nspname || '.' || c.conname || ' ' || pg_get_constraintdef(c.oid) FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace WHERE ` +
		notSystem("n.nspname"),
	`SELECT 'trigger ' || t.tgrelid::regclass::text || ' ' || pg_get_triggerdef(t.oid) FROM pg_trigger t WHERE NOT t.tgisinternal`,
	`SELECT 'view ' || schemaname || '.' || viewname || ' ' || definition FROM pg_views WHERE ` + notSystem("schemaname"),
	`SELECT 'function ' || p.oid::regprocedure::text || ' ' || md5(pg_get_functiondef(p.oid)) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE p.prokind IN ('f', 'p') AND ` + notSystem("n.nspname"),
	`SELECT 'statistics object ' || n.nspname || '.' || s.stxname || ' ' || pg_get_statisticsobjdef(s.oid) FROM pg_statistic_ext s JOIN pg_namespace n ON n.oid = s.stxnamespace`,
	`SELECT 'sequence ' || schemaname || '.' || sequencename || ' ' || coalesce(last_value::text, 'not called') FROM pg_sequences WHERE ` + notSystem("schemaname"),
	`SELECT 'planner statistics rows ' || ` + testStatisticsRows,
}

// testStatisticsRows counts the rows of statistics of the planner that ANALYZE (or the autovacuum's) made for the tables and the
// statistics objects of a database.
const testStatisticsRows = `((SELECT count(*) FROM pg_stats WHERE schemaname NOT IN ('pg_catalog', 'information_schema'))
	+ (SELECT count(*) FROM pg_stats_ext WHERE schemaname NOT IN ('pg_catalog', 'information_schema')))::text`

// DumpTestDatabase is what a test can see of a database, as sorted lines to compare a copy of a template with a database
// made the slow way (ADR-0120): every relation of the schemas that are not the system's, with its kind and the planner's
// idea of its size, every column, index, constraint, trigger, view, function and statistics object, the value of every
// sequence, every row of every table, and how many rows of planner statistics there are. A time within a day of the
// server's clock is shown as recent, so two databases made a moment apart compare equal; other times are shown as they are.
func DumpTestDatabase(ctx context.Context, dsn string) ([]string, error) {
	rows, err := testQueryLines(ctx, dsn, testDumpRows)
	var out []string
	for _, query := range append(append([]string(nil), testDumpParts...), rows...) {
		var lines []string
		if err == nil {
			lines, err = testQueryLines(ctx, dsn, query)
		}
		out = append(out, lines...)
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// testQueryLines runs a statement that gives one text a line on the database of dsn.
func testQueryLines(ctx context.Context, dsn, query string) ([]string, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
