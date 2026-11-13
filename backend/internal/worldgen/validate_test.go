package worldgen

import (
	"strings"
	"testing"
)

const hdr = `{"kind":"header","format":"praxium-worldgen-truth","version":1}` + "\n"

func validateText(t *testing.T, body string) (*Report, error) {
	t.Helper()
	return Validate(loadReg(t), strings.NewReader(hdr+body))
}

// The validator is the check of the truth format; each flaw it knows is
// found when it is planted.
func TestValidateFindsPlantedFlaws(t *testing.T) {
	site := `{"kind":"object","id":"site.0","type":"ot.site","props":{"name":"s","site_code":"c1","lifecycle_state":"active"},"grade":"fact","basis":"declared"}` + "\n"
	cases := map[string]string{
		"unknown type":        `{"kind":"object","id":"x","type":"ot.nope","props":{}}` + "\n",
		"duplicate id":        site + site,
		"unknown property":    `{"kind":"object","id":"x","type":"ot.site","props":{"nope":1}}` + "\n",
		"missing required":    `{"kind":"object","id":"x","type":"ot.site","props":{"name":"a"}}` + "\n",
		"bad enum":            `{"kind":"object","id":"x","type":"ot.site","props":{"name":"a","site_code":"c","lifecycle_state":"zzz"}}` + "\n",
		"bad integer":         `{"kind":"object","id":"x","type":"ot.site","props":{"name":"a","site_code":"c","lifecycle_state":"active","utility_count":"many"}}` + "\n",
		"bad boolean":         `{"kind":"object","id":"y","type":"ot.virtual_machine","props":{"ha_protected":"maybe"}}` + "\n",
		"bad normal form":     `{"kind":"object","id":"y","type":"ot.network_adapter","props":{}}` + "\n" + `{"kind":"object","id":"z","type":"ot.interface","props":{"mac_address":"AA:BB:CC:DD:EE:FF"}}` + "\n",
		"unknown link type":   site + `{"kind":"link","id":"l","type":"lt.nope","from":"site.0","to":"site.0"}` + "\n",
		"missing endpoint":    site + `{"kind":"link","id":"l","type":"lt.located_in","from":"site.0","to":"gone"}` + "\n",
		"no binding":          site + `{"kind":"link","id":"l","type":"lt.runs_on","from":"site.0","to":"site.0"}` + "\n",
		"duplicate link":      site + `{"kind":"link","id":"l","type":"lt.located_in","from":"site.0","to":"site.0"}` + "\n" + `{"kind":"link","id":"l","type":"lt.located_in","from":"site.0","to":"site.0"}` + "\n",
		"link property":       site + `{"kind":"link","id":"l","type":"lt.located_in","from":"site.0","to":"site.0","props":{"nope":1}}` + "\n",
		"group too small":     `{"kind":"object","id":"g","type":"ot.ha_pair","props":{"min_alive":2,"mode":"quorum"}}` + "\n",
		"same identity key":   site + strings.Replace(site, `"id":"site.0"`, `"id":"site.1"`, 1),
		"unknown record kind": `{"kind":"thing"}` + "\n",
	}
	for name, body := range cases {
		rep, err := validateText(t, body)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if rep.OK() {
			t.Errorf("%s: not found", name)
		}
	}
}

func TestValidateReportsAtMostFiftyProblems(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 80; i++ {
		b.WriteString(`{"kind":"object","id":"x` + itoa(i) + `","type":"ot.nope"}` + "\n")
	}
	rep, _ := validateText(t, b.String())
	if len(rep.Problems) != maxProblems || rep.Dropped != 30 {
		t.Fatalf("%d problems, %d dropped", len(rep.Problems), rep.Dropped)
	}
}

func TestValidateRejectsBadFiles(t *testing.T) {
	reg := loadReg(t)
	for name, text := range map[string]string{
		"no header":     `{"kind":"object"}` + "\n",
		"wrong version": `{"kind":"header","format":"praxium-worldgen-truth","version":9}` + "\n",
		"wrong format":  `{"kind":"header","format":"other","version":1}` + "\n",
		"not json":      "hello\n",
		"broken record": hdr + "{oops\n",
	} {
		if _, err := Validate(reg, strings.NewReader(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Validate(nil, strings.NewReader(hdr)); err == nil {
		t.Error("no registry")
	}
}
