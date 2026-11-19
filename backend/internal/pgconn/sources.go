package pgconn

import "embed"

// ownSources holds the Go files of this unit, for the test that scans them for what may not stay on a session
// (SessionStateViolations). Nothing else reads it.
//
//go:embed *.go
var ownSources embed.FS
