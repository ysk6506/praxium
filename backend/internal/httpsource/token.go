package httpsource

import (
	"encoding/json"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The token of a page (ingest.Page.Token) is what lets a read go on after that page: it holds what is needed to make
// the next request again, and nothing else (ADR-0092). It holds no secret, no header and no address; it is read back from
// storage, so it is not trusted: one that does not read, or that was made for another definition, is ignored and the
// read starts from the first page, as the port allows.
//
//	pagination  position the token holds
//	none        how many records of the one response are already given (p, o, c are empty)
//	page        the number of the page to ask for (p) and how many records of it are already given (s)
//	offset      the offset to ask for (o) and how many records of the response are already given (s)
//	cursor      the cursor to ask for (c; empty for the first request) and how many records are already given (s)
//	next_url,   no token: the address the source gave may be signed by its query, so the read cannot go on from the
//	link_header middle and starts again
//
// Every token also holds the number of records given before the response it points at (b), so that the numbers of the
// lines and the check against the total go on across a resume; a fingerprint (fp) of the parts of the definition that
// the position depends on; and e when nothing is left to read.
type resumeToken struct {
	V      int    `json:"v"`
	FP     string `json:"fp"`
	Page   int    `json:"p,omitempty"`
	Offset int    `json:"o,omitempty"`
	Cursor string `json:"c,omitempty"`
	Skip   int    `json:"s,omitempty"`
	Base   int    `json:"b,omitempty"`
	End    bool   `json:"e,omitempty"`
}

const tokenVersion = 1

func encodeToken(t resumeToken) []byte {
	t.V = tokenVersion
	data, _ := json.Marshal(t) // a struct of numbers, text and a flag always marshals
	return data
}

// startPoint tells where to begin: the position, how many records of its response to
// pass over, how many records came before it, and whether there is nothing left.
type startPoint struct {
	at   pos
	skip int
	base int
	done bool
}

func (rn *run) startAt(fp string, resume []byte) startPoint {
	start := startPoint{at: rn.pager.first()}
	if len(resume) == 0 || !rn.pager.resumable() {
		return start
	}
	var t resumeToken
	if err := json.Unmarshal(resume, &t); err != nil || t.V != tokenVersion || t.FP != fp || t.Skip < 0 || t.Base < 0 {
		return start
	}
	return startPoint{at: pos{page: t.Page, offset: t.Offset, cursor: t.Cursor}, skip: t.Skip, base: t.Base, done: t.End}
}

func tokenAt(fp string, at pos, skip, base int) []byte {
	return encodeToken(resumeToken{FP: fp, Page: at.page, Offset: at.offset, Cursor: at.cursor, Skip: skip, Base: base})
}

func tokenEnd(fp string, base int) []byte {
	return encodeToken(resumeToken{FP: fp, Base: base, End: true})
}

// fingerprint names the parts of the definition a position depends on: where the requests go, what they ask, and how
// the pages are told apart. The limits (max_pages) and the columns are not among them, so that changing those does not
// throw a token away.
func fingerprint(set settings, ds ingest.Dataset) string {
	pg := ingest.Pagination{Kind: ingest.PageNone}
	if ds.Pagination != nil {
		pg = *ds.Pagination
	}
	pg.MaxPages = 0
	data, _ := json.Marshal(struct { // a struct of text, numbers and the definition's own values always marshals
		Origin  string
		Dataset string
		Request *ingest.Request
		Records string
		Page    ingest.Pagination
	}{set.scheme + "://" + set.host + ":" + itoa(set.port) + set.basePath, ds.ID, ds.Request, ds.RecordsPath, pg})
	return kernel.HashBytes(data).String()[:24]
}

func itoa(n int) string { return strconv.Itoa(n) }
