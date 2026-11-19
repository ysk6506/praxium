package pgstore

import (
	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// identifiers applies the changes in the order they were given: a record closed and opened again in one chunk must
// close the old one, so consecutive changes of one kind are one statement and the kinds do not cross.
func (c *kgChunk) identifiers(changes []graph.IdentifierChange) {
	for lo := 0; lo < len(changes); {
		hi := lo
		for hi < len(changes) && changes[hi].Closes == changes[lo].Closes && hi-lo < batchRows {
			hi++
		}
		c.identifierRun(changes[lo:hi])
		lo = hi
	}
}

func (c *kgChunk) identifierRun(run []graph.IdentifierChange) {
	var schemes, values, scopes []string
	var nos, from, to []int64
	for _, ch := range run {
		r := ch.Record
		nos, schemes, values, scopes = append(nos, c.objNo(r.Object)), append(schemes, r.Scheme), append(values, r.Value), append(scopes, r.Scope)
		from, to = append(from, r.SeqFrom), append(to, r.SeqTo)
	}
	table := c.a.t(pgschema.TableObjectIdentifier)
	if run[0].Closes {
		c.exec("close identifiers", `UPDATE `+table+` t SET seq_to = $5 FROM unnest($1::bigint[], $2::text[], $3::text[], $4::text[]) AS u(obj_no, scheme, value, scope)
			WHERE t.obj_no = u.obj_no AND t.scheme = u.scheme AND t.value = u.value AND t.scope = u.scope AND t.seq_to IS NULL`,
			pq.Array(nos), pq.Array(schemes), pq.Array(values), pq.Array(scopes), c.seq)
		return
	}
	c.exec("open identifiers", `INSERT INTO `+table+` (obj_no, scheme, value, scope, seq_from, seq_to)
		SELECT u.obj_no, u.scheme, u.value, u.scope, u.seq_from, NULLIF(u.seq_to, 0)
		FROM unnest($1::bigint[], $2::text[], $3::text[], $4::text[], $5::bigint[], $6::bigint[]) AS u(obj_no, scheme, value, scope, seq_from, seq_to)`,
		pq.Array(nos), pq.Array(schemes), pq.Array(values), pq.Array(scopes), pq.Array(from), pq.Array(to))
}

// events writes the identity events. Those of one change number keep the order they were given in.
func (c *kgChunk) events(events []graph.IdentityEvent) {
	table := c.a.t(pgschema.TableIdentityEvent)
	for lo := 0; lo < len(events); {
		hi := lo
		for hi < len(events) && events[hi].Seq == events[lo].Seq && hi-lo < batchRows {
			hi++
		}
		var ns, retired, into []int64
		var kinds, maps []string
		for i, e := range events[lo:hi] {
			raw, err := linkMapJSON(e.LinkMap)
			if err != nil {
				c.fail(err)
				return
			}
			ns, kinds, maps = append(ns, int64(i)), append(kinds, string(e.Kind)), append(maps, string(raw))
			retired, into = append(retired, c.objNo(e.Retired)), append(into, c.objNo(e.Into))
		}
		c.exec("write identity events", `INSERT INTO `+table+` (change_seq, ord, event_kind, retired_obj_no, into_obj_no, link_ref_map)
			SELECT $1, (SELECT COALESCE(MAX(ord) + 1, 0) FROM `+table+` WHERE change_seq = $1) + u.n, u.kind, u.retired, u.into_no, u.map_json::jsonb
			FROM unnest($2::bigint[], $3::text[], $4::bigint[], $5::bigint[], $6::text[]) AS u(n, kind, retired, into_no, map_json)`,
			events[lo].Seq, pq.Array(ns), pq.Array(kinds), pq.Array(retired), pq.Array(into), pq.Array(maps))
		lo = hi
	}
}
