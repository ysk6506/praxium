package httpsource

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// Preview reads the first pages of a dataset for a person to look at: the pages as masked rows (at most MaxRows rows in all,
// in at most MaxPages pages) and the requests that made them as a record of exchanges (ADR-0090). What a person is shown goes through the masks of core.ingest: the value
// of a column whose name is a secret's, the headers but the three that are shown, the query of an address the source gave,
// and, as a second wall, any value of an open secret that turns up in a cell.
func (r *Reader) Preview(ctx context.Context, source string, ds ingest.Dataset, opts ingest.PreviewOptions) (ingest.Preview, error) {
	opts = opts.WithDefaults()
	rn, err := r.begin(ctx, source, ds, secrets.PurposeDiscover)
	if err != nil {
		return ingest.Preview{}, err
	}
	defer rn.close()
	rn.keep = true
	var out ingest.Preview
	var failed error
	rows := 0
	// The rows are shared out over the pages: a preview of two pages and a hundred rows has pages of fifty.
	pageRows := (opts.MaxRows + opts.MaxPages - 1) / opts.MaxPages
	rn.read(ctx, ds, ingest.PageOptions{MaxRows: pageRows}, func(p ingest.Page, err error) bool {
		if err != nil {
			failed = err
			return false
		}
		if room := opts.MaxRows - rows; len(p.Rows) > room {
			p.Rows, p.Unreadable = p.Rows[:room], nil
		}
		rows += p.Received()
		page, masked := ingest.MaskPage(p)
		out.Masked = out.Masked || masked
		out.Pages = append(out.Pages, rn.wipeValues(page))
		return len(out.Pages) < opts.MaxPages && rows < opts.MaxRows
	})
	if failed != nil {
		return ingest.Preview{}, failed
	}
	out.Exchanges = rn.exchanges
	return out, nil
}

// wipeValues takes the values of the open secrets out of the cells of a page.
func (rn *run) wipeValues(p ingest.Page) ingest.Page {
	texts := rn.texts()
	if len(texts) == 0 {
		return p
	}
	for i, row := range p.Rows {
		fields := make(map[string]string, len(row.Fields))
		for k, v := range row.Fields {
			fields[k] = ingest.MaskText(v, texts...)
		}
		p.Rows[i].Fields = fields
	}
	return p
}
