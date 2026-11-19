package pgstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The answer loader of the tests of the staged apply (ADR-0099). The rows path writes a KG and the loader writes what
// the rows path wrote, as the stored difference (ingest_diff) and the staged rows, so the staged apply can be
// checked against it. The loader is the test's: it takes the rows (and so the content hashes, the row sums and the
// ids) from the applier of the product through a store that records the chunks it is given, and it gives them the
// shape of the staged tables. No meaning of a row is made a second time here. Every name starts with sap.

// sapRecorder is a store that passes every chunk on and keeps it.
type sapRecorder struct {
	graph.Store
	mu     sync.Mutex
	chunks []graph.ChunkWrite
}

func (r *sapRecorder) ApplyChunk(ctx context.Context, w graph.ChunkWrite) error {
	if err := r.Store.ApplyChunk(ctx, w); err != nil {
		return err
	}
	r.mu.Lock()
	r.chunks = append(r.chunks, w)
	r.mu.Unlock()
	return nil
}

// take gives the chunks that were recorded since the last call.
func (r *sapRecorder) take() []graph.ChunkWrite {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.chunks
	r.chunks = nil
	return out
}

// sapLine is a line of the stored difference.
type sapLine struct {
	kind, typeNo, change int16
	entity, ctx          int64
	ref                  string
	old, new, sig        []byte
}

// sapStager writes the staged rows and the difference of the chunks it is given into one KG. The numbers of the objects
// and the links are the ones the KG has taken from its sequences, and are kept from one step to the next.
type sapStager struct {
	st      *Store
	a       *kgAccess
	objNo   map[string]int64
	objType map[string]int
	linkNo  map[string]int64
	ord     int64
	chunk   int
}

func sapNewStager(ctx context.Context, st *Store, kg graph.KGID) (*sapStager, error) {
	a, err := st.access(ctx, kg)
	if err != nil {
		return nil, err
	}
	return &sapStager{st: st, a: a, objNo: map[string]int64{}, objType: map[string]int{}, linkNo: map[string]int64{}}, nil
}

// reset empties the staged tables and the difference (a new run) and starts the numbering of chunks and lines again.
func (s *sapStager) reset(ctx context.Context) error {
	s.ord, s.chunk = 0, 0
	return s.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, s.a.stagedSQL(`TRUNCATE {ingest_diff}, {stage_object}, {stage_link}, {stage_ident}, {stage_volatile}`))
		return err
	})
}

// numbers takes n numbers from a function of the database.
func (s *sapStager) numbers(ctx context.Context, tx *sql.Tx, fn string, n int) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT n FROM public.`+fn+`($1) AS t(n) ORDER BY n`, n)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var no int64
		if err := rows.Scan(&no); err != nil {
			return nil, err
		}
		out = append(out, no)
	}
	return out, rows.Err()
}

// stage writes the chunk as one chunk of the difference (and its staged rows), lines in the order of the kinds.
func (s *sapStager) stage(ctx context.Context, w graph.ChunkWrite) error {
	return s.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		var lines []sapLine
		steps := []func(context.Context, *sql.Tx, graph.ChunkWrite) ([]sapLine, error){s.objects, s.links, s.identifiers}
		for _, step := range steps {
			got, err := step(ctx, tx, w)
			if err != nil {
				return err
			}
			lines = append(lines, got...)
		}
		return s.writeLines(ctx, tx, lines)
	})
}

func (s *sapStager) objects(ctx context.Context, tx *sql.Tx, w graph.ChunkWrite) ([]sapLine, error) {
	var fresh int
	for _, ch := range w.Objects {
		if ch.Old == nil && ch.New != nil {
			fresh++
		}
	}
	nos, err := s.numbers(ctx, tx, "praxium_next_obj_nos", fresh)
	if err != nil {
		return nil, err
	}
	var lines, keys []sapLine
	var a struct {
		nos, types, grades                 []int64
		refs, names, docs, srcs, firstKeys []string
		content, sums                      [][]byte
		fresh                              []bool
	}
	for _, ch := range w.Objects {
		switch {
		case ch.New == nil:
			lines = append(lines, sapLine{kind: pgschema.DiffKindObject, typeNo: int16(ch.Old.TypeNo), change: pgschema.ChangeKindRetired,
				entity: s.objNo[ch.Old.ID.String()], ref: ch.Old.ID.String(), old: hashBytes(ch.Old.ContentHash)})
			continue
		case ch.Old == nil:
			s.objNo[ch.New.ID.String()], nos = nos[0], nos[1:]
		}
		r := ch.New
		no := s.objNo[r.ID.String()]
		s.objType[r.ID.String()] = r.TypeNo
		doc, err := encodeAttrs(r.Attrs)
		refs, rerr := refsJSON(r.SourceRefs)
		grade, gerr := gradeNo(r.MinGrade)
		sum, serr := graph.RowSum(entityNameObject, r.ID.String(), r.ContentHash)
		key, kerr := keyJSON(ch.Key)
		if e := firstErr(err, rerr, gerr, serr, kerr); e != nil {
			return nil, e
		}
		text, _ := key.(string)
		a.nos, a.types, a.grades = append(a.nos, no), append(a.types, int64(r.TypeNo)), append(a.grades, int64(grade))
		a.refs, a.names, a.docs, a.srcs, a.firstKeys = append(a.refs, r.ID.String()), append(a.names, r.Name), append(a.docs, string(doc)), append(a.srcs, string(refs)), append(a.firstKeys, text)
		a.content, a.sums, a.fresh = append(a.content, hashBytes(r.ContentHash)), append(a.sums, sumBytes(sum)), append(a.fresh, ch.Old == nil)
		line := sapLine{kind: pgschema.DiffKindObject, typeNo: int16(r.TypeNo), change: pgschema.ChangeKindAdded, entity: no, ref: r.ID.String(), new: hashBytes(r.ContentHash)}
		if ch.Old != nil {
			line.change, line.old = pgschema.ChangeKindChanged, hashBytes(ch.Old.ContentHash)
		}
		lines = append(lines, line)
	}
	for _, ch := range w.Objects {
		if ch.Old != nil || ch.New == nil {
			continue
		}
		k, err := s.keyLine(ch, s.objNo[ch.New.ID.String()])
		if err != nil {
			return nil, err
		}
		if k != nil {
			keys = append(keys, *k)
		}
	}
	if len(a.nos) > 0 {
		_, err := tx.ExecContext(ctx, s.a.stagedSQL(`INSERT INTO {stage_object} ({obj_no}, {ref}, {type_no}, {name}, {min_grade}, {doc}, {source_refs}, {content_hash}, {row_hash}, {first_key}, {is_new})
			SELECT u.n, u.r, u.t, u.nm, u.g, u.d::jsonb, u.s::jsonb, u.c, u.h, NULLIF(u.k, '')::jsonb, u.f
			FROM unnest($1::bigint[], $2::text[], $3::bigint[], $4::text[], $5::bigint[], $6::text[], $7::text[], $8::bytea[], $9::bytea[], $10::text[], $11::boolean[])
				AS u(n, r, t, nm, g, d, s, c, h, k, f)`), pq.Array(a.nos), pq.Array(a.refs), pq.Array(a.types), pq.Array(a.names), pq.Array(a.grades),
			pq.Array(a.docs), pq.Array(a.srcs), pq.ByteaArray(a.content), pq.ByteaArray(a.sums), pq.Array(a.firstKeys), pq.Array(a.fresh))
		if err != nil {
			return nil, err
		}
	}
	return append(lines, keys...), nil
}

// keyLine is the line of the difference for the key of a new object: the type, the object that the key hangs from and
// the hash of the shape of the key (the hash the identification index has).
func (s *sapStager) keyLine(ch graph.ObjectChange, no int64) (*sapLine, error) {
	ctxRef, hash, _, ok, err := keySignature(ch.Key)
	if err != nil || !ok {
		return nil, err
	}
	var ctxNo int64
	if ctxRef != "" {
		ctxNo = s.objNo[ctxRef]
	}
	row, err := pgschema.KeyRowHash(int16(ch.New.TypeNo), ctxNo, hash)
	if err != nil {
		return nil, err
	}
	return &sapLine{kind: pgschema.DiffKindKey, typeNo: int16(ch.New.TypeNo), change: pgschema.ChangeKindAdded, entity: no, ctx: ctxNo,
		ref: ch.New.ID.String(), new: row[:], sig: hash}, nil
}

func (s *sapStager) links(ctx context.Context, tx *sql.Tx, w graph.ChunkWrite) ([]sapLine, error) {
	fresh := 0
	for _, ch := range w.Links {
		if ch.New != nil && ch.Old == nil {
			fresh++
		}
	}
	nos, err := s.numbers(ctx, tx, "praxium_next_link_nos", fresh)
	if err != nil {
		return nil, err
	}
	var lines []sapLine
	var a struct {
		nos, types, src, dst                   []int64
		refs, discs, grades, bases, docs, srcs []string
		content, sums                          [][]byte
	}
	for _, ch := range w.Links {
		if ch.New == nil {
			lines = append(lines, sapLine{kind: pgschema.DiffKindLink, typeNo: int16(ch.Old.TypeNo), change: pgschema.ChangeKindRetired,
				entity: s.linkNo[ch.Old.ID.String()], ref: ch.Old.ID.String(), old: hashBytes(ch.Old.ContentHash)})
			continue
		}
		r := ch.New
		if ch.Old == nil {
			s.linkNo[r.ID.String()], nos = nos[0], nos[1:]
		}
		doc, err := encodeAttrs(r.Attrs)
		refs, rerr := refsJSON(r.SourceRefs)
		sum, serr := graph.RowSum(entityNameLink, r.ID.String(), r.ContentHash)
		if e := firstErr(err, rerr, serr); e != nil {
			return nil, e
		}
		no := s.linkNo[r.ID.String()]
		a.nos, a.types, a.src, a.dst = append(a.nos, no), append(a.types, int64(r.TypeNo)), append(a.src, s.objNo[r.Src.String()]), append(a.dst, s.objNo[r.Dst.String()])
		a.refs, a.discs, a.grades, a.bases = append(a.refs, r.ID.String()), append(a.discs, r.Discriminator), append(a.grades, string(r.Grade)), append(a.bases, string(r.Basis))
		a.docs, a.srcs, a.content, a.sums = append(a.docs, string(doc)), append(a.srcs, string(refs)), append(a.content, hashBytes(r.ContentHash)), append(a.sums, sumBytes(sum))
		line := sapLine{kind: pgschema.DiffKindLink, typeNo: int16(r.TypeNo), change: pgschema.ChangeKindAdded, entity: no, ref: r.ID.String(), new: hashBytes(r.ContentHash)}
		if ch.Old != nil {
			line.change, line.old = pgschema.ChangeKindChanged, hashBytes(ch.Old.ContentHash)
		}
		lines = append(lines, line)
	}
	if len(a.nos) > 0 {
		_, err := tx.ExecContext(ctx, s.a.stagedSQL(`INSERT INTO {stage_link} ({link_no}, {ref}, {def_no}, {type_no}, {src}, {dst}, {discriminator}, {grade}, {basis}, {doc}, {source_refs}, {content_hash}, {row_hash})
			SELECT u.n, u.r, 0, u.t, u.s, u.d, u.dc, u.g, u.b, u.doc::jsonb, u.sr::jsonb, u.c, u.h
			FROM unnest($1::bigint[], $2::text[], $3::bigint[], $4::bigint[], $5::bigint[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::bytea[], $12::bytea[])
				AS u(n, r, t, s, d, dc, g, b, doc, sr, c, h)`), pq.Array(a.nos), pq.Array(a.refs), pq.Array(a.types), pq.Array(a.src), pq.Array(a.dst),
			pq.Array(a.discs), pq.Array(a.grades), pq.Array(a.bases), pq.Array(a.docs), pq.Array(a.srcs), pq.ByteaArray(a.content), pq.ByteaArray(a.sums))
		if err != nil {
			return nil, err
		}
	}
	return lines, nil
}

func (s *sapStager) identifiers(ctx context.Context, tx *sql.Tx, w graph.ChunkWrite) ([]sapLine, error) {
	var lines []sapLine
	var nos []int64
	var schemes, values, scopes []string
	for _, ch := range w.Identifiers {
		r := ch.Record
		h := pgschema.IdentRowHash(r.Scheme, r.Value, r.Scope)
		line := sapLine{kind: pgschema.DiffKindIdentifier, change: pgschema.ChangeKindAdded, entity: s.objNo[r.Object.String()], ref: r.Object.String(), new: h[:]}
		if ch.Closes {
			line.change, line.old, line.new = pgschema.ChangeKindRetired, h[:], nil
		} else {
			nos, schemes, values, scopes = append(nos, line.entity), append(schemes, r.Scheme), append(values, r.Value), append(scopes, r.Scope)
		}
		lines = append(lines, line)
	}
	if len(nos) > 0 {
		_, err := tx.ExecContext(ctx, s.a.stagedSQL(`INSERT INTO {stage_ident} ({obj_no}, {scheme}, {value}, {scope})
			SELECT * FROM unnest($1::bigint[], $2::text[], $3::text[], $4::text[]) ON CONFLICT DO NOTHING`), pq.Array(nos), pq.Array(schemes), pq.Array(values), pq.Array(scopes))
		if err != nil {
			return nil, err
		}
	}
	return lines, nil
}

// writeLines gives the lines of the chunk their order and writes them.
func (s *sapStager) writeLines(ctx context.Context, tx *sql.Tx, lines []sapLine) error {
	if len(lines) == 0 {
		return nil
	}
	var ords, kinds, types, entities, changes, ctxs []int64
	var refs []string
	var olds, news, sigs []string
	for _, l := range lines {
		s.ord++
		ords, kinds, types, entities = append(ords, s.ord), append(kinds, int64(l.kind)), append(types, int64(l.typeNo)), append(entities, l.entity)
		changes, ctxs, refs = append(changes, int64(l.change)), append(ctxs, l.ctx), append(refs, l.ref)
		olds, news, sigs = append(olds, sapHex(l.old)), append(news, sapHex(l.new)), append(sigs, sapHex(l.sig))
	}
	_, err := tx.ExecContext(ctx, s.a.stagedSQL(`INSERT INTO {ingest_diff} ({ord}, {chunk_no}, {kind}, {type_no}, {entity_no}, {ref}, {change_kind}, {old_hash}, {new_hash}, {ctx_no}, {sig_hash})
		SELECT u.o, $1, u.k, u.t, u.e, u.r, u.c, decode(NULLIF(u.old, ''), 'hex'), decode(NULLIF(u.new, ''), 'hex'), u.x, decode(NULLIF(u.sg, ''), 'hex')
		FROM unnest($2::bigint[], $3::bigint[], $4::bigint[], $5::bigint[], $6::text[], $7::bigint[], $8::text[], $9::text[], $10::bigint[], $11::text[])
			AS u(o, k, t, e, r, c, old, new, x, sg)`), s.chunk, pq.Array(ords), pq.Array(kinds), pq.Array(types), pq.Array(entities), pq.Array(refs),
		pq.Array(changes), pq.Array(olds), pq.Array(news), pq.Array(ctxs), pq.Array(sigs))
	s.chunk++
	return err
}

// volatile stages the volatile values of the chunks (the entry of prov each value makes).
func (s *sapStager) volatile(ctx context.Context, w graph.ChunkWrite) error {
	if len(w.Volatile) == 0 {
		return nil
	}
	var nos, types, attrs []int64
	var entries []string
	for _, v := range w.Volatile {
		raw, err := json.Marshal(provEntry{Value: v.Value, Grade: string(v.Grade), Basis: string(v.Basis), Seen: v.SeenAt, Ref: v.Ref})
		if err != nil {
			return err
		}
		nos, types, attrs, entries = append(nos, s.objNo[v.Object.String()]), append(types, int64(s.objType[v.Object.String()])), append(attrs, int64(v.AttrNo)), append(entries, string(raw))
	}
	return s.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, s.a.stagedSQL(`INSERT INTO {stage_volatile} ({obj_no}, {type_no}, {attr_no}, {entry})
			SELECT u.n, u.t, u.a, u.e::jsonb FROM unnest($1::bigint[], $2::bigint[], $3::bigint[], $4::text[]) AS u(n, t, a, e)
			ON CONFLICT ({obj_no}, {attr_no}) DO UPDATE SET {entry} = EXCLUDED.{entry}`), pq.Array(nos), pq.Array(types), pq.Array(attrs), pq.Array(entries))
		return err
	})
}

// sapHex is a hash as hex text, and no hash as the empty text: a line of the difference that has no old (or no new) hash
// must hold NULL, which an array of bytea of the driver does not carry (it writes an empty value).
func sapHex(b []byte) string { return hex.EncodeToString(b) }
