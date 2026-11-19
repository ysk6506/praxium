package ingest

import (
	"context"
	"errors"
	"iter"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// sxTexts is a read port over texts: the text of each dataset, read page by page as a file is.
type sxTexts map[string]string

func (m sxTexts) ReadPages(ctx context.Context, _ string, ds Dataset, opts PageOptions) iter.Seq2[Page, error] {
	text, ok := m[ds.ID]
	if !ok {
		return func(yield func(Page, error) bool) {
			yield(Page{}, kernel.Errorf(kernel.CodeNotFound, "no text for %s", ds.ID))
		}
	}
	return StreamPages(ctx, strings.NewReader(text), ds, opts)
}

func (m sxTexts) Check(ctx context.Context, source string, ds Dataset) CheckResult {
	return CheckFirstPage(ctx, m, source, ds)
}

// sxMemory is a sink that keeps every page it is given, as the memory of a test.
type sxMemory struct {
	pages  []StagePage
	failOn int
}

func (s *sxMemory) Stage(_ context.Context, p StagePage) error {
	if s.failOn > 0 && len(s.pages)+1 == s.failOn {
		return errors.New("the store is full")
	}
	s.pages = append(s.pages, p)
	return nil
}

// sxReceive receives the texts into a memory and returns what was stored and the report.
func sxReceive(t *testing.T, plan *Plan, texts sxTexts, opts PageOptions) (*sxMemory, IntakeReport) {
	t.Helper()
	mem := &sxMemory{}
	rep, err := Intake{Reader: texts, Sink: mem, Plan: plan, Options: opts}.Run(context.Background())
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	return mem, rep
}

func (s *sxMemory) records(dataset string) (out []StageRecord) {
	for _, p := range s.pages {
		if p.Dataset == dataset {
			out = append(out, p.Records...)
		}
	}
	return out
}

func (s *sxMemory) sxProblemsOf(dataset string) (out []StageProblem) {
	for _, p := range s.pages {
		if p.Dataset == dataset {
			out = append(out, p.Problems...)
		}
	}
	return out
}

// sxKeysOf is the keys of the page of a dataset that a feed gave for a record of a page, by the order the feed tries them.
func sxKeysOf(pages []StagePage, dataset string, feed, no int) []StageKey {
	var out []StageKey
	for _, p := range pages {
		if p.Dataset != dataset {
			continue
		}
		for _, k := range p.Keys {
			if k.Feed == feed && k.No == no {
				out = append(out, k)
			}
		}
	}
	return out
}

const (
	sxServersWithTrouble = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\nSV2,,Zorvane,RK2,off\nSV3,sn300,Zorvane,RK1,maybe\nSV4,sn400,Zorvane,,on\n"
)

func sxTextsOfTheRig() sxTexts {
	return sxTexts{"ds_racks": racksV1, "ds_servers": sxServersWithTrouble, "ds_vms": vmsV1}
}

// sxOldRun runs the current flow over the same texts, to compare what it made of them with what the new flow prepares.
func sxOldRun(t *testing.T, mapping string, texts sxTexts) (Report, *State) {
	t.Helper()
	r := newRig(t, sourceJSON, mapping)
	for id, text := range texts {
		r.files[id] = text
	}
	rep := r.run()
	st, err := r.runner.States.Load(context.Background(), "src_ledger")
	if err != nil {
		t.Fatal(err)
	}
	return rep, st
}

func sxScalePlan(t *testing.T, source, mapping string) *Plan {
	t.Helper()
	plan, err := sxCompile(t, source, mapping, true)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return plan
}

func TestPrepareGivesTheRawRecordsTheCurrentFlowKeeps(t *testing.T) {
	texts := sxTextsOfTheRig()
	_, st := sxOldRun(t, mappingJSON, texts)
	mem, _ := sxReceive(t, sxScalePlan(t, sourceJSON, mappingJSON), texts, PageOptions{MaxRows: 1})
	for _, id := range []string{"ds_racks", "ds_servers", "ds_vms"} {
		recs := mem.records(id)
		if len(recs) != len(st.Datasets[id].Open) {
			t.Fatalf("%s: %d records, the current flow keeps %d", id, len(recs), len(st.Datasets[id].Open))
		}
		for _, rec := range recs {
			old := st.Datasets[id].Open[rec.Key]
			if old == nil || old.Hash != rec.RawHash || old.Line != rec.Line || rec.Filtered {
				t.Errorf("%s %s: %+v against %+v", id, rec.Key, rec, old)
			}
		}
	}
	servers := mem.records("ds_servers")
	if !slices.Equal(servers[0].Fields, []string{"SV1", "sn100", "Zorvane", "RK1", "on"}) {
		t.Fatalf("the cells are those of the declared columns in the order of the columns: %q", servers[0].Fields)
	}
}

func TestPrepareGivesTheCandidateIdsTheCurrentFlowGivesTheObjects(t *testing.T) {
	texts := sxTextsOfTheRig()
	_, st := sxOldRun(t, mappingJSON, texts)
	mem, _ := sxReceive(t, sxScalePlan(t, sourceJSON, mappingJSON), texts, PageOptions{})
	cands := map[string]bool{}
	for _, p := range mem.pages {
		for _, k := range p.Keys {
			if k.Cand == "" || k.Ord != 0 || k.LinkRef != "" {
				t.Errorf("a key with no context of a creating feed has the id of its object: %+v", k)
			}
			cands[k.Cand] = true
		}
	}
	if len(cands) != len(st.Objects) {
		t.Fatalf("%d candidates, the current flow made %d objects", len(cands), len(st.Objects))
	}
	for id := range st.Objects {
		if !cands[id] {
			t.Errorf("the object %s of the current flow is no candidate", id)
		}
	}
}

func TestPrepareReportsTheProblemsOfTheRowsWithTheFieldAndTheWordsOfTheCurrentFlow(t *testing.T) {
	texts := sxTextsOfTheRig()
	rep, _ := sxOldRun(t, mappingJSON, texts)
	mem, _ := sxReceive(t, sxScalePlan(t, sourceJSON, mappingJSON), texts, PageOptions{})
	got := mem.sxProblemsOf("ds_servers")
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	want := map[string]string{ProblemMissingIdentity: "serial", ProblemInvalidValue: "power_state"}
	for _, p := range got {
		field, known := want[p.Kind]
		if !known || p.Field != field || p.Feed != "f_server" || p.No < 0 {
			t.Errorf("%+v", p)
		}
		found := false
		for _, o := range rep.Problems {
			if o.Kind == p.Kind && o.Key == p.Key && o.Line == p.Line && o.Reason == p.Reason && o.Dataset == "ds_servers" && o.Feed == p.Feed {
				found = true
			}
		}
		if !found {
			t.Errorf("the current flow reports nothing like %+v among %+v", p, rep.Problems)
		}
	}
	if len(mem.sxProblemsOf("ds_racks"))+len(mem.sxProblemsOf("ds_vms")) != 0 {
		t.Fatal("rows with nothing wrong have no problem")
	}
}

func TestPrepareGivesTheIdentifiersAndTheKeysOfTheEndsOfLinks(t *testing.T) {
	mem, _ := sxReceive(t, sxScalePlan(t, sourceJSON, mappingJSON), sxTextsOfTheRig(), PageOptions{})
	reg := sxRegistry(t)
	scheme, _ := reg.IdentifierSchemeByName("vm_instance_uuid")
	var claims []StageClaim
	var from, to []string
	for _, p := range mem.pages {
		claims = append(claims, p.Claims...)
		for _, e := range p.Ends {
			if e.Def != 0 || e.Held || e.LookupHash == (KeyHash{}) {
				t.Errorf("%+v", e)
			}
			if e.Side == "from" {
				from = append(from, e.Lookup)
			} else {
				to = append(to, e.Lookup)
			}
		}
	}
	if len(claims) != 2 || claims[0].Scheme != scheme.ID.String() || claims[0].Value != "vm-1" || claims[0].ValueHash == claims[1].ValueHash || claims[0].Feed != 2 {
		t.Fatalf("a key that is a scheme claims its identifier: %+v", claims)
	}
	if !slices.Equal(from, []string{"RK1", "RK2", "RK1"}) || !slices.Equal(to, []string{"RK1", "RK2"}) {
		t.Fatalf("a row with no rack has no end: from %q to %q", from, to)
	}
}

const sxIdentifierMapping = `{"feeds":[{"id":"f_rack","dataset":"ds_racks","object_type":"Rack","creates_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack_name"}],
  "identity":[{"components":[{"property":"asset_tag","column":"asset_id"}]}],
  "identifiers":[{"scheme":"host_id","column":"rack_name"}]}]}`

func TestPrepareClaimsTheIdentifierColumnsOfAFeedThatFitTheScheme(t *testing.T) {
	reg := sxRegistry(t)
	scheme, ok := reg.IdentifierSchemeByName("host_id")
	if !ok {
		t.Fatal("the model has no identifier scheme host_id")
	}
	plan := sxScalePlan(t, sourceJSON, sxIdentifierMapping)
	mem, _ := sxReceive(t, plan, sxTexts{"ds_racks": "asset_id,rack_name,total_u\nRK1,host-1,42\nRK2,,45\n"}, PageOptions{})
	claims := mem.pages[0].Claims
	value, err := model.NormalizeValue(scheme.Normalizer, "host-1")
	if err != nil || len(claims) != 1 || claims[0].Scheme != scheme.ID.String() || claims[0].No != 0 || claims[0].Value != value {
		t.Fatalf("an empty cell claims nothing: %+v", claims)
	}
}

func TestPrepareFindsTheObjectOfARowByTheKeysInTheOrderTheModelGivesThem(t *testing.T) {
	reg := sxRegistry(t)
	rackType, _ := reg.ObjectTypeByName("Rack")
	linkType, _ := reg.LinkTypeByName("LOCATED_IN")
	var nameProp, tagProp model.Property
	for _, p := range reg.EffectiveProperties(rackType.ID) {
		switch p.Name {
		case "name":
			nameProp = p
		case "asset_tag":
			tagProp = p
		}
	}
	row := "r1,SITE-A,B1,F1,R101,rack-01,AT-1\n"
	for name, priorities := range map[string][2]int{"context key first": {1, 2}, "asset tag first": {2, 1}} {
		feed := `{"id":"f_rack","dataset":"ds_places","object_type":"Rack","creates_objects":true,"basis":"declared","attributes":[{"attribute":"name","column":"rack"}],
		  "identity":[` + sxWithPriority(sxRackKey(), priorities[0]) + `,` + sxWithPriority(sxKey(sxProp("asset_tag", "tag")), priorities[1]) + `]}`
		plan := sxScalePlan(t, sxPlacesSource, sxMapping(feed, ""))
		mem, _ := sxReceive(t, plan, sxTexts{"ds_places": "row_id,site,building,floor,room,rack,tag\n" + row}, PageOptions{})
		keys := sxKeysOf(mem.pages, "ds_places", 0, 0)
		if len(keys) != 2 {
			t.Fatalf("%s: %+v", name, keys)
		}
		byModel := map[int]StageKey{keys[0].ModelOrd: keys[0], keys[1].ModelOrd: keys[1]}
		ctx, tag := byModel[0], byModel[1]
		if (keys[0].ModelOrd == 0) != (priorities[0] == 1) || keys[0].Ord != 0 || keys[1].Ord != 1 {
			t.Errorf("%s: the feed tries its keys by priority and the model orders them by itself: %+v", name, keys)
		}
		tagValue, err := model.NormalizeValue(tagProp.ValueType, "AT-1")
		if err != nil {
			t.Fatal(err)
		}
		wantID, err := graph.NewObjectID(rackType.ID, []graph.KeyPart{{Kind: graph.PartProperty, Ref: tagProp.ID.String(), Value: tagValue}})
		if err != nil {
			t.Fatal(err)
		}
		if tag.Cand != wantID.String() || tag.LinkRef != "" || tag.TypeNo != rackType.TypeNo || tag.Held {
			t.Errorf("%s: the id of a key with no context is the id of the model: %+v", name, tag)
		}
		nameValue, _ := model.NormalizeValue(nameProp.ValueType, "rack-01")
		wantSig := strings.Join([]string{"context", linkType.ID.String(), "", "property", nameProp.ID.String(), nameValue}, keySep)
		if ctx.Cand != "" || ctx.LinkRef != linkType.ID.String() || ctx.Sig != wantSig || ctx.SigHash == tag.SigHash {
			t.Errorf("%s: a key with a context has no id yet: %+v want sig %q", name, ctx, wantSig)
		}
	}
}

// sxFeedsOfPlaces are feeds for a rack, a building and a site of the same rows, to compare what a child says of its parents
// with what the parents say of themselves.
func sxFeedsOfPlaces() string {
	return sxRackFeed() + "," + sxFeedOf("f_building", "Building", sxBuildingKey()) + "," + sxFeedOf("f_site", "Site", sxSiteKey())
}

// [REQ-639]
func TestPrepareGivesAContextKeyTheKeysOfItsParentsThatTheParentsGiveThemselves(t *testing.T) {
	plan := sxScalePlan(t, sxPlacesSource, sxMapping(sxFeedsOfPlaces(), ""))
	mem, _ := sxReceive(t, plan, sxTexts{"ds_places": "row_id,site,building,floor,room,rack,tag\nr1,SITE-A,B1,F1,R101,rack-01,AT-1\n"}, PageOptions{})
	page := mem.pages[0]
	var racks []StageParent
	for _, p := range page.Parents {
		if p.Feed == 0 {
			racks = append(racks, p)
		}
	}
	if len(racks) != 4 || len(page.Parents) != 5 {
		t.Fatalf("the rack has four parents, the building one: %+v", page.Parents)
	}
	reg := sxRegistry(t)
	wantTypes := []string{"Room", "Floor", "Building", "Site"}
	located, _ := reg.LinkTypeByName("LOCATED_IN")
	linkID := located.ID.String()
	for i, p := range racks {
		ot, _ := reg.ObjectTypeByName(wantTypes[i])
		if p.Level != i+1 || p.TypeNo != ot.TypeNo || p.Feed != 0 || p.Ord != 0 || p.No != 0 || p.LinkRef != linkID || (i == 3 && strings.Contains(p.Sig, "context")) || (i < 3 && !strings.HasPrefix(p.Sig, "context"+keySep)) {
			t.Errorf("level %d: %+v", i+1, p)
		}
	}
	building, site := sxKeysOf([]StagePage{page}, "ds_places", 1, 0)[0], sxKeysOf([]StagePage{page}, "ds_places", 2, 0)[0]
	if racks[2].Sig != building.Sig || racks[2].SigHash != building.SigHash || racks[2].TypeNo != building.TypeNo {
		t.Errorf("the building the rack names is the key of the building: %+v against %+v", racks[2], building)
	}
	if racks[3].Sig != site.Sig || racks[3].SigHash != site.SigHash || site.Cand == "" || building.Cand != "" {
		t.Errorf("the site the rack names is the key of the site, which needs no parent: %+v against %+v", racks[3], site)
	}
	other, _ := sxReceive(t, plan, sxTexts{"ds_places": "row_id,site,building,floor,room,rack,tag\nr1,SITE-A,B2,F1,R101,rack-01,AT-1\n"}, PageOptions{})
	if other.pages[0].Parents[2].SigHash == racks[2].SigHash || other.pages[0].Parents[3].SigHash != racks[3].SigHash {
		t.Fatal("another building is another signature, the same site the same")
	}
}

// [REQ-639]
func TestPrepareNamesTheFieldThatKeepsAContextKeyFromBeingFormed(t *testing.T) {
	plan := sxScalePlan(t, sxPlacesSource, sxMapping(sxRackFeed(), ""))
	head := "row_id,site,building,floor,room,rack,tag\n"
	cases := map[string]string{
		"a parent that has no name":                         "r1,SITE-A,B1,F1,,rack-01,\n",
		"the site, the farthest parent":                     "r1,,B1,F1,R101,rack-01,\n",
		"the object itself":                                 "r1,SITE-A,B1,F1,R101,,\n",
		"the nearest parent first when several are lacking": "r1,,B1,,,rack-01,\n",
	}
	want := map[string]string{"a parent that has no name": "room", "the site, the farthest parent": "site", "the object itself": "rack", "the nearest parent first when several are lacking": "room"}
	for name, row := range cases {
		mem, _ := sxReceive(t, plan, sxTexts{"ds_places": head + row}, PageOptions{})
		probs := mem.sxProblemsOf("ds_places")
		if len(probs) != 1 || probs[0].Kind != ProblemMissingIdentity || probs[0].Field != want[name] || len(mem.pages[0].Keys) != 0 {
			t.Errorf("%s: %+v", name, probs)
		}
	}
	mem, _ := sxReceive(t, plan, sxTexts{"ds_places": head + "r1,SITE-A,B1,F1,,rack-01,AT-1\n"}, PageOptions{})
	keys := mem.pages[0].Keys
	if len(mem.sxProblemsOf("ds_places")) != 0 || len(keys) != 1 || keys[0].LinkRef != "" || len(mem.pages[0].Parents) != 0 {
		t.Fatalf("a row that gives the second key only is identified by it: %+v", mem.pages[0])
	}
}

// [REQ-360]
func TestPrepareMarksTheRowsARefinementFiltersOutAndGivesTheirKeysAsHeldWithTheRewritesAfterTheFilter(t *testing.T) {
	filter := `{"op":"filter","column":"power_state","in":["on"]}`
	rewrite := `{"op":"replace","column":"serial","function":"upper"}`
	mapping := strings.Replace(mappingJSON, `{"feeds":[`, `{"refinements":[{"id":"r_on","input":"ds_servers","ops":[`+filter+`]},{"id":"r_up","input":"r_on","ops":[`+rewrite+`]}],"feeds":[`, 1)
	mapping = strings.Replace(mapping, `"id":"f_server","dataset":"ds_servers"`, `"id":"f_server","dataset":"r_up"`, 1)
	plain := strings.Replace(mapping, `ops":[`+filter+`]`, `ops":[`+strings.Replace(filter, `"in":["on"]`, `"not_in":["never"]`, 1)+`]`, 1)
	texts := sxTexts{"ds_racks": racksV1, "ds_servers": "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\nSV2,sn200,Zorvane,RK2,off\n", "ds_vms": vmsV1}
	rep, st := sxOldRun(t, mapping, texts)
	filtered, _ := sxReceive(t, sxScalePlan(t, sourceJSON, mapping), texts, PageOptions{})
	unfiltered, _ := sxReceive(t, sxScalePlan(t, sourceJSON, plain), texts, PageOptions{})
	recs := filtered.records("ds_servers")
	if recs[0].Filtered || recs[0].FilteredBy != "" || !recs[1].Filtered || recs[1].FilteredBy != "r_on#1" {
		t.Fatalf("the row of SV2 is the one the rule filtered out: %+v", recs)
	}
	if len(st.Datasets["ds_servers"].Filtered) != 1 || rep.Datasets[1].Filtered != 1 {
		t.Fatalf("the current flow filters the same row: %+v", rep.Datasets)
	}
	keys := sxKeysOf(filtered.pages, "ds_servers", 1, 1)
	same := sxKeysOf(unfiltered.pages, "ds_servers", 1, 1)
	if len(keys) != 1 || !keys[0].Held || keys[0].Sig != same[0].Sig || keys[0].Cand != same[0].Cand || sxKeysOf(filtered.pages, "ds_servers", 1, 0)[0].Held {
		t.Fatalf("a filtered row still gives its key, held, the rewrite of the later refinement done: %+v against %+v", keys, same)
	}
	for _, p := range filtered.pages {
		for _, e := range p.Ends {
			if e.Def == 0 && e.Side == "from" && (e.No == 1) != e.Held {
				t.Errorf("an end of a filtered row is held: %+v", e)
			}
		}
	}
	if len(filtered.sxProblemsOf("ds_servers")) != 0 {
		t.Fatalf("a filtered row has no problem: %+v", filtered.sxProblemsOf("ds_servers"))
	}
}

func TestPrepareTellsTheProblemsOfThePageThatAreAboutNoRecord(t *testing.T) {
	ds := `{"id":"src_p","adapter":"file","datasets":[{"id":"ds_p","parser":"csv","natural_key":["id"],"columns":[{"name":"id"},{"name":"name"}]}]}`
	mapping := `{"feeds":[{"id":"f_p","dataset":"ds_p","object_type":"Rack","creates_objects":true,"basis":"declared","attributes":[{"attribute":"name","column":"name"}],"identity":[{"components":[{"property":"asset_tag","column":"id"}]}]}]}`
	plan := sxScalePlan(t, ds, mapping)
	rep := func(text string, opts PageOptions) []StageProblem {
		mem, _ := sxReceive(t, plan, sxTexts{"ds_p": text}, opts)
		var out []StageProblem
		for _, p := range mem.pages {
			out = append(out, p.Problems...)
		}
		return out
	}
	kinds := func(ps []StageProblem) string {
		var k []string
		for _, p := range ps {
			k = append(k, p.Kind)
		}
		return strings.Join(k, ",")
	}
	if got := rep("id,name\n1,a\n,b\n,\n,\n", PageOptions{}); kinds(got) != "unreadable_line,unreadable_line,unreadable_line" || !strings.Contains(got[0].Reason, noKeyHint) || strings.Contains(got[1].Reason, noKeyHint) || got[0].No != -1 || got[0].Line != 3 {
		t.Fatalf("the hint about rows with no key is given once: %+v", got)
	}
	if got := rep("id\n1\n", PageOptions{}); kinds(got) != "shape_changed" || !strings.Contains(got[0].Reason, "the file lacks the columns [name]") {
		t.Fatalf("%+v", got)
	}
	if got := rep("id,name,more,other\n1,a,b,c\n", PageOptions{}); kinds(got) != "undeclared_column,undeclared_column" || !strings.Contains(got[0].Reason, "more") {
		t.Fatalf("%+v", got)
	}
	if got := rep("id,name\n1,a\n2,b\n3,c\n", PageOptions{MaxDatasetRows: 2}); kinds(got) != "incomplete_capture" || !strings.Contains(got[0].Reason, "2 data rows") {
		t.Fatalf("%+v", got)
	}
}

func TestNewPreparerOfADatasetTheSourceLacksIsNotFound(t *testing.T) {
	plan := sxScalePlan(t, sourceJSON, mappingJSON)
	if _, err := NewPreparer(plan, "ds_nothing"); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("%v", err)
	}
}
