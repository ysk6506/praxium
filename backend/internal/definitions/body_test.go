package definitions

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-332]
func TestNormalizeBodyGivesOneFormToOneContent(t *testing.T) {
	same := [][]string{
		{`{"b":1,"a":[1,2,{"y":true,"x":null}]}`, " {\n \"a\" : [1, 2, {\"x\":null,\"y\":true}],\n \"b\":1 }\n"},
		{`{"n":1}`, `{"n":1.0}`},
		{`{"n":100}`, `{"n":1e2}`},
		{`{"n":0}`, `{"n":-0}`},
		{`{"n":0}`, `{"n":-0.0}`},
		{`{"s":"<&>é"}`, `{"s":"<&>é"}`},
	}
	for _, c := range same {
		a, errA := NormalizeBody([]byte(c[0]))
		b, errB := NormalizeBody([]byte(c[1]))
		if errA != nil || errB != nil || string(a) != string(b) {
			t.Fatalf("%s and %s: %s, %s (%v, %v)", c[0], c[1], a, b, errA, errB)
		}
	}
	got, _ := NormalizeBody([]byte(`{"b":0.5,"a":12345678901234567890,"c":1.5e300,"d":"<&>"}`))
	if string(got) != `{"a":12345678901234567890,"b":0.5,"c":1.5e+300,"d":"<&>"}` {
		t.Fatalf("normal form: %s", got)
	}
}

// [REQ-332]
func TestNormalizeBodyRefusesWhatIsNotOneObject(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxDepth+2) + "1" + strings.Repeat("}", maxDepth+2)
	deepList := `{"a":` + strings.Repeat("[", maxDepth+2) + strings.Repeat("]", maxDepth+2) + "}"
	for name, text := range map[string]string{
		"empty": "", "text": "nope", "list": "[1]", "number": "1", "null": "null",
		"duplicate key": `{"a":1,"a":2}`, "nested duplicate": `{"a":{"b":1,"b":2}}`, "trailing": `{"a":1} {}`,
		"unclosed": `{"a":1`, "unclosed list": `{"a":[1`, "bad key": `{1:2}`, "deep": deep, "deep list": deepList,
		"bad utf8": "{\"a\":\"\xff\"}", "float range": `{"a":1e999}`,
	} {
		if _, err := NormalizeBody([]byte(text)); code(err) != kernel.CodeInvalidValue {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// [REQ-333]
func TestSetHashIsTheBaselineWithoutItemsAndDoesNotDependOnOrder(t *testing.T) {
	if h, err := SetHash("base-hash", nil); err != nil || h != "base-hash" {
		t.Fatalf("no entries: %q %v", h, err)
	}
	one := SetEntry{ItemID: "a", RevNo: 1, ContentHash: "h1"}
	two := SetEntry{ItemID: "b", RevNo: 7, ContentHash: "h2"}
	x, _ := SetHash("base", []SetEntry{one, two})
	y, _ := SetHash("base", []SetEntry{two, one})
	other, _ := SetHash("other", []SetEntry{one, two})
	renumbered := two
	renumbered.RevNo = 1
	z, _ := SetHash("base", []SetEntry{one, renumbered})
	if x != y || x == other || x == "base" || x != z {
		t.Fatalf("hashes: %s %s %s %s", x, y, other, z)
	}
}

// [REQ-336]
func TestBranchChangesHashFollowsTheChangesAndNotTheOrder(t *testing.T) {
	a := BranchEntry{ItemID: "a", Op: OpAdd, ContentHash: "1"}
	b := BranchEntry{ItemID: "b", Op: OpRemove}
	x, _ := BranchChangesHash([]BranchEntry{a, b})
	y, _ := BranchChangesHash([]BranchEntry{b, a})
	none, _ := BranchChangesHash(nil)
	a.ContentHash = "2"
	z, _ := BranchChangesHash([]BranchEntry{a, b})
	if x != y || x == z || none == "" || none == x {
		t.Fatalf("hashes: %s %s %s %q", x, y, z, none)
	}
}
