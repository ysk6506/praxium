package filesource

import (
	"context"
	"iter"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// PageSource is the file adapter of the new ingestion flow (ADR-0086): it gives the file of a dataset as a stream of pages,
// reading it as it goes and never holding the file. The file limit of the current flow is gone; what bounds a dataset
// here is its row limit (ingest.PageOptions). The parsing is the pipeline's, ingest.StreamPages; the adapter opens the
// file by the name it was given and closes it when the pages are not wanted any more. It has no method that writes.
//
// A page carries no token: a file is read from its start every time, and the Resume option is not used.
type PageSource struct {
	Open  Opener
	Files map[string]string
}

var _ ingest.PageReader = PageSource{}

// ReadPages gives the file of the dataset page by page. A dataset with no file is not collected in this run (not_found),
// a file that cannot be opened is an error, a dataset whose parser reads whole files (a workbook) is not supported, and a
// read that stops short ends the dataset as not complete.
func (s PageSource) ReadPages(ctx context.Context, _ string, ds ingest.Dataset, opts ingest.PageOptions) iter.Seq2[ingest.Page, error] {
	return func(yield func(ingest.Page, error) bool) {
		name, ok := s.Files[ds.ID]
		if !ok {
			yield(ingest.Page{}, kernel.Errorf(kernel.CodeNotFound, "no file was given for dataset %s", ds.ID))
			return
		}
		f, err := s.Open(name)
		if err != nil {
			yield(ingest.Page{}, kernel.Wrap(kernel.CodeInvalidValue, err, "dataset "+ds.ID+": cannot open the file"))
			return
		}
		defer f.Close()
		for page, err := range ingest.StreamPages(ctx, f, ds, opts) {
			if !yield(page, err) || err != nil {
				return
			}
		}
	}
}

// Check reads the first page of the dataset's file, so that it tells what the setting gives and not only that a file is there.
func (s PageSource) Check(ctx context.Context, source string, ds ingest.Dataset) ingest.CheckResult {
	return ingest.CheckFirstPage(ctx, s, source, ds)
}
