package pgstore

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// links closes and opens the links of the chunk. The links of an object that the chunk retires are closed first,
// whoever made them (the meaning of the apply by rows: a link needs both its ends), then the links the difference
// names. A line that retires a link which is not open is the same retirement seen twice (the link went with its
// object in a chunk before), so it is not a mismatch.
func (c *stagedChunk) links() {
	if c.n(pgschema.DiffKindObject, pgschema.ChangeKindRetired) > 0 {
		c.cascadeLinks()
	}
	if c.total(pgschema.DiffKindLink) == 0 {
		return
	}
	c.closeLinks()
	c.openLinks()
}

// linkColumnsOf is the columns a link has in the table of the open links and in the history (which adds seq_to),
// each with the prefix.
func linkColumnsOf(prefix string) string { return prefixed(prefix, kgLinkInsertColumns) }

// stagedLinkEnds is the select of the links that have the object the chunk retires at their <end>.
const stagedLinkEnds = `SELECT l2.link_no FROM {link} l2 JOIN {ingest_diff} d ON l2.<end> = d.{entity_no}
	WHERE d.{chunk_no} = $1 AND d.{kind} = {KO} AND d.{change_kind} = {CR}`

// cascadeLinks closes the open links whose src or dst is an object the chunk retires, with an entry of the kind
// retired for each. The two ends are read by their own index and joined by a union (an OR would read every link).
func (c *stagedChunk) cascadeLinks() {
	hit := strings.ReplaceAll(stagedLinkEnds, "<end>", "src") + " UNION " + strings.ReplaceAll(stagedLinkEnds, "<end>", "dst")
	rows := c.hashRows("close the links of the retired objects", c.q(`WITH gone AS (DELETE FROM {link} l USING (`+hit+`) hit
			WHERE l.link_no = hit.link_no RETURNING `+linkColumnsOf("l.")+`),
		hist AS (INSERT INTO {link_hist} (`+kgLinkInsertColumns+`, seq_to) SELECT `+kgLinkInsertColumns+`, $2 FROM gone),
		ent AS (INSERT INTO {change_entry} (change_seq, entity_kind, entity_no, change_kind, old_hash, new_hash, ord)
			SELECT $2, {EL}, link_no, {CR}, content_hash, NULL,
				(SELECT count(*) FROM {change_entry} WHERE change_seq = $2)::int + (row_number() OVER (ORDER BY link_no))::int - 1 FROM gone)
		SELECT type_no, row_hash FROM gone`), c.from.NextChunk, c.seq)
	c.move(entityNameLink, rows, true)
}

// closeLinks closes the links the difference changes or retires. The row to close must be open with the content hash
// the difference names; a retirement of a link that is not open is skipped (see links).
func (c *stagedChunk) closeLinks() {
	changes, retires := c.n(pgschema.DiffKindLink, pgschema.ChangeKindChanged), c.n(pgschema.DiffKindLink, pgschema.ChangeKindRetired)
	if changes+retires == 0 {
		return
	}
	absent := c.queryCount("count the links that are already retired", c.q(`SELECT count(*) FROM {ingest_diff} d
		WHERE d.{chunk_no} = $1 AND d.{kind} = {KL} AND d.{change_kind} = {CR} AND NOT EXISTS (SELECT 1 FROM {link} l WHERE l.link_no = d.{entity_no})`), c.from.NextChunk)
	rows := c.hashRows("close links", c.q(`WITH gone AS (DELETE FROM {link} l USING {ingest_diff} d
			WHERE d.{chunk_no} = $1 AND d.{kind} = {KL} AND d.{change_kind} IN ({CC}, {CR}) AND l.link_no = d.{entity_no} AND l.content_hash = d.{old_hash}
			RETURNING `+linkColumnsOf("l.")+`, d.{change_kind} AS ck),
		hist AS (INSERT INTO {link_hist} (`+kgLinkInsertColumns+`, seq_to) SELECT `+kgLinkInsertColumns+`, $2 FROM gone),
		ent AS (INSERT INTO {change_entry} (change_seq, entity_kind, entity_no, change_kind, old_hash, new_hash, ord)
			SELECT $2, {EL}, link_no, {CR}, content_hash, NULL,
				(SELECT count(*) FROM {change_entry} WHERE change_seq = $2)::int + (row_number() OVER (ORDER BY link_no))::int - 1 FROM gone WHERE ck = {CR})
		SELECT type_no, row_hash FROM gone`), c.from.NextChunk, c.seq)
	c.expect("the links to close", len(rows), changes+retires-absent)
	c.move(entityNameLink, rows, true)
}

// openLinks opens the links the difference adds or changes from the staged links whose content hash is the one it
// names, in the order of the src and the ref (as the rows path does), and writes their entries.
func (c *stagedChunk) openLinks() {
	want := c.n(pgschema.DiffKindLink, pgschema.ChangeKindAdded) + c.n(pgschema.DiffKindLink, pgschema.ChangeKindChanged)
	if want == 0 {
		return
	}
	rows := c.hashRows("open links", c.q(`INSERT INTO {link} (`+kgLinkInsertColumns+`)
		SELECT sl.{link_no}, sl.{ref}, sl.{type_no}, sl.{src}, sl.{dst}, sl.{discriminator}, sl.{grade}, sl.{basis}, sl.{doc}, sl.{source_refs}, sl.{content_hash}, sl.{row_hash}, $2
		FROM {ingest_diff} d JOIN {stage_link} sl ON sl.{link_no} = d.{entity_no} AND sl.{content_hash} = d.{new_hash}
		WHERE d.{chunk_no} = $1 AND d.{kind} = {KL} AND d.{change_kind} IN ({CA}, {CC})
		ORDER BY sl.{src}, sl.{ref}
		RETURNING type_no, row_hash`), c.from.NextChunk, c.seq)
	c.expect("the links to open", len(rows), want)
	c.move(entityNameLink, rows, false)
	c.exec("write the entries of the new links", c.q(`INSERT INTO {change_entry} (change_seq, entity_kind, entity_no, change_kind, old_hash, new_hash, ord)
		SELECT $2, {EL}, d.{entity_no}, d.{change_kind}, d.{old_hash}, d.{new_hash},
			(SELECT count(*) FROM {change_entry} WHERE change_seq = $2)::int + (row_number() OVER (ORDER BY d.{ord}))::int - 1
		FROM {ingest_diff} d WHERE d.{chunk_no} = $1 AND d.{kind} = {KL} AND d.{change_kind} IN ({CA}, {CC})`), c.from.NextChunk, c.seq)
}

// queryCount runs a statement that gives one number.
func (c *stagedChunk) queryCount(what, query string, args ...any) int {
	if c.err != nil {
		return 0
	}
	var n int
	if err := c.tx.QueryRowContext(c.ctx, query, args...).Scan(&n); err != nil {
		c.fail(dbErr(err, what))
	}
	return n
}
