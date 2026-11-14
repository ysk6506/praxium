package filesource

import (
	"context"
	"io"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// DefaultMaxBytes is the most one file may have (ingestion.md 10: the file limit of the design, 20 MB).
const DefaultMaxBytes = 20 << 20

// Opener opens a file for reading by the name the composition root gave it.
type Opener func(name string) (io.ReadCloser, error)

// Source gives the files of one source: the name of the file of each dataset. A dataset without a file is not
// collected in this run, which is no failure.
type Source struct {
	Open     Opener
	Files    map[string]string
	MaxBytes int64
}

var _ ingest.Fetcher = Source{}

// Fetch reads the file of a dataset. A file that cannot be opened is an error of the run. A file that cannot be read
// to its end, or is larger than the limit, is a capture that is not complete.
func (s Source) Fetch(_ context.Context, _ string, ds ingest.Dataset) (ingest.Capture, error) {
	name, ok := s.Files[ds.ID]
	if !ok {
		return ingest.Capture{}, kernel.Errorf(kernel.CodeNotFound, "no file was given for dataset %s", ds.ID)
	}
	f, err := s.Open(name)
	if err != nil {
		return ingest.Capture{}, kernel.Wrap(kernel.CodeInvalidValue, err, "dataset "+ds.ID+": cannot open the file")
	}
	defer f.Close()
	limit := s.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	c := ingest.Capture{Dataset: ds.ID, Data: data, Complete: true}
	switch {
	case err != nil:
		c.Complete, c.Reason = false, "the read of the file stopped before its end"
	case int64(len(data)) > limit:
		c.Data, c.Complete, c.Reason = nil, false, "the file is larger than the limit of the product"
	}
	c.Hash = kernel.HashBytes(c.Data).String()
	return c, nil
}
