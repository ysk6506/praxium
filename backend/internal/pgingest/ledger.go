package pgingest

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The record of runs (ingest.RunLedger) on the table sync_run. A run is held by an attempt: the number goes up each time a
// server takes the run over, and every write of a lease names it, so a write of a server that woke up late changes nothing.

// lease is the end of a lease as the database counts it, from the parameter n that holds the seconds a lease lasts.
func lease(n string) string { return "now() + make_interval(secs => " + n + "::float8)" }

// overStages is the list of the stages in which a run is over, as the text of an SQL list.
func overStages() string {
	words := make([]string, 0, len(pgschema.RunOver()))
	for _, s := range pgschema.RunOver() {
		words = append(words, "'"+s+"'")
	}
	return "(" + strings.Join(words, ", ") + ")"
}

// notOver is the condition that a run has not ended.
func notOver() string { return pgschema.ColStage + " NOT IN " + overStages() }

// OpenRun makes a run of a source of a KG in the stage receiving, held by the caller with the first attempt. The source must
// have been put to the KG (AttachSource). A source that has a run that is not over is refused with CodeRunOpen: the unique
// index of the table decides, so two servers that open at once cannot both succeed.
func (s *Store) OpenRun(ctx context.Context, open ingest.RunOpen) (ingest.RunLease, error) {
	if err := open.Validate(); err != nil {
		return ingest.RunLease{}, err
	}
	out := ingest.RunLease{Attempt: 1, Owner: open.Owner, Term: open.Term}
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		// a KG that is being deleted (ADR-0125) or made (ADR-0126) starts no run; one that is marked after this run is opened takes it
		// off with its source.
		if err := refuseClosed(ctx, tx, open.KG.String()); err != nil {
			return err
		}
		var src int64
		err := tx.QueryRowContext(ctx, "SELECT "+pgschema.ColSrcNo+" FROM "+pgschema.TableSourceState+
			" WHERE "+pgschema.ColKGID+" = $1 AND "+pgschema.ColSourceID+" = $2", open.KG.String(), open.Source).Scan(&src)
		if isNoRows(err) {
			return kernel.Errorf(kernel.CodeNotFound, "the source %q is not put to the KG %q: put it first", open.Source, open.KG)
		}
		if err != nil {
			return dbErr(err, "find the source")
		}
		err = tx.QueryRowContext(ctx, "INSERT INTO "+pgschema.TableSyncRun+" ("+strings.Join([]string{
			pgschema.ColKGID, pgschema.ColSrcNo, pgschema.ColKind, pgschema.ColTrigger, pgschema.ColApplyMode, pgschema.ColStage,
			pgschema.ColDefSetHash, pgschema.ColOwner, pgschema.ColAttempt, pgschema.ColLeaseUntil}, ", ")+
			") VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, "+lease("$9")+") RETURNING "+pgschema.ColRunNo,
			open.KG.String(), src, orDefault(open.Kind, ingest.RunKindFull), orDefault(open.Trigger, ingest.TriggerManual),
			orDefault(open.ApplyMode, ingest.ApplyAutomatic), string(ingest.RunReceiving), open.DefinitionSet[:], open.Owner,
			open.Term.Seconds()).Scan(&out.No)
		if pgconn.IsUniqueViolation(err) {
			return kernel.Errorf(ingest.CodeRunOpen, "the source %q has a run that is not over", open.Source)
		}
		return dbErr(err, "open the run")
	})
	return out, err
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// AcquireRun takes over a run whose lease has run out by the clock of the database, or one the same owner holds, with the next
// attempt. A run that is over is CodeRunStage, one that does not exist is not found, and a lease that is alive and another
// server's is CodeRunHeld.
func (s *Store) AcquireRun(ctx context.Context, no int64, owner string, term time.Duration) (ingest.RunLease, error) {
	if owner == "" || term <= 0 {
		return ingest.RunLease{}, kernel.Errorf(kernel.CodeInvalidValue, "a run is taken over by a server that names itself, for a term")
	}
	out := ingest.RunLease{No: no, Owner: owner, Term: term}
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColOwner+" = $2, "+pgschema.ColAttempt+" = "+
			pgschema.ColAttempt+" + 1, "+pgschema.ColLeaseUntil+" = "+lease("$3")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+
			pgschema.ColRunNo+" = $1 AND "+notOver()+" AND ("+pgschema.ColLeaseUntil+" IS NULL OR "+pgschema.ColLeaseUntil+" < now() OR "+
			pgschema.ColOwner+" = $2) RETURNING "+pgschema.ColAttempt, no, owner, term.Seconds()).Scan(&out.Attempt)
		if !isNoRows(err) {
			return dbErr(err, "take the run over")
		}
		var stage string
		err = tx.QueryRowContext(ctx, "SELECT "+pgschema.ColStage+" FROM "+pgschema.TableSyncRun+" WHERE "+pgschema.ColRunNo+" = $1", no).Scan(&stage)
		switch {
		case isNoRows(err):
			return kernel.Errorf(kernel.CodeNotFound, "no run %d", no)
		case err != nil:
			return dbErr(err, "read the run")
		case ingest.RunStage(stage).Over():
			return kernel.Errorf(ingest.CodeRunStage, "the run %d is over (%s)", no, stage)
		}
		return kernel.Errorf(ingest.CodeRunHeld, "the lease of the run %d is alive and another server's", no)
	})
	return out, err
}

// guard is the condition of a write that a lease holds: the run, the attempt, and that the run is not over. It takes the
// parameters $1 (the run) and $2 (the attempt).
func guard() string {
	return pgschema.ColRunNo + " = $1 AND " + pgschema.ColAttempt + " = $2 AND " + notOver()
}

// miss says why a write that a lease holds changed nothing: the run is not there, or another attempt holds it (the lease
// is lost), or the run is not in the stage the call needs.
func miss(ctx context.Context, tx *sql.Tx, l ingest.RunLease) error {
	var attempt int64
	var stage string
	err := tx.QueryRowContext(ctx, "SELECT "+pgschema.ColAttempt+", "+pgschema.ColStage+" FROM "+pgschema.TableSyncRun+
		" WHERE "+pgschema.ColRunNo+" = $1", l.No).Scan(&attempt, &stage)
	switch {
	case isNoRows(err):
		return kernel.Errorf(ingest.CodeRunLeaseLost, "the run %d is not there any more", l.No)
	case err != nil:
		return dbErr(err, "read the run")
	case attempt != l.Attempt:
		return kernel.Errorf(ingest.CodeRunLeaseLost, "the run %d is held by attempt %d, not %d", l.No, attempt, l.Attempt)
	}
	return kernel.Errorf(ingest.CodeRunStage, "the run %d is not in a stage that takes this call (%s)", l.No, stage)
}

// RenewRun extends the lease of a run by its term, counted from the clock of the database.
func (s *Store) RenewRun(ctx context.Context, l ingest.RunLease) error {
	return s.tx(ctx, roleApplier, func(tx *sql.Tx) error { return renewIn(ctx, tx, l) })
}

// renewIn is RenewRun in a transaction a unit already holds (ADR-0116: Go's read of the end of a search extends the lease as it goes).
func renewIn(ctx context.Context, tx *sql.Tx, l ingest.RunLease) error {
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColLeaseUntil+" = "+lease("$3")+", "+
		pgschema.ColUpdatedAt+" = now() WHERE "+guard(), l.No, l.Attempt, l.Term.Seconds())
	if err != nil {
		return dbErr(err, "renew the lease")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

// The token of the page last stored is written to the run as text: nothing when the reader gave none, "t:" and the token in
// base64 when it did, and "last" when the page was the last of its dataset, which has nothing to go on from.
const tokenLast = "last"

func encodeToken(token []byte, last bool) any {
	switch {
	case last:
		return tokenLast
	case len(token) == 0:
		return nil
	}
	return "t:" + base64.StdEncoding.EncodeToString(token)
}

func decodeToken(text sql.NullString) (token []byte, last bool, err error) {
	switch {
	case !text.Valid:
		return nil, false, nil
	case text.String == tokenLast:
		return nil, true, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(text.String, "t:"))
	if err != nil {
		return nil, false, kernel.Errorf(kernel.CodeInternal, "the token of a run is not one this unit wrote")
	}
	return raw, false, nil
}

// ReceivePlace says where the receiving of a run stands: the last page stored, with its dataset and its token.
func (s *Store) ReceivePlace(ctx context.Context, no int64) (ingest.RunPlace, error) {
	var (
		place   ingest.RunPlace
		stage   string
		dataset sql.NullString
		token   sql.NullString
	)
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT r."+pgschema.ColStage+", d."+pgschema.ColDatasetID+", r."+pgschema.ColRecvPage+", r."+pgschema.ColRecvToken+
			" FROM "+pgschema.TableSyncRun+" r LEFT JOIN "+pgschema.TableSourceDataset+" d ON d."+pgschema.ColDatasetNo+" = r."+pgschema.ColRecvData+
			" WHERE r."+pgschema.ColRunNo+" = $1", no).Scan(&stage, &dataset, &place.Page, &token)
	})
	if isNoRows(err) {
		return place, kernel.Errorf(kernel.CodeNotFound, "no run %d", no)
	}
	if err != nil {
		return place, dbErr(err, "read where the receiving stands")
	}
	place.Stage, place.Dataset = ingest.RunStage(stage), dataset.String
	place.Token, place.Last, err = decodeToken(token)
	return place, err
}

// AdvanceRun moves the work of a run from one stage to the next, by the moves ingest.CanAdvance allows. Moving to computing
// takes the gate of the KG: the unique index lets one run of a KG be at work, and the move of another is CodeKGGateTaken, with
// the run left in the stage it was in.
func (s *Store) AdvanceRun(ctx context.Context, l ingest.RunLease, from, to ingest.RunStage) error {
	if !ingest.CanAdvance(from, to) {
		return kernel.Errorf(kernel.CodeInvalidValue, "the work of a run does not go from %s to %s", from, to)
	}
	return s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColStage+" = $3, "+pgschema.ColLeaseUntil+" = "+lease("$5")+
			", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = $4", l.No, l.Attempt, string(to), string(from), l.Term.Seconds())
		if pgconn.IsUniqueViolation(err) {
			return kernel.Errorf(ingest.CodeKGGateTaken, "another run of the KG is at work: the run %d stays in %s", l.No, from)
		}
		if err != nil {
			return dbErr(err, "move the run to the next stage")
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return miss(ctx, tx, l)
		}
		return nil
	})
}

// EndRun ends a run: the stage it ends in, and for a failure the class, the code and the words. The lease is let go.
func (s *Store) EndRun(ctx context.Context, l ingest.RunLease, end ingest.RunEnd) error {
	if err := end.Validate(); err != nil {
		return err
	}
	return s.tx(ctx, roleApplier, func(tx *sql.Tx) error { return endIn(ctx, tx, l, end) })
}

// endIn is EndRun in a transaction the caller holds (the last job of the cleaning ends the run in its own transaction, ADR-0128).
func endIn(ctx context.Context, tx *sql.Tx, l ingest.RunLease, end ingest.RunEnd) error {
	var class any
	if end.Class != "" {
		class = string(end.Class)
	}
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColStage+" = $3, "+pgschema.ColEndedAt+" = now(), "+
		pgschema.ColUpdatedAt+" = now(), "+pgschema.ColLeaseUntil+" = NULL, "+pgschema.ColEndClass+" = $4, "+pgschema.ColEndCode+" = $5, "+
		pgschema.ColEndReason+" = $6 WHERE "+guard(), l.No, l.Attempt, string(end.Stage), class, nullIfEmpty(end.Code), nullIfEmpty(end.Reason))
	if err != nil {
		return dbErr(err, "end the run")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
