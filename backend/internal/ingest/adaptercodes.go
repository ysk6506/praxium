package ingest

import (
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var adapterCodeTable []*errCode

// regAdapter adds a code of the definition of an HTTP or PostgreSQL source to its own table. The table of the codes of
// the file flow (codeTable) is the one a test of the current definitions runs through, and these codes are given only
// when a source is read for the adapters (ParseAdapterSource), so they are kept apart, as the codes of the new compile are.
func regAdapter(code string, k kernel.Code, meaning, params string) *errCode {
	ec := &errCode{code: code, kernel: k, doc: "source", meaning: meaning}
	if params != "" {
		ec.params = strings.Split(params, ",")
	}
	adapterCodeTable = append(adapterCodeTable, ec)
	return ec
}

// AdapterErrorCodes is the table of the codes only the reading of an HTTP or PostgreSQL source gives, sorted by code.
func AdapterErrorCodes() []ErrorCode {
	out := make([]ErrorCode, 0, len(adapterCodeTable))
	for _, ec := range adapterCodeTable {
		out = append(out, ErrorCode{Code: ec.code, Kernel: ec.kernel, Document: ec.doc, Meaning: ec.meaning, Params: append([]string{}, ec.params...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

const (
	inv  = kernel.CodeInvalidValue
	unsp = kernel.CodeUnsupported
)

var (
	ecAdConnMissing  = regAdapter("source.connection.missing", inv, "An http or postgres source has no connection.", "source,adapter")
	ecAdConnNotFile  = regAdapter("source.connection.not_for_adapter", inv, "A file source has a connection, which only the http and postgres adapters read.", "source,adapter")
	ecAdConnField    = regAdapter("source.connection.field_not_for_adapter", inv, "A field of the connection belongs to the other adapter.", "source,adapter,field")
	ecAdConnRequired = regAdapter("source.connection.field_missing", inv, "A field the connection needs is not given.", "source,adapter,field")
	ecAdBaseURL      = regAdapter("source.connection.base_url_invalid", inv, "The base address is not an http or https address of a host with a path and nothing else.", "source,reason")
	ecAdHost         = regAdapter("source.connection.host_invalid", inv, "The host is not one name or one IP address.", "source,reason")
	ecAdPort         = regAdapter("source.connection.port_invalid", inv, "The port is not between 1 and 65535.", "source,port")
	ecAdSSLMode      = regAdapter("source.connection.sslmode_invalid", inv, "The sslmode is not one of the two.", "source,sslmode,supported")
	ecAdCAPEM        = regAdapter("source.connection.ca_pem_invalid", inv, "The ca_pem is not one or more certificates in PEM form.", "source,reason")
	ecAdRange        = regAdapter("source.connection.value_out_of_range", inv, "A number of the connection is outside the range it may have.", "source,field,value,min,max")
	ecAdHeader       = regAdapter("source.connection.header_not_allowed", inv, "A header of the connection or of the authentication is not a valid name, is one the product sets itself or that servers log, or has a bad value or a bad source.", "source,header,reason")
	ecAdAuthKind     = regAdapter("source.auth.kind_unsupported", unsp, "The kind of the authentication is not one of the six.", "source,kind,supported")
	ecAdAuthMissing  = regAdapter("source.auth.field_missing", inv, "A field the kind of the authentication needs is not given.", "source,kind,field")
	ecAdAuthNot      = regAdapter("source.auth.field_not_for_kind", inv, "A field of the authentication is not read by its kind.", "source,kind,field")
	ecAdSecretID     = regAdapter("source.credential.secret_id_invalid", inv, "The id of a secret does not follow the grammar of an id.", "source,field,id,pattern,max_length")
	ecAdTokenReq     = regAdapter("source.auth.token_request_invalid", inv, "The request that gives a token, or how its token is read and sent, has a path, a format, a body or a value that is not valid.", "source,field,reason")
	ecAdPlaceholder  = regAdapter("source.auth.placeholder_invalid", inv, "A text has a {{credential.<field>}} or {{auth.token}} inside it where the whole value has to be the placeholder.", "source,field,value")
	ecAdElsewhere    = regAdapter("source.placeholder.not_allowed_here", inv, "A {{credential.<field>}} or {{auth.token}} is written where it may not be: in an address, in a name, in a text that is not a whole value of a body, or where the authentication gives none.", "source,field")
	ecAdBraces       = regAdapter("source.text.braces_not_allowed", inv, "A text has a { or a } that is not written as {{ or }}; no variable is defined yet.", "source,field")
	ecAdPathBad      = regAdapter("source.path.invalid", inv, "A path into a record or a response is not a path.", "source,dataset,field,path,reason")
	ecAdPathUnsup    = regAdapter("source.path.unsupported", unsp, "A path uses a notation that is written down and not read yet ([*] or [?name=value]).", "source,dataset,field,path,notation")
	ecAdDsParser     = regAdapter("source.dataset.parser_not_for_adapter", inv, "A dataset of an http or postgres source has a parser; its records are JSON objects already.", "source,dataset,adapter")
	ecAdDsField      = regAdapter("source.dataset.field_not_for_adapter", inv, "A field of the dataset belongs to another adapter.", "source,dataset,adapter,field")
	ecAdRequest      = regAdapter("source.dataset.request_missing", inv, "A dataset of an http source has no request.", "source,dataset")
	ecAdMethod       = regAdapter("source.request.method_invalid", inv, "The method of the request is not GET or POST.", "source,dataset,method,supported")
	ecAdPostRead     = regAdapter("source.request.post_not_declared", inv, "A dataset reads with POST and does not say post_reads.", "source,dataset")
	ecAdBodyPost     = regAdapter("source.request.body_needs_post", inv, "A body, or a cursor sent in the body, is given for a request that is not a POST.", "source,dataset")
	ecAdReqPath      = regAdapter("source.request.path_invalid", inv, "The path of a request is not a path under the base address.", "source,dataset,field,reason")
	ecAdPageKind     = regAdapter("source.pagination.kind_unsupported", unsp, "The kind of the pagination is not one of the six.", "source,dataset,kind,supported")
	ecAdPageMissing  = regAdapter("source.pagination.field_missing", inv, "A field the kind of the pagination needs is not given.", "source,dataset,kind,field")
	ecAdPageNot      = regAdapter("source.pagination.field_not_for_kind", inv, "A field of the pagination is not read by its kind.", "source,dataset,kind,field")
	ecAdPageRange    = regAdapter("source.pagination.value_out_of_range", inv, "A number of the pagination is outside the range it may have.", "source,dataset,field,value,min,max")
	ecAdPageParam    = regAdapter("source.pagination.param_invalid", inv, "The name of a parameter of the pagination is not a name, or is the name of a parameter the query already has.", "source,dataset,field,reason")
	ecAdTableSQL     = regAdapter("source.dataset.table_or_sql", inv, "A dataset of a postgres source has both table and sql, or neither.", "source,dataset,reason")
	ecAdTable        = regAdapter("source.dataset.table_invalid", inv, "The table is not a name, or a schema and a name, of plain identifiers.", "source,dataset,table")
	ecAdSQL          = regAdapter("source.dataset.sql_invalid", inv, "The statement is empty or has a character a statement cannot have.", "source,dataset,reason")
	ecAdCollation    = regAdapter("source.dataset.key_collation_invalid", inv, "The key_collation is not a name of plain identifiers.", "source,dataset,key_collation")
)
