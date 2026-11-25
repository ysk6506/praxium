package pgingest

import (
	"context"
	"database/sql"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// Signal marks that a source changed: the signal of a receiving webhook (ADR-0107). It sets the time of the first signal
// that has not been taken on every row of the source (the source may be put to more than one KG), and only where it is
// empty, so a signal that comes again writes nothing. A source that is put to no KG has no row and nothing to mark.
//
// It does not open a run: that is the state machine's, which reads the mark.
func (s *Store) Signal(ctx context.Context, sourceID string) error {
	return s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSourceState+" SET "+pgschema.ColSignalPending+" = now() WHERE "+
			pgschema.ColSourceID+" = $1 AND "+pgschema.ColSignalPending+" IS NULL", sourceID); err != nil {
			return dbErr(err, "mark the signal of the source")
		}
		return nil
	})
}
