package ingest

import (
	"strings"
	"testing"
)

// [REQ-382]
func TestRowsWithoutAKeyAreSkippedSilentlyWhenTheDatasetSaysSo(t *testing.T) {
	src := strings.Replace(sourceJSON, `"id":"ds_racks",`, `"id":"ds_racks","ignore_rows_without_key":true,`, 1)
	r := newRig(t, src, mappingJSON)
	r.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,42\n,,87\nRK2,rack-b,45\n"
	r.files["ds_servers"], r.files["ds_vms"] = serversV1, vmsV1
	rep := r.run()
	racks := datasetReport(rep, "ds_racks")
	if racks.Skipped[SkipNoKey] != 1 || racks.Received != 2 || racks.Unreadable != 0 || !racks.Balanced() {
		t.Fatalf("the row is passed over and no part of the rows received: %+v", racks)
	}
	if rep.ProblemCounts[ProblemUnreadable] != 0 {
		t.Fatalf("no problem: %v", rep.ProblemCounts)
	}
}

// [REQ-382]
func TestWithoutTheOptionAnEmptyKeyIsReportedAndTheProblemNamesTheOption(t *testing.T) {
	r := newRig(t, sourceJSON, mappingJSON)
	r.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,42\n,,87\n,,88\n"
	r.files["ds_servers"], r.files["ds_vms"] = serversV1, vmsV1
	rep := r.run()
	var hinted int
	for _, p := range rep.Problems {
		if p.Kind == ProblemUnreadable && strings.Contains(p.Reason, "ignore_rows_without_key") {
			hinted++
		}
	}
	if rep.ProblemCounts[ProblemUnreadable] != 2 || hinted != 1 {
		t.Fatalf("two problems, the option named once: %v %d", rep.ProblemCounts, hinted)
	}
}

// the workbook rig has a row without an asset number (row 5) and a cell that shows an error (row 3)
// [REQ-383]
func TestACellOfARowThatARuleFiltersOutIsNoProblem(t *testing.T) {
	r, _ := bookRig(t)
	if got := r.run().ProblemCounts[ProblemCellError]; got != 1 {
		t.Fatalf("the error cell of a used row is a problem: %d", got)
	}
	keep := `{"op":"filter","column":"serial","in":["SN1"]}`
	b, _ := bookRig(t)
	useMapping(b, strings.Replace(strings.Replace(xlsxMappingJSON, `{"feeds":[`, `{"refinements":[{"id":"r_servers","input":"ds_servers","ops":[`+keep+`]}],"feeds":[`, 1),
		`"id":"f_server","dataset":"ds_servers"`, `"id":"f_server","dataset":"r_servers"`, 1))
	rep := b.run()
	if rep.ProblemCounts[ProblemCellError] != 0 {
		t.Fatalf("the row is filtered out, so its cell is no problem: %v", rep.ProblemCounts)
	}
	d := datasetReport(rep, "ds_servers")
	if d.Filtered != 1 || !d.Balanced() {
		t.Fatalf("the counts still add up: %+v", d)
	}
}

// [REQ-385]
func TestAWrongTypeNameSuggestsANearOneOrTheCommandThatListsThem(t *testing.T) {
	reg := newRig(t, sourceJSON, mappingJSON).runner.Reg
	src, _ := ParseSource(strings.NewReader(sourceJSON))
	compile := func(from, to string) string {
		m, err := ParseMapping(strings.NewReader(strings.Replace(mappingJSON, from, to, 1)))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Compile(reg, src, m)
		if err == nil {
			t.Fatal("a wrong type name is refused")
		}
		return err.Error()
	}
	if got := compile(`"object_type":"Rack"`, `"object_type":"Rackk"`); !strings.Contains(got, "did you mean Rack") {
		t.Errorf("a near name is suggested: %s", got)
	}
	if got := compile(`"object_type":"Rack"`, `"object_type":"Qzxwv"`); !strings.Contains(got, "praxium types list --all") {
		t.Errorf("a far one names the command: %s", got)
	}
	if got := compile(`"link_type":"MOUNTED_IN"`, `"link_type":"Qzxwv"`); !strings.Contains(got, "praxium types list --kind link --all") {
		t.Errorf("a far link type names the command: %s", got)
	}
}

// [REQ-386]
func TestAMissingLinkTargetNamesTheValueAndTheColumns(t *testing.T) {
	r := newRig(t, sourceJSON, mappingJSON)
	r.files["ds_racks"] = racksV1
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,R-Z99,on\n"
	r.files["ds_vms"] = vmsV1
	rep := r.run()
	for _, p := range rep.Problems {
		if p.Kind == ProblemLinkTarget {
			for _, want := range []string{`"R-Z99"`, "column rack_asset_id", "ds_racks", "column asset_id"} {
				if !strings.Contains(p.Reason, want) {
					t.Errorf("%q is not in %q", want, p.Reason)
				}
			}
			return
		}
	}
	t.Fatalf("no link_target_missing: %v", rep.ProblemCounts)
}

// [REQ-387]
func TestUTF16WithoutAByteOrderMarkIsRecognisedByItsNulBytes(t *testing.T) {
	utf16le := func(s string) []byte {
		var out []byte
		for _, r := range s {
			out = append(out, byte(r), byte(r>>8))
		}
		return out
	}
	text := "asset_id,rack_name\nRK1,rack-a\nRK2,rack-b\n"
	err := CheckEncoding(utf16le(text), ParserCSV)
	if err == nil || !strings.Contains(err.Error(), "UTF-16") || strings.Contains(err.Error(), "EUC-KR") {
		t.Fatalf("a file of UTF-16 text without a mark is UTF-16: %v", err)
	}
	be := make([]byte, 0, 2*len(text))
	for _, r := range text {
		be = append(be, byte(r>>8), byte(r))
	}
	if err := CheckEncoding(be, ParserCSV); err == nil || !strings.Contains(err.Error(), "UTF-16") {
		t.Fatalf("also big-endian: %v", err)
	}
	if err := CheckEncoding([]byte(text), ParserCSV); err != nil {
		t.Fatalf("plain text is fine: %v", err)
	}
	if err := CheckEncoding([]byte("a,b\n\xb0\xa1,\xb3\xaa\n"), ParserCSV); err == nil || !strings.Contains(err.Error(), "EUC-KR") {
		t.Fatalf("a local code page is still guessed: %v", err)
	}
}

// [REQ-389]
func TestAValueOfTheWrongKindIsSaidInPlainWords(t *testing.T) {
	cases := map[string][2]string{
		"a bool given text":   {`"parser":"csv","natural_key":["asset_id"]`, `"parser":"csv","ignore_undeclared_columns":"yes","natural_key":["asset_id"]`},
		"a list given text":   {`"natural_key":["asset_id"]`, `"natural_key":"asset_id"`},
		"text given a number": {`"parser":"csv"`, `"parser":3`},
	}
	want := map[string][]string{
		"a bool given text":   {`"datasets[0].ignore_undeclared_columns"`, "true or false (no quotes)", "the value given is text", "line "},
		"a list given text":   {`"datasets[0].natural_key"`, "a list in [ ]", "the value given is text"},
		"text given a number": {`"datasets[0].parser"`, "text in quotes", "the value given is a number"},
	}
	for name, c := range cases {
		_, err := ParseSource(strings.NewReader(strings.Replace(sourceJSON, c[0], c[1], 1)))
		if err == nil {
			t.Fatalf("%s: a value of the wrong kind is refused", name)
		}
		msg := err.Error()
		if strings.Contains(msg, "cannot unmarshal") || strings.Contains(msg, "Go struct") {
			t.Errorf("%s: no Go in the message: %s", name, msg)
		}
		for _, w := range want[name] {
			if !strings.Contains(msg, w) {
				t.Errorf("%s: %q is not in %q", name, w, msg)
			}
		}
	}
}

// [REQ-390]
func TestCardinalityAndSourceConflictProblemsSayWhereTheRowIs(t *testing.T) {
	src := strings.Replace(sourceJSON, `{"name":"power_state"}]}`, `{"name":"power_state"},{"name":"alt_rack"}]}`, 1)
	mp := strings.Replace(mappingJSON, `"links":[`, `"links":[{"id":"l_alt","link_type":"MOUNTED_IN","from":{"feed":"f_server","columns":["alt_rack"]},"to":{"feed":"f_rack","columns":["asset_id"]},"condition":"same_key"},`, 1)
	r := newRig(t, src, mp)
	r.files["ds_racks"], r.files["ds_vms"] = racksV1, vmsV1
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state,alt_rack\nSV1,sn100,Zorvane,RK1,on,RK2\nSV2,sn200,Zorvane,RK2,off,\n"
	got := problemsOf(r.run(), ProblemCardinality)
	if len(got) != 1 || got[0].Dataset != "ds_servers" || got[0].Key != "SV1" || got[0].Line != 2 {
		t.Fatalf("the problem names the dataset and the row: %+v", got)
	}
	if strings.Contains(got[0].Reason, "lt.") || strings.Contains(got[0].Reason, "ob.") || !strings.Contains(got[0].Reason, "MOUNTED_IN") {
		t.Errorf("the reason names the link type and the objects, not their ids: %s", got[0].Reason)
	}

	p := powerSheetRig(t, ``)
	p.files["ds_power"] = "serial,maker,state\nsn100,Zorvane,off\nsn200,Zorvane,off\n"
	conflicts := problemsOf(p.run(), ProblemSourceConflict)
	if len(conflicts) != 1 || conflicts[0].Dataset == "" || conflicts[0].Line == 0 || conflicts[0].Key == "" {
		t.Fatalf("the conflict names the dataset and the row that lost: %+v", conflicts)
	}
}

// [REQ-391]
func TestAnIdentifierConflictNamesTheSchemeNotItsId(t *testing.T) {
	src := strings.Replace(sourceJSON, `{"name":"vcpu","paths":["cpu"]}`, `{"name":"vcpu","paths":["cpu"]},{"name":"native","paths":["id"]}`, 1)
	mp := strings.Replace(mappingJSON, `"identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}]`, `"identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}],"identifiers":[{"scheme":"vm_native_id","column":"native"}]`, 1)
	r := newRig(t, src, mp)
	r.files["ds_racks"], r.files["ds_servers"] = racksV1, serversV1
	r.files["ds_vms"] = `{"instance_uuid":"vm-1","name":"vm-one","cpu":4,"id":"n-1"}` + "\n" + `{"instance_uuid":"vm-2","name":"vm-two","cpu":2,"id":"n-1"}` + "\n"
	got := problemsOf(r.run(), ProblemIdentityConflict)
	if len(got) != 1 || !strings.Contains(got[0].Reason, "identifier vm_native_id n-1") {
		t.Fatalf("%+v", got)
	}
}

func problemsOf(rep Report, kind string) []Problem {
	var out []Problem
	for _, p := range rep.Problems {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out
}
