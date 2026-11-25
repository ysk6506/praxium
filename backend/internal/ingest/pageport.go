package ingest

import (
	"context"
	"errors"
	"iter"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The read port of the new ingestion flow (ADR-0086). A source gives one dataset as a stream of pages, a page being a
// bundle of rows, so that whoever reads holds one page and never the whole dataset. An adapter (a file, an HTTP API,
// a database) stands on this port; the pipeline never opens a source itself.

// The starting values of the size of a page and of a dataset (ingestion-at-scale.md 3, P2, and 8, decision 12). They
// are values of the product to be measured, and a caller may give its own in PageOptions.
const (
	DefaultPageRows       = 10_000
	DefaultPageBytes      = 8 << 20
	DefaultDatasetRowsMax = 50_000_000
)

// PageOptions are the sizes a reader cuts pages by. A zero value is the default.
type PageOptions struct {
	// MaxRows and MaxBytes cut a page at the row count or the size of the cells, whichever it reaches first. A single
	// row larger than MaxBytes is a page of its own.
	MaxRows  int
	MaxBytes int
	// MaxDatasetRows is the most data rows a dataset may have. A dataset that goes over it is cut off and the last page
	// says it is not complete.
	MaxDatasetRows int
	// Resume is the token of the page after which to go on. It is opaque and only the source reads it; a reader that
	// cannot go on from a token reads from the first page and ignores it.
	Resume []byte
}

// WithDefaults gives the options with the default for every value that is not set.
func (o PageOptions) WithDefaults() PageOptions {
	if o.MaxRows <= 0 {
		o.MaxRows = DefaultPageRows
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultPageBytes
	}
	if o.MaxDatasetRows <= 0 {
		o.MaxDatasetRows = DefaultDatasetRowsMax
	}
	return o
}

// Completeness says whether the whole dataset was read. It is the last page's to give: a read that stopped at a limit,
// or at an error of the source, is not complete, and the reason says which.
type Completeness struct {
	Complete bool
	Reason   string
}

// Shape is what the first page of a table with a header says about the header: the declared columns it lacks, the
// names no column reads, the names it read and a hint at why the header may not be the one meant. A header that lacks
// a column is a changed shape: that dataset has no rows and is not used (ingestion.md 9.1).
type Shape struct {
	Missing []string
	Extra   []string
	Header  []string
	Hint    string
}

// Page is one bundle of the rows of a dataset. The rows are the lines that gave a natural key; Unreadable are the lines
// that did not, and Skipped counts the lines passed over on purpose. Token is what the source needs to go on after this
// page, kept with the page and never read by anyone else. Only the last page has Last and End.
type Page struct {
	Dataset    string
	No         int
	Rows       []Row
	Unreadable []Unreadable
	Skipped    map[string]int
	// Shape is set on the first page of a table that has a header.
	Shape *Shape
	// Bytes is the size of the cells of the rows, the measure MaxBytes cuts by.
	Bytes int
	Token []byte
	Last  bool
	End   Completeness
}

// Received is every line of the page that was a data line, read or not.
func (p Page) Received() int { return len(p.Rows) + len(p.Unreadable) }

// PageReader is the read port of the sources. It has no method that writes to the source (ingestion.md 3.2).
//
// ReadPages gives the dataset as a sequence of pages: the caller holds one page at a time, and a page is not given again.
// The sequence ends with a page that has Last set, whose End says whether the whole dataset was read; an error ends it at
// once, and an error with the code not_found says the dataset is not given this time, which is no failure. The caller
// stops by leaving the loop, and the reader closes what it has open then. It looks at the context between pages.
//
// Check is the connection check: it reads the first page for real, so that it tells what the sources's setting and
// access give and not only that a connection opens (ADR-0086). CheckFirstPage is the one way to make it.
type PageReader interface {
	ReadPages(ctx context.Context, source string, dataset Dataset, opts PageOptions) iter.Seq2[Page, error]
	Check(ctx context.Context, source string, dataset Dataset) CheckResult
}

// CheckKind is how a check came out: it worked, or the setting is wrong (the person fixes it and no retry helps), or
// the trouble is passing (a retry may help), or the product is at fault.
type CheckKind string

const (
	CheckOK        CheckKind = "ok"
	CheckConfig    CheckKind = "config"
	CheckTransient CheckKind = "transient"
	CheckProduct   CheckKind = "product"
)

// CheckResult is what a connection check found: the kind, the words for the person and the rows of the first page.
// Steps is the same told item by item (the connection, the sign-in, the right to read, the first page) for a reader
// that can say which step it was; a reader that cannot leaves it empty.
type CheckResult struct {
	Kind    CheckKind
	Message string
	Rows    int
	Steps   []CheckStep
	// Notes are things the person should know that are no failure and no step (the data goes without encryption, the
	// certificate that is pinned is accepted after it has expired). They go with a result of any kind.
	Notes []CheckNote
	// Cert is the certificate the server presented, set when the check failed because the certificate could not be
	// verified or is not one of the pins. It is there so that a person who is allowed to change the connection can compare it
	// with what the owner of the server gave them and pin it. The texts of it are the server's own, cut and cleaned.
	Cert *CertSummary
}

// The codes of the notes of a check.
const (
	// NotePlaintext says the connection uses no TLS: the credentials and the data cross the network as they are.
	NotePlaintext = "source.plaintext_transport"
	// NotePinExpiry says a certificate that is pinned is accepted whatever its dates are.
	NotePinExpiry = "source.pin_expiry_ignored"
)

// CheckNote is a thing a check tells that is not a failure: a closed code and the words for it.
type CheckNote struct {
	Code    string
	Message string
}

// CertSummary is the certificate a server presented, in words that are safe to show (the server's own words, cut and with
// the control and formatting characters taken out). SPKISHA256 is the pin of it: what tls.pins takes to accept it. The words
// were written by the server, so a screen shows them as text and always escapes them; Truncated says that something was cut.
type CertSummary struct {
	Subject    string
	Issuer     string
	SANs       []string
	SANsMore   int
	NotBefore  time.Time
	NotAfter   time.Time
	SPKISHA256 string
	Truncated  bool
}

// CheckStep is one item of a check: its name (a closed phrase of the reader's), how it came out and the words for it.
type CheckStep struct {
	Name    string
	Kind    CheckKind
	Message string
}

// CheckFirstPage makes the check of a reader by reading its first page and stopping.
func CheckFirstPage(ctx context.Context, r PageReader, source string, ds Dataset) CheckResult {
	for page, err := range r.ReadPages(ctx, source, ds, PageOptions{}) {
		if err != nil {
			return CheckResult{Kind: ClassifyCheckError(err), Message: plainMessage(err)}
		}
		return firstPageResult(page)
	}
	return CheckResult{Kind: CheckProduct, Message: "the reader gave no page"}
}

func firstPageResult(p Page) CheckResult {
	rows := p.Received()
	if p.Shape != nil && len(p.Shape.Missing) > 0 {
		return CheckResult{Kind: CheckConfig, Message: shapeReason(Table{Missing: p.Shape.Missing, Header: p.Shape.Header, Hint: p.Shape.Hint})}
	}
	if p.Last && !p.End.Complete {
		return CheckResult{Kind: CheckTransient, Message: p.End.Reason, Rows: rows}
	}
	return CheckResult{Kind: CheckOK, Message: "the first page was read: " + itoa(rows) + " rows", Rows: rows}
}

// ClassifyCheckError says which kind of trouble an error of a read is. A deadline or an error that says it is passing
// is transient; a refusal of the setting (an invalid value, a missing or unsupported thing, a write to a real system)
// is the setting's; anything else is the product's. An error an adapter made as a SourceError says its class itself.
func ClassifyCheckError(err error) CheckKind {
	if se, ok := SourceErrorOf(err); ok {
		return se.Class
	}
	var passing interface{ Temporary() bool }
	var late interface{ Timeout() bool }
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &late) && late.Timeout(), errors.As(err, &passing) && passing.Temporary():
		return CheckTransient
	}
	switch kernel.CodeOf(err) {
	case kernel.CodeInvalidValue, kernel.CodeNotFound, kernel.CodeUnsupported, kernel.CodeReadOnlyViolation, kernel.CodeConflict:
		return CheckConfig
	}
	return CheckProduct
}
