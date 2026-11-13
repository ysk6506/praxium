package worldgen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func mustID[K any](t *testing.T, s string) kernel.ID[K] {
	t.Helper()
	id, err := kernel.ParseID[K](s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// groupRules is the redundancy structure a generated world has per domain:
// a group type, what makes its members and what the consumers use.
var groupRules = []struct {
	domain, group, consumer string
}{
	{"cooling", "ot.cooling_redundancy_group", "lt.cooled_by"},
	{"power", "ot.power_redundancy_group", "lt.powered_by"},
	{"network mlag", "ot.mlag_domain", "lt.uplinked_via"},
	{"network lag", "ot.lag", "lt.uplinked_via"},
	{"network ecmp", "ot.ecmp_group", "lt.uplinked_via"},
	{"network edge pair", "ot.ha_pair", "lt.uplinked_via"},
	{"storage", "ot.ha_pair", "lt.runs_on"},
	{"cluster capacity", "ot.compute_cluster", "lt.realized_by"},
	{"service pool", "ot.lb_pool", "lt.realized_by"},
	{"service database", "ot.database_cluster", "lt.depends_on"},
	{"platform", "ot.control_plane", "lt.runs_on"},
}

// Every redundancy domain has real groups: members that belong to the
// group, and consumers that depend on the group, not on one member.
func TestEveryDomainHasGroupsWithMembersAndConsumers(t *testing.T) {
	data, _ := truthOf(t, smallWorld(t, 3, 40000, func(p *Params) { p.Sites = 2 }))
	typeOf, members, consumers := map[string]string{}, map[string]int{}, map[string]map[string]int{}
	err := ReadTruth(bytes.NewReader(data), func(r *Record) error {
		switch r.Kind {
		case "object":
			typeOf[r.ID] = r.Type
		case "link":
			if r.Type == "lt.member_of" {
				members[r.To]++
			}
			if consumers[r.To] == nil {
				consumers[r.To] = map[string]int{}
			}
			consumers[r.To][r.Type]++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range groupRules {
		found := 0
		for id, typ := range typeOf {
			if typ != rule.group || members[id] == 0 || consumers[id][rule.consumer] == 0 {
				continue
			}
			found++
		}
		if found == 0 {
			t.Errorf("%s: no %s has members and a consumer by %s", rule.domain, rule.group, rule.consumer)
		}
	}
}

func problemsOf(t *testing.T, body string) string {
	t.Helper()
	rep, err := validateText(t, body)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(rep.Problems, "\n")
}

// The rules of the groups each have a failing example.
func TestGroupRulesHaveFailingExamples(t *testing.T) {
	sw := func(id string) string {
		return `{"kind":"object","id":"` + id + `","type":"ot.switch","props":{}}` + "\n"
	}
	grp := func(id string) string {
		return `{"kind":"object","id":"` + id + `","type":"ot.mlag_domain","props":{"min_alive":1,"mode":"active_active"}}` + "\n"
	}
	lnk := func(id, lt, from, to string) string {
		return `{"kind":"link","id":"` + id + `","type":"lt.` + lt + `","from":"` + from + `","to":"` + to + `"}` + "\n"
	}
	members := sw("a") + sw("b") + grp("g") + lnk("m1", "member_of", "a", "g") + lnk("m2", "member_of", "b", "g")
	cases := []struct{ name, body, want string }{
		{"a group nobody depends on", members, "nothing depends on the group"},
		{"a link that goes to one member", members + lnk("c1", "uplinked_via", "a", "b"), "the end is a member of a group"},
		{"two groups with the same members", members + grp("h") + lnk("m3", "member_of", "a", "h") + lnk("m4", "member_of", "b", "h"), "have the same identity key"},
	}
	for _, c := range cases {
		if got := problemsOf(t, c.body); !strings.Contains(got, c.want) {
			t.Errorf("%s: problems %q, want one with %q", c.name, got, c.want)
		}
	}
	ok := members + lnk("c2", "uplinked_via", "a", "g")
	if got := problemsOf(t, ok); strings.Contains(got, "nothing depends") || strings.Contains(got, "member of a group") || strings.Contains(got, "identity key") {
		t.Errorf("a group with a consumer is flagged: %s", got)
	}
}

// An object whose type has no identity key, or whose key the recipe
// cannot form, is flagged as not ingestible. No standard type is keyless now,
// so the rule is shown on types that are made up here.
func TestClassifyFlagsObjectsThatCannotBeKeyed(t *testing.T) {
	setKey := keySpec{comps: []keyComp{{prop: -1, ctx: lMemberOf, set: true, incoming: true, size: 2}}, strong: true}
	cases := []struct {
		name string
		t    *typeSpec
		ctx  uint32
		want string
	}{
		{"no key", &typeSpec{}, 0, "no_identity_key"},
		{"a set the recipe does not promise", &typeSpec{ingest: true, keys: []keySpec{setKey}}, 0, "key_not_formed"},
		{"a set the recipe promises", &typeSpec{ingest: true, keys: []keySpec{setKey}}, 1 << lMemberOf, ""},
		{"a key of a skipped property", &typeSpec{ingest: true, props: []propSpec{{hint: hSkip}}, keys: []keySpec{{comps: []keyComp{{prop: 0, ctx: -1}}}}}, 0, "key_not_formed"},
	}
	for _, c := range cases {
		b := &builder{t: c.t, ctx: c.ctx}
		o := &Object{}
		b.classify(o)
		if o.NI != c.want {
			t.Errorf("%s: not ingestible = %q, want %q", c.name, o.NI, c.want)
		}
	}
}

// A set key reads the links that leave the object or the ones that arrive at it.
func TestSetKeysReadLinksInBothDirections(t *testing.T) {
	ts := &typeSpec{props: []propSpec{{}}, keys: []keySpec{
		{comps: []keyComp{{prop: 0, ctx: -1}, {prop: -1, ctx: lOwnedBy, set: true}, {prop: -1, ctx: lMemberOf, set: true, incoming: true}}},
	}}
	ts.markKeyParts()
	if !ts.props[0].keyPart || ts.setOut != 1<<lOwnedBy || ts.setIn != 1<<lMemberOf {
		t.Fatalf("keyPart %v, setOut %b, setIn %b", ts.props[0].keyPart, ts.setOut, ts.setIn)
	}
	o := &objInfo{}
	o.addSet(lOwnedBy, false, ts.setOut, "t2")
	o.addSet(lOwnedBy, false, ts.setOut, "t1")
	o.addSet(lMemberOf, true, ts.setIn, "x")
	o.addSet(lPartOf, false, ts.setOut, "ignored")
	out, ok := o.setText(keyComp{ctx: lOwnedBy, set: true})
	if !ok || out != "t1,t2" {
		t.Errorf("outgoing set = %q, %v, want the ids sorted", out, ok)
	}
	if _, ok := o.setText(keyComp{ctx: lOwnedBy, set: true, size: 3}); ok {
		t.Error("a set of two formed a key that demands three")
	}
	if got, ok := o.setText(keyComp{ctx: lMemberOf, set: true, incoming: true, size: 1}); !ok || got != "x" {
		t.Errorf("incoming set = %q, %v", got, ok)
	}
}

// A bigger UPS bank is N+1 and still has its panels depend on the bank.
func TestUpsBankWithMoreThanOneUnitIsNPlusOne(t *testing.T) {
	data, _ := truthOf(t, smallWorld(t, 2, 6000, func(p *Params) { p.UpsPerSide = 2 }))
	if !bytes.Contains(data, []byte(`"mode":"n_plus_1"`)) {
		t.Fatal("no n_plus_1 group")
	}
	rep, err := Validate(loadReg(t), bytes.NewReader(data))
	if err != nil || !rep.OK() {
		t.Fatalf("%v %v", err, rep.Problems)
	}
}

// A group that is flagged as not ingestible is not checked as a group.
func TestValidateSkipsGroupsThatAreNotIngestible(t *testing.T) {
	body := `{"kind":"object","id":"g","type":"ot.ha_pair","props":{"min_alive":2,"mode":"quorum"},"not_ingestible":"no_identity_key"}` + "\n"
	if got := problemsOf(t, body); strings.Contains(got, "min_alive") {
		t.Errorf("the group was checked: %s", got)
	}
}

// A key that names something the generator cannot make is left out, not guessed.
func TestKeySpecLeavesOutWhatTheGeneratorCannotForm(t *testing.T) {
	sch, err := newSchemaFor(loadReg(t))
	if err != nil {
		t.Fatal(err)
	}
	ts := sch.types[tRack]
	id := func(s string) model.LinkTypeID { return mustID[model.LinkTypeKind](t, s) }
	cases := map[string]model.IdentityKey{
		"a property the type does not have":     {Components: []model.KeyComponent{{Property: mustID[model.PropertyKind](t, "pr.nothing.nothing")}}},
		"a link type that does not exist":       {Components: []model.KeyComponent{{Context: id("lt.nothing")}}},
		"a link type the generator never makes": {Components: []model.KeyComponent{{Context: id("lt.uses_peer_link")}}},
	}
	for name, k := range cases {
		if _, ok := sch.keySpecOf(ts, k, map[model.PropertyID]string{}); ok {
			t.Errorf("%s: the key was formed", name)
		}
	}
}
