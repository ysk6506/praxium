package pgschema

import "strconv"

// The tables of the ingestion at scale (ADR-0086, ADR-0095). The units that write to them (the receiving, the
// computing, the applying and the cleaning of a run) take every name from here, so a table or a column is
// spelled in one place. The permanent ones stay between runs; the staged ones (stage_*, calc_*, ingest_diff) are
// emptied with TRUNCATE and are logged tables, so a run that is cut off goes on from them (I3: they are only
// inserted into).
const (
	// Permanent, in the storage schema of the KG.
	TableRawRecord     = "raw_record"
	TableRecordWait    = "record_wait"
	TableRecordRef     = "record_ref"
	TableRecordProblem = "record_problem"

	// Staged, one set per source (a partition per source of each, see SourceTables).
	TableStageRecord = "stage_record"
	TableStageKey    = "stage_key"
	TableStageEnd    = "stage_end"
	TableStageClaim  = "stage_claim"

	// Staged, one set per KG (a KG has one computing run at a time, so no row says which run it is of).
	TableCalcRecord  = "calc_record"
	TableCalcWait    = "calc_wait"
	TableCalcKey     = "calc_key"
	TableCalcEnd     = "calc_end"
	TableCalcClaim   = "calc_claim"
	TableCalcBind    = "calc_bind"
	TableCalcNewKey  = "calc_newkey"
	TableCalcDropped = "calc_dropped"
	TableCalcObject  = "calc_object"
	TableStageObject = "stage_object"
	TableStageLink   = "stage_link"
	TableStageVolat  = "stage_volatile"
	TableStageIdent  = "stage_ident"
	TableCalcProblem = "calc_problem"
	TableCalcRef     = "calc_ref"
	TableIngestDiff  = ReservedIngestDiff
)

// The shared tables of the runs (migration 0006), outside the storage schemas.
const (
	TableSyncRun       = "sync_run"
	TableSourceState   = "source_state"
	TableSourceDataset = "source_dataset"
	// The sequences of the shared tables.
	SeqSyncRun = "sync_run_no_seq"
	SeqSource  = "source_no_seq"
	SeqDataset = "dataset_no_seq"
)

// The columns of the ingestion tables. A column that has the same meaning in several tables has the same name.
const (
	ColSrcNo       = "src_no"
	ColDatasetNo   = "dataset_no"
	ColKey         = "key"
	ColRawHash     = "raw_hash"
	ColFields      = "fields"
	ColDeclRev     = "decl_rev"
	ColOpenedSeq   = "opened_seq"
	ColFate        = "fate"
	ColKind        = "kind"
	ColRuns        = "runs"
	ColFirstAt     = "first_at"
	ColExpired     = "expired"
	ColRole        = "role"
	ColDefNo       = "def_no"
	ColSub         = "sub"
	ColLookupHash  = "lookup_hash"
	ColTargetNo    = "target_no"
	ColState       = "state"
	ColRetirable   = "retirable"
	ColField       = "field"
	ColReason      = "reason"
	ColPage        = "page"
	ColRowNo       = "row_no"
	ColPlace       = "place"
	ColFiltered    = "filtered"
	ColHeld        = "held"
	ColChangeKind  = "change_kind"
	ColProblems    = "problems"
	ColFeedNo      = "feed_no"
	ColKeyOrd      = "key_ord"
	ColTypeNo      = "type_no"
	ColSigHash     = "sig_hash"
	ColCandRef     = "cand_ref"
	ColAncTypes    = "anc_types"
	ColAncSigs     = "anc_sigs"
	ColSide        = "side"
	ColSchemeNo    = "scheme_no"
	ColValueHash   = "value_hash"
	ColValue       = "value"
	ColRid         = "rid"
	ColWhy         = "why"
	ColObjNo       = "obj_no"
	ColIsNew       = "is_new"
	ColRound       = "round_no"
	ColCtxNo       = "ctx_no"
	ColOrd         = "ord"
	ColChunkNo     = "chunk_no"
	ColEntityNo    = "entity_no"
	ColOldHash     = "old_hash"
	ColNewHash     = "new_hash"
	ColRef         = "ref"
	ColName        = "name"
	ColMinGrade    = "min_grade"
	ColDoc         = "doc"
	ColSourceRefs  = "source_refs"
	ColContentHash = "content_hash"
	ColRowHash     = "row_hash"
	ColFirstKey    = "first_key"
	ColLinkNo      = "link_no"
	ColSrc         = "src"
	ColDst         = "dst"
	ColDiscrim     = "discriminator"
	ColGrade       = "grade"
	ColBasis       = "basis"
	ColAttrNo      = "attr_no"
	ColEntry       = "entry"
	ColScheme      = "scheme"
	ColScope       = "scope"
)

// The codes of the small enumerations the ingestion tables keep as numbers, where a table has many rows.
const (
	// FateUsed and the others are the fate of a raw record: what the last run that saw it made of it.
	FateUsed     int16 = 0
	FateProblem  int16 = 1
	FateFiltered int16 = 2
	FateUnused   int16 = 3

	// WaitUnseen and WaitFiltered are the two things a record can be waited for: it is no longer in the source, or
	// a rule filters it out. One record may be both.
	WaitUnseen   int16 = 0
	WaitFiltered int16 = 1

	// The roles of a record reference: what the record asked to find (ingestion-at-scale.md 5.1).
	RefRoleIdentity int16 = 1
	RefRoleLinkFrom int16 = 2
	RefRoleLinkTo   int16 = 3
	RefRoleClaim    int16 = 4

	// The states of a record reference. Anything but RefResolved is in the list of what is not solved.
	RefResolved   int16 = 0
	RefUnresolved int16 = 1
	RefLostClaim  int16 = 2
	RefHeld       int16 = 3

	// What the receiving found of a row against the raw record: it is new, it changed, or it is the same.
	ReceiveNew     int16 = 0
	ReceiveChanged int16 = 1
	ReceiveSame    int16 = 2

	// Why a record is in the set of the run: it changed, it is unresolved, or it is only read again because it
	// feeds an object that is computed again (ingestion-at-scale-identity.md 3).
	CalcWhyChanged    int16 = 0
	CalcWhyUnresolved int16 = 1
	CalcWhyExpanded   int16 = 2

	// The kinds of a row of ingest_diff, in the order the apply writes them: the objects (the registry, the type
	// tables, the history, the retirement), the identification keys, the links, the identifiers.
	DiffKindObject     int16 = 0
	DiffKindKey        int16 = 1
	DiffKindLink       int16 = 2
	DiffKindIdentifier int16 = 3
)

// The kinds of problem a record can have, a closed list: a person who mends a mapping groups by it. The words are
// those of the report of a run (ingest.Problem*) and the two that the context keys add (ADR-0086 and
// ingestion-at-scale-identity.md 2.5). record_problem and calc_problem refuse any other word.
const (
	ProblemUnreadable       = "unreadable_line"
	ProblemDuplicateKey     = "duplicate_key"
	ProblemShapeChanged     = "shape_changed"
	ProblemIncomplete       = "incomplete_capture"
	ProblemInvalidValue     = "invalid_value"
	ProblemMissingIdentity  = "missing_identity"
	ProblemOrphanValue      = "value_without_object"
	ProblemSourceConflict   = "source_conflict"
	ProblemIdentityConflict = "identity_conflict"
	ProblemLinkTarget       = "link_target_missing"
	ProblemLinkAmbiguous    = "link_target_ambiguous"
	ProblemCardinality      = "cardinality"
	ProblemExtraColumn      = "undeclared_column"
	ProblemCellError        = "cell_error"
	ProblemFormulaNoValue   = "formula_without_value"
	ProblemContextParent    = "context_parent_missing"
	ProblemMissingName      = "missing_name"
)

// ProblemKinds is the closed list of the kinds of problem.
func ProblemKinds() []string {
	return []string{
		ProblemUnreadable, ProblemDuplicateKey, ProblemShapeChanged, ProblemIncomplete, ProblemInvalidValue,
		ProblemMissingIdentity, ProblemOrphanValue, ProblemSourceConflict, ProblemIdentityConflict, ProblemLinkTarget,
		ProblemLinkAmbiguous, ProblemCardinality, ProblemExtraColumn, ProblemCellError, ProblemFormulaNoValue,
		ProblemContextParent, ProblemMissingName,
	}
}

// The stages of a run and how it ended (sync_run.stage). The first group is the work of a run; the second is how it
// ends. A run in a stage of the second group is over.
const (
	RunReceiving            = "receiving"
	RunComputing            = "computing"
	RunAwaitingConfirmation = "awaiting_confirmation"
	RunApplying             = "applying"
	RunVolatile             = "volatile"
	RunSettling             = "settling"

	RunSucceeded = "succeeded"
	RunFailed    = "failed"
	RunCanceled  = "canceled"
	RunDiscarded = "discarded"
)

// RunStages lists the stages, the work first and then the ends.
func RunStages() []string {
	return []string{RunReceiving, RunComputing, RunAwaitingConfirmation, RunApplying, RunVolatile, RunSettling,
		RunSucceeded, RunFailed, RunCanceled, RunDiscarded}
}

// RunHoldsKG lists the stages in which a run holds the gate of its KG: only one run of a KG is in one of them.
// A run that waits for a confirmation lets the gate go, and takes it again when it is confirmed.
func RunHoldsKG() []string { return []string{RunComputing, RunApplying, RunVolatile, RunSettling} }

// RunOver lists the stages in which a run is over.
func RunOver() []string { return []string{RunSucceeded, RunFailed, RunCanceled, RunDiscarded} }

// The three ways a run fails (sync_run.end_class), which the record of runs and the screen show apart: the person
// mends a setting and does not try again, the trouble is passing and the run may be tried again, or the product
// is at fault.
const (
	EndClassConfig    = "config"
	EndClassTransient = "transient"
	EndClassProduct   = "product"
)

// EndClasses lists the ways a run fails.
func EndClasses() []string { return []string{EndClassConfig, EndClassTransient, EndClassProduct} }

// The kinds of a run (the full one is the only one made now) and what starts it.
const (
	RunKindFull        = "full"
	RunKindIncremental = "incremental"
	RunKindStream      = "stream"
)

// SourcePartition is the name of the partition of a source in a table that has one per source.
func SourcePartition(table string, srcNo int64) string {
	return table + "_p" + strconv.FormatInt(srcNo, 10)
}

// ComputeTables lists the tables that are one set per KG, in the order they are emptied at the start of a
// computation.
func ComputeTables() []string {
	return []string{
		TableCalcRecord, TableCalcWait, TableCalcKey, TableCalcEnd, TableCalcClaim, TableCalcBind, TableCalcNewKey,
		TableCalcDropped, TableCalcObject, TableStageObject, TableStageLink, TableStageVolat, TableStageIdent,
		TableCalcProblem, TableCalcRef, TableIngestDiff,
	}
}

// SourceTables lists the tables that have a partition per source: the raw records, which stay, and the four
// that a source is received into, which are emptied when its run ends or is discarded.
func SourceTables() []string {
	return []string{TableRawRecord, TableStageRecord, TableStageKey, TableStageEnd, TableStageClaim}
}

// ReceiveTables lists the tables of SourceTables that a run receives a source into.
func ReceiveTables() []string {
	return []string{TableStageRecord, TableStageKey, TableStageEnd, TableStageClaim}
}
