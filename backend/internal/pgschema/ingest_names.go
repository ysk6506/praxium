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
	// TableObjectRetire holds the objects that a feed that retires made and that are held to for good: an object whose number is here is
	// retired once nothing stands under it (the old flow's "once on, always on", ADR-0118). It is a table beside the registry of the
	// objects and not a column of it, so that adjusting an older KG makes a table and does not take the registry from a reader.
	TableObjectRetire = "object_retire"

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
	TableCalcSlot    = "calc_slot"
	TableCalcLabel   = "calc_label"
	TableCalcKLabel  = "calc_klabel"
	TableCalcAsk     = "calc_ask"
	TableCalcNewObj  = "calc_newobj"
	// TableCalcXKey holds the keys (an end key, an object, an identifier) the expansion has read the rows of that records share, so that
	// a key that many records of the run have is read by one unit and not by every unit that has a record of it (ADR-0118).
	TableCalcXKey    = "calc_xkey"
	TableStageObject = "stage_object"
	TableStageLink   = "stage_link"
	TableStageVolat  = "stage_volatile"
	TableStageIdent  = "stage_ident"
	TableCalcProblem = "calc_problem"
	TableCalcRef     = "calc_ref"
	// TableCalcOwner holds the number of the run that the tables of the computation are of (one row, written when the computation
	// begins, emptied with the others): the tables are one set per KG, so a run that comes back to the KG after another source
	// began its computation finds that they are not its own (ADR-0102).
	TableCalcOwner  = "calc_owner"
	TableIngestDiff = ReservedIngestDiff
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
	ColPid         = "pid"
	ColLabel       = "label"
	ColIter        = "iter"
	ColFoundNo     = "found_no"
	ColParentNo    = "parent_no"
	ColParentRef   = "parent_ref"
	ColMissing     = "missing"
	ColAnswer      = "answer"
	ColKeyNo       = "key_no"
	ColKeyHash     = "key_hash"
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

	// The other reasons a record of the receiving is in the set of the run, which the comparison with what is kept (step 2)
	// gives: the key is new, the record was waited for and is shown again, its wait for the absence is up (it is closed),
	// its wait for the rule that filters it is up (it is let go of), or the key began or stopped to repeat in the batch.
	CalcWhyNew        int16 = 3
	CalcWhyReturned   int16 = 4
	CalcWhyClosed     int16 = 5
	CalcWhyReleased   int16 = 6
	CalcWhyDuplicates int16 = 7

	// CalcJobRawDiff is the job of the computing that compares the received rows with the raw records (step 2); the value
	// of calc_job before any job is 0, and the part is the next bucket of the job.
	CalcJobRawDiff int16 = 1
	// CalcJobIdentify is the job that finds the objects of the records of the run (step 3, ADR-0108); its place is in the counts.
	CalcJobIdentify int16 = 2

	// ObjectWhyIdentified is why an object is in calc_object: a record of the run was bound to it, or made it, in the identification.
	ObjectWhyIdentified int16 = 1

	// CalcJobExpand is the job that widens the set of the run (step 3 to 4, ADR-0114); its place is in the counts.
	CalcJobExpand int16 = 3
	// The other reasons an object is in calc_object, which the expansion gives: a record of the run had a reference to it before, a record
	// that claims the same identifier as a record of the run has it, or it is a candidate for retirement (no open record supports it and
	// a reference to it is retirable). An object is in the table once, so an object that is identified has the first reason of the run.
	ObjectWhyOldRef int16 = 2
	ObjectWhyClaim  int16 = 3
	ObjectWhyRetire int16 = 4

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
		TableCalcRecord, TableCalcWait, TableCalcOwner, TableCalcKey, TableCalcEnd, TableCalcClaim, TableCalcBind, TableCalcNewKey,
		TableCalcDropped, TableCalcObject, TableCalcSlot, TableCalcLabel, TableCalcKLabel, TableCalcAsk, TableCalcNewObj, TableCalcXKey, TableStageObject, TableStageLink, TableStageVolat, TableStageIdent,
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
