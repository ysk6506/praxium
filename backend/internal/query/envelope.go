package query

import (
	"sort"
	"time"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// Answer is one answer: the envelope (the "meta" of the HTTP answer) and the body.
type Answer[T any] struct {
	Meta Envelope `json:"meta"`
	Data T        `json:"data"`
}

// Envelope is the part of an answer every query shares (kg-queries.md 4).
// health_hint, model_limits and served_by's built_at are not filled yet: no
// health data and no projection exist.
type Envelope struct {
	AsOf           AsOf           `json:"as_of"`
	VolatileAsOf   string         `json:"volatile_as_of"`
	ServedBy       ServedBy       `json:"served_by"`
	Freshness      Freshness      `json:"freshness"`
	Evidence       Evidence       `json:"evidence"`
	ResultComplete ResultComplete `json:"result_complete"`
	Hidden         Hidden         `json:"hidden"`
	ModelLimits    []string       `json:"model_limits"`
	Warnings       []string       `json:"warnings"`
}

// AsOf says up to which point the answer reflects the KG.
type AsOf struct {
	KGID          string  `json:"kg_id"`
	Generation    int     `json:"generation"`
	ChangeSeq     int64   `json:"change_seq"`
	SealedVersion *string `json:"sealed_version"`
}

// ServedBy names the role that answered; the reference implementation is `api_sql`.
type ServedBy struct {
	Role string `json:"role"`
}

// Freshness is the worst source among the values the answer used.
type Freshness struct {
	WorstSourceID    *string `json:"worst_source_id"`
	OldestAgeSeconds *int64  `json:"oldest_age_seconds"`
	State            string  `json:"state"`
}

// Freshness states. No source declares an expected period yet, so only the
// third occurs; fresh and stale are kept for when periods exist (ADR-0011).
const (
	FreshnessFresh    = "fresh"
	FreshnessStale    = "stale"
	FreshnessNoPeriod = "no_expected_period"
)

// Evidence is what the answer stands on: the lowest grade used, the counts per
// grade, the kinds of basis and the age of the oldest observation.
type Evidence struct {
	MinGrade              *model.Grade `json:"min_grade"`
	Counts                GradeCounts  `json:"counts"`
	Kinds                 []string     `json:"kinds"`
	ObservationAgeSeconds *int64       `json:"observation_age_seconds"`
}

// GradeCounts counts used inputs per grade.
type GradeCounts struct {
	Fact    int `json:"fact"`
	Assumed int `json:"assumed"`
	Unknown int `json:"unknown"`
}

// ResultComplete says whether the answer holds everything the query matched.
type ResultComplete struct {
	Total      int     `json:"total"`
	Returned   int     `json:"returned"`
	Truncated  bool    `json:"truncated"`
	Reason     *string `json:"reason"`
	NextCursor *string `json:"next_cursor"`
}

// Hidden is what the read filter took out of the answer. A caller whose scope
// is not the whole gets presence only (the counts are null).
type Hidden struct {
	PassedCount  *int `json:"hidden_passed_count"`
	ReachedCount *int `json:"hidden_reached_count"`
	Passed       bool `json:"hidden_passed"`
	Reached      bool `json:"hidden_reached"`
}

// usage collects the inputs an answer used, to fill evidence and freshness.
type usage struct {
	counts   GradeCounts
	grades   []model.Grade
	kinds    map[model.Basis]bool
	oldest   time.Time
	oldestID string
	oldObs   time.Time
	now      time.Time
}

func newUsage(now time.Time) *usage {
	return &usage{kinds: map[model.Basis]bool{}, now: now}
}

// add records one value or link: its grade, basis, when the source saw it and which source.
func (u *usage) add(g model.Grade, b model.Basis, seen, source string) {
	switch g {
	case model.GradeFact:
		u.counts.Fact++
	case model.GradeAssumed:
		u.counts.Assumed++
	default:
		u.counts.Unknown++
	}
	u.grades = append(u.grades, g)
	if b != "" {
		u.kinds[b] = true
	}
	t, err := time.Parse(time.RFC3339, seen)
	if err != nil {
		return
	}
	if u.oldest.IsZero() || t.Before(u.oldest) {
		u.oldest, u.oldestID = t, source
	}
	if b == model.BasisObserved && (u.oldObs.IsZero() || t.Before(u.oldObs)) {
		u.oldObs = t
	}
}

func (u *usage) age(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	s := int64(u.now.Sub(t) / time.Second)
	if s < 0 {
		s = 0
	}
	return &s
}

func (u *usage) freshness() Freshness {
	f := Freshness{State: FreshnessNoPeriod, OldestAgeSeconds: u.age(u.oldest)}
	if !u.oldest.IsZero() && u.oldestID != "" {
		id := u.oldestID
		f.WorstSourceID = &id
	}
	return f
}

func (u *usage) evidence() Evidence {
	e := Evidence{Counts: u.counts, Kinds: []string{}, ObservationAgeSeconds: u.age(u.oldObs)}
	if len(u.grades) > 0 {
		low := model.Lowest(u.grades...)
		e.MinGrade = &low
	}
	for k := range u.kinds {
		e.Kinds = append(e.Kinds, string(k))
	}
	sort.Strings(e.Kinds)
	return e
}

// hiddenCounts counts the objects the read filter removed.
type hiddenCounts struct {
	passed  map[string]bool
	reached map[string]bool
}

func newHidden() *hiddenCounts {
	return &hiddenCounts{passed: map[string]bool{}, reached: map[string]bool{}}
}

func (h *hiddenCounts) result(exact bool) Hidden {
	out := Hidden{Passed: len(h.passed) > 0, Reached: len(h.reached) > 0}
	if exact {
		p, r := len(h.passed), len(h.reached)
		out.PassedCount, out.ReachedCount = &p, &r
	}
	return out
}
