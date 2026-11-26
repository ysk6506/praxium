package graph

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The stored forms again (ADR-0119): the review found that nothing told a key whose objects are not sorted from one whose objects are,
// and that nothing but the best grade was tried for the lowest grade of the attributes of a row.

// [REQ-975]
func TestRcpTheObjectsOfAContextPartOfAFirstKeyAreStoredInTheOrderOfTheirIdsWhateverTheOrderTheyAreGivenIn(t *testing.T) {
	a, err := kernel.ParseID[ObjectKind]("obj-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := kernel.ParseID[ObjectKind]("obj-b")
	if err != nil {
		t.Fatal(err)
	}
	c, err := kernel.ParseID[ObjectKind]("obj-c")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, objects := range [][]ObjectID{{c, a, b}, {b, c, a}, {a, b, c}} {
		stored, err := EncodeKey([]KeyPart{{Kind: PartContext, Ref: "LOCATED_IN", Objects: objects}})
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, stored.(string))
	}
	if texts[0] != texts[1] || texts[1] != texts[2] {
		t.Errorf("the same objects are stored as %v", texts)
	}
	if !strings.Contains(texts[0], `"obj-a","obj-b","obj-c"`) {
		t.Errorf("the objects are not in the order of their ids: %s", texts[0])
	}
}

// [REQ-975]
func TestRcpTheLowestGradeOfTheAttributesOfARowIsTheLowestOfThemAndNotTheBest(t *testing.T) {
	grades := model.Grades()
	if len(grades) < 3 {
		t.Fatalf("the grades are %v", grades)
	}
	for name, attrs := range map[string]map[int]Attr{
		"one that is not the best": {1: {Value: "x", Grade: grades[1]}},
		"the best and the lowest":  {1: {Value: "x", Grade: grades[0]}, 2: {Value: "y", Grade: grades[len(grades)-1]}, 3: {Value: "z", Grade: grades[1]}},
	} {
		want := grades[1]
		if name == "the best and the lowest" {
			want = grades[len(grades)-1]
		}
		if got := MinGrade(attrs); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
}
