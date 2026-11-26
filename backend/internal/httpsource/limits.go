package httpsource

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The limits of the requests a read makes, for the datasets that ask with the values of another dataset (ADR-0111) and for the run
// they are in. A definition may lower a limit of its dataset or raise it up to the ceiling the server has; the server has the
// values below until the operator's setting says others (a server setting is made when the server is put together).

// Ceilings are what no definition may pass, and the limits of the whole run. A zero field is the value in the comment; one above
// its hard limit is the hard limit.
type Ceilings struct {
	// ChildRequests is the most parent.max_requests may be (50,000), RunRequests the most requests of a run (200,000), ChildTime
	// the most parent.max_seconds may be (4 hours) and RunTime the longest a run may take (2 hours, at most 6), measured on the
	// clock the reader was given from the first request. The time of the run is held to by the datasets that have a parent, which
	// are the reads that take long; the others are held to the requests only.
	ChildRequests int
	RunRequests   int
	ChildTime     time.Duration
	RunTime       time.Duration
	// BatchSize is the most values of a batch (1,000); ParentValues and ParentBytes are the most values (500,000) and bytes (64
	// MiB) of the values of the parent that are held, and URLBytes and BodyBytes the longest address (8 KiB) and body (1 MiB) of a
	// request of a child.
	BatchSize    int
	ParentValues int
	ParentBytes  int
	URLBytes     int
	BodyBytes    int
}

// The hard limits and the starting values (ADR-0111, from the table of Q14).
const (
	hardRunTime       = 6 * time.Hour
	defaultRunTime    = 2 * time.Hour
	defaultRunRequest = 200_000
	defaultParentVals = 500_000
	defaultParentByte = 64 << 20
	defaultURLBytes   = 8 << 10
	defaultBodyBytes  = 1 << 20
	maxChildRequests  = ingest.MaxChildRequests
	maxBatchSize      = ingest.MaxBatchSize
)

func (c Ceilings) withDefaults() Ceilings {
	c.ChildRequests = capped(c.ChildRequests, maxChildRequests, maxChildRequests)
	c.RunRequests = capped(c.RunRequests, defaultRunRequest, defaultRunRequest)
	c.ChildTime = cappedTime(c.ChildTime, ingest.MaxChildSeconds*time.Second, ingest.MaxChildSeconds*time.Second)
	c.RunTime = cappedTime(c.RunTime, defaultRunTime, hardRunTime)
	c.BatchSize = capped(c.BatchSize, maxBatchSize, maxBatchSize)
	c.ParentValues = capped(c.ParentValues, defaultParentVals, defaultParentVals)
	c.ParentBytes = capped(c.ParentBytes, defaultParentByte, defaultParentByte)
	c.URLBytes = capped(c.URLBytes, defaultURLBytes, defaultURLBytes)
	c.BodyBytes = capped(c.BodyBytes, defaultBodyBytes, defaultBodyBytes)
	return c
}

func capped(v, def, hard int) int {
	if v <= 0 {
		return def
	}
	return min(v, hard)
}

func cappedTime(v, def, hard time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return min(v, hard)
}

// budget is what a run has spent: the requests it sent, and when it sent the first. A Reader has one for its whole life, so a
// Reader is made for each run: a second run with the same Reader goes on from what the first spent (ADR-0111).
type budget struct {
	mu       sync.Mutex
	requests int
	begun    bool
	start    time.Time
	// peak is the most values of a parent a read held at one time (a test reads it).
	peak int
}

// childBudget is what one read of a child dataset may spend: its requests (the parent's, the token's and its own), and until when.
type childBudget struct {
	requests, max int
	deadline      time.Time
}

// limitError says that a limit was reached. It is not a failure of the source: the dataset ends not complete with the reason.
type limitError struct{ reason string }

func (e *limitError) Error() string { return e.reason }

var errNotFound = errors.New("the source answered 404")

// charge counts the request that is about to be sent against the limits of the run and of the child dataset being read. It is the
// one place requests are counted, and every request is sent through it: the parent read again, the pages of the child, the
// requests for a token and the sends again of a request that failed.
func (rn *run) charge() error {
	now := rn.r.cfg.Clock.Now()
	b, ceil := rn.r.bud, rn.r.ceil
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.begun {
		b.begun, b.start = true, now
	}
	switch {
	case b.requests >= ceil.RunRequests:
		return &limitError{"the run has sent the most requests it may (" + strconv.Itoa(ceil.RunRequests) + "), so this dataset was not read to its end"}
	}
	if c := rn.child; c != nil {
		switch {
		case now.Sub(b.start) >= ceil.RunTime:
			return &limitError{"the run has taken the longest it may (" + ceil.RunTime.String() + "), so this dataset was not read to its end"}
		case c.requests >= c.max:
			return &limitError{"the dataset needed more than parent.max_requests (" + strconv.Itoa(c.max) + ") requests, the parent's, the token's and its own; the rest was not read"}
		case !now.Before(c.deadline):
			return &limitError{"the dataset took longer than parent.max_seconds; the rest was not read"}
		}
		c.requests++
	}
	b.requests++
	return nil
}

// chargeChild counts a request of the child against what a preview (ten) and a check (one) may send, whatever the size of a unit and
// the number of its pages; the requests of the parent and for a token are not counted. It is called before charge.
func (rn *run) chargeChild(p plan) error {
	if rn.unit == nil || p.token || (!rn.keep && !rn.firstOnly) {
		return nil
	}
	most, what := previewChildRequests, "preview"
	if rn.firstOnly {
		most, what = checkChildRequests, "check"
	}
	if rn.sentOfChild >= most {
		return &limitError{"a " + what + " sends at most " + strconv.Itoa(most) + " requests of a dataset that has a parent"}
	}
	rn.sentOfChild++
	return nil
}

// limitOf finds a limit error inside err.
func limitOf(err error) (*limitError, bool) {
	var le *limitError
	return le, errors.As(err, &le)
}

// isNotFound says that err is the answer 404 of the source.
func isNotFound(err error) bool { return errors.Is(err, errNotFound) }

// notePeak keeps the most values of a parent that a read held at one time.
func (b *budget) notePeak(n int) {
	b.mu.Lock()
	b.peak = max(b.peak, n)
	b.mu.Unlock()
}
