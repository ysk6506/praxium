package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The web UI is static files in a file system the composition root gives (Config.UI). The server answers a GET or
// HEAD that no API route matches with a file of it, or with the page itself for an address the page's own router
// reads (ADR-0035). Everything under /v1 stays the API's: an unknown API path is a problem, never a page.
const (
	uiIndex       = "index.html"
	uiPlaceholder = "placeholder.html"
	uiAssetsDir   = "assets/"
	apiPrefix     = "/v1/"
)

// uiHeaders are sent with every file of the UI: the page may load only its own files, may not be framed, and the
// browser must not guess a type.
var uiHeaders = map[string]string{
	"Content-Security-Policy": "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
	"X-Content-Type-Options":  "nosniff",
	"X-Frame-Options":         "DENY",
	"Referrer-Policy":         "no-referrer",
}

// serveUI answers the request with a file of the UI and reports whether it did.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) bool {
	if s.ui == nil || !isUIMethod(r.Method) || isAPIPath(r.URL.Path) {
		return false
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == uiIndex {
		return s.servePage(w, r)
	}
	if s.serveFile(w, r, name) {
		return true
	}
	// An address without a file extension belongs to the page's own router; one with an extension is a missing file.
	if path.Ext(name) == "" {
		return s.servePage(w, r)
	}
	return false
}

func isUIMethod(method string) bool { return method == http.MethodGet || method == http.MethodHead }

func isAPIPath(p string) bool { return p == "/v1" || strings.HasPrefix(p, apiPrefix) }

// serveFile answers with one file of the UI, if there is one by that name.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	info, err := fs.Stat(s.ui, name)
	if err != nil || info.IsDir() || name == uiPlaceholder {
		return false
	}
	for k, v := range uiHeaders {
		w.Header().Set(k, v)
	}
	if strings.HasPrefix(name, uiAssetsDir) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, s.ui, name)
	return true
}

// servePage answers with the built page, or with the placeholder of a build that has none.
func (s *Server) servePage(w http.ResponseWriter, r *http.Request) bool {
	page, err := fs.ReadFile(s.ui, uiIndex)
	if err != nil {
		page, err = fs.ReadFile(s.ui, uiPlaceholder)
		if err != nil {
			return false
		}
	}
	for k, v := range uiHeaders {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(page)
	}
	return true
}
