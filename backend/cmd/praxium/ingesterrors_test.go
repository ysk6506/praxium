package main

import (
	"strings"
	"testing"
)

// [REQ-400]
// `ingest run` tells every mistake of the definitions, one to a line as its sentence says it, the two documents
// together, and the line after them counts. One mistake is told as it always was, with no count.
func TestIngestRunTellsEveryMistakeOfTheDefinitionsAndCountsThem(t *testing.T) {
	ex := initExamples(t)
	servers := ex + "/csv/servers.csv"
	source := putFile(t, t.TempDir(), "source.json", []byte(`{"id":"s","adapter":"ftp","datasets":[
 {"id":"ds_servers","parser":"csv","natural_key":["nope"],"columns":[{"name":"asset_id"}]}]}`))
	mapping := putFile(t, t.TempDir(), "mapping.json", []byte(`{"feed":[]}`))
	code, _, se := cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping, "-input", "ds_servers="+servers)
	lines := strings.Split(strings.TrimSpace(se), "\n")
	if code != exitFailure || len(lines) != 4 {
		t.Fatalf("%d, %d lines:\n%s", code, len(lines), se)
	}
	for _, want := range []string{`praxium: source s: adapter "ftp" is not one this build has (only file)`, `natural_key names column "nope"`, `"feed" is not a field of the top level`, "praxium: 3 problems found in the definitions"} {
		if !strings.Contains(se, want) {
			t.Errorf("%q is not in:\n%s", want, se)
		}
	}
	one := putFile(t, t.TempDir(), "one.json", []byte(`{"id":"s","adapter":"ftp","datasets":[
 {"id":"ds_servers","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"}]}]}`))
	good := putFile(t, t.TempDir(), "good.json", []byte(`{"feeds":[]}`))
	_, _, se = cmdRun(t, "ingest", "run", "-source", one, "-mapping", good, "-input", "ds_servers="+servers)
	if strings.Count(se, "\n") != 1 || strings.Contains(se, "problems found") {
		t.Fatalf("%s", se)
	}
}

// [REQ-400]
// The candidates of a natural key that was never filled in are put in the sentence of the dataset that has the place
// holder, and the other errors of the list stay as they are.
func TestTheCandidatesOfTheKeyAreFoundInAListOfErrors(t *testing.T) {
	ex := initExamples(t)
	servers := ex + "/csv/servers.csv"
	source := putFile(t, t.TempDir(), "source.json", []byte(`{"id":"s","adapter":"ftp","datasets":[
 {"id":"ds_servers","parser":"csv","natural_key":["FILL IN: the column that tells one row from another"],"columns":[{"name":"asset_id"},{"name":"serial_number"}]}]}`))
	mapping := putFile(t, t.TempDir(), "mapping.json", []byte(`{"feeds":[]}`))
	_, _, se := cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping, "-input", "ds_servers="+servers)
	if !strings.Contains(se, "filled in every row and different in each are: asset_id") || !strings.Contains(se, `adapter "ftp"`) || !strings.Contains(se, "2 problems found") {
		t.Fatalf("%s", se)
	}
}

// [REQ-400]
// A list that was cut says how many are in all, and a mapping that cannot be read does not hide the mistakes of the
// source.
func TestACutListSaysHowManyThereAreAndAnUnreadableMappingDoesNotHideTheSourceErrors(t *testing.T) {
	ex := initExamples(t)
	servers := ex + "/csv/servers.csv"
	cols := strings.TrimSuffix(strings.Repeat(`{"name":""},`, 105), ",")
	source := putFile(t, t.TempDir(), "source.json", []byte(`{"id":"s","adapter":"file","datasets":[{"id":"ds_servers","parser":"csv","natural_key":["a"],"columns":[`+cols+`]}]}`))
	mapping := putFile(t, t.TempDir(), "mapping.json", []byte(`{"feeds":[]}`))
	_, _, se := cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping, "-input", "ds_servers="+servers)
	if !strings.Contains(se, "problems found in the definitions; the first 100 are listed") {
		t.Fatalf("%s", se)
	}
	_, _, se = cmdRun(t, "ingest", "run", "-source", source, "-mapping", mapping+".gone", "-input", "ds_servers="+servers)
	if !strings.Contains(se, "column name") || strings.Contains(se, "cannot read") {
		t.Fatalf("%s", se)
	}
}
