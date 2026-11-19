package ingest

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Intake is the receiving step of the new ingestion flow (ADR-0086): it reads the datasets of a source through the read
// port, page by page, turns each page into prepared rows and hands it to the sink, before it reads the next. It holds one
// page at a time, whatever the size of the source: the reader gives the next page only when the loop comes back for it.
// The context is looked at between pages, so a canceled run stops after the page in hand.
type Intake struct {
	Reader  PageReader
	Sink    StageSink
	Plan    *Plan
	Options PageOptions
}

// DatasetIntake says what the receiving of one dataset did: the pages and the rows it gave, whether the dataset was read to
// its end and why not when it was not. NotRead is the reason a header that lacks a declared column gave rows none.
type DatasetIntake struct {
	Dataset    string
	Collected  bool
	Pages      int
	Records    int
	Unreadable int
	Complete   bool
	Reason     string
	NotRead    string
}

// IntakeReport is what one receiving did, a line for every dataset of the source in the order of the definition.
type IntakeReport struct {
	Datasets []DatasetIntake
}

// Run receives every dataset of the source of the plan. A dataset the reader does not give this time (the error not_found)
// is no failure: it is left as it is. Any other error stops the run, and so does the sink's.
func (in Intake) Run(ctx context.Context) (IntakeReport, error) {
	var rep IntakeReport
	for _, ds := range in.Plan.Source.Datasets {
		line, err := in.receive(ctx, ds)
		if err != nil {
			if kernel.CodeOf(err) == kernel.CodeNotFound {
				continue
			}
			return rep, err
		}
		rep.Datasets = append(rep.Datasets, line)
	}
	return rep, nil
}

func (in Intake) receive(ctx context.Context, ds Dataset) (DatasetIntake, error) {
	line := DatasetIntake{Dataset: ds.ID, Collected: true}
	pr, err := NewPreparer(in.Plan, ds.ID)
	if err != nil {
		return line, err
	}
	for page, err := range in.Reader.ReadPages(ctx, in.Plan.Source.ID, ds, in.Options) {
		if err != nil {
			return line, err
		}
		if err := ctx.Err(); err != nil {
			return line, err
		}
		sp, err := pr.Prepare(page)
		if err != nil {
			return line, err
		}
		if err := in.Sink.Stage(ctx, sp); err != nil {
			return line, err
		}
		line.Pages++
		line.Records += len(sp.Records)
		line.Unreadable += sp.Unreadable
		if page.Shape != nil && len(page.Shape.Missing) > 0 {
			line.NotRead = shapeReason(Table{Missing: page.Shape.Missing, Header: page.Shape.Header, Hint: page.Shape.Hint})
		}
		if page.Last {
			line.Complete, line.Reason = page.End.Complete, page.End.Reason
			return line, nil
		}
	}
	return line, kernel.Errorf(kernel.CodeInternal, "the reader of dataset %s ended without a last page", ds.ID)
}
