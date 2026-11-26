package pgschema

import (
	"strconv"
	"strings"
)

func itoa16(v int16) string { return strconv.Itoa(int(v)) }

// col is one line of a CREATE TABLE: the name and what follows it.
func col(name, rest string) string { return name + " " + rest }

// kindCheck is the check that keeps a column of problem kinds to the closed list.
func kindCheck() string {
	words := make([]string, 0, len(ProblemKinds()))
	for _, k := range ProblemKinds() {
		words = append(words, "'"+k+"'")
	}
	return ColKind + " text NOT NULL CHECK (" + ColKind + " IN (" + strings.Join(words, ", ") + "))"
}

const (
	typeSrc     = "integer NOT NULL"
	typeKeyText = `text COLLATE "C" NOT NULL`
	typeHash16  = "bytea NOT NULL"
	// typeHeld says that a key, an end or a claim belongs to a row that a rule filtered out (generator version 4).
	typeHeld = "boolean NOT NULL DEFAULT false"
	// typeDefNo is the number of the definition (the feed or the link) a problem arose in; a problem of the record as a whole,
	// such as a key that repeats in the batch, has none (generator version 6).
	typeDefNo = "integer"
	// typeExpired says that the time of a wait is up: the record is closed (unseen) or let go of (filtered).
	typeExpired = "boolean NOT NULL DEFAULT false"
	typeSmall   = "smallint NOT NULL"
	typeBig     = "bigint NOT NULL"
	typeInt     = "integer NOT NULL"
)

// ingestTableDefs are the tables of the ingestion at scale that every storage schema has: the permanent ones
// and the staged ones. A source's raw records and the staged rows of its receiving are partitioned by the
// number of the source; the partitions are made when the source is put to the KG (SourcePartitionSteps).
func ingestTableDefs() []tableDef {
	return append(append(permanentIngestDefs(), receiveTableDefs()...), computeTableDefs()...)
}

// permanentIngestDefs are the tables that keep what the runs learned: the raw records, what is waited for, what
// the records asked to find, and the problems of the records. The object_key (the identification index) is among
// the registry tables (ADR-0089) and has what the ingestion needs.
func permanentIngestDefs() []tableDef {
	return []tableDef{
		// One partition per source. The hash and the fate are in the key's index, so the comparison of a page
		// with what is kept reads the index only.
		{Name: TableRawRecord, PartitionBy: ColSrcNo, Cols: []string{
			col(ColSrcNo, typeSrc), col(ColDatasetNo, typeInt), col(ColKey, typeKeyText),
			col(ColRawHash, typeHash16), col(ColFields, "text[] NOT NULL"), col(ColDeclRev, typeInt),
			col(ColOpenedSeq, typeBig), col(ColFate, typeSmall),
			"PRIMARY KEY (" + ColSrcNo + ", " + ColDatasetNo + ", " + ColKey + ") INCLUDE (" + ColRawHash + ", " + ColFate + ")",
		}},
		{Name: TableRecordWait, With: vacuumBig, Cols: []string{
			col(ColSrcNo, typeSrc), col(ColDatasetNo, typeInt), col(ColKey, typeKeyText), col(ColKind, typeSmall),
			col(ColRuns, typeInt), col(ColFirstAt, "timestamptz NOT NULL"), col(ColExpired, typeExpired),
			"PRIMARY KEY (" + ColSrcNo + ", " + ColDatasetNo + ", " + ColKey + ", " + ColKind + ")",
		}},
		// What a record asked to find, and what it found. A target of NULL is not solved.
		{Name: TableRecordRef, With: fillTypeTable + ", " + vacuumBig, Cols: []string{
			col(ColSrcNo, typeSrc), col(ColDatasetNo, typeInt), col(ColKey, typeKeyText), col(ColRole, typeSmall),
			col(ColDefNo, typeInt), col(ColSub, typeInt), col(ColLookupHash, "bytea"), col(ColTargetNo, "bigint"),
			col(ColState, typeSmall), col(ColRetirable, "boolean NOT NULL DEFAULT false"),
			"PRIMARY KEY (" + strings.Join([]string{ColSrcNo, ColDatasetNo, ColKey, ColRole, ColDefNo, ColSub}, ", ") + ")",
		}},
		{Name: TableRecordProblem, With: vacuumBig, Cols: []string{
			col(ColSrcNo, typeSrc), col(ColDatasetNo, typeInt), col(ColKey, typeKeyText), col(ColDefNo, typeDefNo),
			col(ColField, "text NOT NULL DEFAULT ''"), kindCheck(), col(ColReason, "text NOT NULL"),
		}},
		objectRetireDef(),
	}
}

// objectRetireDef is the table of the objects that are held to retire for good (ADR-0118): the number of the object is its key.
func objectRetireDef() tableDef {
	return tableDef{Name: TableObjectRetire, With: vacuumBig, Cols: []string{col(ColObjNo, "bigint NOT NULL PRIMARY KEY")}}
}

// receiveTableDefs are the tables a source is received into, one partition per source. A page gives all its rows
// to stage_record (the key, the place, the change against the raw record) and the fields, the keys, the ends of
// the links and the identifier claims only for the rows that are new or changed.
func receiveTableDefs() []tableDef {
	row := func() []string {
		return []string{col(ColSrcNo, typeSrc), col(ColDatasetNo, typeInt), col(ColPage, typeInt), col(ColRowNo, typeInt)}
	}
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	return []tableDef{
		{Name: TableStageRecord, PartitionBy: ColSrcNo, Cols: cat(row(), []string{
			col(ColKey, typeKeyText), col(ColPlace, "text NOT NULL DEFAULT ''"), col(ColFiltered, "boolean NOT NULL DEFAULT false"),
			col(ColChangeKind, typeSmall), col(ColRawHash, "bytea"), col(ColFields, "text[]"), col(ColProblems, "jsonb"),
		})},
		{Name: TableStageKey, PartitionBy: ColSrcNo, Cols: cat(row(), []string{
			col(ColFeedNo, typeInt), col(ColKeyOrd, typeSmall), col(ColTypeNo, typeSmall), col(ColSigHash, typeHash16),
			col(ColCandRef, `text COLLATE "C"`), col(ColAncTypes, "smallint[]"), col(ColAncSigs, "bytea[]"), col(ColHeld, typeHeld),
		})},
		{Name: TableStageEnd, PartitionBy: ColSrcNo, Cols: cat(row(), []string{
			col(ColDefNo, typeInt), col(ColSide, typeSmall), col(ColLookupHash, typeHash16), col(ColHeld, typeHeld),
		})},
		{Name: TableStageClaim, PartitionBy: ColSrcNo, Cols: cat(row(), []string{
			col(ColSchemeNo, typeInt), col(ColFeedNo, typeInt), col(ColValueHash, typeHash16), col(ColValue, "text NOT NULL"), col(ColHeld, typeHeld),
		})},
	}
}

// computeTableDefs are the tables of one computation, one set per KG. They are logged and only inserted into.
// A record of the computation is a rid, a number given in the order they are put in.
func computeTableDefs() []tableDef {
	defs := computeRecordDefs()
	defs = append(defs, computeKeyDefs()...)
	defs = append(defs, identifyTableDefs()...)
	defs = append(defs, expandTableDefs()...)
	defs = append(defs, computeResultDefs()...)
	defs = append(defs, linkTableDefs()...)
	defs = append(defs, isolateTableDefs()...)
	return append(defs, ingestDiffDef())
}

func computeRecordDefs() []tableDef {
	return []tableDef{
		{Name: TableCalcRecord, Cols: []string{
			col(ColRid, "bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY"),
			col(ColSrcNo, typeSrc), col(ColDatasetNo, typeInt), col(ColKey, typeKeyText),
			col(ColFields, "text[] NOT NULL"), col(ColRawHash, typeHash16), col(ColDeclRev, typeInt),
			col(ColOpenedSeq, typeBig), col(ColFate, typeSmall), col(ColWhy, typeSmall),
			col(ColPage, "integer"), col(ColRowNo, "integer"),
		}},
		{Name: TableCalcWait, Cols: []string{
			col(ColSrcNo, typeSrc), col(ColDatasetNo, typeInt), col(ColKey, typeKeyText), col(ColKind, typeSmall),
			col(ColRuns, typeInt), col(ColFirstAt, "timestamptz NOT NULL"), col(ColExpired, "boolean NOT NULL"),
		}},
		calcOwnerDef(),
	}
}

// computeKeyDefs are the keys, the ends and the claims of the records that are read again, and what the
// identification makes of them.
func computeKeyDefs() []tableDef {
	return []tableDef{
		{Name: TableCalcKey, Cols: []string{
			col(ColRid, typeBig), col(ColFeedNo, typeInt), col(ColKeyOrd, typeSmall), col(ColTypeNo, typeSmall),
			col(ColSigHash, typeHash16), col(ColCandRef, `text COLLATE "C"`), col(ColAncTypes, "smallint[]"), col(ColAncSigs, "bytea[]"), col(ColHeld, typeHeld),
		}},
		{Name: TableCalcEnd, Cols: []string{
			col(ColRid, typeBig), col(ColDefNo, typeInt), col(ColSide, typeSmall), col(ColLookupHash, typeHash16), col(ColHeld, typeHeld),
		}},
		{Name: TableCalcClaim, Cols: []string{
			col(ColRid, typeBig), col(ColSchemeNo, typeInt), col(ColFeedNo, typeInt), col(ColValueHash, typeHash16), col(ColValue, "text NOT NULL"), col(ColHeld, typeHeld),
		}},
		// The object a row is bound to, whether it is new, and the round of the identification that bound it.
		{Name: TableCalcBind, Cols: []string{
			col(ColRid, typeBig), col(ColFeedNo, "integer NOT NULL DEFAULT 0"), col(ColKeyOrd, typeSmall), col(ColObjNo, typeBig), col(ColIsNew, "boolean NOT NULL"), col(ColRound, typeSmall),
		}},
		// A key bound in this run, and a key that this run dropped because it led to two objects: it stays dropped
		// for the rest of the run (ingestion-at-scale-identity.md 2.2, rule 3).
		{Name: TableCalcNewKey, Cols: []string{
			col(ColTypeNo, typeSmall), col(ColCtxNo, "bigint NOT NULL DEFAULT 0"), col(ColSigHash, typeHash16),
			col(ColObjNo, typeBig), col(ColRound, typeSmall),
		}},
		{Name: TableCalcDropped, Cols: []string{
			col(ColTypeNo, typeSmall), col(ColCtxNo, "bigint NOT NULL DEFAULT 0"), col(ColSigHash, typeHash16), col(ColRound, typeSmall),
			"PRIMARY KEY (" + ColTypeNo + ", " + ColCtxNo + ", " + ColSigHash + ")",
		}},
		// The objects that are computed again, and why.
		{Name: TableCalcObject, Cols: []string{
			col(ColObjNo, "bigint NOT NULL PRIMARY KEY"), col(ColWhy, typeSmall),
		}},
	}
}

// identifyTableDefs are the working tables of the identification (generator version 7, ADR-0108). They are only inserted into: a
// unit writes its rows and the place of the run in one transaction, so a run that stops goes on from the next unit.
func identifyTableDefs() []tableDef {
	return []tableDef{
		// A key of a record that is ready in a round: its parent found, so its place in the index is known. pid is the record and the
		// feed in one number (rid * 1024 + feed_no); src_no is the source of the record, whose mapping the feed is of. found_no is the object the index has for the key, if any.
		{Name: TableCalcSlot, Cols: []string{
			col(ColRid, typeBig), col(ColSrcNo, typeSrc), col(ColFeedNo, typeInt), col(ColPid, typeBig), col(ColKeyOrd, typeSmall), col(ColTypeNo, typeSmall),
			col(ColCtxNo, "bigint NOT NULL DEFAULT 0"), col(ColSigHash, typeHash16), col(ColRound, typeSmall), col(ColFoundNo, "bigint"),
		}},
		// The label of a record and feed in a round, at each turn of the search for the groups: the smallest pid of the group so far.
		{Name: TableCalcLabel, Cols: []string{
			col(ColRound, typeSmall), col(ColIter, typeSmall), col(ColPid, typeBig), col(ColLabel, typeBig),
		}},
		// The label of a key in a round, at each turn: the smallest label of the records that have the key. A turn is two steps (the
		// keys, then the records), so that a key that many records share is read once and not once for each of them.
		{Name: TableCalcKLabel, Cols: []string{
			col(ColRound, typeSmall), col(ColIter, typeSmall), col(ColTypeNo, typeSmall), col(ColCtxNo, "bigint NOT NULL DEFAULT 0"),
			col(ColSigHash, typeHash16), col(ColLabel, typeBig),
		}},
		// A new group whose object has a context key: what Go needs to make its id, and what Go answers (ref, or that it has no name).
		{Name: TableCalcAsk, Cols: []string{
			col(ColRound, typeSmall), col(ColLabel, typeBig), col(ColRid, typeBig), col(ColFeedNo, typeInt), col(ColKeyOrd, typeSmall),
			col(ColTypeNo, typeSmall), col(ColParentNo, "bigint"), col(ColParentRef, `text COLLATE "C"`),
			col(ColRef, `text COLLATE "C"`), col(ColMissing, "boolean"), col(ColAnswer, "boolean NOT NULL DEFAULT false"),
			"PRIMARY KEY (" + ColRound + ", " + ColLabel + ", " + ColAnswer + ")",
		}},
		// The objects this run makes, by number and by id.
		{Name: TableCalcNewObj, Cols: []string{
			col(ColObjNo, "bigint NOT NULL PRIMARY KEY"), col(ColRef, `text COLLATE "C" NOT NULL`), col(ColTypeNo, typeSmall),
			col(ColRid, typeBig), col(ColFeedNo, typeInt), col(ColRound, typeSmall),
		}},
	}
}

// expandTableDefs is the working table of the expansion (generator version 9, ADR-0118): the keys whose shared rows a unit has read, by
// the rule (kind) and the key (a number and a hash). A unit writes its rows and the keys it read in one transaction, so a key is read by
// the one unit that wrote it.
func expandTableDefs() []tableDef {
	return []tableDef{
		{Name: TableCalcXKey, Cols: []string{
			col(ColKind, typeSmall), col(ColKeyNo, typeBig), col(ColKeyHash, typeHash16),
			"PRIMARY KEY (" + ColKind + ", " + ColKeyNo + ", " + ColKeyHash + ")",
		}},
	}
}

// computeResultDefs are what the computation makes: the objects, the links, the volatile values, the identifier
// claims, the problems and the new record references. The objects and the links have the columns of the tables
// they will be put into, so the apply copies them.
func computeResultDefs() []tableDef {
	return []tableDef{
		{Name: TableStageObject, Cols: []string{
			col(ColObjNo, "bigint NOT NULL PRIMARY KEY"), col(ColRef, typeKeyText), col(ColTypeNo, typeSmall), col(ColName, "text NOT NULL"),
			col(ColMinGrade, typeSmall), col(ColDoc, "jsonb NOT NULL"), col(ColSourceRefs, "jsonb NOT NULL DEFAULT '[]'"),
			col(ColContentHash, typeHash16), col(ColRowHash, typeHash16), col(ColFirstKey, "jsonb"), col(ColIsNew, "boolean NOT NULL"),
		}},
		{Name: TableStageLink, Cols: []string{
			col(ColLinkNo, "bigint NOT NULL PRIMARY KEY"), col(ColRef, typeKeyText), col(ColDefNo, typeInt), col(ColTypeNo, typeSmall),
			col(ColSrc, typeBig), col(ColDst, typeBig), col(ColDiscrim, "text NOT NULL DEFAULT ''"), col(ColGrade, "text NOT NULL"),
			col(ColBasis, "text NOT NULL"), col(ColDoc, "jsonb NOT NULL DEFAULT '{}'"), col(ColSourceRefs, "jsonb NOT NULL DEFAULT '[]'"),
			col(ColContentHash, typeHash16), col(ColRowHash, typeHash16),
		}},
		// The entry of an attribute in the prov of the object's state row (value, grade, basis, time seen).
		{Name: TableStageVolat, Cols: []string{
			col(ColObjNo, typeBig), col(ColTypeNo, typeSmall), col(ColAttrNo, typeInt), col(ColEntry, "jsonb NOT NULL"),
			"PRIMARY KEY (" + ColObjNo + ", " + ColAttrNo + ")",
		}},
		{Name: TableStageIdent, Cols: []string{
			col(ColObjNo, typeBig), col(ColScheme, "text NOT NULL"), col(ColValue, "text NOT NULL"), col(ColScope, "text NOT NULL"),
			"PRIMARY KEY (" + ColObjNo + ", " + ColScheme + ", " + ColValue + ", " + ColScope + ")",
		}},
		{Name: TableCalcProblem, Cols: []string{
			col(ColRid, typeBig), col(ColDefNo, typeDefNo), col(ColField, "text NOT NULL DEFAULT ''"), kindCheck(), col(ColReason, "text NOT NULL"),
		}},
		{Name: TableCalcRef, Cols: []string{
			col(ColRid, typeBig), col(ColRole, typeSmall), col(ColDefNo, typeInt), col(ColSub, typeInt), col(ColLookupHash, "bytea"),
			col(ColTargetNo, "bigint"), col(ColState, typeSmall), col(ColRetirable, "boolean NOT NULL DEFAULT false"),
		}},
		leftTableDef(),
	}
}

// linkTableDefs are the tables the computation of the links makes beside the others (generator version 12, ADR-0122). calc_tokey is the
// count of the objects the To side has for a link end key (targets is 0, 1, or 2 for more than one, and obj_no is one of them), made once
// for each key. calc_linkwin has a row for each link the run makes, and calc_linklost one for each record whose end made a link that
// another record's end made too and that was not used: both have the record (rid), the place of it in the order that decides which of
// them is used (src_no, def_no, held, dataset_no, key) and what a problem about the other attributes says.
func linkTableDefs() []tableDef {
	who := func(extra ...string) []string {
		cols := []string{
			col(ColRef, typeKeyText), col(ColRid, typeBig), col(ColDefNo, "integer NOT NULL"), col(ColSrcNo, typeSrc), col(ColDatasetNo, typeInt),
			col(ColKey, typeKeyText), col(ColHeld, "boolean NOT NULL"), col(ColSig, "text NOT NULL"), col(ColShown, "text NOT NULL"),
			col(ColLinkName, "text NOT NULL"), col(ColDefID, "text NOT NULL"), col(ColDatasetID, "text NOT NULL"),
		}
		return append(cols, extra...)
	}
	return []tableDef{
		{Name: TableCalcToKey, Cols: []string{
			col(ColSrcNo, typeSrc), col(ColDefNo, "integer NOT NULL"), col(ColLookupHash, typeHash16), col(ColTargets, typeSmall), col(ColObjNo, "bigint"),
			"PRIMARY KEY (" + ColSrcNo + ", " + ColDefNo + ", " + ColLookupHash + ")",
		}},
		{Name: TableCalcLinkWin, Cols: who(col(ColLinkNo, typeBig), col(ColLeft, "jsonb NOT NULL DEFAULT '[]'"), "PRIMARY KEY ("+ColRef+")")},
		{Name: TableCalcLinkLost, Cols: who()},
	}
}

// isolateTableDefs are the tables of the fifth step (generator version 13, ADR-0123). calc_isolated has a row for what the step took out
// of the staged rows: an identifier line (kind DiffKindIdentifier, the object in entity_no and the identifier in scheme, value and scope)
// or a link (kind DiffKindLink, its number in entity_no and its id in ref), the check that took it (phase), why and the reason; its key is
// what was taken, so a row is taken once. calc_seat has a row for each place held to one link that a link of the run took (the side, the
// object at that end, the binding) and the link; its key is the place, so no place has two links.
func isolateTableDefs() []tableDef {
	empty := "text NOT NULL DEFAULT ''"
	return []tableDef{
		{Name: TableCalcIsolated, Cols: []string{
			col(ColKind, typeSmall), col(ColEntityNo, typeBig), col(ColScheme, empty), col(ColValue, empty), col(ColScope, empty),
			col(ColRef, `text COLLATE "C" NOT NULL DEFAULT ''`), col(ColPhase, typeSmall), col(ColWhy, typeSmall), col(ColReason, "text NOT NULL"),
			"PRIMARY KEY (" + strings.Join([]string{ColKind, ColEntityNo, ColScheme, ColValue, ColScope}, ", ") + ")",
		}},
		{Name: TableCalcSeat, Cols: []string{
			col(ColSide, typeSmall), col(ColEndNo, typeBig), col(ColBindID, `text COLLATE "C" NOT NULL`), col(ColLinkNo, typeBig),
			"PRIMARY KEY (" + strings.Join([]string{ColSide, ColEndNo, ColBindID}, ", ") + ")",
		}},
	}
}

// isolateIndexDefs are the indexes the fifth step reads by (generator version 13, ADR-0123): the identifier lines of the run by the
// identifier (the lines of one identifier are read together, a range of identifiers at a time), and the links of the run by their id (the
// check of the cardinality goes through them in that order).
func isolateIndexDefs() []indexDef {
	return []indexDef{
		{Name: "stage_ident_value", Table: TableStageIdent, Expr: strings.Join([]string{ColScheme, ColValue, ColScope, ColObjNo}, ", ")},
		{Name: "stage_link_ref", Table: TableStageLink, Expr: ColRef},
	}
}

// leftTableDef is the table of the objects the computation of the objects left (generator version 10, ADR-0119): the object, why (Left*),
// how many lines it had (for one that has more than the limit, the number counted up to the limit and one more), whether it is one the
// run would have made, and the reason as a person reads it. A table of its own, so that the tables of the KG that readers have open
// are not changed.
func leftTableDef() tableDef {
	return tableDef{Name: TableCalcLeft, Cols: []string{
		col(ColObjNo, "bigint NOT NULL PRIMARY KEY"), col(ColWhy, typeSmall), col(ColLines, typeInt), col(ColIsNew, "boolean NOT NULL"), col(ColReason, "text NOT NULL"),
	}}
}

// ingestDiffDef is the difference between the computed rows and the stored ones, in the order the apply writes
// it. ord is the order (kind, type number, id) and chunk_no is stored, so the list of chunks, the preview, the
// fingerprint and the digest all read the same rows. A row of the kind key keeps the key it binds in ctx_no and
// sig_hash.
func ingestDiffDef() tableDef {
	return tableDef{Name: TableIngestDiff, Cols: []string{
		col(ColOrd, "bigint NOT NULL PRIMARY KEY"), col(ColChunkNo, typeInt), col(ColKind, typeSmall), col(ColTypeNo, typeSmall),
		col(ColEntityNo, typeBig), col(ColRef, `text COLLATE "C"`), col(ColChangeKind, typeSmall),
		col(ColOldHash, "bytea"), col(ColNewHash, "bytea"), col(ColCtxNo, "bigint NOT NULL DEFAULT 0"), col(ColSigHash, "bytea"),
	}}
}

// ingestIndexDefs are the indexes of the ingestion tables, besides the keys. The tables that are emptied and
// filled again have only the ones a step reads by; the others are made by a later change if a measurement asks.
func ingestIndexDefs() []indexDef {
	identity := ColRole + " = " + itoa16(RefRoleIdentity)
	out := []indexDef{
		// From an object to the records that feed it.
		{Name: "record_ref_target", Table: TableRecordRef, Expr: ColTargetNo, Where: identity},
		// From a link to the record whose end made it.
		{Name: "record_ref_link", Table: TableRecordRef, Expr: ColTargetNo, Where: ColRole + " = " + itoa16(RefRoleLinkFrom)},
		// Find an end or a claim by its key.
		{Name: "record_ref_lookup", Table: TableRecordRef, Expr: strings.Join([]string{ColRole, ColDefNo, ColLookupHash}, ", "), Where: ColLookupHash + " IS NOT NULL"},
		// The list of what is not solved, as the records that have a part of it.
		{Name: "record_ref_open", Table: TableRecordRef, Expr: strings.Join([]string{ColSrcNo, ColDatasetNo, ColKey}, ", "), Where: ColState + " <> " + itoa16(RefResolved)},
		{Name: "record_problem_record", Table: TableRecordProblem, Expr: strings.Join([]string{ColSrcNo, ColDatasetNo, ColKey}, ", ")},
		{Name: "calc_record_key", Table: TableCalcRecord, Expr: strings.Join([]string{ColSrcNo, ColDatasetNo, ColKey}, ", "), Unique: true},
		{Name: "ingest_diff_chunk", Table: TableIngestDiff, Expr: ColChunkNo + ", " + ColOrd},
		// The comparison of the rows received with the raw records reads them in the order of (dataset, key), a range at a time, and
		// takes the last row of a key by the place.
		stageRecordKeyIndex(),
	}
	return append(append(append(append(out, identifyIndexDefs()...), recomputeIndexDefs()...), linkIndexDefs()...), isolateIndexDefs()...)
}

// linkIndexDefs are the indexes the computation of the links reads by (generator version 12, ADR-0122): the ends of the run by their
// side, the link definition and the key (the order the ends are read in, a page after another, and how the ends that have a key are found
// without reading the others), and the records that lost a link by the link.
func linkIndexDefs() []indexDef {
	return []indexDef{
		{Name: "calc_end_key", Table: TableCalcEnd, Expr: strings.Join([]string{ColSide, ColDefNo, ColLookupHash, ColRid}, ", ")},
		{Name: "calc_linklost_ref", Table: TableCalcLinkLost, Expr: ColRef},
	}
}

// recomputeIndexDefs are the indexes the computation of the objects reads by (generator version 8, ADR-0115): from an object to the
// references of the records of the run that are bound to it (the identity and the identifier claims), as record_ref_target is for the
// references that are kept; and from the record and the feed of a new object to the question that asked for its parent's id.
func recomputeIndexDefs() []indexDef {
	return []indexDef{
		{Name: "calc_ref_target", Table: TableCalcRef, Expr: ColRole + ", " + ColTargetNo, Where: ColTargetNo + " IS NOT NULL"},
		{Name: "calc_ask_pair", Table: TableCalcAsk, Expr: ColRid + ", " + ColFeedNo, Where: ColParentRef + " IS NOT NULL"},
	}
}

// identifyIndexDefs are the indexes the identification reads by (ADR-0108). The rows received are read by a range of pages, which is
// how they were put in, so the index of a source's rows is a BRIN (it costs the COPY of the receiving next to nothing).
func identifyIndexDefs() []indexDef {
	pages := strings.Join([]string{ColDatasetNo, ColPage}, ", ")
	return []indexDef{
		{Name: "stage_key_page", Table: TableStageKey, Method: "brin", Expr: pages},
		{Name: "stage_end_page", Table: TableStageEnd, Method: "brin", Expr: pages},
		{Name: "stage_claim_page", Table: TableStageClaim, Method: "brin", Expr: pages},
		{Name: "calc_record_row", Table: TableCalcRecord, Expr: strings.Join([]string{ColDatasetNo, ColPage, ColRowNo}, ", "), Where: ColPage + " IS NOT NULL"},
		{Name: "calc_key_rid", Table: TableCalcKey, Expr: ColRid + ", " + ColFeedNo},
		{Name: "calc_end_rid", Table: TableCalcEnd, Expr: ColRid},
		{Name: "calc_claim_rid", Table: TableCalcClaim, Expr: ColRid},
		{Name: "calc_claim_value", Table: TableCalcClaim, Expr: ColSchemeNo + ", " + ColValueHash},
		{Name: "calc_bind_pair", Table: TableCalcBind, Expr: ColRid + ", " + ColFeedNo},
		{Name: "calc_newkey_key", Table: TableCalcNewKey, Unique: true, Expr: strings.Join([]string{ColTypeNo, ColCtxNo, ColSigHash}, ", "), Include: ColObjNo},
		{Name: "calc_slot_pid", Table: TableCalcSlot, Expr: ColRound + ", " + ColPid},
		{Name: "calc_slot_key", Table: TableCalcSlot, Expr: strings.Join([]string{ColRound, ColTypeNo, ColCtxNo, ColSigHash}, ", ")},
		{Name: "calc_label_pid", Table: TableCalcLabel, Unique: true, Expr: strings.Join([]string{ColRound, ColIter, ColPid}, ", "), Include: ColLabel},
		{Name: "calc_klabel_key", Table: TableCalcKLabel, Unique: true, Expr: strings.Join([]string{ColRound, ColIter, ColTypeNo, ColCtxNo, ColSigHash}, ", "), Include: ColLabel},
		{Name: "calc_label_label", Table: TableCalcLabel, Expr: strings.Join([]string{ColRound, ColIter, ColLabel}, ", ")},
		{Name: "calc_newobj_ref", Table: TableCalcNewObj, Unique: true, Expr: ColRef},
		{Name: "calc_ref_pair", Table: TableCalcRef, Expr: ColRid + ", " + ColRole},
	}
}

// stageRecordKeyIndex is the index the comparison of the rows received reads by (ADR-0102): the key in the order of the
// raw records' key, then the place of the row. The source's partitions have it with the table.
func stageRecordKeyIndex() indexDef {
	return indexDef{Name: "stage_record_key", Table: TableStageRecord, Expr: strings.Join([]string{ColDatasetNo, ColKey, ColPage, ColRowNo}, ", ")}
}

// calcOwnerDef is the table that says which run the tables of the computation are of: one row, the number of the run.
func calcOwnerDef() tableDef {
	return tableDef{Name: TableCalcOwner, Cols: []string{col(ColRunNo, "bigint NOT NULL PRIMARY KEY")}}
}
