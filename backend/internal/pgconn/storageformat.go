package pgconn

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// StorageFormat is the number of the storage format this build knows (the row of the table storage_format, made
// by migration 0003). A change that breaks the shape of the tables raises it in the same step as its migration.
const StorageFormat = 2

// CodeStorageFormatTooNew is a database whose storage format is newer than this build knows: the build does not
// start, because it would misread the tables. It is listed apart from Codes(), whose catalogue the tests of the
// version gate fix.
const CodeStorageFormatTooNew kernel.Code = "pgconn_storage_format_too_new"

// CodeStorageFormatOld is a database that still holds KGs in the tables of the storage format 1 (the alpha
// releases): the build does not start on it, because there is no way to move the rows to the new storage and the
// step that drops the old tables would throw them away. It is listed apart from Codes() for the same reason.
const CodeStorageFormatOld kernel.Code = "pgconn_storage_format_old"

// CheckStorageFormat is the gate of the storage format: nil for a number this build knows or older, otherwise an
// error that says why Praxium does not start.
func CheckStorageFormat(stored int) error {
	if stored <= StorageFormat {
		return nil
	}
	return kernel.Errorf(CodeStorageFormatTooNew, "cannot start: the database has the storage format %d and this build knows the format %d "+
		"and older; use a build that is as new as the database", stored, StorageFormat)
}

// storageGate reads the number of the storage format behind the write pool and checks it. A database without
// the row (it should not happen after the migrations) is refused as damaged.
func storageGate(ctx context.Context, h *Handle) error {
	var stored int
	err := h.QueryRowContext(ctx, `SELECT version FROM storage_format WHERE id = 1`).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return kernel.Errorf(kernel.CodeInternal, "cannot start: the table storage_format has no row")
	}
	if err != nil {
		return DBErr(err, "read the storage format")
	}
	return CheckStorageFormat(stored)
}

// legacyGate refuses a database that holds KGs in the old tables (the table kg of migration 0001, which migration
// 0005 drops). A database with the old tables and no KG in them goes on: the migrations drop them. It runs before
// the migrations, so nothing is changed in the database it refuses.
func legacyGate(ctx context.Context, h *Handle) error {
	var found bool
	if err := h.QueryRowContext(ctx, `SELECT to_regclass('public.kg') IS NOT NULL`).Scan(&found); err != nil {
		return DBErr(err, "look for the tables of the storage format 1")
	}
	if !found {
		return nil
	}
	var any bool
	if err := h.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.kg)`).Scan(&any); err != nil {
		return DBErr(err, "look for the KGs of the storage format 1")
	}
	if any {
		return kernel.Errorf(CodeStorageFormatOld, "cannot start: this database holds KGs in the storage format 1 (the tables of the alpha releases). "+
			"There is no way to move them to the storage format %d: load the sources again into a new database", StorageFormat)
	}
	return nil
}
