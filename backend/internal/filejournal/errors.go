package filejournal

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The error codes of this unit. A code is never renamed or reused.
const (
	// CodeLocked is a data folder that another live process has open.
	CodeLocked kernel.Code = "filejournal_locked"
	// CodeCorrupt is a record or snapshot that is damaged where no crash can explain it, or a log with records missing.
	CodeCorrupt kernel.Code = "filejournal_corrupt"
	// CodeUnsupportedFormat is a file whose format name or version this build does not know.
	CodeUnsupportedFormat kernel.Code = "filejournal_unsupported_format"
	// CodeIO is a read or write of the folder that failed; after a failed write the journal refuses all further writes.
	CodeIO kernel.Code = "filejournal_io"
)

var codeMeanings = map[kernel.Code]string{
	CodeLocked:            "another live process has the data folder open",
	CodeCorrupt:           "a record or snapshot is damaged in a way a crash does not explain, or records are missing",
	CodeUnsupportedFormat: "a file of the data folder has a format name or version this build does not know",
	CodeIO:                "a read or write of the data folder failed",
}

// Codes lists the codes of this unit, sorted.
func Codes() []kernel.Code {
	out := make([]kernel.Code, 0, len(codeMeanings))
	for c := range codeMeanings {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CodeMeaning is the one-line meaning of a code of this unit, or false.
func CodeMeaning(code kernel.Code) (string, bool) {
	m, ok := codeMeanings[code]
	return m, ok
}
