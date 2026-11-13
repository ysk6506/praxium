package cli

import (
	"errors"
	"fmt"
)

// Exit codes of the client. A problem's group decides the code, so a script
// can tell "log in again" from "not allowed" from "not there" without reading text.
const (
	ExitOK         = 0
	ExitGeneral    = 1
	ExitUsage      = 2
	ExitTransport  = 3
	ExitAuth       = 4
	ExitPermission = 5
	ExitNotFound   = 6
	ExitInvalid    = 7
	ExitConflict   = 8
	ExitRate       = 9
	ExitBlocked    = 10
	ExitStale      = 11
	ExitServer     = 12
)

var groupExit = map[string]int{
	"auth": ExitAuth, "permission": ExitPermission, "not_found": ExitNotFound,
	"invalid_input": ExitInvalid, "conflict": ExitConflict, "rate_limited": ExitRate,
	"blocked": ExitBlocked, "stale": ExitStale, "server": ExitServer,
}

// ProblemError is a problem+json answer of the server.
type ProblemError struct {
	Status  int            `json:"status"`
	Code    string         `json:"code"`
	Title   string         `json:"title"`
	TraceID string         `json:"trace_id"`
	Params  map[string]any `json:"params"`
}

func (p *ProblemError) Error() string {
	return fmt.Sprintf("%s (HTTP %d, trace %s)", p.Code, p.Status, p.TraceID)
}

// Group is the part of the code before the dot.
func (p *ProblemError) Group() string {
	for i := 0; i < len(p.Code); i++ {
		if p.Code[i] == '.' {
			return p.Code[:i]
		}
	}
	return p.Code
}

// usageError is a command line the client cannot read.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// transportError is a request that got no answer.
type transportError struct{ err error }

func (e transportError) Error() string { return "cannot reach the server: " + e.err.Error() }
func (e transportError) Unwrap() error { return e.err }

// ExitCode maps an error to the exit code of the command.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var problem *ProblemError
	var usage usageError
	var transport transportError
	switch {
	case errors.As(err, &problem):
		if code, ok := groupExit[problem.Group()]; ok {
			return code
		}
		return ExitGeneral
	case errors.As(err, &usage):
		return ExitUsage
	case errors.As(err, &transport):
		return ExitTransport
	}
	return ExitGeneral
}
