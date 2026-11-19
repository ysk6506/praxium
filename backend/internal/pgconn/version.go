package pgconn

import (
	"context"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// MinServerVersion is the lowest server_version_num Praxium starts with: PostgreSQL 18 (ADR-0082, ADR-0084).
const MinServerVersion = 180000

// readServerVersion asks the server for server_version_num.
func readServerVersion(ctx context.Context, db *Handle) (int, error) {
	var text string
	if err := db.QueryRowContext(ctx, `SHOW server_version_num`).Scan(&text); err != nil {
		return 0, err
	}
	return strconv.Atoi(text)
}

// CheckServerVersion is the version gate: nil for a server of major version 18 or newer, otherwise an error that
// says why Praxium does not start.
func CheckServerVersion(num int) error {
	if num >= MinServerVersion {
		return nil
	}
	return kernel.Errorf(CodeServerTooOld, "cannot start: the PostgreSQL server is version %d (server_version_num %d) and Praxium needs "+
		"version 18 or newer (ADR-0082); use a server of version 18 or newer", num/10000, num)
}

// gate reads the version of the server behind a pool and checks it.
func gate(ctx context.Context, read func(context.Context, *Handle) (int, error), h *Handle) error {
	if read == nil {
		read = readServerVersion
	}
	num, err := read(ctx, h)
	if err != nil {
		return DBErr(err, "read the version of the server")
	}
	return CheckServerVersion(num)
}
