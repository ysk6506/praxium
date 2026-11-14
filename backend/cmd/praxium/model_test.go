package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/definitionsfile"
)

// orgModel is the file of a person who adds a department and a year to the physical server.
const orgModel = `{
  "format": "praxium-org-model", "version": 1,
  "overlays": [ { "type": "PhysicalServer", "properties": [
    {"name": "org.management_department", "value_type": "string", "display_names": {"ko": "관리 부서"}},
    {"name": "org.introduced_year", "value_type": "integer"} ] } ]
}`

// modelWiring is a wiring that reads real files.
func modelWiring(t *testing.T) wiring {
	t.Helper()
	d, _, stop := apiDeps(nil, nil)
	t.Cleanup(stop)
	d.open = systemOpen
	return d
}

// modelCmd runs `praxium model ...` in this process.
func modelCmd(t *testing.T, d wiring, args ...string) (code int, out, errOut string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code = execute(append([]string{"model"}, args...), &stdout, &stderr, d)
	return code, stdout.String(), stderr.String()
}

// writeModelFile writes a file of organization properties into a temporary folder.
func writeModelFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "org-model.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// appliedFolder is a data folder with orgModel applied.
func appliedFolder(t *testing.T, d wiring) (dir, file string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "data")
	file = writeModelFile(t, orgModel)
	if code, out, errOut := modelCmd(t, d, "apply", "-file", file, "-data-dir", dir, "-m", "first"); code != exitOK || !strings.Contains(out, "stored D-1") {
		t.Fatalf("setup: %d %s%s", code, out, errOut)
	}
	return dir, file
}

// [REQ-366]
func TestModelCheckSaysEveryErrorAtOnceAndStoresNothing(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	bad := writeModelFile(t, `{"format":"praxium-org-model","version":1,"overlays":[{"type":"PhysicalServer","properties":[
	  {"name":"management_department","value_type":"string"},
	  {"name":"org.Bad Name","value_type":"string"},
	  {"name":"org.fine","value_type":"string"}]}]}`)
	code, out, errOut := modelCmd(t, d, "check", "-file", bad, "-data-dir", dir)
	if code != exitFailure || strings.Count(out, "error ") < 2 || !strings.Contains(out, "management_department") || !strings.Contains(out, "Bad Name") ||
		!strings.Contains(out, "not ok") || !strings.Contains(errOut, "would refuse") {
		t.Fatalf("both names are wrong and both must be told at once: %d\n%s%s", code, out, errOut)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("check must not make the folder")
	}
	good := writeModelFile(t, orgModel)
	code, out, errOut = modelCmd(t, d, "check", "-file", good)
	if code != exitOK || !strings.Contains(out, "the standard model alone") || !strings.Contains(out, "ok: apply would store 2 properties added") ||
		!strings.Contains(out, "org.management_department") {
		t.Fatalf("without a folder the file is checked against the standard model: %d\n%s%s", code, out, errOut)
	}
}

// [REQ-366]
func TestModelCheckComparesWithWhatIsStoredAndTakesNoLock(t *testing.T) {
	d := modelWiring(t)
	dir, file := appliedFolder(t, d)
	disk, err := openDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := disk.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()
	code, out, errOut := modelCmd(t, d, "check", "-file", file, "-data-dir", dir, "--json")
	var rep struct {
		OK        bool  `json:"ok"`
		StoredSet int64 `json:"stored_set"`
		Added     int   `json:"added"`
		Breaking  int   `json:"breaking"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil || code != exitOK || !rep.OK || rep.StoredSet != 1 || rep.Added != 0 || rep.Breaking != 0 {
		t.Fatalf("a folder that another process holds can be checked, and the same file adds nothing: %d %v\n%s%s", code, err, out, errOut)
	}
	swapped := strings.Replace(orgModel, `{"name": "org.introduced_year", "value_type": "integer"}`, `{"name": "org.contract_no", "value_type": "string"}`, 1)
	code, out, _ = modelCmd(t, d, "check", "-file", writeModelFile(t, swapped), "-data-dir", dir)
	if code != exitFailure || !strings.Contains(out, "stored definitions D-1") || !strings.Contains(out, "org.contract_no") || !strings.Contains(out, "Breaking") ||
		!strings.Contains(out, "org.introduced_year is gone") {
		t.Fatalf("a property the file leaves out is gone, and a new one is added:\n%s", out)
	}
}

// [REQ-367]
func TestModelApplyStoresOnceAndSaysWhenNothingChanged(t *testing.T) {
	d := modelWiring(t)
	dir, file := appliedFolder(t, d)
	if _, err := os.Stat(filepath.Join(dir, definitionsfile.FileName)); err != nil {
		t.Fatalf("the definitions are a file of the folder: %v", err)
	}
	code, out, errOut := modelCmd(t, d, "apply", "-file", file, "-data-dir", dir)
	if code != exitOK || !strings.Contains(out, "nothing changed") {
		t.Fatalf("the same file again: %d %s%s", code, out, errOut)
	}
	if code, out, _ = modelCmd(t, d, "history", "-data-dir", dir); code != exitOK || strings.Count(out, "D-") != 1 {
		t.Fatalf("a second apply of the same content makes no set: %s", out)
	}
	_, exported, _ := modelCmd(t, d, "export", "-data-dir", dir)
	again := writeModelFile(t, exported)
	if code, out, errOut = modelCmd(t, d, "apply", "-file", again, "-data-dir", dir); code != exitOK || !strings.Contains(out, "nothing changed") {
		t.Fatalf("what export gives, applied again, changes nothing: %d %s%s", code, out, errOut)
	}
}

// [REQ-367]
func TestModelApplyRenamesRetiresAndNeverGivesANumberTwice(t *testing.T) {
	d := modelWiring(t)
	dir, _ := appliedFolder(t, d)
	numbers := func() map[string]float64 {
		_, out, _ := modelCmd(t, d, "export", "-data-dir", dir)
		var f struct {
			Overlays []struct {
				Properties []struct {
					Name   string  `json:"name"`
					ID     string  `json:"id"`
					AttrNo float64 `json:"attr_no"`
				} `json:"properties"`
			} `json:"overlays"`
		}
		if err := json.Unmarshal([]byte(out), &f); err != nil {
			t.Fatal(err)
		}
		got := map[string]float64{}
		for _, p := range f.Overlays[0].Properties {
			got[p.Name] = p.AttrNo
		}
		return got
	}
	before := numbers()
	if before["org.management_department"] < 100000 || before["org.introduced_year"] < 100000 || before["org.management_department"] == before["org.introduced_year"] {
		t.Fatalf("the numbers start at 100000 and differ: %v", before)
	}
	// Renaming keeps the number and the old name as an alias; the id comes from the export.
	_, exported, _ := modelCmd(t, d, "export", "-data-dir", dir)
	renamed := writeModelFile(t, strings.Replace(exported, `"org.management_department"`, `"org.owning_department"`, 1))
	if code, out, errOut := modelCmd(t, d, "apply", "-file", renamed, "-data-dir", dir, "-m", "rename"); code != exitOK || !strings.Contains(out, "stored D-2") {
		t.Fatalf("a rename is a compatible change: %d %s%s", code, out, errOut)
	}
	if after := numbers(); after["org.owning_department"] != before["org.management_department"] {
		t.Fatalf("a rename keeps the number: %v then %v", before, after)
	}
	_, shown, _ := modelCmd(t, d, "export", "-data-dir", dir)
	if !strings.Contains(shown, `"org.management_department"`) {
		t.Fatalf("the old name stays as an alias: %s", shown)
	}
	// A refused apply gives no number away; the next new property gets the next one.
	broken := writeModelFile(t, strings.Replace(exported, `"value_type": "integer"`, `"value_type": "string"`, 1))
	if code, _, _ := modelCmd(t, d, "apply", "-file", broken, "-data-dir", dir); code != exitFailure {
		t.Fatalf("a breaking change must be refused")
	}
	withNew := writeModelFile(t, strings.Replace(shown, `"properties": [`, `"properties": [{"name": "org.contract_no", "value_type": "string"},`, 1))
	if code, out, errOut := modelCmd(t, d, "apply", "-file", withNew, "-data-dir", dir); code != exitOK || !strings.Contains(out, "1 properties added") {
		t.Fatalf("a new property: %d %s%s", code, out, errOut)
	}
	if got := numbers()["org.contract_no"]; got != 100002 {
		t.Fatalf("the third number is 100002, never one given before: %v", got)
	}
}

// [REQ-367]
func TestModelApplyRefusesBreakingChangesWithTheWayAroundAndKeepsTheStore(t *testing.T) {
	d := modelWiring(t)
	dir, _ := appliedFolder(t, d)
	_, exported, _ := modelCmd(t, d, "export", "-data-dir", dir)
	raw, err := os.ReadFile(filepath.Join(dir, definitionsfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ edited, advice string }{
		"a value type":    {strings.Replace(exported, `"value_type": "integer"`, `"value_type": "string"`, 1), "add a new property, fill it and deprecate the old one"},
		"a property gone": {dropProperty(t, exported, "org.introduced_year"), "deprecate it with status deprecated instead"},
	} {
		file := writeModelFile(t, c.edited)
		code, out, errOut := modelCmd(t, d, "apply", "-file", file, "-data-dir", dir)
		if code != exitFailure || !strings.Contains(out, "Breaking") || !strings.Contains(out, c.advice) ||
			!strings.Contains(errOut, "nothing was stored") {
			t.Errorf("%s: %d\n%s%s", name, code, out, errOut)
		}
		if now, _ := os.ReadFile(filepath.Join(dir, definitionsfile.FileName)); !bytes.Equal(now, raw) {
			t.Fatalf("%s: a refused apply must leave the file as it was", name)
		}
	}
	if code, _, _ := modelCmd(t, d, "apply", "-file", writeModelFile(t, exported), "-data-dir", dir, "-force"); code != exitUsage {
		t.Fatalf("there is no flag that forces a breaking change")
	}
}

// [REQ-367]
func TestModelApplyRefusesAFolderAnotherProcessHolds(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	disk, err := openDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := disk.Lock()
	if err != nil {
		t.Fatal(err)
	}
	file := writeModelFile(t, orgModel)
	code, out, errOut := modelCmd(t, d, "apply", "-file", file, "-data-dir", dir)
	if code != exitFailure || !strings.Contains(errOut, "in use") || !strings.Contains(errOut, "only when it starts") {
		t.Fatalf("a folder in use is refused with the reason: %d %s%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, definitionsfile.FileName)); err == nil {
		t.Fatalf("nothing may be written under a server")
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut = modelCmd(t, d, "apply", "-file", file, "-data-dir", dir); code != exitOK {
		t.Fatalf("after the lock is gone: %d %s%s", code, out, errOut)
	}
}

// [REQ-367]
func TestModelStoreFlagAndUsageErrors(t *testing.T) {
	d := modelWiring(t)
	file := writeModelFile(t, orgModel)
	code, _, errOut := modelCmd(t, d, "apply", "-file", file, "-store", "postgres", "-store-dsn-file", "dsn")
	if code != exitUsage || !strings.Contains(errOut, "PostgreSQL store of the definitions is not there yet") {
		t.Fatalf("postgres: %d %s", code, errOut)
	}
	for _, args := range [][]string{{}, {"nope"}, {"apply", "-file", file}, {"check"}, {"show"}, {"history", "-data-dir", "x", "extra"}} {
		if code, _, errOut = modelCmd(t, d, args...); code != exitUsage || !strings.Contains(errOut, "usage: praxium model") {
			t.Errorf("%v: %d %s", args, code, errOut)
		}
	}
}

// [REQ-368]
func TestModelShowExportAndHistoryReadWithoutALockAndByNumber(t *testing.T) {
	d := modelWiring(t)
	dir, _ := appliedFolder(t, d)
	_, exported, _ := modelCmd(t, d, "export", "-data-dir", dir)
	deprecated := writeModelFile(t, strings.Replace(exported, `"value_type": "integer"`, `"value_type": "integer", "status": "deprecated"`, 1))
	if code, out, errOut := modelCmd(t, d, "apply", "-file", deprecated, "-data-dir", dir, "-m", "retire the year"); code != exitOK {
		t.Fatalf("deprecating is compatible: %d %s%s", code, out, errOut)
	}
	disk, err := openDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := disk.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()
	code, out, errOut := modelCmd(t, d, "show", "-data-dir", dir)
	if code != exitOK || !strings.Contains(out, "D-2") || !strings.Contains(out, "deprecated") || !strings.Contains(out, "ko=관리 부서") {
		t.Fatalf("show: %d %s%s", code, out, errOut)
	}
	if _, out, _ = modelCmd(t, d, "show", "-data-dir", dir, "-set", "1"); !strings.Contains(out, "D-1") || strings.Contains(out, "deprecated") {
		t.Fatalf("an older set shows its own content: %s", out)
	}
	if code, _, errOut = modelCmd(t, d, "show", "-data-dir", dir, "-set", "9"); code != exitFailure || !strings.Contains(errOut, "no definition set D-9") {
		t.Fatalf("an unknown set: %d %s", code, errOut)
	}
	_, out, _ = modelCmd(t, d, "history", "-data-dir", dir)
	if !strings.Contains(out, "retire the year") || !strings.Contains(out, "+2 added") || !strings.Contains(out, "+0 added, 1 changed") {
		t.Fatalf("history says what each set changed:\n%s", out)
	}
	var rows []struct {
		Set     int64  `json:"set"`
		Message string `json:"message"`
	}
	_, out, _ = modelCmd(t, d, "history", "-data-dir", dir, "--json")
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 || rows[0].Set != 2 || rows[1].Message != "first" {
		t.Fatalf("history as JSON, newest first: %v %s", err, out)
	}
	_, old, _ := modelCmd(t, d, "export", "-data-dir", dir, "-set", "1")
	if strings.Contains(old, "deprecated") || old != exported {
		t.Fatalf("export of the older set is what was exported then")
	}
}

// [REQ-368]
func TestModelReadsOfAFolderWithNothingStored(t *testing.T) {
	d := modelWiring(t)
	dir := t.TempDir()
	code, out, _ := modelCmd(t, d, "show", "-data-dir", dir)
	if code != exitOK || !strings.Contains(out, "standard model alone") {
		t.Fatalf("show: %d %s", code, out)
	}
	if _, out, _ = modelCmd(t, d, "history", "-data-dir", dir); !strings.Contains(out, "no definitions are stored") {
		t.Fatalf("history: %s", out)
	}
	_, out, _ = modelCmd(t, d, "export", "-data-dir", dir)
	var f struct {
		Overlays []json.RawMessage `json:"overlays"`
	}
	if err := json.Unmarshal([]byte(out), &f); err != nil || len(f.Overlays) != 0 {
		t.Fatalf("an empty file that applies: %v %s", err, out)
	}
	if code, _, errOut := modelCmd(t, d, "show", "-data-dir", filepath.Join(dir, "none")); code != exitFailure || !strings.Contains(errOut, "does not exist") {
		t.Fatalf("a folder that is not there: %d %s", code, errOut)
	}
}

// dropProperty takes a property out of an exported file.
func dropProperty(t *testing.T, file, name string) string {
	t.Helper()
	var f struct {
		Format   string `json:"format"`
		Version  int    `json:"version"`
		Overlays []struct {
			Type       string            `json:"type"`
			Properties []json.RawMessage `json:"properties"`
		} `json:"overlays"`
	}
	if err := json.Unmarshal([]byte(file), &f); err != nil {
		t.Fatal(err)
	}
	for i := range f.Overlays {
		var kept []json.RawMessage
		for _, p := range f.Overlays[i].Properties {
			if !strings.Contains(string(p), `"`+name+`"`) {
				kept = append(kept, p)
			}
		}
		f.Overlays[i].Properties = kept
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
