package definitions

import (
	"fmt"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Limits are the bounds the service keeps. A field that is zero takes its default.
type Limits struct {
	// MaxBodyBytes bounds the normalized body of a revision.
	MaxBodyBytes int
	// MaxItemsPerSet bounds the items of a definition set.
	MaxItemsPerSet int
	// MaxBranchEntries bounds the changed items of a branch.
	MaxBranchEntries int
	// MaxOpenBranches bounds the branches that are not merged or closed.
	MaxOpenBranches int
	// MaxNameLen bounds the length of item names, branch names and titles, in bytes.
	MaxNameLen int
	// MaxMessageLen bounds the length of messages and summaries, in bytes.
	MaxMessageLen int
}

// The default limits.
const (
	DefaultMaxBodyBytes     = 1 << 20
	DefaultMaxItemsPerSet   = 20000
	DefaultMaxBranchEntries = 2000
	DefaultMaxOpenBranches  = 100
	DefaultMaxNameLen       = 200
	DefaultMaxMessageLen    = 4000
)

func (l Limits) withDefaults() Limits {
	pick := func(v, def int) int {
		if v > 0 {
			return v
		}
		return def
	}
	return Limits{
		MaxBodyBytes:     pick(l.MaxBodyBytes, DefaultMaxBodyBytes),
		MaxItemsPerSet:   pick(l.MaxItemsPerSet, DefaultMaxItemsPerSet),
		MaxBranchEntries: pick(l.MaxBranchEntries, DefaultMaxBranchEntries),
		MaxOpenBranches:  pick(l.MaxOpenBranches, DefaultMaxOpenBranches),
		MaxNameLen:       pick(l.MaxNameLen, DefaultMaxNameLen),
		MaxMessageLen:    pick(l.MaxMessageLen, DefaultMaxMessageLen),
	}
}

// LimitError says which limit a request went over. Its code is invalid_value.
type LimitError struct {
	// Limit is the name of the field of Limits.
	Limit string
	Max   int
	Got   int
	err   *kernel.Error
}

// Error implements error.
func (e *LimitError) Error() string { return e.err.Error() }

// Unwrap gives the kernel error that carries the code.
func (e *LimitError) Unwrap() error { return e.err }

func overLimit(name string, max, got int) error {
	if got <= max {
		return nil
	}
	return &LimitError{Limit: name, Max: max, Got: got,
		err: kernel.Errorf(kernel.CodeInvalidValue, "the limit %s is %d and this is %d", name, max, got)}
}

// StaleError is the refusal to merge a branch whose items were changed on the main line after its base.
type StaleError struct {
	BaseSetNo int64
	HeadSetNo int64
	// Items are the items that both the branch and the main line changed.
	Items []string
	err   *kernel.Error
}

// Error implements error.
func (e *StaleError) Error() string { return e.err.Error() }

// Unwrap gives the kernel error that carries the code (conflict).
func (e *StaleError) Unwrap() error { return e.err }

func staleError(base, head int64, items []string) error {
	return &StaleError{BaseSetNo: base, HeadSetNo: head, Items: items,
		err: kernel.Errorf(kernel.CodeConflict, "stale: D-%d changed %d item(s) of this branch since its base D-%d: %s",
			head, len(items), base, fmt.Sprint(items))}
}

// CheckFailedError is the refusal to merge because the check found problems; the set was not made.
type CheckFailedError struct {
	Problems []Problem
	err      *kernel.Error
}

// Error implements error.
func (e *CheckFailedError) Error() string { return e.err.Error() }

// Unwrap gives the kernel error that carries the code (invalid_value).
func (e *CheckFailedError) Unwrap() error { return e.err }

func checkFailed(problems []Problem) error {
	return &CheckFailedError{Problems: problems,
		err: kernel.Errorf(kernel.CodeInvalidValue, "the check found %d problem(s) in the merged set; nothing was merged", len(problems))}
}
