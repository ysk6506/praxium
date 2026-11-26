package pgingest

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// AskPage gives the new objects of the round that have a parent and no id yet: see ingest.Identifier. The questions are given in the
// order of their labels, a page at a time, and a page is answered whole before the next is asked (the place keeps the last label
// answered), so what a page reads is the questions after it and none of the ones answered.
func (s *Store) AskPage(ctx context.Context, lease ingest.RunLease) ([]ingest.AskRow, error) {
	var out []ingest.AskRow
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.loadIdent(ctx, tx, lease)
		if err != nil {
			return err
		}
		if r.st.Phase != phaseAsk {
			return kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d is at %s, not at the new objects that have a parent", lease.No, r.st.Phase)
		}
		query := r.q("SELECT a.label, a.rid, c.src_no, d.dataset_id, c.key, c.fields, a.feed_no, a.key_ord, a.parent_ref FROM {ask} a " +
			"JOIN {rec} c ON c.rid = a.rid JOIN " + pgschema.TableSourceDataset + " d ON d.dataset_no = c.dataset_no WHERE a.round_no = {R} AND NOT a.answer " +
			"AND a.label > $2 ORDER BY a.label LIMIT $1")
		r.saw(tx, "read the questions", query, r.st.Size, r.st.Cur)
		rows, err := tx.QueryContext(ctx, query, r.st.Size, r.st.Cur)
		if err != nil {
			return dbErr(err, "read the questions")
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			a := ingest.AskRow{Round: r.st.Round}
			var cells string
			var parent sql.NullString
			if err := rows.Scan(&a.Label, &a.Rid, &a.SrcNo, &a.Dataset, &a.Key, &cells, &a.Feed, &a.Ord, &parent); err != nil {
				return dbErr(err, "read a question")
			}
			a.ParentRef = parent.String
			if a.Fields, err = parseTextArray(cells); err != nil {
				return err
			}
			out = append(out, a)
		}
		return dbErr(rows.Err(), "read the questions")
	})
	return out, err
}

// PutAnswers takes the ids Go made, makes the objects of the groups and binds their records and keys: see ingest.Identifier.
func (s *Store) PutAnswers(ctx context.Context, lease ingest.RunLease, answers []ingest.Answer) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.loadIdent(ctx, tx, lease)
		if err != nil {
			return err
		}
		st := r.st
		if st.Phase != phaseAsk {
			return kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d is at %s, not at the new objects that have a parent", lease.No, st.Phase)
		}
		var labels, refs, missing []string
		var top int64
		for _, a := range answers {
			top = max(top, a.Label)
			if a.Round != st.Round {
				return kernel.Errorf(kernel.CodeInvalidValue, "an answer is of the round %d and the run is at %d", a.Round, st.Round)
			}
			labels, refs, missing = append(labels, strconv.FormatInt(a.Label, 10)), append(refs, a.Ref), append(missing, strconv.FormatBool(a.Missing))
		}
		if len(answers) > 0 {
			if err := r.settleAnswers(ctx, tx, answerSet{textArray(labels), textArray(refs), textArray(missing), len(answers), top}); err != nil {
				return err
			}
			st.Cur = top
		}
		if st.AskLeft == 0 {
			if err := r.nextRound(ctx, tx); err != nil {
				return err
			}
		}
		return writeIdent(ctx, tx, lease, st, false)
	})
}

// answerSet is the answers of a page: the labels, the ids and whether each has no name, as the text of arrays, how many there are and
// the last label.
type answerSet struct {
	labels, refs, missing string
	asked                 int
	top                   int64
}

// settleAnswers writes the answers and does for the groups they name what decide does for the groups it makes ids of itself.
func (r *identRun) settleAnswers(ctx context.Context, tx *sql.Tx, a answerSet) error {
	labels, refs, missing, asked, top := a.labels, a.refs, a.missing, a.asked, a.top
	const un = "unnest($1::text::bigint[], $2::text::text[], $3::text::boolean[]) AS u(label, ref, missing)"
	// the pairs of the groups of the page, by one probe of the labels for each of them (that is not a test of each label against the whole
	// array, which is the square of the page)
	pl := "pl AS (SELECT l.pid, q.label FROM unnest($1::text::bigint[]) AS q(label)" +
		scan("x.pid", "{label}", "l", "x.round_no = {R} AND x.iter = {F} AND x.label = q.label") + "), " + psSQL
	answered := "answers AS (SELECT u.label, a.rid, a.feed_no, a.key_ord, a.type_no, u.ref, u.missing FROM " + un +
		scan("x.rid, x.feed_no, x.key_ord, x.type_no", "{ask}", "a", "x.round_no = {R} AND x.label = u.label AND x.answer = false") + ")"
	c := &r.st.Counts
	steps := []unitStep{
		{"write the answers", "WITH " + answered + " INSERT INTO {ask} (round_no, label, rid, feed_no, key_ord, type_no, ref, missing, answer) " +
			"SELECT {R}, label, rid, feed_no, key_ord, type_no, CASE WHEN missing THEN NULL ELSE ref END, missing, true FROM answers", []*int{&r.st.AskLeft}, false},
		{"make the objects under a parent", "WITH " + pl + ", " + answered + ", ex AS (SELECT a.label, a.rid, a.feed_no, a.key_ord, a.type_no, a.ref, o.obj_no AS reg " +
			"FROM answers a" + maybe("x.obj_no", "{obj}", "o", "x.ref = a.ref") + " WHERE NOT a.missing), " + makeSQL, []*int{&c.NewObjects}, false},
		{"bind the new keys", r.newKeysSQL(pl), []*int{&c.NewKeys, &c.Dropped}, true},
		{"leave the records of the objects with no name", "WITH " + pl + ", p AS (SELECT DISTINCT s.rid, s.feed_no FROM ps s" +
			absent("{ask}", "ha", "x.round_no = {R} AND x.label = s.label AND x.answer AND x.missing") + " WHERE ha.hit IS NOT NULL), " +
			"r AS (" + refUnresolved("p") + " RETURNING 1) INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, " + pgschema.ProblemDefFeedSQL("feed_no") + ", '', '" + pgschema.ProblemMissingName +
			"', 'the object this record would make has no name: the attribute name has no value and the key has no part but its context' FROM p", []*int{&c.Unresolved}, false},
	}
	args := [][]any{{labels, refs, missing}, {labels, refs, missing}, {labels}, {labels}}
	if err := r.noOpenQuestions(ctx, tx, top, asked); err != nil {
		return err
	}
	for i, s := range steps {
		if i == 0 {
			// every answer is for a question that is asked and not answered
			n, err := r.exec(ctx, tx, s.what, r.q(s.query), args[i]...)
			if err != nil {
				return err
			}
			if n != asked {
				return kernel.Errorf(kernel.CodeInvalidValue, "%d of the %d answers are for questions that are not asked", asked-n, asked)
			}
			r.st.AskLeft -= n
			continue
		}
		if err := r.run(ctx, tx, s, args[i]...); err != nil {
			return err
		}
	}
	return nil
}

// noOpenQuestions checks, before the answers are written, that every question of the page up to the last answer has one: the questions
// there are not answered yet, and they are as many as the answers. It looks at the page and at one question more.
func (r *identRun) noOpenQuestions(ctx context.Context, tx *sql.Tx, top int64, asked int) error {
	open, err := r.scanInt(ctx, tx, "find the questions of the page that are not answered", r.q("SELECT count(*) FROM (SELECT 1 FROM {ask} a WHERE a.round_no = {R} AND a.label > $1 AND a.label <= $2 "+
		"AND a.answer = false LIMIT $3) q"), r.st.Cur, top, asked+1)
	if err != nil {
		return err
	}
	if int(open) > asked {
		return kernel.Errorf(kernel.CodeInvalidValue, "%d questions of the page are left without an answer", int(open)-asked)
	}
	return nil
}
