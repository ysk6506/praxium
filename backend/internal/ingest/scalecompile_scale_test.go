package ingest

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

const sxPlacesSource = `{"id":"src_places","adapter":"file","datasets":[
 {"id":"ds_places","parser":"csv","natural_key":["row_id"],"columns":[{"name":"row_id"},{"name":"site"},{"name":"building"},{"name":"floor"},{"name":"room"},{"name":"rack"},{"name":"tag"}]}
]}`

func sxProp(prop, column string) string {
	return fmt.Sprintf(`{"property":%q,"column":%q}`, prop, column)
}

func sxCtx(link, parentType, parentKey string) string {
	return fmt.Sprintf(`{"context":%q,"parent":{"object_type":%q,"identity":%s}}`, link, parentType, parentKey)
}

func sxKey(components ...string) string {
	return `{"components":[` + strings.Join(components, ",") + `]}`
}

func sxWithPriority(key string, priority int) string {
	return strings.TrimSuffix(key, "}") + fmt.Sprintf(`,"priority":%d}`, priority)
}

// sxSiteKey and the keys after it are the identity keys of a site, a building in it, a floor of the building, a room on the
// floor and a rack in the room: each but the first is unique only inside its parent.
func sxSiteKey() string { return sxKey(sxProp("site_code", "site")) }

func sxBuildingKey() string {
	return sxKey(sxCtx("LOCATED_IN", "Site", sxSiteKey()), sxProp("name", "building"))
}

func sxFloorKey() string {
	return sxKey(sxCtx("LOCATED_IN", "Building", sxBuildingKey()), sxProp("name", "floor"))
}

func sxRoomKey() string {
	return sxKey(sxCtx("LOCATED_IN", "Floor", sxFloorKey()), sxProp("name", "room"))
}

func sxRackKey() string {
	return sxKey(sxCtx("LOCATED_IN", "Room", sxRoomKey()), sxProp("name", "rack"))
}

// sxRackFeed is a feed of racks whose first key is the context key and whose second is the asset tag.
func sxRackFeed() string {
	return `{"id":"f_rack","dataset":"ds_places","object_type":"Rack","creates_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack"},{"attribute":"asset_tag","column":"tag"}],
  "identity":[` + sxWithPriority(sxRackKey(), 1) + `,` + sxWithPriority(sxKey(sxProp("asset_tag", "tag")), 2) + `]}`
}

func sxMapping(feeds string, links string) string {
	if links != "" {
		links = `,"links":[` + links + `]`
	}
	return `{"feeds":[` + feeds + `]` + links + `}`
}

func sxRegistry(t *testing.T) *model.Registry {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func sxCompile(t *testing.T, source, mapping string, scale bool) (*Plan, error) {
	t.Helper()
	src, err := ParseSource(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMapping(strings.NewReader(mapping))
	if err != nil {
		t.Fatal(err)
	}
	if scale {
		return CompileScale(sxRegistry(t), src, m)
	}
	return Compile(sxRegistry(t), src, m)
}

// sxErrors is the codes with the places of the errors of a compile.
func sxErrors(err error) []string {
	list, ok := DefinitionErrorsOf(err)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range list.List {
		out = append(out, e.Code+" "+e.Pointer)
	}
	return out
}

// [REQ-639]
func TestCompileScaleReadsAKeyWithContextsFiveLevelsDeepAndTheCurrentCompileRefusesIt(t *testing.T) {
	text := sxMapping(sxRackFeed(), "")
	if _, err := sxCompile(t, sxPlacesSource, text, false); err == nil {
		t.Fatal("the compile of the current flow accepted a context key")
	}
	plan, err := sxCompile(t, sxPlacesSource, text, true)
	if err != nil {
		t.Fatalf("%v", err)
	}
	keys := plan.feeds[0].keys
	if len(keys) != 2 || keys[0].ctx == nil || keys[1].ctx != nil || keys[0].modelOrd != 0 || keys[1].modelOrd != 1 {
		t.Fatalf("the context key is the first of the model, the asset tag the second: %+v", keys)
	}
	depth := 0
	for c := keys[0].ctx; c != nil; c = c.key.ctx {
		depth++
	}
	if depth != 4 || keys[0].ctx.parent.Name != "Room" || keys[0].ctx.link.Name != "LOCATED_IN" || len(keys[0].parts) != 1 {
		t.Fatalf("the parents are Room, Floor, Building and Site: %d levels, %+v", depth, keys[0].ctx.parent.Name)
	}
}

// [REQ-639]
func TestCompileScaleGivesTheSamePlanForADefinitionTheCurrentCompileAccepts(t *testing.T) {
	old, err := sxCompile(t, sourceJSON, mappingJSON, false)
	if err != nil {
		t.Fatal(err)
	}
	scale, err := sxCompile(t, sourceJSON, mappingJSON, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.feeds, scale.feeds) || !reflect.DeepEqual(old.links, scale.links) || !reflect.DeepEqual(old.refs, scale.refs) {
		t.Fatal("the plans differ")
	}
	for _, f := range scale.feeds {
		for i, k := range f.keys {
			if k.ctx != nil || k.modelOrd < 0 || (i > 0 && k.priority < f.keys[i-1].priority) {
				t.Errorf("%+v", k)
			}
		}
	}
}

// [REQ-639]
func TestMappingWithoutContextFieldsReadsAndWritesAsBefore(t *testing.T) {
	m, err := ParseMapping(strings.NewReader(mappingJSON))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(m)
	if err != nil || strings.Contains(string(out), "context") || strings.Contains(string(out), "parent") {
		t.Fatalf("a definition without a context writes none: %s %v", out, err)
	}
	withContext, err := ParseMapping(strings.NewReader(sxMapping(sxRackFeed(), "")))
	if err != nil || withContext.Feeds[0].Identity[0].Components[0].Parent.ObjectType != "Room" || withContext.Feeds[0].Identity[0].Components[0].Context != "LOCATED_IN" {
		t.Fatalf("%+v %v", withContext, err)
	}
	if _, err := ParseMapping(strings.NewReader(strings.Replace(sxMapping(sxRackFeed(), ""), `"parent":{"object_type"`, `"parent":{"object_typo"`, 1))); err == nil {
		t.Fatal("a field of the parent that is not one is accepted")
	}
}

// sxWrongKey is a feed of the given type whose identity is the key.
func sxFeedOf(id, typ, key string) string {
	return `{"id":"` + id + `","dataset":"ds_places","object_type":"` + typ + `","creates_objects":true,"basis":"declared","attributes":[{"attribute":"name","column":"rack"}],"identity":[` + key + `]}`
}

// [REQ-639]
func TestCompileScaleRefusesWhatIsNotAContextKeyOfTheModel(t *testing.T) {
	floorsInFloors := sxKey(sxProp("name", "floor"))
	for i := 0; i < 5; i++ {
		floorsInFloors = sxKey(sxCtx("LOCATED_IN", "Floor", floorsInFloors), sxProp("name", "floor"))
	}
	cases := []struct{ name, mapping, want string }{
		{"a context that is not first", sxFeedOf("f", "Rack", sxKey(sxProp("name", "rack"), sxCtx("LOCATED_IN", "Room", sxRoomKey()))), "mapping.identity.context_component_invalid /feeds/0/identity/0/components/1"},
		{"two contexts", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_IN", "Room", sxRoomKey()), sxCtx("LOCATED_IN", "Room", sxRoomKey()))), "mapping.identity.context_component_invalid /feeds/0/identity/0/components/1"},
		{"a context with no parent", sxFeedOf("f", "Rack", sxKey(`{"context":"LOCATED_IN"}`, sxProp("name", "rack"))), "mapping.identity.context_component_invalid /feeds/0/identity/0/components/0"},
		{"a parent with no context", sxFeedOf("f", "Rack", sxKey(`{"parent":{"object_type":"Room","identity":`+sxRoomKey()+`}}`, sxProp("name", "rack"))), "mapping.identity.context_component_invalid /feeds/0/identity/0/components/0"},
		{"a context that also reads a column", sxFeedOf("f", "Rack", sxKey(strings.Replace(sxCtx("LOCATED_IN", "Room", sxRoomKey()), `{"context"`, `{"column":"room","context"`, 1), sxProp("name", "rack"))), "mapping.identity.context_component_invalid /feeds/0/identity/0/components/0"},
		{"an unknown link type", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_INN", "Room", sxRoomKey()), sxProp("name", "rack"))), "mapping.identity.context_unknown_link_type /feeds/0/identity/0/components/0/context"},
		{"an unknown parent type", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_IN", "Roomm", sxRoomKey()), sxProp("name", "rack"))), "mapping.identity.context_unknown_parent_type /feeds/0/identity/0/components/0/parent/object_type"},
		{"a link the model does not allow to that parent", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_IN", "Region", sxKey(sxProp("code", "site"))), sxProp("name", "rack"))), "mapping.identity.context_link_not_allowed /feeds/0/identity/0/components/0/context"},
		{"a parent key the parent type does not have", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_IN", "Room", sxKey(sxProp("name", "room"))), sxProp("name", "rack"))), "mapping.identity.no_such_key /feeds/0/identity/0/components/0/parent/identity/components"},
		{"a parent key of properties the parent does not have", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_IN", "Room", sxKey(sxProp("site_code", "site"))), sxProp("name", "rack"))), "mapping.identity.unknown_property /feeds/0/identity/0/components/0/parent/identity/components/0/property"},
		{"a column of the parent the dataset lacks", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_IN", "Room", sxKey(sxCtx("LOCATED_IN", "Floor", sxFloorKey()), sxProp("name", "roomm"))), sxProp("name", "rack"))), "mapping.identity.component_unknown_column /feeds/0/identity/0/components/0/parent/identity/components/1/column"},
		{"a property the type does not have", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_IN", "Room", sxRoomKey()), sxProp("nam", "rack"))), "mapping.identity.unknown_property /feeds/0/identity/0/components/1/property"},
		{"a column of the key the dataset lacks", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_IN", "Room", sxRoomKey()), sxProp("name", "rackk"))), "mapping.identity.component_unknown_column /feeds/0/identity/0/components/1/column"},
		{"properties the context key does not have", sxFeedOf("f", "Rack", sxKey(sxCtx("LOCATED_IN", "Room", sxRoomKey()), sxProp("asset_tag", "tag"))), "mapping.identity.no_such_key /feeds/0/identity/0/components"},
		{"parents nested too deep", sxFeedOf("f", "Floor", floorsInFloors), "mapping.identity.context_too_deep /feeds/0/identity/0/components/0/parent/identity/components/0/parent/identity/components/0/parent/identity/components/0/parent/identity/components/0/parent"},
		{"a type that is identified by a set", sxFeedOf("f", "MlagDomain", sxKey(sxProp("name", "rack"))), "mapping.identity.set_key_unsupported /feeds/0/identity/0/components"},
		{"a key with a context for a type that is identified by a set", sxFeedOf("f", "MlagDomain", sxKey(sxCtx("PART_OF", "Chassis", sxKey(sxProp("manufacturer", "site"), sxProp("serial_number", "building"))), sxProp("name", "rack"))), "mapping.identity.set_key_unsupported /feeds/0/identity/0/components"},
	}
	for _, c := range cases {
		_, err := sxCompile(t, sxPlacesSource, sxMapping(c.mapping, ""), true)
		got := sxErrors(err)
		if !strings.Contains(strings.Join(got, "\n"), c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// [REQ-639]
func TestCompileScaleRefusesALinkDefinitionThatMakesTheLinkOfTheContext(t *testing.T) {
	site := sxFeedOf("f_site", "Site", sxSiteKey())
	building := sxFeedOf("f_building", "Building", sxBuildingKey())
	link := func(id, linkType, from string) string {
		return `{"id":"` + id + `","link_type":"` + linkType + `","from":{"feed":"` + from + `","columns":["site"]},"to":{"feed":"f_site","columns":["site"]},"condition":"same_key","basis":"declared"}`
	}
	_, err := sxCompile(t, sxPlacesSource, sxMapping(site+","+building, link("l_in", "LOCATED_IN", "f_building")), true)
	want := "mapping.link.duplicates_context /links/0/link_type"
	if got := sxErrors(err); !reflect.DeepEqual(got, []string{want}) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if _, err := sxCompile(t, sxPlacesSource, sxMapping(site+","+building, link("l_part", "PART_OF", "f_building")), true); err == nil || strings.Contains(strings.Join(sxErrors(err), ""), "duplicates_context") {
		t.Fatalf("a link of another type is no duplicate; got %v", sxErrors(err))
	}
	if _, err := sxCompile(t, sxPlacesSource, sxMapping(site+","+sxFeedOf("f_other", "Site", sxSiteKey()), link("l_in", "LOCATED_IN", "f_other")), true); err != nil {
		t.Fatalf("a feed with no context may have the link: %v", err)
	}
}

// [REQ-639]
func TestScaleErrorCodesAreTheTableOfTheCodesOnlyTheNewCompileGives(t *testing.T) {
	codes := ScaleErrorCodes()
	if len(codes) != 7 {
		t.Fatalf("%d codes", len(codes))
	}
	known := map[string]bool{}
	for _, c := range ErrorCodes() {
		known[c.Code] = true
	}
	for i, c := range codes {
		if known[c.Code] || !strings.HasPrefix(c.Code, "mapping.") || !strings.HasSuffix(c.Meaning, ".") || (i > 0 && codes[i-1].Code >= c.Code) || c.Document != "mapping" {
			t.Errorf("the row of %s is not a code of its own with a sentence of meaning: %+v", c.Code, c)
		}
	}
}
