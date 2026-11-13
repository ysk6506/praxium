package devload

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// edit passes every record of a truth through change and writes the result; change returns false to drop the record.
func edit(t *testing.T, truth []byte, change func(rec map[string]any) bool) []byte {
	t.Helper()
	var out bytes.Buffer
	for i, line := range bytes.Split(bytes.TrimSpace(truth), []byte("\n")) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatal(err)
		}
		if i == 0 || change(rec) {
			raw, _ := json.Marshal(rec)
			out.Write(raw)
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

func tryLoad(t *testing.T, truth []byte) (Report, error) {
	t.Helper()
	reg := standard(t)
	clock := kernel.NewManualClock(loadedAt)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	store := graph.NewScratchStore()
	kg, err := NewKG(context.Background(), store, reg, ids, clock, "dev")
	if err != nil {
		t.Fatal(err)
	}
	return Loader{Store: store, Reg: reg, Clock: clock, IDs: ids}.Load(context.Background(), kg.ID, bytes.NewReader(truth))
}

// [REQ-258]
func TestWhatNoSourceCanDeliverIsCountedAndLeftOut(t *testing.T) {
	g, base := sharedWorld(t, 0)
	victim := ""
	truth := edit(t, g.truth, func(rec map[string]any) bool {
		switch rec["kind"] {
		case "object":
			if rec["type"] == "ot.fan" && victim == "" {
				victim = rec["id"].(string)
				rec["not_ingestible"] = "no_identity_key"
			}
		case "link":
			if rec["from"] == victim || rec["to"] == victim {
				rec["not_ingestible"] = "endpoint_not_ingestible"
			}
		}
		return true
	})
	rep, err := tryLoad(t, truth)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ObjectsNotIngestible != 1 || rep.LinksNotIngestible == 0 || rep.Objects != base.report.Objects-1 ||
		rep.Links != base.report.Links-rep.LinksNotIngestible || rep.LinksDangling != 0 {
		t.Fatalf("base %+v\nreport %+v", base.report, rep)
	}
}

// [REQ-258]
func TestAnObjectThatCannotFormItsKeyIsCountedWithTheLinksThatLoseAnEnd(t *testing.T) {
	g, base := sharedWorld(t, 0)
	done := false
	truth := edit(t, g.truth, func(rec map[string]any) bool {
		if rec["kind"] == "object" && rec["type"] == "ot.region" && !done {
			delete(rec["props"].(map[string]any), "code")
			done = true
		}
		return true
	})
	rep, err := tryLoad(t, truth)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ObjectsUnkeyed == 0 || rep.LinksDangling == 0 || rep.Objects != base.report.Objects-rep.ObjectsUnkeyed {
		t.Fatalf("base %+v\nreport %+v", base.report, rep)
	}
}

// [REQ-258]
func TestATruthThatBreaksTheModelIsRefusedWithAReason(t *testing.T) {
	g, _ := sharedWorld(t, 0)
	tests := []struct {
		name   string
		change func(rec map[string]any) bool
		want   string
	}{
		{"an unknown object type", func(r map[string]any) bool {
			if r["kind"] == "object" && r["id"] == "site.0" {
				r["type"] = "ot.nothing"
			}
			return true
		}, "ot.nothing"},
		{"an unknown link type", func(r map[string]any) bool {
			if r["kind"] == "link" {
				r["type"] = "lt.nothing"
			}
			return true
		}, "lt.nothing"},
		{"a link with an end that does not exist", func(r map[string]any) bool {
			if r["kind"] == "link" {
				r["to"] = "nowhere"
			}
			return true
		}, "end that does not exist"},
		{"an object twice", func(r map[string]any) bool { return true }, "twice"},
		{"an unknown kind of record", func(r map[string]any) bool {
			if r["id"] == "site.0" {
				r["kind"] = "note"
			}
			return true
		}, "note"},
		{"an integer that is not one", func(r map[string]any) bool {
			if r["kind"] == "object" && r["type"] == "ot.rack" {
				r["props"].(map[string]any)["total_u"] = "many"
			}
			return true
		}, "total_u"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truth := edit(t, g.truth, tt.change)
			if tt.name == "an object twice" {
				lines := bytes.Split(bytes.TrimSpace(truth), []byte("\n"))
				truth = append(append(truth, lines[1]...), '\n')
			}
			_, err := tryLoad(t, truth)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want it to mention %q", err, tt.want)
			}
		})
	}
	if _, err := tryLoad(t, []byte(`{"kind":"header","format":"other","version":1}`+"\n")); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("a file with another header: %v", err)
	}
	reg := standard(t)
	if _, err := (Loader{Reg: reg, Clock: kernel.NewManualClock(loadedAt), IDs: kernel.NewRandomIDs(kernel.NewSeededRng(1))}).
		Load(context.Background(), graph.KGID{}, bytes.NewReader(g.truth)); err == nil {
		t.Fatal("a loader without a store loaded")
	}
}

// [REQ-258]
func TestConvertGivesTheFormTheGraphTakes(t *testing.T) {
	tests := []struct {
		base model.BaseKind
		raw  string
		want any
		fail bool
	}{
		{model.BaseString, `"abc"`, "abc", false},
		{model.BaseString, `12`, "12", false},
		{model.BaseInteger, `42`, int64(42), false},
		{model.BaseInteger, `"42"`, int64(42), false},
		{model.BaseInteger, `4.5`, nil, true},
		{model.BaseBoolean, `true`, true, false},
		{model.BaseBoolean, `"false"`, false, false},
		{model.BaseBoolean, `"maybe"`, nil, true},
		{model.BaseNumber, `623.1`, "623.1", false},
		{model.BaseEnum, `"up"`, "up", false},
		{model.BaseInteger, `{`, nil, true},
	}
	for _, tt := range tests {
		got, err := convert(tt.base, json.RawMessage(tt.raw))
		if (err != nil) != tt.fail || (!tt.fail && got != tt.want) {
			t.Fatalf("%s %s: got %v (%v), want %v", tt.base, tt.raw, got, err, tt.want)
		}
	}
	list, err := convert(model.BaseList, json.RawMessage(`["a",3,1.5]`))
	if err != nil || len(list.([]any)) != 3 || list.([]any)[1] != int64(3) || list.([]any)[2] != "1.5" {
		t.Fatalf("a list: %v %v", list, err)
	}
	m, err := convert(model.BaseMap, json.RawMessage(`{"k":{"n":2}}`))
	if err != nil || m.(map[string]any)["k"].(map[string]any)["n"] != int64(2) {
		t.Fatalf("a map: %v %v", m, err)
	}
	if textOf(json.RawMessage(`"x y"`)) != "x y" || textOf(json.RawMessage(`7`)) != "7" {
		t.Fatal("textOf")
	}
}
