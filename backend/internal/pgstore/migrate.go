package pgstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLockKey is the key of the transaction-level advisory lock that makes two runners take turns. It is
// the bytes "praxium1". There is no session lock: the lock ends with the transaction of the step.
const migrationLockKey int64 = 0x7072617869756d31

// migrateTries is how many times a step is tried again when it could not get its locks in time.
const migrateTries = 5

// migration is one step: the file, its place in the order and the checksum that is kept in the database.
type migration struct {
	version  int
	name     string
	sql      string
	checksum string
	// limits are the settings the file asks for in its leading comments; zero means the role's own limit.
	statement, lockWait time.Duration
}

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// loadMigrations reads the numbered files of a folder. The numbers start at 1 and have no gap, so a file that
// goes missing, or two with the same number, is an error at start and not a surprise in production.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot list the migrations")
	}
	var out []migration
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the migration file %q is not named NNNN_name.sql", e.Name())
		}
		raw, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot read a migration")
		}
		version, _ := strconv.Atoi(m[1])
		sum := sha256.Sum256(raw)
		mig := migration{version: version, name: m[2], sql: string(raw), checksum: hex.EncodeToString(sum[:])}
		if err := readDirectives(&mig); err != nil {
			return nil, err
		}
		out = append(out, mig)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the migrations must be numbered 1, 2, 3 without a gap: found %04d where %04d was due", m.version, i+1)
		}
	}
	return out, nil
}

// embedded is the migrations of this build.
func embedded() ([]migration, error) { return loadMigrations(migrationFiles, "migrations") }

// readDirectives reads the lines at the top of a file that look like "-- praxium:lock_timeout=3s". Only
// lock_timeout and statement_timeout exist; anything else is an error so that a typo does not pass.
func readDirectives(m *migration) error {
	for _, line := range strings.Split(m.sql, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "--") {
			return nil
		}
		rest, ok := strings.CutPrefix(line, "-- praxium:")
		if !ok {
			continue
		}
		key, value, _ := strings.Cut(rest, "=")
		d, err := time.ParseDuration(value)
		if err != nil || d < 0 {
			return kernel.Errorf(kernel.CodeInvalidValue, "migration %04d: %q is not a duration", m.version, value)
		}
		switch key {
		case "lock_timeout":
			m.lockWait = d
		case "statement_timeout":
			m.statement = d
		default:
			return kernel.Errorf(kernel.CodeInvalidValue, "migration %04d: unknown directive %q", m.version, key)
		}
	}
	return nil
}

// migrate applies the steps that are not applied yet, in order, each in its own transaction. A step whose lock
// wait ran out is tried again a few times, so that several servers started together take turns.
func migrate(ctx context.Context, sh *shard, steps []migration) error {
	for _, m := range steps {
		if err := retryLocks(ctx, func() error { return applyStep(ctx, sh, m) }); err != nil {
			return err
		}
	}
	return nil
}

// retryLocks runs step, and runs it again (waiting a little longer each time) while it fails for want of a lock.
func retryLocks(ctx context.Context, step func() error) error {
	var err error
	for try := 1; try <= migrateTries; try++ {
		if err = step(); !lockTimedOut(err) {
			return err
		}
		timer := time.NewTimer(time.Duration(try) * 100 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return kernel.Wrap(kernel.CodeInternal, ctx.Err(), "the migration was cancelled while it waited for a lock")
		}
	}
	return err
}

func lockTimedOut(err error) bool {
	var pe *pq.Error
	return errors.As(err, &pe) && string(pe.Code) == sqlLockNotAvail
}

// applyStep applies one step unless the database has it already. Under the advisory lock it is the only runner:
// it creates the bookkeeping table if needed, compares the checksum of a step that is there, and otherwise runs
// the file and records it, in one transaction.
func applyStep(ctx context.Context, sh *shard, m migration) error {
	return sh.tx(ctx, RoleMigration, false, func(tx *sql.Tx) error {
		if err := stepLimits(ctx, tx, m); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockKey); err != nil {
			return dbErr(err, "take the migration lock")
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			version int PRIMARY KEY, name text NOT NULL, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return dbErr(err, "create the table of migrations")
		}
		var have string
		err := tx.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version = $1`, m.version).Scan(&have)
		if err == nil {
			return sameChecksum(m, have)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return dbErr(err, "read the table of migrations")
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return dbErr(err, "run migration "+strconv.Itoa(m.version))
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`, m.version, m.name, m.checksum)
		return dbErr(err, "record the migration")
	})
}

// stepLimits applies the limits the file asks for on top of the role's.
func stepLimits(ctx context.Context, tx *sql.Tx, m migration) error {
	for _, l := range []struct {
		name string
		d    time.Duration
	}{{"lock_timeout", m.lockWait}, {"statement_timeout", m.statement}} {
		if l.d == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, true)`, l.name, millis(l.d)); err != nil {
			return dbErr(err, "set a limit of the migration")
		}
	}
	return nil
}

// sameChecksum refuses a database whose recorded step is not the file this build holds: a step that has been
// applied is never edited, a change is a new step.
func sameChecksum(m migration, have string) error {
	if have != m.checksum {
		return kernel.Errorf(kernel.CodeConflict, "migration %04d_%s was applied with another content than this build holds; an applied migration is never edited, add a new one", m.version, m.name)
	}
	return nil
}
