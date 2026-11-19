package pgstore

import (
	"sort"
	"strings"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// kgLinkInsertColumns are the columns a link has in the table of the open links and in the history (which adds
// seq_to).
const kgLinkInsertColumns = `link_no, ref, type_no, src, dst, discriminator, grade, basis, doc, source_refs, content_hash, row_hash, seq_from`

// linkPut is a link row to close or to open with its number and the numbers of its ends.
type linkPut struct {
	no       int64
	src, dst int64
	row      graph.LinkRow
}

// links closes the open rows the chunk replaces or retires and opens the new ones. A link that changes keeps its
// number; one that is new gets the next ones, in the order of its src and its ref (ADR-0089 decision 31). A row to
// close must be open with the content hash the chunk was planned against, or the head has moved.
func (c *kgChunk) links(changes []graph.LinkChange) {
	if err := c.openLinkNos(changes); err != nil {
		c.fail(err)
		return
	}
	var closes, opens []linkPut
	var fresh []int
	for _, ch := range changes {
		if ch.Old != nil {
			closes = append(closes, linkPut{no: c.linkNo[ch.Old.ID.String()], row: *ch.Old})
		}
		if ch.New != nil {
			opens = append(opens, linkPut{src: c.objNo(ch.New.Src), dst: c.objNo(ch.New.Dst), row: *ch.New})
		}
	}
	for i := range opens {
		if no, ok := c.linkNo[opens[i].row.ID.String()]; ok {
			opens[i].no = no
		} else {
			fresh = append(fresh, i)
		}
	}
	sort.Slice(opens, func(i, j int) bool { return less(opens[i], opens[j]) })
	nos := c.allocate("praxium_next_link_nos", len(fresh))
	next := 0
	for i := range opens {
		if opens[i].no == 0 && c.err == nil {
			opens[i].no, next = nos[next], next+1
		}
		c.linkNo[opens[i].row.ID.String()] = opens[i].no
	}
	sort.Slice(closes, func(i, j int) bool { return closes[i].no < closes[j].no })
	c.closeLinks(closes)
	c.openLinks(opens)
}

// less orders the links that open: by the number of the src, then by the ref.
func less(a, b linkPut) bool {
	if a.src != b.src {
		return a.src < b.src
	}
	return a.row.ID.String() < b.row.ID.String()
}

// openLinkNos finds the numbers of the open rows the chunk closes, by ref.
func (c *kgChunk) openLinkNos(changes []graph.LinkChange) error {
	var refs []string
	for _, ch := range changes {
		if ch.Old != nil {
			refs = append(refs, ch.Old.ID.String())
		}
	}
	if len(refs) == 0 {
		return nil
	}
	type pair struct {
		ref string
		no  int64
	}
	got, err := collect(c.ctx, c.tx, func(sc scanner) (pair, error) {
		var p pair
		return p, sc.Scan(&p.ref, &p.no)
	}, "look the links up", `SELECT ref, link_no FROM `+c.a.t(pgschema.TableLink)+` WHERE ref = ANY($1)`, pq.Array(refs))
	if err != nil {
		return err
	}
	for _, p := range got {
		c.linkNo[p.ref] = p.no
	}
	for _, ref := range refs {
		if _, ok := c.linkNo[ref]; !ok {
			return kernel.Errorf(graph.CodeHeadMoved, "the link %s is not an open row", ref)
		}
	}
	return nil
}

func prefixed(prefix, columns string) string {
	parts := strings.Split(columns, ", ")
	for i, p := range parts {
		parts[i] = prefix + p
	}
	return strings.Join(parts, ", ")
}

func (c *kgChunk) closeLinks(rows []linkPut) {
	query := `WITH gone AS (DELETE FROM ` + c.a.t(pgschema.TableLink) + ` l USING unnest($1::bigint[], $2::bytea[]) AS c(link_no, content_hash)
			WHERE l.link_no = c.link_no AND l.content_hash = c.content_hash RETURNING ` + prefixed("l.", kgLinkInsertColumns) + `)
		INSERT INTO ` + c.a.t(pgschema.TableLinkHist) + ` (` + kgLinkInsertColumns + `, seq_to) SELECT ` + kgLinkInsertColumns + `, $3 FROM gone`
	slices(len(rows), func(lo, hi int) {
		var nos []int64
		var hashes [][]byte
		for _, r := range rows[lo:hi] {
			nos, hashes = append(nos, r.no), append(hashes, hashBytes(r.row.ContentHash))
		}
		got := c.exec("close links", query, pq.Array(nos), pq.ByteaArray(hashes), c.seq)
		if c.err == nil && got != int64(hi-lo) {
			c.fail(kernel.Errorf(graph.CodeHeadMoved, "%d of %d links are not the open rows the chunk was planned against", int64(hi-lo)-got, hi-lo))
		}
	})
}

func (c *kgChunk) openLinks(rows []linkPut) {
	slices(len(rows), func(lo, hi int) {
		var a struct {
			refs, discs, grades, bases, docs, srcs []string
			nos, types, src, dst, from             []int64
			content, sums                          [][]byte
		}
		for _, p := range rows[lo:hi] {
			r := p.row
			doc, err := encodeAttrs(r.Attrs)
			refs, rerr := refsJSON(r.SourceRefs)
			sum, serr := graph.RowSum(entityNameLink, r.ID.String(), r.ContentHash)
			if err = firstErr(err, rerr, serr); err != nil {
				c.fail(err)
				return
			}
			a.nos, a.refs, a.types, a.src, a.dst = append(a.nos, p.no), append(a.refs, r.ID.String()), append(a.types, int64(r.TypeNo)), append(a.src, p.src), append(a.dst, p.dst)
			a.discs, a.grades, a.bases, a.docs = append(a.discs, r.Discriminator), append(a.grades, string(r.Grade)), append(a.bases, string(r.Basis)), append(a.docs, string(doc))
			a.srcs, a.content, a.sums, a.from = append(a.srcs, string(refs)), append(a.content, hashBytes(r.ContentHash)), append(a.sums, sumBytes(sum)), append(a.from, r.SeqFrom)
		}
		c.exec("open links", `INSERT INTO `+c.a.t(pgschema.TableLink)+` (`+kgLinkInsertColumns+`)
			SELECT u.link_no, u.ref, u.type_no, u.src, u.dst, u.discriminator, u.grade, u.basis, u.doc::jsonb, u.source_refs::jsonb, u.content_hash, u.row_hash, u.seq_from
			FROM unnest($1::bigint[], $2::text[], $3::bigint[], $4::bigint[], $5::bigint[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::bytea[], $12::bytea[], $13::bigint[])
				AS u(link_no, ref, type_no, src, dst, discriminator, grade, basis, doc, source_refs, content_hash, row_hash, seq_from)`,
			pq.Array(a.nos), pq.Array(a.refs), pq.Array(a.types), pq.Array(a.src), pq.Array(a.dst), pq.Array(a.discs), pq.Array(a.grades),
			pq.Array(a.bases), pq.Array(a.docs), pq.Array(a.srcs), pq.ByteaArray(a.content), pq.ByteaArray(a.sums), pq.Array(a.from))
	})
}
