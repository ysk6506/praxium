package pgstore

import (
	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
)

const identOpenSQL = `INSERT INTO object_identifier (kg_id, object_id, scheme, value, scope, seq_from, seq_to)
SELECT $1, u.object_id, u.scheme, u.value, u.scope, u.seq_from, NULLIF(u.seq_to, 0)
FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::bigint[], $7::bigint[]) AS u(object_id, scheme, value, scope, seq_from, seq_to)`

const identCloseSQL = `UPDATE object_identifier t SET seq_to = $6
FROM unnest($2::text[], $3::text[], $4::text[], $5::text[]) AS u(object_id, scheme, value, scope)
WHERE t.kg_id = $1 AND t.object_id = u.object_id AND t.scheme = u.scheme AND t.value = u.value AND t.scope = u.scope AND t.seq_to IS NULL`

// identifiers applies the changes in the order they were given: a record closed and opened again in one chunk
// must close the old one, so consecutive changes of one kind are one statement and the kinds do not cross.
func (w *writer) identifiers(changes []graph.IdentifierChange, seq int64) {
	for lo := 0; lo < len(changes); {
		hi := lo
		for hi < len(changes) && changes[hi].Closes == changes[lo].Closes && hi-lo < batchRows {
			hi++
		}
		w.identifierRun(changes[lo:hi], seq)
		lo = hi
	}
}

func (w *writer) identifierRun(run []graph.IdentifierChange, seq int64) {
	var objects, schemes, values, scopes []string
	var from, to []int64
	for _, c := range run {
		r := c.Record
		objects, schemes, values, scopes = append(objects, r.Object.String()), append(schemes, r.Scheme), append(values, r.Value), append(scopes, r.Scope)
		from, to = append(from, r.SeqFrom), append(to, r.SeqTo)
	}
	if run[0].Closes {
		w.exec("close identifiers", identCloseSQL, w.kg, pq.Array(objects), pq.Array(schemes), pq.Array(values), pq.Array(scopes), seq)
		return
	}
	w.exec("open identifiers", identOpenSQL, w.kg, pq.Array(objects), pq.Array(schemes), pq.Array(values), pq.Array(scopes), pq.Array(from), pq.Array(to))
}

const eventsSQL = `INSERT INTO identity_event (kg_id, change_seq, ord, event_kind, retired_object_id, into_object_id, link_id_map)
SELECT $1, $2, (SELECT COALESCE(MAX(ord) + 1, 0) FROM identity_event WHERE kg_id = $1 AND change_seq = $2) + u.n, u.kind, u.retired, u.into_id, u.map_json::jsonb
FROM unnest($3::int[], $4::text[], $5::text[], $6::text[], $7::text[]) AS u(n, kind, retired, into_id, map_json)`

// events writes the identity events. Those of one change number keep the order they were given in.
func (w *writer) events(events []graph.IdentityEvent) {
	for lo := 0; lo < len(events); {
		hi := lo
		for hi < len(events) && events[hi].Seq == events[lo].Seq && hi-lo < batchRows {
			hi++
		}
		var ns []int64
		var kinds, retired, into, maps []string
		for i, e := range events[lo:hi] {
			raw, err := linkMapJSON(e.LinkMap)
			if err != nil {
				w.fail(err)
				return
			}
			ns, kinds, retired, into, maps = append(ns, int64(i)), append(kinds, string(e.Kind)), append(retired, e.Retired.String()), append(into, e.Into.String()), append(maps, string(raw))
		}
		w.exec("write identity events", eventsSQL, w.kg, events[lo].Seq, pq.Array(ns), pq.Array(kinds), pq.Array(retired), pq.Array(into), pq.Array(maps))
		lo = hi
	}
}

const batchSQL = `INSERT INTO change_batch (kg_id, change_seq, kind, source_id, apply_id, chunk_no, chunk_count, ingest_seq, attempt, applied_at, objects, links)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

func (w *writer) batch(b graph.ChangeBatch) {
	w.exec("write the change batch", batchSQL, w.kg, b.Seq, string(b.Kind), b.SourceID, b.ApplyID, b.ChunkNo, b.ChunkCount, b.IngestSeq,
		b.Attempt, pgTime(b.AppliedAt), b.Objects, b.Links)
}

const orderSQL = `INSERT INTO ingest_order (kg_id, ingest_seq, event_kind, ref, digest, change_seq) VALUES ($1, $2, $3, $4, $5, $6)`

func (w *writer) order(e graph.OrderEvent) {
	w.exec("write the ordering event", orderSQL, w.kg, e.IngestSeq, string(e.Kind), e.Ref, hashBytes(e.Digest), e.ChangeSeq)
}
