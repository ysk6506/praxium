package worldgen

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func TestSetEveryParameterKind(t *testing.T) {
	p := DefaultParams(1, 10000)
	cases := map[string]string{
		"seed": "42", "scale": "20000", "sites": "3", "host_share": "0.5", "false_completeness": "false",
		"change_events": "10", "late_event_ratio": "0.1", "service_depth": "5",
	}
	for name, v := range cases {
		if err := p.Set(name, v); err != nil {
			t.Fatalf("Set(%s): %v", name, err)
		}
	}
	if p.Seed != 42 || p.Scale != 20000 || p.Sites != 3 || p.HostShare != 0.5 || p.Imperfections.FalseCompleteness || p.Changes.Events != 10 {
		t.Fatalf("values not set: %+v", p)
	}
}

func TestSetRejects(t *testing.T) {
	p := DefaultParams(1, 10000)
	for _, c := range [][2]string{{"nope", "1"}, {"scale", "x"}, {"host_share", "x"}, {"shape_drift", "maybe"}, {"seed", "-1"}} {
		err := p.Set(c[0], c[1])
		if err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Fatalf("Set(%v) = %v", c, err)
		}
	}
}

func TestParamNamesAreSettable(t *testing.T) {
	names := ParamNames()
	if len(names) < 40 {
		t.Fatalf("only %d parameters", len(names))
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Fatalf("duplicate parameter %s", n)
		}
		seen[n] = true
	}
}

func TestValidateRanges(t *testing.T) {
	good := DefaultParams(1, 10000)
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Params){
		"scale":         func(p *Params) { p.Scale = 10 },
		"host_share":    func(p *Params) { p.HostShare = 1.5 },
		"racks_per_row": func(p *Params) { p.RacksPerRow = 0 },
		"late":          func(p *Params) { p.Imperfections.LateEventRatio = 2 },
	} {
		p := good
		mut(&p)
		err := p.Validate()
		if err == nil || !strings.Contains(err.Error(), "outside") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := Build(loadReg(t), Params{Scale: 1}); err == nil {
		t.Fatal("a world with scale 1 was built")
	}
}

func TestNewWorldNeedsRegistry(t *testing.T) {
	if _, err := NewWorld(kernel.NewSeededRng(1), nil, DefaultParams(1, 10000)); err == nil {
		t.Fatal("no registry, no world")
	}
}
