package mcpapi

import (
	"log/slog"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// logRead writes the read record of a tool call: one line per call (REQ-514, kg-mcp.md 9). It names the token and the
// trace of the call, the KG, the tool, the object types touched, the ids the caller named, how many results and whether
// they were cut, the version that answered, and the client as it called itself (cleaned and cut short). It carries no
// value of a filter and no value of an attribute.
func (h *Handler) logRead(ex *exchange, tool string, res Result, took time.Duration) {
	outcome := "ok"
	if res.Problem != nil {
		outcome = res.Problem.Code
	}
	h.cfg.Log.Info("mcp_read",
		slog.Bool("read_record", true),
		slog.String("trace_id", ex.a.trace),
		slog.String("token_id", ex.subject.TokenID),
		slog.String("account", ex.subject.ID),
		slog.String("kg", cleanShort(res.Read.KG)),
		slog.String("tool", tool),
		slog.Any("object_types", cleanList(res.Read.ObjectTypes)),
		slog.Any("object_ids", cleanList(res.Read.ObjectIDs)),
		slog.Int("result_count", res.Read.Count),
		slog.Bool("truncated", res.Read.Truncated),
		slog.String("outcome", outcome),
		slog.String("protocol_version", ex.version),
		slog.String("client_info", clientInfoOf(ex.req)),
		slog.String("traceparent", metaText(ex.req, "traceparent", 55)),
		slog.Int64("duration_ms", took.Milliseconds()),
	)
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, cleanShort(s))
	}
	return out
}

// clientInfoOf is "name/version" of the client as it wrote itself in _meta (2026-07-28 only; the older versions say it
// once at initialize and the server remembers nothing), cleaned and cut short: it is the caller's own text.
func clientInfoOf(req *rpcRequest) string {
	info, ok := objectAt(req.params, "_meta", metaInfoKey)
	if !ok {
		return ""
	}
	name, _ := info["name"].(string)
	ver, _ := info["version"].(string)
	n, _ := kgread.CleanText(name, clientText)
	v, _ := kgread.CleanText(ver, clientText)
	if v == "" {
		return n
	}
	return n + "/" + v
}

// metaText is a string member of params._meta, cleaned and cut to max characters.
func metaText(req *rpcRequest, key string, max int) string {
	meta, ok := objectAt(req.params, "_meta")
	if !ok {
		return ""
	}
	s, _ := meta[key].(string)
	out, _ := kgread.CleanText(s, max)
	return out
}
