package model_test

import (
	"flag"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// The answers of the standard model: for every (link type, start type, end type) the link may join, the cardinality
// and the parameters the impact query reads. The table is kept in testdata/binding_answers.txt. To make it again
// (only when a decision changes an answer, never to pass a check):
//
//	cd backend && go test ./internal/model -run TestStandardBindingAnswersAreKept -update-binding-answers
var updateBindingAnswers = flag.Bool("update-binding-answers", false, "write testdata/binding_answers.txt")

const bindingAnswersFile = "testdata/binding_answers.txt"

// answerTail is the part of a row that does not name the two types.
func answerTail(r *model.Registry, lt model.LinkType, from, to model.ObjectTypeID) (string, bool) {
	b, ok := r.BindingFor(from, to, lt.ID)
	if !ok {
		return "", false
	}
	p := paramsForAnswers(lt, b)
	return strings.Join([]string{string(b.Cardinality), "strength=" + p.strength, "whole=" + p.whole, "resource=" + p.resource,
		"failover=" + strings.Join(p.failover, ",")}, " "), true
}

type answerParams struct {
	strength, whole, resource string
	failover                  []string
}

// paramsForAnswers reads the parameters the way the impact query read them before the order of the values was
// made one function: the link type fixes some, a single allowed value is taken, several allowed values of
// whole_needs_part are read as soft.
func paramsForAnswers(lt model.LinkType, b model.Binding) answerParams {
	c := b.ParamConstraints
	strength := lt.MeaningParams["strength"]
	if strength == "" && len(c["strength"]) == 1 {
		strength = c["strength"][0]
	}
	if strength != "soft" {
		strength = "hard"
	}
	whole := lt.MeaningParams["whole_needs_part"]
	if whole == "" {
		switch len(c["whole_needs_part"]) {
		case 1:
			whole = c["whole_needs_part"][0]
		case 0:
		default:
			whole = "soft"
		}
	}
	if whole == "" {
		whole = "none"
	}
	return answerParams{strength: strength, whole: whole, resource: lt.MeaningParams["resource"], failover: c["failover_to"]}
}

// bindingAnswers lists the table: one line per link type, start type and row tail, with the end types in a row.
func bindingAnswers(r *model.Registry) string {
	var lines []string
	for _, lt := range r.LinkTypes() {
		for _, from := range r.ObjectTypes() {
			byTail := map[string][]string{}
			for _, to := range r.ObjectTypes() {
				if tail, ok := answerTail(r, lt, from.ID, to.ID); ok {
					byTail[tail] = append(byTail[tail], to.Name)
				}
			}
			for tail, tos := range byTail {
				sort.Strings(tos)
				lines = append(lines, lt.Name+" "+from.Name+" | "+tail+" | "+strings.Join(tos, " "))
			}
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// [REQ-421]
func TestStandardBindingAnswersAreKept(t *testing.T) {
	// The impact answers do not change when the winning binding does: the cardinality and the parameters of every
	// allowed pair of types are the ones in the table.
	got := bindingAnswers(loadStd(t))
	if *updateBindingAnswers {
		if err := os.WriteFile(bindingAnswersFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(bindingAnswersFile)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		inGot, inWant := map[string]bool{}, map[string]bool{}
		for _, l := range gl {
			inGot[l] = true
		}
		for _, l := range wl {
			inWant[l] = true
		}
		shown := 0
		for _, l := range gl {
			if !inWant[l] && shown < 10 {
				t.Errorf("new row: %s", l)
				shown++
			}
		}
		for _, l := range wl {
			if !inGot[l] && shown < 20 {
				t.Errorf("lost row: %s", l)
				shown++
			}
		}
		t.Fatalf("the answers of the standard model differ from %s", bindingAnswersFile)
	}
}
