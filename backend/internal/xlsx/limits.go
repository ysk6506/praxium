package xlsx

import (
	"fmt"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The lists a reader keeps in memory while it reads a workbook, and the length of each text it keeps in them (ADR-0103). They are
// fixed values: no source definition changes them, since they bound what a part of a few hundred kilobytes can make a reader hold.
const (
	maxSheets          = 4096 // sheets of a workbook (the limit of Excel is far lower in practice)
	maxSheetNameBytes  = 255  // a sheet name (Excel allows 31 characters)
	maxSheetStateBytes = 32
	maxRelationships   = 10_000 // relationships of one part
	maxRelIDBytes      = 64     // the Id of a relationship, and the r:id of a sheet
	maxRelTypeBytes    = 256
	maxRelTargetBytes  = 512
	maxNumberFormats   = 4096 // custom number formats
	maxFormatCodeBytes = 256  // the code of a number format (Excel allows 255 characters)
	maxStyles          = 65_536
	maxColElements     = 16_384 // <col> elements of a sheet: the columns of Excel
	maxMergeRanges     = 100_000
)

// fixedLimit is the error of a part that passes a limit that cannot be raised: it names the limit.
func fixedLimit(name string, value any, where string) error {
	return kernel.Errorf(kernel.CodeInvalidValue, "the workbook is over the safety limit %s (%v) %s; this limit is fixed", name, value, where)
}

// checkLen refuses a text of a list that is longer than its limit.
func checkLen(name string, limit int, s, where string) error {
	if len(s) > limit {
		return fixedLimit(name, limit, fmt.Sprintf("by %s", where))
	}
	return nil
}
