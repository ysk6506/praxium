package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/definitionsfile"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-367] [REQ-368]
func TestModelCommandsAnswerInJSON(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	file := writeModelFile(t, orgModel)
	var applied struct {
		Applied bool   `json:"applied"`
		Set     int64  `json:"set"`
		Hash    string `json:"hash"`
		Added   int    `json:"added"`
	}
	_, out, errOut := modelCmd(t, d, "apply", "-file", file, "-data-dir", dir, "--json")
	if err := json.Unmarshal([]byte(out), &applied); err != nil || !applied.Applied || applied.Set != 1 || applied.Added != 2 || len(applied.Hash) != 64 {
		t.Fatalf("apply as JSON: %v %s%s", err, out, errOut)
	}
	_, out, _ = modelCmd(t, d, "apply", "-file", file, "-data-dir", dir, "--json")
	if err := json.Unmarshal([]byte(out), &applied); err != nil || applied.Applied || applied.Set != 1 {
		t.Fatalf("a second apply as JSON says nothing was applied: %v %s", err, out)
	}
	var shown struct {
		Set   int64 `json:"set"`
		Model struct {
			Overlays []struct {
				Type string `json:"type"`
			} `json:"overlays"`
		} `json:"model"`
	}
	_, out, _ = modelCmd(t, d, "show", "-data-dir", dir, "--json")
	if err := json.Unmarshal([]byte(out), &shown); err != nil || shown.Set != 1 || len(shown.Model.Overlays) != 1 || shown.Model.Overlays[0].Type != "PhysicalServer" {
		t.Fatalf("show as JSON: %v %s", err, out)
	}
	bad := writeModelFile(t, strings.Replace(orgModel, `"org.introduced_year"`, `"introduced_year"`, 1))
	var rep struct {
		OK     bool `json:"ok"`
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	code, out, _ := modelCmd(t, d, "check", "-file", bad, "-data-dir", dir, "--json")
	if err := json.Unmarshal([]byte(out), &rep); err != nil || code != exitFailure || rep.OK || len(rep.Errors) == 0 || rep.Errors[0].Code == "" {
		t.Fatalf("check as JSON: %d %v %s", code, err, out)
	}
	code, out, _ = modelCmd(t, d, "apply", "-file", bad, "-data-dir", dir, "--json")
	if code != exitFailure || !strings.Contains(out, `"ok": false`) {
		t.Fatalf("a refused apply as JSON gives the report: %d %s", code, out)
	}
}

// [REQ-367]
func TestModelAddsToATraitAndWarnsOfASimilarStandardProperty(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	file := writeModelFile(t, `{"format":"praxium-org-model","version":1,"overlays":[
	  {"trait":"Locatable","properties":[{"name":"org.cost_center","value_type":"string"}]},
	  {"type":"PhysicalServer","properties":[{"name":"org.hostname","value_type":"string"}]}]}`)
	code, out, errOut := modelCmd(t, d, "apply", "-file", file, "-data-dir", dir)
	if code != exitOK || !strings.Contains(out, "stored D-1") {
		t.Fatalf("a trait and a type: %d %s%s", code, out, errOut)
	}
	_, out, _ = modelCmd(t, d, "check", "-file", file, "-data-dir", dir)
	if !strings.Contains(out, "warning  the property org.hostname looks like the standard property hostname") {
		t.Fatalf("a look-alike of a standard property is a warning: %s", out)
	}
	if _, out, _ = modelCmd(t, d, "show", "-data-dir", dir); !strings.Contains(out, "Locatable") || !strings.Contains(out, "org.cost_center") {
		t.Fatalf("the trait is shown by name: %s", out)
	}
	// An overlay the file leaves out is a property gone, for every property of it.
	only := writeModelFile(t, `{"format":"praxium-org-model","version":1,"overlays":[{"type":"PhysicalServer","properties":[{"name":"org.hostname","value_type":"string"}]}]}`)
	code, out, _ = modelCmd(t, d, "apply", "-file", only, "-data-dir", dir)
	if code != exitFailure || !strings.Contains(out, "org.cost_center is gone") || !strings.Contains(out, "Locatable") {
		t.Fatalf("leaving an overlay out is a breaking change: %d %s", code, out)
	}
}

// [REQ-366]
func TestModelCommandsSayWhyAFileOrAFolderCannotBeRead(t *testing.T) {
	d := modelWiring(t)
	dir, _ := appliedFolder(t, d)
	for name, args := range map[string][]string{
		"check, no such file":    {"check", "-file", filepath.Join(dir, "nope.json")},
		"apply, no such file":    {"apply", "-file", filepath.Join(dir, "nope.json"), "-data-dir", dir},
		"check, not a model":     {"check", "-file", writeModelFile(t, `{"format":"other","version":1,"overlays":[]}`)},
		"check, a trailing text": {"check", "-file", writeModelFile(t, orgModel+" x")},
	} {
		if code, _, errOut := modelCmd(t, d, args...); code != exitFailure || !strings.HasPrefix(errOut, "praxium: ") {
			t.Errorf("%s: %d %s", name, code, errOut)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, definitionsfile.FileName), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"show", "-data-dir", dir}, {"history", "-data-dir", dir}, {"export", "-data-dir", dir}, {"check", "-file", writeModelFile(t, orgModel), "-data-dir", dir}} {
		if code, _, errOut := modelCmd(t, d, args...); code != exitFailure || !strings.Contains(errOut, definitionsfile.FileName) {
			t.Errorf("%v on a damaged file: %d %s", args, code, errOut)
		}
	}
	if code, _, errOut := modelCmd(t, d, "apply", "-file", writeModelFile(t, orgModel), "-data-dir", dir); code != exitFailure || !strings.Contains(errOut, "move it away") {
		t.Errorf("apply on a damaged file must not overwrite it: %d %s", code, errOut)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, definitionsfile.FileName)); string(raw) != "not json" {
		t.Errorf("the damaged file must be left as it is: %s", raw)
	}
}

// [REQ-369]
func TestAStoredOverlayWhoseItemDoesNotMatchItsTargetRefusesTheStart(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	body := `{"schema_version":1,"kind":"type_overlay","target":{"kind":"object_type","id":"ot.rack"},"properties":[]}`
	putOverlay(t, d, dir, "ot.physical_server", body)
	_, _, errOut := ingestLedger(t, d, dir, writeLedger(t, "org.management_department"))
	if !strings.Contains(errOut, "cannot start") || !strings.Contains(errOut, "holds the overlay of ot.rack") {
		t.Fatalf("an item that holds another target's overlay: %s", errOut)
	}
	for _, c := range []string{"history", "show", "export"} {
		if code, _, errOut := modelCmd(t, d, c, "-data-dir", dir); code == exitOK || !strings.Contains(errOut, "holds the overlay of ot.rack") {
			t.Errorf("model %s reads the bad item and says so: %d %s", c, code, errOut)
		}
	}
	many := t.TempDir()
	for i := 0; i < 7; i++ {
		name := "ot.no_such_" + string(rune('a'+i))
		putOverlay(t, d, many, name, `{"schema_version":1,"kind":"type_overlay","target":{"kind":"object_type","id":"`+name+`"},"properties":[
		  {"id":"pr.org.x`+string(rune('a'+i))+`.p","attr_no":10000`+string(rune('0'+i))+`,"name":"org.p","value_type":"vt.string","change_class":"slow"}]}`)
	}
	_, _, errOut = ingestLedger(t, d, many, writeLedger(t, "org.management_department"))
	if !strings.Contains(errOut, "problems:") || !strings.Contains(errOut, "and more") {
		t.Fatalf("many problems are counted and cut short: %s", errOut)
	}
}

// [REQ-368]
func TestTheReadersDiskChangesNothing(t *testing.T) {
	dir := t.TempDir()
	disk, err := openDefinitionsDisk(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disk.Create("x"); err == nil {
		t.Fatalf("a reader must not create a file")
	}
	if err := disk.Rename("x", "y"); err == nil {
		t.Fatalf("a reader must not rename")
	}
	if err := disk.Remove("x"); err != nil || disk.SyncDir() != nil {
		t.Fatalf("a reader removes and syncs nothing without failing")
	}
	if _, err := openDefinitionsDisk("", true); err == nil {
		t.Fatalf("a folder is needed")
	}
}

// [REQ-367]
func TestModelSmallAnswers(t *testing.T) {
	err := applyFailed(kernel.Errorf(kernel.CodeConflict, "the head is D-2, not the D-1 that was expected"))
	if !strings.Contains(errText(err), "run praxium model check again") {
		t.Errorf("a head that moved says what to do: %v", err)
	}
	other := kernel.Errorf(kernel.CodeInternal, "disk")
	if applyFailed(other) != other {
		t.Errorf("another failure goes through as it is")
	}
	if applyLocked("d", other) != other {
		t.Errorf("only a lock is explained as a lock")
	}
	if short("abc") != "abc" || short("0123456789") != "01234567" {
		t.Errorf("the short form of a hash is its first 8 characters")
	}
	w := wiring{}
	if applyActor(w) != "cli" {
		t.Errorf("without an environment the author is cli")
	}
	w.env.Lookup = func(name string) (string, bool) { return "ada", name == "USER" }
	if applyActor(w) != "cli:ada" {
		t.Errorf("the author is the person at the command line: %s", applyActor(w))
	}
}

// [REQ-368]
func TestModelHistoryLimit(t *testing.T) {
	d := modelWiring(t)
	dir, _ := appliedFolder(t, d)
	_, exported, _ := modelCmd(t, d, "export", "-data-dir", dir)
	renamed := writeModelFile(t, strings.Replace(exported, `"org.management_department"`, `"org.owning_department"`, 1))
	if code, _, _ := modelCmd(t, d, "apply", "-file", renamed, "-data-dir", dir); code != exitOK {
		t.Fatal("rename")
	}
	_, out, _ := modelCmd(t, d, "history", "-data-dir", dir, "-limit", "1")
	if strings.Count(out, "D-") != 1 || !strings.Contains(out, "D-2") {
		t.Fatalf("only the newest set: %s", out)
	}
}

// [REQ-368]
func TestModelFlagsAreChecked(t *testing.T) {
	d := modelWiring(t)
	dir, _ := appliedFolder(t, d)
	for _, args := range [][]string{
		{"show", "-data-dir", dir, "-store", "mongo"},
		{"show", "-data-dir", dir, "-set", "-1"},
		{"history", "-data-dir", dir, "-limit", "-2"},
		{"check"},
		{"show"},
		{"show", "-data-dir", dir, "extra"},
	} {
		if code, _, _ := modelCmd(t, d, args...); code == exitOK {
			t.Errorf("%v should be refused", args)
		}
	}
	for _, c := range []string{"show", "export"} {
		if code, _, _ := modelCmd(t, d, c, "-data-dir", dir, "-set", "99"); code == exitOK {
			t.Errorf("%s of a set that does not exist should be refused", c)
		}
	}
}
