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
	// TableCalcLeft has a row for each object of the run that the computation of the objects did not make (generator version 9,
	// ADR-0119): why, how many lines it had and what the reason is. An object the KG has is left as it is; one the run would have
	// made is not made, and what pointed at it is taken back.
	TableCalcLeft = "calc_left"
	// TableCalcToKey has a row for each link end key (source, link definition, key hash) that the ends of the From side of the run
	// were resolved by: how many objects the To side has for it (none, one, or more than one) and one of them. The To records that
	// share a key are read once, whatever the number of the From records that look for it (generator version 12, ADR-0122).
	TableCalcToKey = "calc_tokey"
	// TableCalcLinkWin has a row for each link the run makes: the record whose end made it (the one that wins when several make it),
	// the number it has and what the later steps need to say why another record lost. TableCalcLinkLost has the records that lost, in
	// the same columns (generator version 12, ADR-0122).
	TableCalcLinkWin  = "calc_linkwin"
	TableCalcLinkLost = "calc_linklost"
	// TableCalcIsolated has a row for each identifier line and each link that the fifth step took out of what the run staged before the
	// difference reads it (generator version 13, ADR-0123): in which check (the phase: the identifiers, the ends of the links, the
	// cardinality), why, what it was and the reason as a person reads it. A link that goes with an object that retires is there too, with
	// its own reason, and is no problem.
	TableCalcIsolated = "calc_isolated"
	// TableCalcSeat has a row for each place a binding holds to one link (the side, the object at that end, the binding) that a link of the
	// run took in the check of the cardinality (generator version 13, ADR-0123): its key is the place, so a place has one link.
	TableCalcSeat = "calc_seat"
	// TableCalcDiffRow has a row for each row of the difference before it is numbered, in the order it is numbered in, and TableCalcDiffPlan a
	// row for each kind and type of the difference: how many rows, and where their order and their chunks begin (generator version 14,
	// ADR-0127).
	TableCalcDiffRow  = "calc_diffrow"
	TableCalcDiffPlan = "calc_diffplan"
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
	ColLines       = "lines"
	ColKeyNo       = "key_no"
	ColKeyHash     = "key_hash"
	ColTargets     = "targets"
	ColSig         = "sig"
	ColShown       = "shown"
	ColLeft        = "left_cells"
	ColLinkName    = "link_name"
	ColDefID       = "def_id"
	ColPhase       = "phase"
	ColEndNo       = "end_no"
	ColBindID      = "bind_id"
	ColTie         = "tie"
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

	// CalcJobRecompute is the job that computes the objects again (step 4, ADR-0115); its place is in the counts.
	CalcJobRecompute int16 = 4

	// ObjectWhyIdentified is why an object is in calc_object: a record of the run was bound to it, or made it, in the identification.
	ObjectWhyIdentified int16 = 1

	// Why the computation of the objects left an object as it is (calc_left.why, ADR-0119): no record stands under it, a line of it
	// cannot be read, no name is known for it, it has more lines than the limit, or it is made under a parent that was left.
	LeftEmpty      int16 = 1
	LeftUnreadable int16 = 2
	LeftNameless   int16 = 3
	LeftTooMany    int16 = 4
	LeftParent     int16 = 5

	// CalcJobExpand is the job that widens the set of the run (step 3 to 4, ADR-0114); its place is in the counts.
	CalcJobExpand int16 = 3
	// The other reasons an object is in calc_object, which the expansion gives: a record of the run had a reference to it before, a record
	// that claims the same identifier as a record of the run has it, or it is a candidate for retirement (no open record supports it and
	// a reference to it is retirable). An object is in the table once, so an object that is identified has the first reason of the run.
	ObjectWhyOldRef int16 = 2
	ObjectWhyClaim  int16 = 3
	ObjectWhyRetire int16 = 4

	// CalcJobIsolate is the job that takes out of what the run staged the rows the KG cannot have (step 5, ADR-0123): the identifiers
	// another object keeps, the links whose end is not there, and the links that break the cardinality. Its place is in the counts.
	CalcJobIsolate int16 = 5

	// The checks of the fifth step (calc_isolated.phase), in the order they are made, each once (ingestion-at-scale-identity.md 4).
	IsolatePhaseIdents int16 = 1
	IsolatePhaseEnds   int16 = 2
	IsolatePhaseCard   int16 = 3

	// Why a row is in calc_isolated: an identifier that another object of the run keeps, one that an object the KG has keeps, a link whose
	// end is neither an object of the KG nor one the run makes, a link whose end retires (it goes with the object, and is no problem), and a
	// link that a place held to one link has given to another.
	IsolatedIdentRun    int16 = 1
	IsolatedIdentKG     int16 = 2
	IsolatedEndMissing  int16 = 3
	IsolatedEndRetires  int16 = 4
	IsolatedCardinality int16 = 5

	// CalcJobDiff is the job that makes the difference of what the run staged with what the KG has (step 5, ADR-0127): the rows, their
	// order and chunks, the counts by kind, type and change, and the fingerprint of the change. Its place is in the counts.
	CalcJobDiff int16 = 6

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

// StepFourProblemKinds lists the kinds of problem that the fourth step (the new computation of the objects and the links) owns: the
// problems of a record that the step gives again for every record it reads again, the records that were read again only because they
// feed an object that is computed again included. The kinds of the receiving and of the identification are given for the records that
// changed alone. It is the kinds the step writes: the objects' side gives source_conflict, the links' side gives link_target_missing and
// link_target_ambiguous, and source_conflict for a link that two records make with other attributes (ADR-0122: no kind is added, as in
// ADR-0119). cardinality is the fifth step's (it holds back a link that would give an object a
// second one of a link type that allows one), so it is not owned here. The problems of the records under an object that was left
// (shape_changed, missing_name, identity_conflict, context_parent_missing) are of the records of the run and are the ones of their own
// steps' kinds, so they are not owned here.
func StepFourProblemKinds() []string {
	return []string{ProblemSourceConflict, ProblemLinkTarget, ProblemLinkAmbiguous}
}

// StepFiveProblemKinds lists the kinds of problem the fifth step writes when it takes a row out (ADR-0123): an identifier that another
// object keeps, a link whose end is not there, and a link that breaks the cardinality. No kind is added (REQ-680): they are in the closed
// list already.
func StepFiveProblemKinds() []string {
	return []string{ProblemIdentityConflict, ProblemLinkTarget, ProblemCardinality}
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
		TableCalcProblem, TableCalcRef, TableCalcLeft, TableCalcToKey, TableCalcLinkWin, TableCalcLinkLost, TableCalcIsolated, TableCalcSeat, TableCalcDiffRow, TableCalcDiffPlan, TableIngestDiff,
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
