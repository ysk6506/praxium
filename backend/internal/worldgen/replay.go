package worldgen

import (
	"bufio"
	"encoding/json"
	"io"
)

// Entity is the state of one object or link after replaying the change stream.
type Entity struct {
	Kind    string
	Type    string
	Present bool
	Time    string
	Props   map[string]string
}

// Replayed is the result of a replay.
type Replayed struct {
	Entities map[string]*Entity // by the id the change feed uses
	Applied  int64
	Late     int64 // older than what was already applied to the entity: ignored
	Dups     int64 // the same event_id again: ignored
	Rejected int64 // lines that do not parse
}

type wireEvent struct {
	EventID string                     `json:"event_id"`
	Time    string                     `json:"source_time"`
	Op      string                     `json:"op"`
	Kind    string                     `json:"kind"`
	Type    string                     `json:"type"`
	ID      string                     `json:"id"`
	Props   map[string]json.RawMessage `json:"props"`
}

// ReplayChanges applies a change stream the way a reader should: lines that
// do not parse are counted and skipped, an event_id seen before is ignored,
// and an event older than the last one applied to its entity is ignored (the
// late-value protection by source time).
func ReplayChanges(r io.Reader) (*Replayed, error) {
	res := &Replayed{Entities: map[string]*Entity{}}
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<24)
	for sc.Scan() {
		var ev wireEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil || ev.EventID == "" || ev.ID == "" || ev.Time == "" || ev.Op == "" {
			res.Rejected++
			continue
		}
		if seen[ev.EventID] {
			res.Dups++
			continue
		}
		seen[ev.EventID] = true
		e := res.Entities[ev.ID]
		if e == nil {
			e = &Entity{Kind: ev.Kind, Type: ev.Type, Props: map[string]string{}}
			res.Entities[ev.ID] = e
		}
		if ev.Time < e.Time {
			res.Late++
			continue
		}
		e.Time = ev.Time
		e.Present = ev.Op != "deleted"
		for name, raw := range ev.Props {
			e.Props[name] = rawText(raw)
		}
		res.Applied++
	}
	return res, sc.Err()
}

func rawText(raw json.RawMessage) string {
	var s string
	if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// Expected is one line of expected.jsonl.
type Expected struct {
	Kind  string                     `json:"kind"`
	ID    string                     `json:"id"`
	Final string                     `json:"final"`
	Type  string                     `json:"type"`
	Props map[string]json.RawMessage `json:"props"`
}

// ReadExpected reads the expected final state of a plan.
func ReadExpected(r io.Reader) ([]Expected, error) {
	var out []Expected
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<24)
	for sc.Scan() {
		var e Expected
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// CheckReplay compares a replay with the expected state and returns the differences.
func CheckReplay(res *Replayed, exp []Expected) []string {
	var diffs []string
	for _, e := range exp {
		got := res.Entities[feedID(e.Kind, e.ID)]
		if got == nil {
			diffs = append(diffs, e.Kind+" "+e.ID+": no event")
			continue
		}
		if (e.Final == "present") != got.Present {
			diffs = append(diffs, e.Kind+" "+e.ID+": expected "+e.Final)
			continue
		}
		for name, raw := range e.Props {
			if got.Props[name] != rawText(raw) {
				diffs = append(diffs, e.Kind+" "+e.ID+": property "+name+" is "+got.Props[name]+", expected "+rawText(raw))
			}
		}
	}
	return diffs
}
