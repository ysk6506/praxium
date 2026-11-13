package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type ctxKey int

const traceKey ctxKey = 1

// traceOf returns the trace id the middleware put on a request's context.
func traceOf(ctx context.Context) string {
	id, _ := ctx.Value(traceKey).(string)
	return id
}

// chain wraps a handler in the middleware, outermost first: trace id, access
// log, panic recovery, body limit and media type of the body.
func (s *Server) chain(next http.Handler) http.Handler {
	return s.traceMiddleware(s.logMiddleware(s.recoverMiddleware(s.bodyMiddleware(next))))
}

// statusWriter remembers the status and the size of a response for the log.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wrote {
		w.status, w.wrote = status, true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.status, w.wrote = http.StatusOK, true
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// traceMiddleware gives the request a trace id: the one of a valid W3C
// traceparent header, else a new one. It goes on the context and in X-Trace-Id.
func (s *Server) traceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := traceFromParent(r.Header.Get("Traceparent"))
		if !ok {
			id = s.ids.Next("tr")
		}
		w.Header().Set("X-Trace-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceKey, id)))
	})
}

// traceFromParent reads the trace id of `00-<32 hex>-<16 hex>-<2 hex>`.
func traceFromParent(header string) (string, bool) {
	parts := strings.Split(header, "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return "", false
	}
	all := strings.Join(parts[1:], "")
	return parts[1], isLowerHex(all) && strings.Trim(all, "0") != ""
}

// isLowerHex reports whether s is only lowercase hexadecimal digits.
func isLowerHex(s string) bool {
	for _, ch := range s {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

// logMiddleware writes one structured line per request after it is answered.
func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.clock.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.log.Info("request",
			"trace_id", traceOf(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"bytes", sw.bytes,
			"duration_ms", s.clock.Now().Sub(start).Milliseconds(),
		)
	})
}

// recoverMiddleware turns a panic of a handler into a 500 problem; the panic is
// logged with the trace id and never shown to the caller.
func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("a handler panicked", "panic", fmt.Sprint(v), "trace_id", traceOf(r.Context()))
				c := &call{s: s, w: w, r: r, trace: traceOf(r.Context())}
				c.fail("server.internal", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// bodyMiddleware limits the size of a body and, for methods that carry one,
// checks its media type is JSON.
func (s *Server) bodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := &call{s: s, w: w, r: r, trace: traceOf(r.Context())}
		if r.ContentLength > s.maxBody {
			c.fail("invalid_input.body_too_large", map[string]any{"max_bytes": s.maxBody})
			return
		}
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, s.maxBody)
			if carriesBody(r.Method) && r.ContentLength != 0 && !jsonBody(r.Header.Get("Content-Type")) {
				c.fail("invalid_input.unsupported_media_type", map[string]any{"content_type": r.Header.Get("Content-Type")})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func carriesBody(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch
}

func jsonBody(contentType string) bool {
	media := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	return media == "application/json" || media == "application/merge-patch+json"
}

// acceptsJSON implements the part of content negotiation this API needs: it
// answers only in JSON, so a request that lists media types must list one of
// them (with a quality above zero), and a request without Accept takes JSON.
func acceptsJSON(header string) bool {
	if strings.TrimSpace(header) == "" {
		return true
	}
	for _, part := range strings.Split(header, ",") {
		media, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		media = strings.ToLower(strings.TrimSpace(media))
		if (media == "*/*" || media == "application/*" || media == "application/json" || media == problemMediaType) && qualityOf(params) > 0 {
			return true
		}
	}
	return false
}

// qualityOf reads q=... from the parameters of a media range; the default is 1.
func qualityOf(params string) float64 {
	for _, p := range strings.Split(params, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok && strings.EqualFold(key, "q") {
			var q float64
			if _, err := fmt.Sscanf(value, "%g", &q); err == nil {
				return q
			}
			return 0
		}
	}
	return 1
}
