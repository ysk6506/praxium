package ingest

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// A definition set made up for these tests: two file sources and the mappings that read them.

func dsetSource(id string, datasets ...string) string {
	var ds []string
	for _, d := range datasets {
		ds = append(ds, fmt.Sprintf(`{"id":%q,"parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial_number"},{"name":"manufacturer"}]}`, d))
	}
	return fmt.Sprintf(`{"version":1,"id":%q,"adapter":"file","datasets":[%s]}`, id, strings.Join(ds, ","))
}

// dsetFeed is a feed of physical servers from a dataset.
func dsetFeed(id, dataset string) string {
	return fmt.Sprintf(`{"id":%q,"dataset":%q,"object_type":"PhysicalServer","creates_objects":true,"basis":"declared",
	  "attributes":[{"attribute":"name","column":"asset_id"},{"attribute":"manufacturer","column":"manufacturer"},{"attribute":"serial_number","column":"serial_number"}],
	  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial_number"}],"priority":1}]}`, id, dataset)
}

func dsetMapping(id string, extra string, feeds ...string) string {
	idField := ""
	if id != "" {
		idField = fmt.Sprintf(`"id":%q,`, id)
	}
	return fmt.Sprintf(`{"version":1,%s%s"feeds":[%s]}`, idField, extra, strings.Join(feeds, ","))
}

func dsetSet(t *testing.T, sources []string, mappings []string) DefinitionSet {
	t.Helper()
	var set DefinitionSet
	for _, s := range sources {
		src, err := ParseAdapterSource(strings.NewReader(s))
		if err != nil {
			t.Fatalf("a source of the test does not read: %v", err)
		}
		set.Sources = append(set.Sources, src)
	}
	for _, m := range mappings {
		mp, err := ParseMapping(strings.NewReader(m))
		if err != nil {
			t.Fatalf("a mapping of the test does not read: %v", err)
		}
		set.Mappings = append(set.Mappings, mp)
	}
	return set
}

func dsetRegistry(t *testing.T) *model.Registry {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// dsetPairs are the source and mapping of each plan, in order.
func dsetPairs(plans []*Plan) []string {
	var out []string
	for _, p := range plans {
		out = append(out, p.Source.ID+"/"+p.Mapping.ID)
	}
	return out
}

// [REQ-704]
// A mapping is compiled with the source of its id or, else, with the one source that defines what it reads; the plans
// and the errors are the same whatever the order of the documents.
func TestDsetEachMappingIsCompiledWithItsSourceInAnyOrder(t *testing.T) {
	reg := dsetRegistry(t)
	sources := []string{dsetSource("src_b", "ds_hosts"), dsetSource("src_a", "ds_servers")}
	mappings := []string{dsetMapping("m_hosts", "", dsetFeed("f_hosts", "ds_hosts")), dsetMapping("src_a", "", dsetFeed("f_servers", "ds_servers"))}
	want := []string{"src_b/m_hosts", "src_a/src_a"}
	for round := 0; round < 2; round++ {
		set := dsetSet(t, sources, mappings)
		plans, err := CompileSet(reg, set)
		if err != nil || !slices.Equal(dsetPairs(plans), want) {
			t.Fatalf("round %d: %v %v", round, dsetPairs(plans), err)
		}
		slices.Reverse(sources)
		slices.Reverse(mappings)
	}
	// a mapping with the id of a source that does not define what it reads goes with the source that does
	set := dsetSet(t, []string{dsetSource("src_a", "ds_servers"), dsetSource("src_b", "ds_hosts")},
		[]string{dsetMapping("src_a", "", dsetFeed("f_hosts", "ds_hosts"))})
	if plans, err := CompileSet(reg, set); err != nil || !slices.Equal(dsetPairs(plans), []string{"src_b/src_a"}) {
		t.Fatalf("%v %v", dsetPairs(plans), err)
	}
	// a source with no mapping is no error, and an empty set is no error
	if plans, err := CompileSet(reg, dsetSet(t, sources[:1], nil)); err != nil || len(plans) != 0 {
		t.Fatalf("%v %v", plans, err)
	}
}

// dsetCase is a set that gives one code at one place of one document.
type dsetCase struct {
	name             string
	sources, maps    []string
	code, doc, place string
}

func dsetCases() []dsetCase {
	a, b := dsetSource("src_a", "ds_servers"), dsetSource("src_b", "ds_hosts")
	twin := dsetSource("src_c", "ds_servers")
	return []dsetCase{
		{"no source", []string{b}, []string{dsetMapping("m_x", "", dsetFeed("f_x", "ds_none"))}, "mapping.source.missing", "m_x", ""},
		{"no source at all", nil, []string{dsetMapping("m_x", "", dsetFeed("f_x", "ds_servers"))}, "mapping.source.missing", "m_x", ""},
		{"mixed", []string{a, b}, []string{dsetMapping("m_x", "", dsetFeed("f_x", "ds_servers"), dsetFeed("f_y", "ds_hosts"))}, "mapping.source.mixed", "m_x", ""},
		{"ambiguous", []string{a, twin}, []string{dsetMapping("m_x", "", dsetFeed("f_x", "ds_servers"))}, "mapping.source.ambiguous", "m_x", ""},
		{"feed taken", []string{a}, []string{dsetMapping("m_a", "", dsetFeed("f_x", "ds_servers")), dsetMapping("m_b", "", dsetFeed("f_x", "ds_servers"))}, "mapping.feed.id_taken", "m_b", "/feeds/0/id"},
		{"refinement taken", []string{a}, []string{
			dsetMapping("m_a", `"refinements":[{"id":"r_on","input":"ds_servers","ops":[{"op":"filter","column":"asset_id","not_empty":true}]}],`, dsetFeed("f_a", "r_on")),
			dsetMapping("m_b", `"refinements":[{"id":"r_on","input":"ds_servers","ops":[{"op":"filter","column":"asset_id","not_empty":true}]}],`, dsetFeed("f_b", "r_on"))},
			"mapping.refinement.id_taken", "m_b", "/refinements/0/id"},
		{"link taken", []string{a}, []string{
			dsetMapping("m_a", `"links":[{"id":"l_x","link_type":"no_such","from":{"feed":"f_a","columns":["asset_id"]},"to":{"feed":"f_a","columns":["asset_id"]},"condition":"same_key"}],`, dsetFeed("f_a", "ds_servers")),
			dsetMapping("m_b", `"links":[{"id":"l_x","link_type":"no_such","from":{"feed":"f_b","columns":["asset_id"]},"to":{"feed":"f_b","columns":["asset_id"]},"condition":"same_key"}],`, dsetFeed("f_b", "ds_servers"))},
			"mapping.link.id_taken", "m_b", "/links/0/id"},
		{"no id", []string{a}, []string{dsetMapping("", "", dsetFeed("f_a", "ds_servers"))}, "mapping.id.missing_in_set", "", "/id"},
	}
}

var dsetCodeName = regexp.MustCompile(`^(source|mapping|definition)(\.[a-z]+(_[a-z]+)*)+$`)

// [REQ-704] [REQ-705]
// Every code of the table of the set is given by a case at the place it names, in the document it names; no code is in
// another table, and the sentence names the other mapping.
func TestDsetEveryCodeOfTheSetTableIsGivenAtItsPlace(t *testing.T) {
	reg := dsetRegistry(t)
	table := map[string]ErrorCode{}
	others := append(append(ErrorCodes(), AdapterErrorCodes()...), ScaleErrorCodes()...)
	for _, ec := range SetErrorCodes() {
		if _, dup := table[ec.Code]; dup || !dsetCodeName.MatchString(ec.Code) || !strings.HasSuffix(ec.Meaning, ".") {
			t.Errorf("the row %+v is a duplicate or not a stable name with a one-line meaning", ec)
		}
		if slices.ContainsFunc(others, func(o ErrorCode) bool { return o.Code == ec.Code }) {
			t.Errorf("%s is in another table too", ec.Code)
		}
		table[ec.Code] = ec
	}
	given := map[string]bool{}
	for _, c := range dsetCases() {
		_, err := CompileSet(reg, dsetSet(t, c.sources, c.maps))
		se, ok := SetErrorsOf(err)
		if !ok {
			t.Errorf("%s: no set error: %v", c.name, err)
			continue
		}
		hit := false
		for _, e := range se.List {
			if e.Code == c.code && e.Name == c.doc && e.Pointer == c.place && e.Document == DocumentMapping {
				hit = true
			}
			if row, known := table[e.Code]; known {
				given[e.Code] = true
				deCheckRow(t, c.name, e.DefinitionError, row)
			}
		}
		if !hit {
			t.Errorf("%s: no %s at %s %q: %s", c.name, c.code, c.doc, c.place, err)
		}
		if c.code == "mapping.feed.id_taken" && !strings.Contains(err.Error(), "mapping m_b at /feeds/0/id: feed f_x of mapping m_b is also a feed of mapping m_a") {
			t.Errorf("the sentence names both mappings: %s", err)
		}
	}
	for _, c := range dsetSecretCases() {
		de, ok := DefinitionErrorsOf(SecretValues(c.doc, []byte(c.text)))
		if !ok {
			continue
		}
		for _, e := range de.List {
			if row, known := table[e.Code]; known {
				given[e.Code] = true
				deCheckRow(t, c.name, e, row)
			}
		}
	}
	for code := range table {
		if !given[code] {
			t.Errorf("no case gives %s", code)
		}
	}
}

// [REQ-704]
// The errors of a compile are those of the document they are in, named by its id, and the first is the kernel code of
// the error; a mapping that also has a taken id says both.
func TestDsetTheErrorsOfACompileNameTheirDocument(t *testing.T) {
	reg := dsetRegistry(t)
	set := dsetSet(t, []string{dsetSource("src_a", "ds_servers")}, []string{
		dsetMapping("m_a", "", dsetFeed("f_x", "ds_servers")),
		dsetMapping("m_b", "", dsetFeed("f_x", "ds_servers"), strings.Replace(dsetFeed("f_y", "ds_servers"), "PhysicalServer", "NoSuchType", 1))})
	_, err := CompileSet(reg, set)
	se, ok := SetErrorsOf(err)
	if !ok || len(se.List) != 2 {
		t.Fatalf("%v", err)
	}
	if se.List[0].Code != "mapping.feed.id_taken" || se.List[1].Name != "m_b" || se.List[1].Pointer != "/feeds/1/object_type" {
		t.Errorf("%+v", se.List)
	}
	if u := se.Unwrap(); u == nil || !strings.Contains(u.Error(), "f_x") {
		t.Errorf("unwrap: %v", se.Unwrap())
	}
	if (&SetErrors{}).Unwrap() != nil {
		t.Errorf("an empty list unwraps to something")
	}
	if _, ok := SetErrorsOf(fmt.Errorf("plain")); ok {
		t.Errorf("a plain error is no set error")
	}
	many := &SetErrors{List: se.List[:1], Omitted: 3}
	if !strings.Contains(many.Error(), "3 more problems are left out") {
		t.Errorf("%s", many.Error())
	}
	// the list stops at the most a check lists
	var maps []string
	for i := 0; i < MaxDefinitionErrors+5; i++ {
		maps = append(maps, dsetMapping(fmt.Sprintf("m_%03d", i), "", dsetFeed(fmt.Sprintf("f_%03d", i), "ds_none")))
	}
	_, err = CompileSet(reg, dsetSet(t, nil, maps))
	if se, ok := SetErrorsOf(err); !ok || len(se.List) != MaxDefinitionErrors || se.Omitted != 5 {
		t.Fatalf("%v", err)
	}
}

type dsetSecretCase struct {
	name, doc, text string
	code, place     string
}

// The values in these cases are made up.
func dsetSecretCases() []dsetSecretCase {
	src := func(conn string) string {
		return `{"id":"s","adapter":"http","connection":` + conn + `,"datasets":[]}`
	}
	return []dsetSecretCase{
		{"query in a path", DocumentSource, src(`{"base_url":"https://api.example.test/v1?api_key=zz-fake-111"}`), "definition.secret.name_value", "/connection/base_url"},
		{"sql literal", DocumentSource, `{"datasets":[{"sql":"SELECT a FROM t WHERE password='zz-fake-222'"}]}`, "definition.secret.name_value", "/datasets/0/sql"},
		{"in a mapping", DocumentMapping, `{"feeds":[{"attributes":[{"value":"x&Token=zz-fake-333"}]}]}`, "definition.secret.name_value", "/feeds/0/attributes/0/value"},
		{"query member", DocumentSource, `{"datasets":[{"request":{"query":{"access_token":"zz-fake-444"}}}]}`, "definition.secret.field_value", "/datasets/0/request/query/access_token"},
		{"header member", DocumentSource, src(`{"headers":{"X-Api-Key":"zz-fake-555"}}`), "definition.secret.field_value", "/connection/headers/X-Api-Key"},
		{"authorization", DocumentSource, src(`{"headers":{"Authorization":"Bearer zz-fake-666"}}`), "definition.secret.field_value", "/connection/headers/Authorization"},
		{"account", DocumentSource, src(`{"base_url":"https://svc_user:zz-fake-777@api.example.test/v1"}`), "definition.secret.address_account", "/connection/base_url"},
		{"account without password", DocumentMapping, `{"feeds":[{"id":"postgres://svc_user@db.example.test/x"}]}`, "definition.secret.address_account", "/feeds/0/id"},
		{"private key", DocumentSource, `{"connection":{"ca_pem":"-----BEGIN RSA PRIVATE KEY-----\nzz-fake-888\n-----END RSA PRIVATE KEY-----"}}`, "definition.secret.private_key", "/connection/ca_pem"},
	}
}

// [REQ-705]
// What is certainly a value of a secret is refused at its place and the value is never repeated; references,
// placeholders, names that only hold a word of a secret and a certificate pass.
func TestDsetSecretValuesAreRefusedWithoutTheValue(t *testing.T) {
	for _, c := range dsetSecretCases() {
		err := SecretValues(c.doc, []byte(c.text))
		de, ok := DefinitionErrorsOf(err)
		if !ok || len(de.List) != 1 || de.List[0].Code != c.code || de.List[0].Pointer != c.place || de.List[0].Document != c.doc {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if strings.Contains(err.Error(), "zz-fake") || strings.Contains(fmt.Sprint(de.List[0].Params), "zz-fake") {
			t.Errorf("%s: the value is repeated: %s", c.name, err)
		}
	}
	pass := map[string]string{
		"http":           adpHTTP,
		"postgres":       adpPG,
		"certificate":    `{"connection":{"ca_pem":` + fmt.Sprintf("%q", adpCert) + `}}`,
		"placeholders":   `{"connection":{"auth":{"request":{"body":{"password":"{{credential.password}}","token":""}}}}}`,
		"sql parameters": `{"datasets":[{"sql":"SELECT a FROM t WHERE token=$1 AND secret=? AND password=:pw AND api_key=%s"}]}`,
		"spaced sql":     `{"datasets":[{"sql":"SELECT a FROM t WHERE token = 'x'"}]}`,
		"word inside":    `{"datasets":[{"request":{"query":{"token_path":"token","is_secret":"true"}},"sql":"is_secret=true AND next_token_id=4"}]}`,
		"a reference":    `{"connection":{"credential":{"secret_id":"sec_db"},"token":{"secret_id":"sec_api"}}}`,
		"map of values":  `{"refinements":[{"ops":[{"op":"replace","function":"map","map":{"secret":"confidential"}}]}]}`,
		"plain address":  `{"connection":{"base_url":"https://api.example.test/v1?page=1&mail=ops@example.test"}}`,
		"not json":       `{"id": `,
	}
	for name, text := range pass {
		doc := DocumentSource
		if name == "map of values" {
			doc = DocumentMapping
		}
		if err := SecretValues(doc, []byte(text)); err != nil {
			t.Errorf("%s is refused: %v", name, err)
		}
	}
}
