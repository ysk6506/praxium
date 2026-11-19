package ingest

import "context"

// What an adapter tells the service that runs it, and the optional port that reads a few pages for a person to see
// (ADR-0090). An adapter writes no log of its own: it tells an Observer what happened and the service decides what to
// log. That keeps the question "can a secret get into a log" to one place, the Event, which has no field that could
// hold one.

// EventKind is what happened.
type EventKind string

const (
	EventRequest    EventKind = "request"    // a request or a query begins
	EventResponse   EventKind = "response"   // the answer came
	EventRetry      EventKind = "retry"      // the read waits and tries again
	EventPage       EventKind = "page"       // a page was given
	EventIncomplete EventKind = "incomplete" // the dataset will not be read to its end
	EventFailed     EventKind = "failed"     // the read ended with an error
)

// Event is what an adapter may tell about one thing that happened. It is made only of what a log may hold: where (the
// source, the dataset, the host and port), what (the method, the path without its query, the status), how it went (the
// attempt, the time, the bytes, the page, the rows) and the class and kind of a failure. It has no field for a query,
// a header, a body, a statement, a key value or a secret, and a test keeps it so.
type Event struct {
	Kind          EventKind
	Source        string
	Dataset       string
	Host          string
	Port          int
	Method        string
	Path          string
	Status        int
	Attempt       int
	ElapsedMillis int64
	Bytes         int
	Page          int
	Rows          int
	Class         CheckKind
	ErrorKind     string
	// Reason is a phrase of the reader's own (why it waits, why the dataset is not complete), never a sentence of the
	// source or of a driver.
	Reason string
}

// Observer is told what happens in a read. An adapter that is given none tells nobody.
type Observer interface {
	Observe(Event)
}

// ObserverFunc is a function as an Observer.
type ObserverFunc func(Event)

// Observe calls the function.
func (f ObserverFunc) Observe(e Event) { f(e) }

// The sizes of a preview: how many rows and pages a person is shown (REQ-491 gives the most: 1,000 rows).
const (
	DefaultPreviewPages = 1
	DefaultPreviewRows  = 200
	MaxPreviewRows      = 1000
)

// PreviewOptions says how much to read for a preview. A value that is not set is the default and one over the most is
// the most.
type PreviewOptions struct {
	MaxPages int
	MaxRows  int
}

// WithDefaults gives the options with the defaults and the most applied.
func (o PreviewOptions) WithDefaults() PreviewOptions {
	if o.MaxPages <= 0 {
		o.MaxPages = DefaultPreviewPages
	}
	if o.MaxRows <= 0 {
		o.MaxRows = DefaultPreviewRows
	}
	o.MaxRows = min(o.MaxRows, MaxPreviewRows)
	return o
}

// Exchange is one request and its answer as a person may see them: the method, the path, the query the definition wrote
// (the names and the values the definition gave), the status, and the headers by name with the values of the few that
// are shown. A header, a body and the query of an address the server gave are not here.
type Exchange struct {
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Query   []NameValue `json:"query,omitempty"`
	Status  int         `json:"status"`
	Headers []NameValue `json:"headers,omitempty"`
}

// Preview is what a Previewer gives: the first pages as rows, the exchanges that made them, and whether any value was
// left out because its name is a secret's.
type Preview struct {
	Pages     []Page
	Exchanges []Exchange
	Masked    bool
}

// Previewer is the optional port of a reader that can read the first pages of a dataset for a person to look at. A
// reader that is a PageReader may also be a Previewer; the service asks with a type assertion, so that no reader and no
// fake has to have the method. The rows are made by MaskPage and the exchanges by MaskHeaders, so that what is shown has
// no secret's value, and a failure is a SourceError.
type Previewer interface {
	Preview(ctx context.Context, source string, dataset Dataset, opts PreviewOptions) (Preview, error)
}
