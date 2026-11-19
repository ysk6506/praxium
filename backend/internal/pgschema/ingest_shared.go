package pgschema

import "strings"

// The columns of the shared tables of the runs (migration 0006 of adapter.pgconn). The migration is a file of
// SQL that this package cannot read, so a test of the executor holds the two to each other: the columns here are
// the columns of the tables the migration made.
const (
	ColRunNo      = "run_no"
	ColKGID       = "kg_id"
	ColSourceID   = "source_id"
	ColDatasetID  = "dataset_id"
	ColStage      = "stage"
	ColTrigger    = "trigger_kind"
	ColApplyMode  = "apply_mode"
	ColRecvData   = "recv_dataset_no"
	ColRecvPage   = "recv_page"
	ColRecvToken  = "recv_token"
	ColCalcJob    = "calc_job"
	ColCalcPart   = "calc_part"
	ColVolPart    = "volatile_part"
	ColSettleJob  = "settle_job"
	ColSettlePart = "settle_part"
	ColCounts     = "counts"
	ColApplyID    = "apply_id"
	ColDefSetHash = "definition_set_hash"
	ColFingerpr   = "change_fingerprint"
	ColOwner      = "owner"
	ColAttempt    = "attempt"
	ColLeaseUntil = "lease_until"
	ColStartedAt  = "started_at"
	ColUpdatedAt  = "updated_at"
	ColEndedAt    = "ended_at"
	ColEndClass   = "end_class"
	ColEndCode    = "end_code"
	ColEndReason  = "end_reason"
)

// The names of the two partial unique indexes that make the gate of the runs.
const (
	IndexOneOpenRunPerSource = "sync_run_one_open_per_source"
	IndexOneWorkingRunPerKG  = "sync_run_one_working_per_kg"
)

// SyncRunColumns lists the columns of sync_run in the order the table has them.
func SyncRunColumns() []string {
	return []string{
		ColRunNo, ColKGID, ColSrcNo, ColKind, ColTrigger, ColApplyMode, ColStage, ColRecvData, ColRecvPage, ColRecvToken,
		ColCalcJob, ColCalcPart, ColVolPart, ColSettleJob, ColSettlePart, ColCounts, ColApplyID, ColDefSetHash, ColFingerpr,
		ColOwner, ColAttempt, ColLeaseUntil, ColStartedAt, ColUpdatedAt, ColEndedAt, ColEndClass, ColEndCode, ColEndReason,
	}
}

// SourceStateColumns lists the columns of source_state.
func SourceStateColumns() []string {
	return []string{ColSrcNo, ColKGID, ColSourceID, "full_receive", "full_compute", "applied_definition_set", "created_at"}
}

// SourceDatasetColumns lists the columns of source_dataset.
func SourceDatasetColumns() []string {
	return []string{ColDatasetNo, ColSrcNo, ColDatasetID, "capture_hash", ColDeclRev, "cursor", "captured_at"}
}

// ColumnsOf lists the columns of a table of the ingestion in the storage schema of a KG, in the order they are
// made. A name that is not one of the tables gives nil.
func ColumnsOf(table string) []string {
	for _, d := range ingestTableDefs() {
		if d.Name != table {
			continue
		}
		var out []string
		for _, c := range d.Cols {
			if name := columnName(c); name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

// columnName is the name that starts a line of a table definition, or "" for a line that is a constraint.
func columnName(line string) string {
	name, _, found := strings.Cut(line, " ")
	if !found || name == "PRIMARY" {
		return ""
	}
	return name
}
