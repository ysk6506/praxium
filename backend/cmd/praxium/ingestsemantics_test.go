package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// [REQ-358] [REQ-360]
func TestIngestRunShowsTheRowsThatOnlyLostACellAndTheObjectsThatWait(t *testing.T) {
	dir := t.TempDir()
	servers := filepath.Join(dir, "servers.csv")
	data := "asset_id,serial_number,manufacturer,rack_asset_id,power_state\nSV-1,SN-1,Zorvane,RK-1001,sleeping\nSV-2,SN-9,Zorvane,RK-1001,on\n"
	if err := os.WriteFile(servers, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.open = systemOpen
	var out, errOut bytes.Buffer
	args := []string{"ingest", "run", "-source", sampleDir + "source.json", "-mapping", sampleDir + "mapping.json",
		"-input", "ds_racks=" + sampleDir + "sample/racks.csv", "-input", "ds_servers=" + servers}
	if code := execute(args, &out, &errOut, d); code != exitOK {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	for _, want := range []string{"waiting_objects=0", "dataset ds_servers collected=true received=2 unreadable=0 read=2 used=2 filtered=0 problem=0 unused=0 used_with_problem=1 cells_left_out=1",
		"problems invalid_value=1", "problem invalid_value dataset=ds_servers key=SV-1", "the object is made without this value"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "link_target_missing") || strings.Contains(out.String(), "missing_identity") {
		t.Errorf("a wrong status cost a row:\n%s", out.String())
	}
}
