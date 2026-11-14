package ingest

import (
	"sort"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The thresholds that take a record that no complete capture shows out of use (defaults.md 3: three complete
// captures in a row, and at least 24 hours since the first). They are defaults of the product, not settings of
// the slice.
const (
	UnseenRuns = 3
	UnseenSpan = 24 * time.Hour
)

// RawChanges counts what a capture did to the raw records of a dataset (ingestion.md 7.1).
type RawChanges struct {
	Added     int `json:"added"`
	Modified  int `json:"modified"`
	Unchanged int `json:"unchanged"`
	Deleted   int `json:"deleted"`
	// Unseen is the records that no capture showed this time and are still counted as present.
	Unseen int `json:"unseen"`
}

// Duplicate is a row whose natural key an earlier row of the capture already had. The later row is used; the earlier
// one is a problem (pipeline.md 3.1: the key repeats, the rule decides which row is used; here the later wins).
type Duplicate struct {
	Key   string
	Line  int
	Where string
}

// dedupe keeps the last row of every natural key, in the order of the key, and returns the rows it set aside.
func dedupe(rows []Row) (kept []Row, dups []Duplicate) {
	last := map[string]int{}
	for i, r := range rows {
		if j, ok := last[r.Key]; ok {
			dups = append(dups, Duplicate{Key: r.Key, Line: rows[j].Line, Where: rows[j].Where})
		}
		last[r.Key] = i
	}
	for _, i := range last {
		kept = append(kept, rows[i])
	}
	sort.Slice(kept, func(a, b int) bool { return kept[a].Key < kept[b].Key })
	return kept, dups
}

func rowHash(r Row) (string, error) {
	h, err := kernel.ContentHash(r.Fields)
	return h.String(), err
}

// applyCapture turns a whole capture into changes of the raw records of the dataset. A record that is new is
// opened; one with the same hash stays; one with another hash is closed and opened again; one that the capture
// does not show is counted unseen, and closed once it was unseen enough (the capture is complete, which the caller
// has checked). It does not read the clock: now is given.
func applyCapture(ds *DatasetState, rows []Row, deletion string, run int, now time.Time) (RawChanges, error) {
	var ch RawChanges
	shown := map[string]bool{}
	for _, r := range rows {
		shown[r.Key] = true
		hash, err := rowHash(r)
		if err != nil {
			return ch, err
		}
		delete(ds.Unseen, r.Key)
		old := ds.Open[r.Key]
		switch {
		case old == nil:
			ch.Added++
		case old.Hash == hash:
			// the content is the same, so the record stays; where it stands in the file may have moved
			ch.Unchanged++
			old.Line, old.Where = r.Line, r.Where
			continue
		default:
			ch.Modified++
			old.ToRun = run
			ds.Closed = append(ds.Closed, *old)
		}
		ds.Open[r.Key] = &RawRecord{Key: r.Key, Hash: hash, Line: r.Line, Where: r.Where, Fields: r.Fields, FromRun: run}
	}
	if deletion == DeletionAbsentInFull {
		ch.countAbsent(ds, shown, run, now)
	}
	return ch, nil
}

func (c *RawChanges) countAbsent(ds *DatasetState, shown map[string]bool, run int, now time.Time) {
	keys := make([]string, 0, len(ds.Open))
	for k := range ds.Open {
		if !shown[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if ds.Unseen == nil {
		ds.Unseen = map[string]Unseen{}
	}
	for _, k := range keys {
		u := ds.Unseen[k]
		if u.Runs == 0 {
			u.First = now.UTC().Format(time.RFC3339)
		}
		u.Runs++
		first, _ := time.Parse(time.RFC3339, u.First)
		if u.Runs >= UnseenRuns && now.Sub(first) >= UnseenSpan {
			rec := ds.Open[k]
			rec.ToRun = run
			ds.Closed = append(ds.Closed, *rec)
			delete(ds.Open, k)
			delete(ds.Unseen, k)
			c.Deleted++
			continue
		}
		ds.Unseen[k] = u
		c.Unseen++
	}
}
