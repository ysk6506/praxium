package pgstore

import "github.com/ysk6506/praxium/backend/internal/pgconn"

// TLSState says how a connection string protects the link to the database (see pgconn.TLSState). The composition
// root reads it through this unit, as it reads everything else of the store.
type TLSState = pgconn.TLSState

// InspectTLS reads the connection string with the parser of the driver and says how it protects the link; it
// connects to nothing (see pgconn.InspectTLS).
func InspectTLS(dsn string) (TLSState, error) { return pgconn.InspectTLS(dsn) }
