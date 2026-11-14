package main

import (
	"io/fs"

	"github.com/ysk6506/praxium/backend/internal/webui"
)

// uiFor is the web UI the server serves: the embedded files, or nothing when -web=false asks for the API only.
func uiFor(o serveOptions) fs.FS {
	if !o.web {
		return nil
	}
	return webui.Files()
}

// uiState names what the server serves at /, for the startup log.
func uiState(o serveOptions) string {
	switch {
	case !o.web:
		return "off"
	case webui.Built(webui.Files()):
		return "built"
	}
	return "placeholder"
}
