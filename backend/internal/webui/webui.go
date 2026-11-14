package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embedded embed.FS

// Index is the file of the built UI that the server answers with for the page itself.
const Index = "index.html"

// Placeholder is the page of a build that has no web UI.
const Placeholder = "placeholder.html"

// Files is the folder of the embedded web UI: the built files when the build script has copied them in, and always
// the placeholder. The files sit at the root of the returned file system.
func Files() fs.FS {
	// Sub fails only for an invalid name; "dist" is a fixed, valid folder of the embed pattern above.
	sub, _ := fs.Sub(embedded, "dist")
	return sub
}

// Built reports whether the files hold the built web UI and not only the placeholder.
func Built(files fs.FS) bool {
	info, err := fs.Stat(files, Index)
	return err == nil && !info.IsDir()
}
