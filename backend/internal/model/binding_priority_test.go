package model_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

const bindingWinnersFile = "testdata/binding_winners.txt"

// bindingWinners lists every (link type, start type, end type) that more than one binding fits, with the binding
// the old rule (the lowest id) took and the one the priority takes now, and gives the number of such triples. To
// make the file again (only when a decision changes the model, never to pass a check):
//
//	cd backend && go test ./internal/model -run TestStandardWinnersThatChangedAreKept -update-binding-answers
func bindingWinners(r *model.Registry) (string, int) {
	var lines []string
	multi := 0
	for _, from := range r.ObjectTypes() {
		for _, to := range r.ObjectTypes() {
			byLink := map[model.LinkTypeID][]model.BindingID{}
			for _, bl := range r.BindingsFor(from.ID, to.ID) {
				byLink[bl.LinkType] = append(byLink[bl.LinkType], bl.Binding.ID)
			}
			for lt, ids := range byLink {
				if len(ids) < 2 {
					continue
				}
				multi++
				oldWinner := ids[0]
				for _, id := range ids {
					if id.String() < oldWinner.String() {
						oldWinner = id
					}
				}
				b, _ := r.BindingFor(from.ID, to.ID, lt)
				if b.ID != oldWinner {
					l, _ := r.LinkType(lt)
					lines = append(lines, fmt.Sprintf("%s %s -> %s: %s, was %s", l.Name, from.Name, to.Name, b.ID, oldWinner))
				}
			}
		}
	}
	sort.Strings(lines)
	return fmt.Sprintf("pairs with more than one binding: %d\nwinner changed: %d\n%s\n", multi, len(lines), strings.Join(lines, "\n")), multi
}

// [REQ-421]
func TestStandardWinnersThatChangedAreKept(t *testing.T) {
	// The priority changes which binding wins for a known set of pairs, and only for them.
	got, _ := bindingWinners(loadStd(t))
	if *updateBindingAnswers {
		if err := os.WriteFile(bindingWinnersFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(bindingWinnersFile)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("the winners differ from %s:\n%s", bindingWinnersFile, got)
	}
}
