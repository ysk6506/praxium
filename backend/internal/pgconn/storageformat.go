package pgconn

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// StorageFormat is the number of the storage format this build knows (the row of the table storage_format, made
// by migration 0003). A change that breaks the shape of the tables raises it in the same step as its migration.
const StorageFormat = 1

// CodeStorageFormatTooNew is a database whose storage format is newer than this build knows: the build does not
// start, because it would misread the tables. It is listed apart from Codes(), whose catalogue the tests of the
// version gate fix.
const CodeStorageFormatTooNew kernel.Code = "pgconn_storage_format_too_new"

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
