package mcpapi

// The protocol versions (revisions of the specification, named by date) the server speaks. 2026-07-28 is the main one
// and has no handshake; the two before it start with initialize. 2025-03-26 and 2024-11-05 are not spoken (ADR-0062 4).
const (
	version2026 = "2026-07-28"
	version2511 = "2025-11-25"
	version2506 = "2025-06-18"
)

// supportedVersions lists every version the server answers, in the order it tells clients. It is the one place the
// list is kept.
var supportedVersions = []string{version2026, version2511, version2506}

// legacyVersions are the versions that start with initialize and carry their version in a header.
var legacyVersions = []string{version2511, version2506}

// The names of the members a client puts in the _meta of a request of 2026-07-28.
const (
	metaVersionKey = "io.modelcontextprotocol/protocolVersion"
	metaCapsKey    = "io.modelcontextprotocol/clientCapabilities"
	metaInfoKey    = "io.modelcontextprotocol/clientInfo"
)

func isLegacy(v string) bool {
	for _, l := range legacyVersions {
		if l == v {
			return true
		}
	}
	return false
}

func isSupported(v string) bool { return v == version2026 || isLegacy(v) }

// noHeaderMessage is the text of the rejection of a request with no version header (the rows H of kg-mcp.md 2.1).
const noHeaderMessage = "The MCP-Protocol-Version header is required. Supported versions: 2026-07-28, 2025-11-25, 2025-06-18."

// rejection is a request that is turned away before it is answered: the HTTP status and the error.
type rejection struct {
	status int
	err    rpcError
}

// verdict is what the order of kg-mcp.md 2 makes of a request: the version whose rules answer it, or the rejection.
// Initialize is the version-less case: it carries the versions of its own negotiation.
type verdict struct {
	version    string
	initialize bool
	reject     *rejection
}

// decideVersion is the order of kg-mcp.md 2: (a) initialize is the negotiation of the older versions and looks at no
// header; (b) a _meta that names 2026-07-28 makes it the new version and the header must agree; (c) otherwise the header
// names an older version, or is missing, or is not one the server speaks, which is refused. The request has been
// read, so its method and _meta are known. headerPresent tells a missing header from an empty one.
func decideVersion(req *rpcRequest, header string, headerPresent bool) verdict {
	if req.method == "initialize" {
		return verdict{initialize: true}
	}
	if metaVer, ok := metaVersion(req); ok {
		return decideByMeta(metaVer, header, headerPresent)
	}
	switch {
	case !headerPresent || header == "":
		return reject(400, codeHeaderMismatch, noHeaderMessage, map[string]any{"supported": supportedVersions})
	case isLegacy(header):
		return verdict{version: header}
	case header == version2026:
		return reject(400, codeInvalidParams, `The request must carry "`+metaVersionKey+`" and "`+metaCapsKey+`" in params._meta.`, nil)
	}
	return unsupported(header)
}

// metaVersion reads the version a request names in its _meta.
func metaVersion(req *rpcRequest) (string, bool) {
	meta, ok := objectAt(req.params, "_meta")
	if !ok {
		return "", false
	}
	v, ok := meta[metaVersionKey].(string)
	return v, ok
}

// decideByMeta settles a request whose _meta names a version. The header must be the same version (HeaderMismatch
// otherwise); an older version named in the _meta follows its own rules and its field is not read further.
func decideByMeta(metaVer, header string, headerPresent bool) verdict {
	if !headerPresent || header != metaVer {
		msg := "Header mismatch: the MCP-Protocol-Version header does not match the protocol version in params._meta."
		if !headerPresent {
			msg = "Header mismatch: the MCP-Protocol-Version header is missing."
		}
		return reject(400, codeHeaderMismatch, msg, nil)
	}
	if !isSupported(metaVer) {
		return unsupported(metaVer)
	}
	return verdict{version: metaVer}
}

func unsupported(requested string) verdict {
	clean := cleanShort(requested)
	return reject(400, codeUnsupportedVers, "Unsupported protocol version.", map[string]any{"supported": supportedVersions, "requested": clean})
}

func reject(status, code int, msg string, data any) verdict {
	return verdict{reject: &rejection{status: status, err: rpcError{Code: code, Message: msg, Data: data}}}
}
