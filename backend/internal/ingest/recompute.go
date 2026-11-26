package ingest

import (
	"cmp"
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The fourth step of the new ingestion flow, for the objects (ADR-0115): the objects of the run (calc_object) are computed again from
// every open record that stands under them, the ones of the run and the ones that are kept. The database cuts the objects into pages by
// the number of lines the records give (not by the number of objects: one object may have very many records) and reads the lines;
// Go converts the cells, chooses the value each attribute gets, makes the name, the document and the hashes (nothing of it is SQL,
// ingestion-at-scale.md P3); the database writes what Go made to stage_object, stage_volatile and stage_ident. This file is the port
// a state machine drives it by, and the parts that are Go's.

const (
	// DefaultRecomputeLines is how many lines of records (one for each record and feed that stands under an object) a page of the
	// computation reads: a page is the objects, in the order of their numbers, up to this many lines in all. The page is what bounds the
	// memory of the step: Make of 10,000 lines of one object peaks at about 30 MB of heap above the lines themselves (measured by
	// TestRcpTheMemoryOfAnObjectThatHasTheMostLinesAPageHoldsIsBounded).
	DefaultRecomputeLines = 10_000
	// DefaultRecomputeObjectMax is the most lines one object may have for the computation to make it, and it is the number of lines of a
	// page, so that an object is always inside one page (ADR-0119). An object that has more is left as it is for this run and counted
	// (RecomputeCounts.TooMany), a calc_left row says so, and nothing is written for it; one the run would have made is not made and what
	// pointed at it is taken back (its records are unresolved, with a problem each). An object that many records stand under is not a
	// case of the intended use but a mapping that makes one object of too many rows, and the run says so instead of making an object of
	// part of them. A full computation after the limit is raised makes it.
	DefaultRecomputeObjectMax = DefaultRecomputeLines
)

// RecomputeStart is what begins the step: the sizes (0 is the default), the time the records were seen (written as the time of the
// attributes: it is the run's, so a server that takes the run over writes the same) and the numbers the schemes are known by.
type RecomputeStart struct {
	Lines     int
	ObjectMax int
	// Seen is the time the values were seen, a time in UTC ending in Z.
	Seen string
	// Schemes is the number the rows received name the identifier schemes by (Plan.SchemeNumbers).
	Schemes map[string]int
}

// SchemesText is the numbers of the schemes as one text in the order of the names, to compare two starts and to keep in a place.
func (s RecomputeStart) SchemesText() string {
	names := make([]string, 0, len(s.Schemes))
	for name := range s.Schemes {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name + "=" + strconv.Itoa(s.Schemes[name]) + ";")
	}
	return b.String()
}

// Validate checks what the step cannot begin without. A limit of the lines of an object that is given is not more than the lines of
// a page, so that an object is always inside one page.
func (s RecomputeStart) Validate() error {
	if s.Lines < 0 || s.ObjectMax < 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the sizes of the computation of the objects are not negative")
	}
	if lines := cmp.Or(s.Lines, DefaultRecomputeLines); s.ObjectMax > lines {
		return kernel.Errorf(kernel.CodeInvalidValue, "the limit of %d lines of an object is more than the %d lines of a page", s.ObjectMax, lines)
	}
	t, err := time.Parse(time.RFC3339, s.Seen)
	if err != nil || t.UTC().Format(time.RFC3339) != s.Seen {
		return kernel.Errorf(kernel.CodeInvalidValue, "the time the values were seen is a time in UTC ending in Z, not %q", s.Seen)
	}
	return nil
}

// RecomputePlace is where the step stands: Unit is how many pages are done, and the next page is the objects after Cursor. Seen and
// Schemes (as SchemesText) are the ones the step began with, which a server that takes the run over must be given again (ADR-0119).
type RecomputePlace struct {
	Stage    RunStage
	Begun    bool
	Finished bool
	Unit     int
	Cursor   int64
	Seen     string
	Schemes  string
}

// Done says whether every page is done.
func (p RecomputePlace) Done() bool { return p.Begun && p.Finished }

// RecomputeCounts is what the step did, in the numbers a report shows. Objects, Volatile, Idents and Problems are the rows written (the
// count is of the rows the statement that wrote them gave back, not of a count made after), Lines the lines read, Added the records that
// were made records of the run for the objects they stand under. Retiring is the candidates for retirement, for which no row is made.
// Empty is the objects that no record stands under and that are no candidates (they are left as they are). Left is the objects that
// were not computed and are left as they are: TooMany of them have more lines than the limit, Unreadable have a record that cannot be
// read (its source has no plan, or its cells are not the ones the dataset declares now); the run says so, and does not make an object of
// part of its records. Isolated is the objects the run would have made that were not made (a calc_left row each, ADR-0119): the ones that
// are in Left and are new, and the ones that stand under a parent that was left.
type RecomputeCounts struct {
	Objects    int `json:"objects"`
	Volatile   int `json:"volatile"`
	Idents     int `json:"idents"`
	Problems   int `json:"problems"`
	Lines      int `json:"lines"`
	Added      int `json:"added"`
	Retiring   int `json:"retiring"`
	Empty      int `json:"empty"`
	Left       int `json:"left"`
	TooMany    int `json:"too_many"`
	Unreadable int `json:"unreadable"`
	Isolated   int `json:"isolated"`
}

// RecomputeClaim is an identifier claim of a line that was decided for the object: the scheme (its number), by the hash of the value.
type RecomputeClaim struct {
	Scheme int
	Hash   KeyHash
}

// RecomputeLine is one record that stands under an object, as one of the feeds of its source reads it. Rid is the number of the record
// in the run, or 0 when it is not in the run (it is kept, and is made a record of the run when the page is put). Why is the reason it is
// in the run, -1 when it is not.
type RecomputeLine struct {
	Rid       int64
	Why       int16
	SrcNo     int64
	DatasetNo int64
	Dataset   string
	Key       string
	Fields    []string
	OpenedSeq int64
	Feed      int
	// Won are the identifier claims of the line that the object has (the references that say they are solved).
	Won []RecomputeClaim
}

// RecomputeObject is an object of the page with the lines under it. FirstKey is the stored first key of an object the KG has, and for
// an object that this run makes the record and the feed that made it (OriginRid, OriginFeed) and, when its key has a context, the id of
// the parent (ParentRef): the first key is made again from them.
type RecomputeObject struct {
	ObjNo      int64
	Ref        string
	TypeNo     int
	IsNew      bool
	Name       string
	FirstKey   []byte
	OriginRid  int64
	OriginFeed int
	ParentRef  string
	Lines      []RecomputeLine
}

// RecomputePage is the objects of a page. Through is the number of the last object the page ends at (the objects that the page does not
// hold, because they have too many lines, are in TooMany), and Last says that no object is left after it.
type RecomputePage struct {
	Unit    int
	Through int64
	Last    bool
	Objects []RecomputeObject
	TooMany []int64
}

// RecomputedVolatile is the staged entry of a volatile attribute of an object.
type RecomputedVolatile struct {
	AttrNo int
	Entry  []byte
}

// RecomputedIdent is an identifier an object has.
type RecomputedIdent struct {
	Scheme string
	Value  string
}

// RecomputedObject is what Go made of an object: the row of stage_object (without the number of the object that the page gives), the
// volatile values and the identifiers.
type RecomputedObject struct {
	ObjNo       int64
	Ref         string
	TypeNo      int
	Name        string
	MinGrade    model.Grade
	Doc         []byte
	SourceRefs  []byte
	ContentHash kernel.Hash
	RowHash     kernel.Hash
	// FirstKey is the stored first key of an object that this run makes (the empty text for the others).
	FirstKey string
	IsNew    bool
	Volatile []RecomputedVolatile
	Idents   []RecomputedIdent
}

// RecomputedProblem is a problem of a record that the computation found: the record by its place, the feed it is read by, the field
// (a column) and the reason.
type RecomputedProblem struct {
	SrcNo     int64
	DatasetNo int64
	Key       string
	Feed      int
	Field     string
	Kind      string
	Reason    string
}

// RecomputedRecord names a record that stands under an object that was computed and is not a record of the run yet.
type RecomputedRecord struct {
	SrcNo     int64
	DatasetNo int64
	Key       string
}

// The reasons an object is left (RecomputedLeft.Why).
const (
	LeftEmpty      = "empty"      // no record stands under it
	LeftUnreadable = "unreadable" // a line of it cannot be read
	LeftNameless   = "nameless"   // no name is known for it
	LeftTooMany    = "too_many"   // it has more lines than the limit (the page did not read them)
)

// RecomputedLeft is an object that Go did not make, and why. Lines is how many lines it had when they were read. The reason of one that
// has too many lines is given by the store, which knows the limit.
type RecomputedLeft struct {
	ObjNo  int64
	Why    string
	Lines  int
	Reason string
}

// RecomputedPage is what Go made of a page: the objects, the problems of the records, the records to make records of the run, the
// objects that no line stands under (Empty) and the ones that cannot be read (Unreadable). Left says why for every object that was not
// made: the ones of Empty and Unreadable and the ones that have too many lines; the numbers of the three must agree with it.
type RecomputedPage struct {
	Unit       int
	Through    int64
	Last       bool
	Objects    []RecomputedObject
	Problems   []RecomputedProblem
	Records    []RecomputedRecord
	Empty      []int64
	Unreadable []int64
	Left       []RecomputedLeft
	// Lines is how many lines the page read and TooMany how many objects it left out for having too many.
	Lines   int
	TooMany int
}

// Recomputer is the port of the step. Every call names the attempt of the lease, and a call whose attempt is not the run's any more is
// refused with CodeRunLeaseLost and changes nothing (reads too), as for RunLedger. A page and the place that moves past it are one
// transaction, so a page is done once or not at all.
//
// BeginRecompute starts the step of a run whose expansion is done; it empties nothing. RecomputePage reads the page the place says
// (it changes nothing) and PutRecomputed takes what Go made of it: the unit it names must be the one the place is at, so a page put twice
// (a server that does not know that its commit went through) is refused with CodeRunStage and nothing is counted twice.
type Recomputer interface {
	BeginRecompute(ctx context.Context, lease RunLease, start RecomputeStart) error
	RecomputePlace(ctx context.Context, lease RunLease) (RecomputePlace, error)
	RecomputePage(ctx context.Context, lease RunLease) (RecomputePage, error)
	PutRecomputed(ctx context.Context, lease RunLease, page RecomputedPage) error
	RecomputeReport(ctx context.Context, lease RunLease) (RecomputeCounts, error)
}

// ObjectMaker is what Go does for the step: it makes the rows of the objects of a page.
type ObjectMaker interface {
	Make(page RecomputePage) (RecomputedPage, error)
}

// RunRecompute does the whole step, or what is left of it: it begins it when it has not begun, and does the pages from where the run
// is. The context is looked at between pages.
func RunRecompute(ctx context.Context, d Recomputer, lease RunLease, start RecomputeStart, maker ObjectMaker) (RecomputeCounts, error) {
	if err := start.Validate(); err != nil {
		return RecomputeCounts{}, err
	}
	place, err := d.RecomputePlace(ctx, lease)
	if err != nil {
		return RecomputeCounts{}, err
	}
	if place.Stage != RunComputing {
		return RecomputeCounts{}, kernel.Errorf(CodeRunStage, "the run %d is %s: the computation of the objects is the work of a run that is computing", lease.No, place.Stage)
	}
	if !place.Begun {
		if err := d.BeginRecompute(ctx, lease, start); err != nil {
			return RecomputeCounts{}, err
		}
	} else if place.Seen != start.Seen || place.Schemes != start.SchemesText() {
		// a server that takes the run over is given what the run began with, or the documents of one run would say two times and the
		// identifiers of a scheme it does not know would be left out (ADR-0119)
		return RecomputeCounts{}, kernel.Errorf(kernel.CodeConflict, "the computation of the objects of the run %d began with the time %q and the schemes %q, not %q and %q",
			lease.No, place.Seen, place.Schemes, start.Seen, start.SchemesText())
	}
	for {
		done, err := recomputeOnePage(ctx, d, lease, maker)
		if err != nil {
			return RecomputeCounts{}, err
		}
		if done {
			return d.RecomputeReport(ctx, lease)
		}
	}
}

// recomputeOnePage does the page the place says, and says when there is none left.
func recomputeOnePage(ctx context.Context, d Recomputer, lease RunLease, maker ObjectMaker) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	place, err := d.RecomputePlace(ctx, lease)
	if err != nil {
		return false, err
	}
	if place.Done() {
		return true, nil
	}
	page, err := d.RecomputePage(ctx, lease)
	if err != nil {
		return false, err
	}
	made, err := maker.Make(page)
	if err != nil {
		return false, err
	}
	return false, d.PutRecomputed(ctx, lease, made)
}
