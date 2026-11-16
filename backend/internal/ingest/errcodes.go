package ingest

import (
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// errCode is one row of the table of the codes a check of a definition gives (ADR-0070). A code is made of lower-case
// words joined by underscores and dots, begins with the document it is about (source, mapping, or definition for
// what is wrong before the document is read as a whole), and is never renamed or reused.
type errCode struct {
	code    string
	kernel  kernel.Code
	doc     string
	meaning string
	params  []string
}

// ErrorCode is the public row of the table.
type ErrorCode struct {
	Code string `json:"code"`
	// Kernel is the kernel code the error has: invalid_value, or unsupported for what this build lacks.
	Kernel kernel.Code `json:"kernel"`
	// Document is source, mapping or both.
	Document string `json:"document"`
	// Meaning is one line in English.
	Meaning string `json:"meaning"`
	// Params are the keys of the params an error of this code may have.
	Params []string `json:"params"`
}

var codeTable []*errCode

// reg adds a code to the table. A check can give only a code that was added here, because it takes the row.
func reg(code string, k kernel.Code, doc, meaning, params string) *errCode {
	ec := &errCode{code: code, kernel: k, doc: doc, meaning: meaning}
	if params != "" {
		ec.params = strings.Split(params, ",")
	}
	codeTable = append(codeTable, ec)
	return ec
}

// ErrorCodes is the table of every code, sorted by code.
func ErrorCodes() []ErrorCode {
	out := make([]ErrorCode, 0, len(codeTable))
	for _, ec := range codeTable {
		out = append(out, ErrorCode{Code: ec.code, Kernel: ec.kernel, Document: ec.doc, Meaning: ec.meaning, Params: append([]string{}, ec.params...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

var (
	ecDefUnreadable       = reg("definition.unreadable", kernel.CodeInvalidValue, "both", "The definition could not be read.", "what")
	ecDefUnknownField     = reg("definition.unknown_field", kernel.CodeInvalidValue, "both", "The definition has a field that is not one of the fields allowed there.", "field,allowed")
	ecDefWrongKind        = reg("definition.wrong_kind", kernel.CodeInvalidValue, "both", "A value of the definition is not of the kind the field takes.", "field,wanted,given,line")
	ecDefMalformed        = reg("definition.malformed", kernel.CodeInvalidValue, "both", "The text is not valid JSON.", "detail")
	ecDefManyDocuments    = reg("definition.multiple_documents", kernel.CodeInvalidValue, "both", "The file holds more than one JSON document.", "")
	ecDefVersion          = reg("definition.unsupported_version", kernel.CodeUnsupported, "both", "The version of the document is not one this build reads.", "version,supported")
	ecSrcIDMissing        = reg("source.id.missing", kernel.CodeInvalidValue, "source", "The source has no id.", "")
	ecSrcIDInvalid        = reg("source.id.invalid", kernel.CodeInvalidValue, "source", "The id of the source is not empty but does not follow the grammar of an id.", "id,pattern,max_length")
	ecSrcAdapter          = reg("source.adapter.unsupported", kernel.CodeUnsupported, "source", "The adapter is not one this build has.", "source,adapter,supported")
	ecSrcLimit            = reg("source.limits.negative", kernel.CodeInvalidValue, "source", "A safety limit is below 0.", "source,field,value")
	ecSrcNoDatasets       = reg("source.datasets.empty", kernel.CodeInvalidValue, "source", "The source has no dataset.", "source")
	ecSrcDatasetID        = reg("source.dataset.id_missing", kernel.CodeInvalidValue, "source", "A dataset has no id.", "source")
	ecSrcDatasetBad       = reg("source.dataset.id_invalid", kernel.CodeInvalidValue, "source", "The id of a dataset is not empty but does not follow the grammar of an id.", "source,id,pattern,max_length")
	ecSrcDatasetDup       = reg("source.dataset.id_duplicate", kernel.CodeInvalidValue, "source", "Two datasets have the same id.", "source,dataset,first")
	ecSrcMode             = reg("source.dataset.unsupported_mode", kernel.CodeUnsupported, "source", "The kind or the receive mode of the dataset is not one this build has.", "dataset,kind,receive_mode")
	ecSrcParser           = reg("source.dataset.unsupported_parser", kernel.CodeUnsupported, "source", "The parser is not one this build has.", "dataset,parser,supported")
	ecSrcDeletion         = reg("source.dataset.unsupported_deletion", kernel.CodeUnsupported, "source", "The deletion rule is not one this build has.", "dataset,deletion,supported")
	ecSrcNoColumns        = reg("source.dataset.columns_empty", kernel.CodeInvalidValue, "source", "The dataset has no column.", "dataset")
	ecSrcColumnName       = reg("source.column.name_invalid", kernel.CodeInvalidValue, "source", "A column name is empty or used twice.", "dataset,name")
	ecSrcSheetParser      = reg("source.dataset.sheet_not_for_parser", kernel.CodeInvalidValue, "source", "Sheet options are given for a parser other than xlsx.", "dataset,parser")
	ecSrcSheetBoth        = reg("source.sheet.name_and_index", kernel.CodeInvalidValue, "source", "The sheet is given both by name and by index.", "dataset")
	ecSrcSheetMin         = reg("source.sheet.below_minimum", kernel.CodeInvalidValue, "source", "A sheet option is below its minimum.", "dataset")
	ecSrcSheetMerged      = reg("source.sheet.merged_cells_invalid", kernel.CodeInvalidValue, "source", "merged_cells is not one of the two ways.", "dataset,merged_cells,allowed")
	ecSrcSheetRange       = reg("source.sheet.range_invalid", kernel.CodeInvalidValue, "source", "The range is not a start cell and an end cell.", "dataset,range")
	ecSrcSheetRangeHeader = reg("source.sheet.range_in_header", kernel.CodeInvalidValue, "source", "The range starts in the header rows.", "dataset,range,header_first,header_last")
	ecSrcDelimParser      = reg("source.dataset.delimiter_not_for_parser", kernel.CodeInvalidValue, "source", "A delimiter is given for a parser other than csv.", "dataset,parser")
	ecSrcDelim            = reg("source.dataset.delimiter_invalid", kernel.CodeInvalidValue, "source", "The delimiter is none of the four.", "dataset,delimiter")
	ecSrcKeyEmpty         = reg("source.natural_key.empty", kernel.CodeInvalidValue, "source", "The natural key names no column.", "dataset,columns")
	ecSrcKeyColumn        = reg("source.natural_key.unknown_column", kernel.CodeInvalidValue, "source", "The natural key names a column the dataset does not have.", "dataset,column,columns")
	ecMapIDInvalid        = reg("mapping.id.invalid", kernel.CodeInvalidValue, "mapping", "The id of the mapping is not empty but does not follow the grammar of an id.", "id,pattern,max_length")
	ecMapRefInput         = reg("mapping.refinement.unknown_input", kernel.CodeInvalidValue, "mapping", "A refinement reads something that is no dataset or earlier refinement.", "refinement,input,available")
	ecMapRefIDMissing     = reg("mapping.refinement.id_missing", kernel.CodeInvalidValue, "mapping", "A refinement has no id.", "")
	ecMapRefIDInvalid     = reg("mapping.refinement.id_invalid", kernel.CodeInvalidValue, "mapping", "The id of a refinement is not empty but does not follow the grammar of an id.", "id,pattern,max_length")
	ecMapRefIDUsed        = reg("mapping.refinement.id_in_use", kernel.CodeInvalidValue, "mapping", "The id of a refinement is already used by a dataset or an earlier refinement.", "id")
	ecMapOpColumn         = reg("mapping.refinement.op.unknown_column", kernel.CodeInvalidValue, "mapping", "An operation names a column its input does not have.", "refinement,operation,column,columns")
	ecMapOpFilter         = reg("mapping.refinement.op.filter_without_test", kernel.CodeInvalidValue, "mapping", "A filter has none of not_empty, in and not_in.", "refinement,operation")
	ecMapOpFill           = reg("mapping.refinement.op.fill_without_value", kernel.CodeInvalidValue, "mapping", "A fill_empty has no value.", "refinement,operation")
	ecMapOpMap            = reg("mapping.refinement.op.map_empty", kernel.CodeInvalidValue, "mapping", "The map function has no map.", "refinement,operation")
	ecMapOpFunc           = reg("mapping.refinement.op.function_unsupported", kernel.CodeInvalidValue, "mapping", "The function of a replace is not in the closed list.", "refinement,operation,function,supported")
	ecMapOpUnsupported    = reg("mapping.refinement.op.unsupported", kernel.CodeInvalidValue, "mapping", "The operation is not one this build has.", "refinement,operation,op,supported")
	ecMapNoFeeds          = reg("mapping.feeds.empty", kernel.CodeInvalidValue, "mapping", "The mapping has no feed.", "")
	ecMapFeedIDMissing    = reg("mapping.feed.id_missing", kernel.CodeInvalidValue, "mapping", "A feed has no id.", "")
	ecMapFeedIDInvalid    = reg("mapping.feed.id_invalid", kernel.CodeInvalidValue, "mapping", "The id of a feed is not empty but does not follow the grammar of an id.", "id,pattern,max_length")
	ecMapFeedDup          = reg("mapping.feed.id_duplicate", kernel.CodeInvalidValue, "mapping", "Two feeds have the same id.", "id,first")
	ecMapFeedDataset      = reg("mapping.feed.unknown_dataset", kernel.CodeInvalidValue, "mapping", "A feed reads something that is no dataset or refinement.", "feed,dataset,available")
	ecMapFeedType         = reg("mapping.feed.unknown_object_type", kernel.CodeInvalidValue, "mapping", "The object type is not one of the model.", "feed,object_type,suggestions")
	ecMapFeedBasis        = reg("mapping.feed.invalid_basis", kernel.CodeInvalidValue, "mapping", "The basis is none of the four.", "feed,basis,valid")
	ecMapFeedRetire       = reg("mapping.feed.retires_without_creates", kernel.CodeInvalidValue, "mapping", "A feed retires objects it does not create.", "feed")
	ecMapAttrUnknown      = reg("mapping.attribute.unknown", kernel.CodeInvalidValue, "mapping", "The object type has no such attribute.", "feed,type,attribute,attributes,org_property")
	ecMapAttrDeprecated   = reg("mapping.attribute.deprecated", kernel.CodeInvalidValue, "mapping", "The attribute is deprecated.", "feed,attribute,replacement")
	ecMapAttrColumn       = reg("mapping.attribute.unknown_column", kernel.CodeInvalidValue, "mapping", "The attribute reads a column the dataset does not have.", "feed,attribute,column,dataset,columns")
	ecMapAttrKind         = reg("mapping.attribute.unsupported_kind", kernel.CodeUnsupported, "mapping", "The value of the attribute is of a kind a file cell does not give.", "feed,attribute,kind")
	ecMapIdentEmpty       = reg("mapping.identity.empty", kernel.CodeInvalidValue, "mapping", "The feed has no identity key.", "feed")
	ecMapIdentForm        = reg("mapping.identity.scheme_or_components", kernel.CodeInvalidValue, "mapping", "An identity key names a scheme or lists components, not both and not neither.", "feed")
	ecMapIdentScheme      = reg("mapping.identity.unknown_scheme", kernel.CodeInvalidValue, "mapping", "The scheme of an identity key is not an identifier scheme of the model.", "feed,scheme,schemes")
	ecMapIdentColumn      = reg("mapping.identity.unknown_column", kernel.CodeInvalidValue, "mapping", "The column of an identity key by scheme is not in the dataset.", "feed,scheme,column,columns")
	ecMapIdentNotKey      = reg("mapping.identity.scheme_not_a_key", kernel.CodeInvalidValue, "mapping", "The scheme is not an identity key of the object type.", "feed,scheme,type")
	ecMapIdentProperty    = reg("mapping.identity.unknown_property", kernel.CodeInvalidValue, "mapping", "A component names a property the object type lacks.", "feed,property,type,properties")
	ecMapIdentOrg         = reg("mapping.identity.org_property", kernel.CodeInvalidValue, "mapping", "A component is an organization property, which an identity key cannot use.", "feed,property")
	ecMapIdentCompColumn  = reg("mapping.identity.component_unknown_column", kernel.CodeInvalidValue, "mapping", "A component reads a column the dataset lacks.", "feed,property,column,columns")
	ecMapIdentNoKey       = reg("mapping.identity.no_such_key", kernel.CodeInvalidValue, "mapping", "The object type has no identity key made of these properties in this order.", "feed,type,properties")
	ecMapIdfScheme        = reg("mapping.identifier.unknown_scheme", kernel.CodeInvalidValue, "mapping", "The scheme of an identifier is not an identifier scheme of the model.", "feed,scheme,schemes")
	ecMapIdfColumn        = reg("mapping.identifier.unknown_column", kernel.CodeInvalidValue, "mapping", "An identifier reads a column the dataset lacks.", "feed,scheme,column,columns")
	ecMapLinkIDMissing    = reg("mapping.link.id_missing", kernel.CodeInvalidValue, "mapping", "A link definition has no id.", "")
	ecMapLinkIDInvalid    = reg("mapping.link.id_invalid", kernel.CodeInvalidValue, "mapping", "The id of a link definition is not empty but does not follow the grammar of an id.", "id,pattern,max_length")
	ecMapLinkType         = reg("mapping.link.unknown_link_type", kernel.CodeInvalidValue, "mapping", "The link type is not one of the model.", "link,link_type,suggestions")
	ecMapLinkCond         = reg("mapping.link.condition_unsupported", kernel.CodeUnsupported, "mapping", "The condition is not one this build has.", "link,condition,supported")
	ecMapLinkFeed         = reg("mapping.link.unknown_feed", kernel.CodeInvalidValue, "mapping", "An end of the link names a feed that does not exist.", "link,end,feed")
	ecMapLinkBinding      = reg("mapping.link.binding_not_allowed", kernel.CodeInvalidValue, "mapping", "The link type does not allow a link between these two object types.", "link,link_type,from_type,to_type")
	ecMapLinkCount        = reg("mapping.link.key_columns_mismatch", kernel.CodeInvalidValue, "mapping", "The two ends have a different number of key columns, or none.", "link,from_count,to_count")
	ecMapLinkColumn       = reg("mapping.link.unknown_column", kernel.CodeInvalidValue, "mapping", "A key column of an end is not in the dataset of its feed.", "link,end,column,dataset")
	ecMapLinkBasis        = reg("mapping.link.invalid_basis", kernel.CodeInvalidValue, "mapping", "The basis of the link is none of the four.", "link,basis,valid")
	ecMapLinkKeyType      = reg("mapping.link.unknown_key_type", kernel.CodeInvalidValue, "mapping", "The key type is not a value type of the model.", "link,key_type")
	ecMapLAUnknown        = reg("mapping.link.attribute.unknown", kernel.CodeInvalidValue, "mapping", "The link type has no such attribute.", "link,link_type,attribute,attributes,org_property")
	ecMapLADeprecated     = reg("mapping.link.attribute.deprecated", kernel.CodeInvalidValue, "mapping", "The attribute of the link type is deprecated.", "link,attribute,replacement")
	ecMapLADup            = reg("mapping.link.attribute.duplicate", kernel.CodeInvalidValue, "mapping", "An attribute is given twice.", "link,attribute,first")
	ecMapLABoth           = reg("mapping.link.attribute.column_and_value", kernel.CodeInvalidValue, "mapping", "An attribute has both a column and a fixed value.", "link,attribute")
	ecMapLANeither        = reg("mapping.link.attribute.no_column_or_value", kernel.CodeInvalidValue, "mapping", "An attribute has neither a column nor a fixed value.", "link,attribute")
	ecMapLAEmpty          = reg("mapping.link.attribute.empty_value", kernel.CodeInvalidValue, "mapping", "The fixed value is empty.", "link,attribute")
	ecMapLAColumn         = reg("mapping.link.attribute.unknown_column", kernel.CodeInvalidValue, "mapping", "The attribute reads a column the dataset of the From feed lacks.", "link,attribute,column,dataset,feed,columns")
	ecMapLAKind           = reg("mapping.link.attribute.unsupported_kind", kernel.CodeUnsupported, "mapping", "The value of the attribute is of a kind a file cell does not give.", "link,attribute,kind")
	ecMapLAValue          = reg("mapping.link.attribute.value_mismatch", kernel.CodeInvalidValue, "mapping", "The fixed value does not fit the attribute.", "link,attribute,value")
)
