package httpsource

import (
	"net/url"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// pos is where a request is in the pagination of a dataset: the number of the page, the offset or the cursor it asks
// for (whichever the kind uses), or, for the two kinds that follow an address the source gave, that address.
type pos struct {
	page   int
	offset int
	cursor string
	link   *url.URL
}

// step is what a response says about what comes next.
type step struct {
	more bool
	next pos
	// complete is the end of a read that went to the end of the data; reason says why it stopped when it did not.
	complete bool
	reason   string
}

// pager is the pagination of one dataset: it adds the parameters of a request and reads the answer to find the next.
type pager struct {
	pg *ingest.Pagination
}

func newPager(ds ingest.Dataset) *pager {
	pg := ds.Pagination
	if pg == nil {
		pg = &ingest.Pagination{Kind: ingest.PageNone}
	}
	return &pager{pg: pg}
}

// resumable says whether a request of this pagination can be made again from what a token holds. The two kinds that
// follow an address the source gave cannot: the address may be signed by its query, and a token never holds it.
func (g *pager) resumable() bool {
	return g.pg.Kind != ingest.PageNextURL && g.pg.Kind != ingest.PageLinkHeader
}

func (g *pager) first() pos {
	if g.pg.Kind == ingest.PagePage && g.pg.FirstPage != nil {
		return pos{page: *g.pg.FirstPage}
	}
	return pos{}
}

// apply adds the parameters of the pagination to the request for a position.
func (g *pager) apply(p *params, at pos) {
	pg := g.pg
	switch pg.Kind {
	case ingest.PagePage:
		g.put(p, false, pg.PageParam, strconv.Itoa(at.page), false)
		g.put(p, false, pg.SizeParam, strconv.Itoa(pg.Size), false)
	case ingest.PageOffset:
		g.put(p, false, pg.OffsetParam, strconv.Itoa(at.offset), false)
		g.put(p, false, pg.SizeParam, strconv.Itoa(pg.Size), false)
	case ingest.PageCursor:
		inBody := pg.CursorIn == ingest.InBody
		if pg.SizeParam != "" {
			g.put(p, inBody, pg.SizeParam, strconv.Itoa(pg.Size), false)
		}
		if at.cursor != "" {
			g.put(p, inBody, pg.CursorParam, at.cursor, true)
		}
	case ingest.PageNextURL, ingest.PageLinkHeader:
		if at.link == nil && pg.SizeParam != "" {
			g.put(p, false, pg.SizeParam, strconv.Itoa(pg.Size), false)
		}
	}
}

// put sets one parameter, in the query or in the body. The value of a parameter the source gave (a cursor) is not shown.
func (g *pager) put(p *params, inBody bool, name, value string, fromSource bool) {
	if name == "" {
		return
	}
	if !inBody {
		p.query.Set(name, value)
		p.shown = append(p.shown, shownValue(name, value, fromSource))
		return
	}
	if p.body == nil {
		p.body = map[string]any{}
	}
	if n, err := strconv.Atoi(value); err == nil && !fromSource {
		p.body[name] = n
	} else {
		p.body[name] = value
	}
}

// advance reads a response to say what comes next. seen is the number of records read up to and with this response.
func (g *pager) advance(at pos, r *reply, doc any, n, seen int) (step, error) {
	switch g.pg.Kind {
	case ingest.PagePage:
		return g.countStep(doc, n, seen, pos{page: at.page + 1}), nil
	case ingest.PageOffset:
		return g.countStep(doc, n, seen, pos{offset: at.offset + n}), nil
	case ingest.PageCursor:
		return g.cursorStep(at, doc), nil
	case ingest.PageNextURL:
		return g.addressStep(r, nextText(g.pg.NextPath, doc))
	case ingest.PageLinkHeader:
		return g.addressStep(r, linkTarget(r.header, g.pg.Rel))
	}
	return step{complete: true}, nil
}

// totalOf reads the number of records the source says the dataset has, when the definition says where.
func (g *pager) totalOf(doc any) (int, bool) {
	if g.pg.TotalPath == "" {
		return 0, false
	}
	path, err := ingest.ParsePath(g.pg.TotalPath)
	if err != nil {
		return 0, false
	}
	text, ok := path.Pick(doc)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(text)
	return n, err == nil && n >= 0
}

// countStep is the end of the pages that count: it stops at an empty response, at a short one when the source gave no
// total, and when the total is reached. A total that is not what was read makes the read not complete.
func (g *pager) countStep(doc any, n, seen int, next pos) step {
	total, hasTotal := g.totalOf(doc)
	done := n == 0 || (hasTotal && seen >= total) || (!hasTotal && n < g.pg.Size)
	if !done {
		return step{more: true, next: next}
	}
	if hasTotal && seen != total {
		return step{reason: "the source says the dataset has " + strconv.Itoa(total) + " records and " + strconv.Itoa(seen) + " were read"}
	}
	return step{complete: true}
}

func (g *pager) cursorStep(at pos, doc any) step {
	next := nextText(g.pg.NextCursorPath, doc)
	switch {
	case next == "":
		return step{complete: true}
	case next == at.cursor:
		return step{reason: "the source gave the same cursor again, so the read cannot go on"}
	}
	return step{more: true, next: pos{cursor: next}}
}

// addressStep follows an address the source gave, if it is of the origin of the request it came with.
func (g *pager) addressStep(r *reply, ref string) (step, error) {
	if ref == "" {
		return step{complete: true}, nil
	}
	next, err := egress.ResolveSameOrigin(r.url, ref)
	if err != nil {
		return step{}, err
	}
	if next.String() == r.url.String() {
		return step{reason: "the source gave the address of the response it was reading as the next, so the read cannot go on"}, nil
	}
	return step{more: true, next: pos{link: next}}, nil
}

// nextText reads a place of a response that gives a cursor or an address; no text is the end.
func nextText(path string, doc any) string {
	p, err := ingest.ParsePath(path)
	if err != nil {
		return ""
	}
	text, _ := p.Pick(doc)
	return text
}
