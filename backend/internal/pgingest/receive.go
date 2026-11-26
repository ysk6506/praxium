package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// Receive is the first step of a run: it receives the datasets of the source of the plan through the reader, page by page, into
// the receiving partition of the source, and moves the run on to computing.
//
// The run must be receiving and held by the lease. The first implementation always begins from the start: the receiving
// partition of the source is emptied (TRUNCATE) and the place of the receiving is reset, in one transaction, and then the first
// dataset is read from its first page. A reader that can go on from a token is not asked to: the place that each page writes to
// the run (ReceivePlace) is there for the step that will.
//
// The report of what every dataset gave (the pages, the records, whether it was read to its end, why not, and the coverage the
// last page said: complete, seen only or incomplete) is written to the counts of the run under "receive", so that the next step can
// leave out a dataset that was not read to its end whole, and count a dataset that was read as what was seen without its absences. The same
// fact is on the last page of such a dataset, as a problem of the page. A dataset that the reader did not give this time has
// no line in the report: it was not received.
//
// When the reading ends, the run is moved to computing. If another run of the KG is at work, the move is refused with
// CodeKGGateTaken, the rows stay where they are and the run stays receiving: the caller moves it on when the gate is free (a
// second Receive would begin again from the start). An error of the reader, of a page or of the context ends the receiving where it
// is and is returned with the report so far; what the run is made of that (failed, canceled, tried again) is for the caller.
func (s *Store) Receive(ctx context.Context, lease ingest.RunLease, in ingest.Intake) (ingest.IntakeReport, error) {
	if in.Plan == nil || in.Reader == nil {
		return ingest.IntakeReport{}, kernel.Errorf(kernel.CodeInvalidValue, "receiving needs a plan and a reader")
	}
	sink, err := s.newSink(ctx, lease, in.Plan)
	if err != nil {
		return ingest.IntakeReport{}, err
	}
	in.Sink = sink
	rep, err := in.Run(ctx)
	if err != nil {
		return rep, err
	}
	if err := s.saveCounts(ctx, sink, rep); err != nil {
		return rep, err
	}
	return rep, s.AdvanceRun(ctx, lease, ingest.RunReceiving, ingest.RunComputing)
}

// newSink makes the sink of the run and starts the receiving from the first page: it finds what the receiving needs to know of
// the run, checks that the run is the one of the source of the plan and is receiving, empties the receiving partition of the
// source and resets the place. Each page the sink is given after that is one transaction.
func (s *Store) newSink(ctx context.Context, lease ingest.RunLease, plan *ingest.Plan) (*Sink, error) {
	sink := &Sink{st: s, lease: lease, schemes: plan.SchemeNumbers(), skipped: map[string]map[string]int{}}
	if err := s.begin(ctx, sink, plan); err != nil {
		return nil, err
	}
	return sink, nil
}

// begin finds what the receiving needs to know of the run, checks that the run is the one of this source and receiving, and
// starts it again from the first page: the receiving partition is emptied and the place is reset in one transaction.
func (s *Store) begin(ctx context.Context, sink *Sink, plan *ingest.Plan) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		var kgID, sourceID, stage string
		var attempt int64
		err := tx.QueryRowContext(ctx, "SELECT r."+pgschema.ColKGID+", r."+pgschema.ColSrcNo+", r."+pgschema.ColStage+", r."+pgschema.ColAttempt+", s."+pgschema.ColSourceID+
			" FROM "+pgschema.TableSyncRun+" r JOIN "+pgschema.TableSourceState+" s ON s."+pgschema.ColSrcNo+" = r."+pgschema.ColSrcNo+
			" WHERE r."+pgschema.ColRunNo+" = $1", sink.lease.No).Scan(&kgID, &sink.srcNo, &stage, &attempt, &sourceID)
		switch {
		case isNoRows(err):
			return kernel.Errorf(ingest.CodeRunLeaseLost, "the run %d is not there any more", sink.lease.No)
		case err != nil:
			return dbErr(err, "read the run")
		case attempt != sink.lease.Attempt:
			return kernel.Errorf(ingest.CodeRunLeaseLost, "the run %d is held by attempt %d, not %d", sink.lease.No, attempt, sink.lease.Attempt)
		case ingest.RunStage(stage) != ingest.RunReceiving:
			return kernel.Errorf(ingest.CodeRunStage, "the run %d is %s: only a run that is receiving receives", sink.lease.No, stage)
		case sourceID != plan.Source.ID:
			return kernel.Errorf(kernel.CodeInvalidValue, "the run is the run of the source %q, and the plan is the plan of %q", sourceID, plan.Source.ID)
		}
		if err := s.loadSource(ctx, tx, sink, kgID, plan); err != nil {
			return err
		}
		return restart(ctx, tx, sink)
	})
}

// loadSource reads the number of the KG and the numbers of the datasets of the source, and checks that every dataset of the plan
// is one the source was put to the KG with.
func (s *Store) loadSource(ctx context.Context, tx *sql.Tx, sink *Sink, kgID string, plan *ingest.Plan) error {
	if err := tx.QueryRowContext(ctx, "SELECT "+catalogKGNo+" FROM "+catalogTable+" WHERE "+catalogKGID+" = $1 FOR SHARE", kgID).Scan(&sink.kgNo); err != nil {
		return dbErr(err, "read the number of the KG")
	}
	if err := refuseClosed(ctx, tx, kgID); err != nil {
		return err
	}
	ref := SourceRef{SrcNo: sink.srcNo, Datasets: map[string]int64{}}
	if err := attachRead(ctx, tx, &ref); err != nil {
		return err
	}
	sink.datasets = ref.Datasets
	for _, d := range plan.Source.Datasets {
		if _, ok := sink.datasets[d.ID]; !ok {
			return kernel.Errorf(kernel.CodeInvalidValue, "the dataset %q is not one the source was put to the KG with: put the source again", d.ID)
		}
	}
	return nil
}

// restart resets the place of the receiving and empties the receiving partition of the source. The place is written first,
// with the attempt as the condition, so a run that another attempt holds is refused before anything is emptied.
func restart(ctx context.Context, tx *sql.Tx, sink *Sink) error {
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColRecvData+" = NULL, "+pgschema.ColRecvPage+" = 0, "+pgschema.ColRecvToken+
		" = NULL, "+pgschema.ColLeaseUntil+" = "+lease("$3")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+string(ingest.RunReceiving)+"'",
		sink.lease.No, sink.lease.Attempt, sink.lease.Term.Seconds())
	if err != nil {
		return dbErr(err, "reset the place of the receiving")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, sink.lease)
	}
	truncate, err := pgschema.TruncateReceiveSQL(sink.kgNo, sink.srcNo)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, truncate)
	return dbErr(err, "empty what the source was received into")
}

// datasetCount is what a dataset gave, as the counts of the run keep it.
type datasetCount struct {
	Dataset    string         `json:"dataset"`
	DatasetNo  int64          `json:"dataset_no"`
	Pages      int            `json:"pages"`
	Records    int            `json:"records"`
	Unreadable int            `json:"unreadable"`
	Skipped    map[string]int `json:"skipped,omitempty"`
	Complete   bool           `json:"complete"`
	Reason     string         `json:"reason,omitempty"`
	NotRead    string         `json:"not_read,omitempty"`
	// Coverage is how much of the dataset the next step may count (ingest.Coverage): complete, seen_only or incomplete. A dataset
	// that gave no rows because its header changed is incomplete however its last page ended.
	Coverage ingest.Coverage `json:"coverage"`
}

// saveCounts writes what the receiving did to the counts of the run, under "receive". The condition is the attempt.
func (s *Store) saveCounts(ctx context.Context, sink *Sink, rep ingest.IntakeReport) error {
	lines := make([]datasetCount, 0, len(rep.Datasets))
	complete := true
	for _, d := range rep.Datasets {
		lines = append(lines, datasetCount{Dataset: d.Dataset, DatasetNo: sink.datasets[d.Dataset], Pages: d.Pages, Records: d.Records, Unreadable: d.Unreadable,
			Skipped: sink.skipped[d.Dataset], Complete: d.Complete, Reason: d.Reason, NotRead: d.NotRead, Coverage: d.Coverage()})
		complete = complete && d.Complete
	}
	text, err := json.Marshal(map[string]any{"receive": map[string]any{"datasets": lines, "complete": complete}})
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "write the counts of the receiving")
	}
	return s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColCounts+" = "+pgschema.ColCounts+" || $3::jsonb, "+pgschema.ColUpdatedAt+
			" = now() WHERE "+guard(), sink.lease.No, sink.lease.Attempt, string(text))
		if err != nil {
			return dbErr(err, "write the counts of the receiving")
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return miss(ctx, tx, sink.lease)
		}
		return nil
	})
}
