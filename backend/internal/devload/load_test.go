package devload

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/query"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

// The small worlds the properties run on: seeds and scales that give different shapes.
var smallWorlds = []struct {
	seed  uint64
	scale int64
	tweak func(*worldgen.Params)
}{
	{1, 500, nil},
	{2, 500, nil},
	{3, 3000, nil},
	{11, 5000, func(p *worldgen.Params) { p.Sites = 2 }},
}

// [REQ-258]
func TestEveryTruthObjectAndLinkIsReadableThroughTheQueries(t *testing.T) {
	reg := standard(t)
	for i, sw := range smallWorlds {
		t.Run(fmt.Sprintf("seed %d scale %d", sw.seed, sw.scale), func(t *testing.T) {
			g, l := sharedWorld(t, i)
			w := ids(t, reg, g.truth)
			for _, o := range w.order {
				data := l.get(t, o.id)
				if data.Object.ObjectType != o.typ.typ.Name || data.Object.DisplayName != nameOf(o) {
					t.Fatalf("%s reads as %+v, truth says %s %q", o.rec.ID, data.Object, o.typ.typ.Name, nameOf(o))
				}
				checkAttributes(t, o, data)
			}
			for _, o := range w.order {
				around := l.neighbors(t, o.id)
				for _, e := range o.out {
					if e.to.ok && !slices.ContainsFunc(around, func(it query.NeighborItem) bool {
						return it.LinkType == e.typ.Name && it.Direction == query.DirOut && it.PeerObjectID == e.to.id.String()
					}) {
						t.Fatalf("link %s %s -> %s is not read from its source", e.typ.Name, e.from.rec.ID, e.to.rec.ID)
					}
				}
			}
		})
	}
}

// checkAttributes compares the scalar attributes the truth gives with what get answers.
func checkAttributes(t *testing.T, o *object, data query.GetData) {
	t.Helper()
	got := map[string]query.AttributeValue{}
	for _, a := range data.Attributes {
		got[a.Name] = a
	}
	for name, raw := range o.rec.Props {
		p, ok := o.typ.props[name]
		if !ok || strings.HasPrefix(string(raw), "[") || strings.HasPrefix(string(raw), "{") {
			continue
		}
		a, ok := got[name]
		if !ok {
			t.Fatalf("%s: attribute %s is not read", o.rec.ID, name)
		}
		if slices.Contains(o.rec.Assumed, name) != (a.Grade == "assumed") && !slices.Contains(o.rec.Unknown, name) {
			t.Fatalf("%s.%s: grade %s, truth assumed=%v", o.rec.ID, name, a.Grade, slices.Contains(o.rec.Assumed, name))
		}
		if fmt.Sprint(a.Value) != textOf(raw) && p.ValueType.String() != "vt.quantity" {
			t.Fatalf("%s.%s reads %v, truth says %s", o.rec.ID, name, a.Value, textOf(raw))
		}
	}
}

// [REQ-258]
func TestCountsMatchTheManifest(t *testing.T) {
	for i, sw := range smallWorlds {
		g, l := sharedWorld(t, i)
		c := g.manifest.Counts
		r := l.report
		if r.TruthObjects != c.Objects || r.TruthLinks != c.Links {
			t.Fatalf("seed %d: the loader read %d objects and %d links, the manifest says %d and %d", sw.seed, r.TruthObjects, r.TruthLinks, c.Objects, c.Links)
		}
		if r.Objects != c.Objects-c.ObjectsNotIngestible || r.Links != c.Links-c.LinksNotIngestible {
			t.Fatalf("seed %d: loaded %d objects and %d links, the manifest leaves %d and %d", sw.seed, r.Objects, r.Links,
				c.Objects-c.ObjectsNotIngestible, c.Links-c.LinksNotIngestible)
		}
		if r.ObjectsNotIngestible != c.ObjectsNotIngestible || r.LinksNotIngestible != c.LinksNotIngestible || r.ObjectsUnkeyed != 0 || r.LinksDangling != 0 {
			t.Fatalf("seed %d: the skipped counts differ from the manifest: %+v", sw.seed, r)
		}
		ans, err := l.engine.Find(context.Background(), query.FindInput{Common: query.Common{KG: graph.MainAlias, Limit: 1}}, query.AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		var total int64
		for _, tc := range ans.Data.CountsByType {
			total += int64(tc.Count)
			if want := c.ObjectsByType[tc.ObjectType]; int64(tc.Count) > want || (c.ObjectsNotIngestible == 0 && int64(tc.Count) != want) {
				t.Fatalf("seed %d: %s has %d objects, the manifest says %d", sw.seed, tc.ObjectType, tc.Count, want)
			}
		}
		if total != r.Objects {
			t.Fatalf("seed %d: find counts %d objects, the loader loaded %d", sw.seed, total, r.Objects)
		}
	}
}

// [REQ-258]
func TestNeighborsAgreeWithTheTruth(t *testing.T) {
	reg := standard(t)
	for i, sw := range smallWorlds[:3] {
		g, l := sharedWorld(t, i)
		w := ids(t, reg, g.truth)
		for _, o := range w.order {
			want := truthAround(o)
			var got []string
			for _, it := range l.neighbors(t, o.id) {
				got = append(got, it.Direction+" "+it.LinkType+" "+it.PeerObjectID)
			}
			sort.Strings(got)
			if !slices.Equal(got, want) {
				t.Fatalf("seed %d: the neighbors of %s are %v, the truth has %v", sw.seed, o.rec.ID, got, want)
			}
		}
	}
}

// truthAround lists the links at an object as the truth has them, in the form the test compares.
func truthAround(o *object) []string {
	var out []string
	for _, e := range o.out {
		if e.rec.NotIngestible == "" && e.to.ok {
			out = append(out, query.DirOut+" "+e.typ.Name+" "+e.to.id.String())
		}
	}
	for _, e := range o.in {
		if e.rec.NotIngestible == "" && e.from.ok {
			out = append(out, query.DirIn+" "+e.typ.Name+" "+e.from.id.String())
		}
	}
	sort.Strings(out)
	return out
}

// [REQ-258]
func TestAReplayGivesTheSameHashesAndVersion(t *testing.T) {
	reg := standard(t)
	g := generate(t, reg, 5, 3000, nil)
	a, b := loadWorld(t, reg, g.truth, 700), loadWorld(t, reg, g.truth, 700)
	if a.report != b.report {
		t.Fatalf("two loads differ:\n%+v\n%+v", a.report, b.report)
	}
	if a.report.VersionID == "" || a.report.GraphHash == "" || a.report.Batches < 3 {
		t.Fatalf("the load should make several batches and a version: %+v", a.report)
	}
	other := loadWorld(t, reg, g.truth, 5000)
	if other.report.GraphHash != a.report.GraphHash {
		t.Fatal("the graph hash depends on how the load was cut into batches")
	}
	if other.report.Batches >= a.report.Batches {
		t.Fatalf("a larger bound should give fewer batches: %d and %d", other.report.Batches, a.report.Batches)
	}
	if other.report.VersionID == a.report.VersionID {
		t.Fatal("the version names the batches it was made from, so a different cut gives another one")
	}
	ha, _ := a.store.Head(context.Background(), a.kg.ID)
	hb, _ := b.store.Head(context.Background(), b.kg.ID)
	if ha.Hash.Sum != hb.Hash.Sum || ha.Hash.Rows != hb.Hash.Rows || ha.InputDigest != hb.InputDigest {
		t.Fatal("the heads of two replays differ")
	}
	c := loadWorld(t, reg, generate(t, reg, 6, 3000, nil).truth, 700)
	if c.report.GraphHash == a.report.GraphHash {
		t.Fatal("two worlds gave one graph hash")
	}
}
