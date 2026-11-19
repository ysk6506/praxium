package pgconn

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The error codes of this unit. A code is never renamed or reused.
const (
	// CodeServerTooOld is a PostgreSQL server of a major version below the lowest one Praxium supports (18).
	CodeServerTooOld kernel.Code = "pgconn_server_too_old"
	// CodeRoleChanged is a server that answered that it cannot take this work any more (read-only, starting up, a
	// connection exception). The connections of the pool were thrown away; the caller may try again.
	CodeRoleChanged kernel.Code = "pgconn_role_changed"
	// CodeConnectionLost is a connection that broke. The connections of the pool were thrown away; the caller may try again.
	CodeConnectionLost kernel.Code = "pgconn_connection_lost"
	// CodeOutcomeUnknown is a COMMIT that got neither a success nor an answer of the server: the transaction may or
	// may not be committed. The caller must find out before it writes again.
	CodeOutcomeUnknown kernel.Code = "pgconn_outcome_unknown"
)

var codeMeanings = map[kernel.Code]string{
	CodeServerTooOld:   "the PostgreSQL server is older than the lowest major version Praxium supports",
	CodeRoleChanged:    "the server answered that it cannot take this work (read-only, starting up, connection exception)",
	CodeConnectionLost: "the connection to the server broke",
	CodeOutcomeUnknown: "a COMMIT got no answer: the transaction may or may not be committed",
}

// Codes lists the codes of this unit, sorted.
func Codes() []kernel.Code {
	out := make([]kernel.Code, 0, len(codeMeanings))
	for c := range codeMeanings {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CodeMeaning is the one-line meaning of a code of this unit, or false.
func CodeMeaning(code kernel.Code) (string, bool) {
	m, ok := codeMeanings[code]
	return m, ok
}

// SQLSTATE codes this unit tells apart.
const (
	sqlUniqueViolation = "23505"
	sqlLockNotAvail    = "55P03"
	sqlQueryCanceled   = "57014"
	sqlIdleInTxTimeout = "25P03"
	sqlReadOnlyTx      = "25006"
	sqlStartingUp      = "57P03"
)

// Kind is what a driver error says about the connection and the server.
type Kind int

// The kinds. The driver turns a fatal error of the server (an administrator ending the session, say) into
// driver.ErrBadConn and a broken link into a network error, so what is not an answer of the server is a lost connection.
const (
	// KindNone is no error.
	KindNone Kind = iota
	// KindCaller is a cancelled or expired context: the caller's own doing.
	KindCaller
	// KindServer is an error the server answered; the connection is fine.
	KindServer
	// KindRoleChanged is an answer that says the server is not the one to use for this work: read-only (25006),
	// starting up (57P03) or a connection exception (class 08).
	KindRoleChanged
	// KindLost is a connection that is gone or cannot be trusted any more.
	KindLost
)

func (k Kind) String() string {
	return [...]string{"none", "caller", "server", "role_changed", "lost"}[k]
}

// Classify says what an error of a driver call means. Rules, in order: a cancelled or expired context is the
// caller's; an error the server answered (*pq.Error) is the server's, and one with SQLSTATE 25006, 57P03 or of class
// 08 says the role changed (57P03 comes as FATAL while connecting, so the code is looked at before the severity); a
// fatal error of another kind, driver.ErrBadConn and every other error of the driver mean the connection is lost.
// The names of the network types are not used: whatever is not an answer of the server is a lost connection.
func Classify(err error) Kind {
	if err == nil {
		return KindNone
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return KindCaller
	}
	var pe *pq.Error
	if errors.As(err, &pe) {
		code := string(pe.Code)
		switch {
		case code == sqlReadOnlyTx || code == sqlStartingUp || strings.HasPrefix(code, "08"):
			return KindRoleChanged
		case pe.Fatal():
			return KindLost
		}
		return KindServer
	}
	return KindLost
}

// fromDriver says whether an error that came out of the function of a transaction looks like one of the driver:
// an answer of the server, driver.ErrBadConn, end of file or an error of the network (anything with Timeout(): the
// types of net are not named here). An error of the caller's own code (a missing row, a rule of the store) is not.
func fromDriver(err error) bool {
	var pe *pq.Error
	var net interface{ Timeout() bool }
	return errors.As(err, &pe) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &net)
}

// kindOfWork is Classify for an error the function of a transaction returned: an error that does not look like
// the driver's is none of the pool's business. A 25006 in a transaction that was opened read-only is the caller's
// own doing (the server is fine), so it does not say the role changed.
func kindOfWork(err error, readOnly bool) Kind {
	if err == nil || !fromDriver(err) {
		return KindNone
	}
	k := Classify(err)
	var pe *pq.Error
	if k == KindRoleChanged && readOnly && errors.As(err, &pe) && string(pe.Code) == sqlReadOnlyTx {
		return KindServer
	}
	return k
}

// Outcome is what a COMMIT is known to have done.
type Outcome int

// The outcomes of a COMMIT. Only a success is OutcomeCommitted; only an error that comes before anything was sent,
// or an answer of the server, tells that nothing was committed; the rest is unknown.
const (
	// OutcomeCommitted is a COMMIT that returned no error.
	OutcomeCommitted Outcome = iota
	// OutcomeNotCommitted is a failure the server answered (or a context that ended before the COMMIT was sent).
	OutcomeNotCommitted
	// OutcomeUnknown is everything else: the connection broke, the answer never came.
	OutcomeUnknown
)

// CommitOutcome sorts the result of tx.Commit. It never answers "committed" or "not committed" for what it cannot
// know: a connection that broke during the COMMIT may have been committed by the server.
func CommitOutcome(err error) Outcome {
	if err == nil {
		return OutcomeCommitted
	}
	var pe *pq.Error
	switch {
	case Classify(err) == KindCaller, errors.Is(err, sql.ErrTxDone), errors.Is(err, pq.ErrInFailedTransaction):
		return OutcomeNotCommitted
	case errors.As(err, &pe) && !pe.Fatal():
		return OutcomeNotCommitted
	}
	return OutcomeUnknown
}

// IsUniqueViolation says whether an error is the server's answer that a unique key is taken (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	var pe *pq.Error
	return errors.As(err, &pe) && string(pe.Code) == sqlUniqueViolation
}

// DBErr gives an error from the database or the driver a code of this backend. An error that already has a code
// passes unchanged. The text of a driver error never carries the DSN.
func DBErr(err error, what string) error {
	if err == nil {
		return nil
	}
	var coded *kernel.Error
	if errors.As(err, &coded) {
		return err
	}
	var pe *pq.Error
	if errors.As(err, &pe) {
		switch string(pe.Code) {
		case sqlUniqueViolation:
			return kernel.Wrap(kernel.CodeConflict, err, what+": a row with that key is already there")
		case sqlLockNotAvail, sqlQueryCanceled, sqlIdleInTxTimeout:
			return kernel.Wrap(kernel.CodeInternal, err, what+": a time limit of the role passed")
		}
	}
	return kernel.Wrap(kernel.CodeInternal, err, what)
}
