package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/definitionsfile"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The definitions of these tests are made up. impSource is a file source of one dataset of servers; impMapping reads a
// dataset into physical servers with the feeds given.

func impSource(id, dataset string) string {
	return fmt.Sprintf(`{"version":1,"id":%q,"adapter":"file","datasets":[{"id":%q,"parser":"csv","natural_key":["asset_id"],
	  "columns":[{"name":"asset_id"},{"name":"serial_number"},{"name":"manufacturer"}]}]}`, id, dataset)
}

func impFeed(id, dataset string) string {
	return fmt.Sprintf(`{"id":%q,"dataset":%q,"object_type":"PhysicalServer","creates_objects":true,"basis":"declared",
	  "attributes":[{"attribute":"name","column":"asset_id"},{"attribute":"manufacturer","column":"manufacturer"},{"attribute":"serial_number","column":"serial_number"}],
	  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial_number"}],"priority":1}]}`, id, dataset)
}

func impMapping(id string, feeds ...string) string {
	idField := ""
	if id != "" {
		idField = fmt.Sprintf(`"id":%q,`, id)
	}
	return `{"version":1,` + idField + `"feeds":[` + strings.Join(feeds, ",") + `]}`
}

// impFiles writes the two documents into files of a temporary folder.
func impFiles(t *testing.T, source, mapping string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	s, m := filepath.Join(dir, "source.json"), filepath.Join(dir, "mapping.json")
	for path, text := range map[string]string{s: source, m: mapping} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return s, m
}

// impRun is `praxium ingest import`.
func impRun(t *testing.T, d wiring, args ...string) (code int, out, errOut string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code = execute(append([]string{"ingest", "import"}, args...), &stdout, &stderr, d)
	return code, stdout.String(), stderr.String()
}

// impImport imports two documents into a folder.
func impImport(t *testing.T, d wiring, dir, source, mapping string) (code int, out, errOut string) {
	t.Helper()
	s, m := impFiles(t, source, mapping)
	return impRun(t, d, "-data-dir", dir, "-source", s, "-mapping", m)
}

// impItems are the items of the head of a folder, by kind and name.
func impItems(t *testing.T, d wiring, dir string) (int64, map[string]definitions.ResolvedItem) {
	t.Helper()
	st, err := openModelState(dir, true, d.deps)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]definitions.ResolvedItem{}
	if !st.hasHead {
		return 0, out
	}
	items, err := st.svc.SetItems(context.Background(), st.head.No)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		out[it.Kind+"/"+it.Name] = it
	}
	return st.head.No, out
}

// impPut writes items straight into the store of a folder, as another writer could.
func impPut(t *testing.T, d wiring, dir string, changes ...definitions.Change) {
	t.Helper()
	disk, err := openDefinitionsDisk(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	store, err := definitionsfile.Open(disk)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := definitions.New(definitions.Config{Store: store, Clock: d.clock, IDs: d.ids})
	if err != nil {
		t.Fatal(err)
	}
	head, _, err := svc.Head(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(context.Background(), definitions.ApplyInput{ExpectHead: head.No, Actor: definitions.Actor{ID: "test"}, Changes: changes}); err != nil {
		t.Fatal(err)
	}
}

// [REQ-704] [REQ-438] [REQ-476]
// The two documents become a source item and a mapping item next to the organization properties, a mapping with no id
// takes the id of the source, importing them again changes nothing, and a changed mapping is a new revision of its item;
// the model commands and ingest run read the same folder as before.
func TestImpStoresTheDocumentsBesideTheOrgPropertiesAndAgainChangesNothing(t *testing.T) {
	d := modelWiring(t)
	dir, orgFile := appliedFolder(t, d)
	ledger := writeLedger(t, "org.management_department")
	code, out, errOut := impRun(t, d, "-data-dir", dir, "-source", ledger.source, "-mapping", ledger.mapping, "-message", "the ledger")
	if code != exitOK || !strings.Contains(out, "stored D-2") || !strings.Contains(out, "source src_asset_ledger added, mapping src_asset_ledger added") ||
		!strings.Contains(out, "ingest run still reads") {
		t.Fatalf("the first import: %d %s%s", code, out, errOut)
	}
	head, items := impItems(t, d, dir)
	src, srcOK := items["source/src_asset_ledger"]
	mp, mapOK := items["mapping/src_asset_ledger"]
	if head != 2 || !srcOK || !mapOK || len(items) != 3 {
		t.Fatalf("the store holds %d items at D-%d: %v", len(items), head, items)
	}
	var doc struct{ ID string }
	if err := json.Unmarshal(mp.Body, &doc); err != nil || doc.ID != "src_asset_ledger" || src.SchemaVersion != ingest.ItemSchemaVersion {
		t.Errorf("the mapping is stored with the id of its source: %s %v", mp.Body, err)
	}
	st, err := openModelState(dir, true, d.deps)
	if err != nil || len(st.stored) != 1 || st.head.Origin != definitions.OriginImport || st.head.Message != "the ledger" {
		t.Fatalf("the overlays are read as before: %v %+v", err, st)
	}
	if code, out, errOut = impRun(t, d, "-data-dir", dir, "-source", ledger.source, "-mapping", ledger.mapping); code != exitOK || !strings.Contains(out, "nothing changed") ||
		!strings.Contains(out, "(D-2)") {
		t.Fatalf("the same files again: %d %s%s", code, out, errOut)
	}
	if again, _ := impItems(t, d, dir); again != 2 {
		t.Errorf("a second import made D-%d", again)
	}
	if code, out, errOut = modelCmd(t, d, "apply", "-file", orgFile, "-data-dir", dir); code != exitOK || !strings.Contains(out, "nothing changed") {
		t.Errorf("the org properties after an import: %d %s%s", code, out, errOut)
	}
	if code, out, errOut = modelCmd(t, d, "show", "-data-dir", dir); code != exitOK || !strings.Contains(out, "org.management_department") || strings.Contains(out, "src_asset_ledger") {
		t.Errorf("model show: %d %s%s", code, out, errOut)
	}
	if code, out, errOut = ingestLedger(t, d, dir, ledger); code != exitOK {
		t.Errorf("ingest run on the folder: %d %s%s", code, out, errOut)
	}
	changed := strings.Replace(impReadText(t, ledger.mapping), `{"attribute":"org.introduced_year","column":"introduced_year"}`, `{"attribute":"org.introduced_year","column":"introduced_year","rank":1}`, 1)
	if err := os.WriteFile(ledger.mapping, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut = impRun(t, d, "-data-dir", dir, "-source", ledger.source, "-mapping", ledger.mapping, "-m", "rank"); code != exitOK ||
		!strings.Contains(out, "stored D-3") || !strings.Contains(out, "source src_asset_ledger unchanged, mapping src_asset_ledger changed") {
		t.Fatalf("a changed mapping: %d %s%s", code, out, errOut)
	}
	if _, after := impItems(t, d, dir); after["mapping/src_asset_ledger"].ItemID != mp.ItemID || after["mapping/src_asset_ledger"].RevNo != 2 {
		t.Errorf("the change is not a revision of the same item: %+v", after["mapping/src_asset_ledger"])
	}
}

func impReadText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// [REQ-704]
// A set that does not compile is refused with the document, its id and the place, and the store stays as it was: a
// broken mapping, a feed id another mapping has, a mapping of no source, and the organization properties it needs.
func TestImpRefusesASetThatDoesNotCompileAndKeepsTheStore(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	if code, out, errOut := impImport(t, d, dir, impSource("src_a", "ds_a"), impMapping("m_a", impFeed("f_x", "ds_a"))); code != exitOK {
		t.Fatalf("setup: %d %s%s", code, out, errOut)
	}
	before := impReadText(t, filepath.Join(dir, definitionsfile.FileName))
	cases := []struct {
		name, source, mapping string
		want                  []string
	}{
		{"a type that is not there", impSource("src_b", "ds_b"), impMapping("", strings.Replace(impFeed("f_b", "ds_b"), "PhysicalServer", "NoSuchType", 1)),
			[]string{"praxium: mapping src_b at /feeds/0/object_type:", "nothing was stored: 1 problems"}},
		{"a feed of another mapping", impSource("src_b", "ds_b"), impMapping("m_b", impFeed("f_x", "ds_b")),
			[]string{"praxium: mapping m_b at /feeds/0/id: feed f_x of mapping m_b is also a feed of mapping m_a"}},
		{"a dataset of another source", impSource("src_b", "ds_b"), impMapping("m_b", impFeed("f_b", "ds_a"), impFeed("f_c", "ds_b")),
			[]string{"mapping m_b: mapping m_b reads datasets of the sources src_a, src_b"}},
		{"an org property that is not there", impSource("src_b", "ds_b"), strings.Replace(impMapping("m_b", impFeed("f_b", "ds_b")), `"column":"asset_id"}`, `"column":"asset_id"},{"attribute":"org.nothing","column":"asset_id"}`, 1),
			[]string{"mapping m_b at /feeds/0/attributes/1/attribute"}},
		{"a source that is wrong", strings.Replace(impSource("src_b", "ds_b"), `"parser":"csv"`, `"parser":"nope"`, 1), impMapping("m_b", impFeed("f_b", "ds_b")),
			[]string{"praxium: source definition ", "source.json at /datasets/0/parser"}},
	}
	for _, c := range cases {
		code, out, errOut := impImport(t, d, dir, c.source, c.mapping)
		for _, want := range c.want {
			if code != exitFailure || !strings.Contains(errOut, want) {
				t.Errorf("%s: %d, want %q in:\n%s%s", c.name, code, want, out, errOut)
			}
		}
		if after := impReadText(t, filepath.Join(dir, definitionsfile.FileName)); after != before {
			t.Errorf("%s: the store changed", c.name)
		}
	}
}

// [REQ-704]
// What the store holds itself is judged too: a stored mapping that no longer compiles, two items of one name in a kind,
// an item whose document has another id and broken organization properties refuse the import with the item.
func TestImpRefusesWhatTheStoreHoldsWrongWithTheItem(t *testing.T) {
	d := modelWiring(t)
	fresh := func(t *testing.T, changes ...definitions.Change) string {
		dir := filepath.Join(t.TempDir(), "data")
		if code, out, errOut := impImport(t, d, dir, impSource("src_a", "ds_a"), impMapping("m_a", impFeed("f_a", "ds_a"))); code != exitOK {
			t.Fatalf("setup: %d %s%s", code, out, errOut)
		}
		impPut(t, d, dir, changes...)
		return dir
	}
	item := func(kind, name, body string) definitions.Change {
		return definitions.Change{Kind: kind, Name: name, SchemaVersion: 1, Body: []byte(body)}
	}
	const noTarget = `{"schema_version":1,"kind":"type_overlay","target":{"kind":"object_type","id":"ot.no_such_type"},"properties":[
	  {"id":"pr.org.no_such_type.x","attr_no":100000,"name":"org.x","value_type":"vt.string","change_class":"slow"}]}`
	cases := map[string]struct {
		changes []definitions.Change
		want    string
	}{
		"a stored mapping that is broken": {[]definitions.Change{item("mapping", "m_old", impMapping("m_old", strings.Replace(impFeed("f_old", "ds_a"), "PhysicalServer", "NoSuchType", 1)))},
			"praxium: mapping m_old at /feeds/0/object_type"},
		"a stored source that does not read": {[]definitions.Change{item("source", "src_old", `{"id":"src_old","adapter":"file","datasets":[]}`)},
			"praxium: source src_old at /datasets"},
		"two sources of one name":   {[]definitions.Change{item("source", "src_a", impSource("src_a", "ds_a"))}, "two source items named src_a"},
		"a name that is not the id": {[]definitions.Change{item("mapping", "m_z", impMapping("m_y", impFeed("f_z", "ds_a")))}, "the mapping item m_z of the definition store holds the document of m_y"},
		"org properties":            {[]definitions.Change{{ItemID: "ov.ot.no_such_type", Kind: model.OverlayKind, Name: "ot.no_such_type", SchemaVersion: 1, Body: []byte(noTarget)}}, "mend them with praxium model apply first"},
	}
	for name, c := range cases {
		dir := fresh(t, c.changes...)
		before := impReadText(t, filepath.Join(dir, definitionsfile.FileName))
		code, out, errOut := impImport(t, d, dir, impSource("src_b", "ds_b"), impMapping("m_b", impFeed("f_b", "ds_b")))
		if code != exitFailure || !strings.Contains(errOut, c.want) {
			t.Errorf("%s: %d %s%s", name, code, out, errOut)
		}
		if after := impReadText(t, filepath.Join(dir, definitionsfile.FileName)); after != before {
			t.Errorf("%s: the store changed", name)
		}
	}
	// a stored mapping without an id is the mapping of its item's name
	dir := fresh(t, item("mapping", "m_noid", impMapping("", impFeed("f_noid", "ds_a"))))
	if code, out, errOut := impImport(t, d, dir, impSource("src_b", "ds_b"), impMapping("m_b", impFeed("f_noid", "ds_b"))); code != exitFailure ||
		!strings.Contains(errOut, "feed f_noid of mapping m_noid is also a feed of mapping m_b") {
		t.Errorf("a stored mapping without an id: %d %s%s", code, out, errOut)
	}
}

// [REQ-705] [REQ-440]
// A value of a secret in either file is refused before anything else is read, and the value is in no output; a
// reference to a secret that is not put in yet is stored.
func TestImpRefusesSecretValuesWithoutRepeatingThemAndStoresReferences(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	httpSource := func(conn string) string {
		return `{"version":1,"id":"src_api","adapter":"http","connection":` + conn + `,
		  "datasets":[{"id":"ds_api","request":{"method":"GET","path":"/servers"},"records_path":"items","natural_key":["asset_id"],
		    "columns":[{"name":"asset_id"},{"name":"serial_number"},{"name":"manufacturer"}]}]}`
	}
	const ref = `{"base_url":"https://inventory.example.test/api","auth":{"kind":"bearer","credential":{"secret_id":"sec_not_put_yet"}}}`
	mapping := impMapping("", impFeed("f_api", "ds_api"))
	cases := map[string][3]string{
		"a key in the address": {httpSource(`{"base_url":"https://inventory.example.test/api?api_key=zz-fake-901"}`), mapping,
			"source.json at /connection/base_url: the text has api_key=... with a value written in it"},
		"an account": {httpSource(`{"base_url":"https://svc:zz-fake-902@inventory.example.test/api"}`), mapping,
			"source.json at /connection/base_url: the address has an account"},
		"an authorization": {httpSource(`{"base_url":"https://inventory.example.test/api","headers":{"Authorization":"Bearer zz-fake-903"}}`), mapping,
			"source.json at /connection/headers/Authorization: the field Authorization has a value written in the definition"},
		// the field is no field of a mapping: the value is refused before the document is read, so no sentence of the
		// parse can repeat it
		"a token in the mapping": {httpSource(ref), strings.Replace(mapping, `"column":"asset_id"}`, `"column":"asset_id"},{"attribute":"description","value":"token=zz-fake-904"}`, 1),
			"praxium: mapping definition "},
	}
	for name, c := range cases {
		code, out, errOut := impImport(t, d, dir, c[0], c[1])
		if code != exitFailure || !strings.Contains(errOut, c[2]) || !strings.Contains(errOut, "nothing was stored: 1 problems") {
			t.Errorf("%s: %d %s%s", name, code, out, errOut)
		}
		if strings.Contains(out+errOut, "zz-fake") {
			t.Errorf("%s: the value is repeated:\n%s%s", name, out, errOut)
		}
		if _, err := os.Stat(filepath.Join(dir, definitionsfile.FileName)); err == nil {
			t.Errorf("%s: something was stored", name)
		}
	}
	if code, out, errOut := impImport(t, d, dir, httpSource(ref), mapping); code != exitOK || !strings.Contains(out, "source src_api added") {
		t.Fatalf("a reference to a secret that is not there: %d %s%s", code, out, errOut)
	}
}

// [REQ-704]
// A folder another process holds is not written, with what to do.
func TestImpRefusesAFolderAnotherProcessHolds(t *testing.T) {
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
	code, out, errOut := impImport(t, d, dir, impSource("src_a", "ds_a"), impMapping("", impFeed("f_a", "ds_a")))
	if code != exitFailure || !strings.Contains(errOut, "in use by another praxium") || !strings.Contains(errOut, "run praxium ingest import again") {
		t.Fatalf("%d %s%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, definitionsfile.FileName)); err == nil {
		t.Errorf("something was stored under a lock")
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut = impImport(t, d, dir, impSource("src_a", "ds_a"), impMapping("", impFeed("f_a", "ds_a"))); code != exitOK {
		t.Errorf("after the lock: %d %s%s", code, out, errOut)
	}
}

// [REQ-704]
// The command line: what is missing or unknown is a usage error, a file that cannot be read or is too large is a
// failure, the help has the usage and an example, and ingest names import among its commands.
func TestImpCommandLineAndFiles(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	s, m := impFiles(t, impSource("src_a", "ds_a"), impMapping("", impFeed("f_a", "ds_a")))
	big := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(big, bytes.Repeat([]byte(" "), definitions.DefaultMaxBodyBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	notJSON, _ := impFiles(t, `{"id": `, "")
	usage := [][]string{{"-source", s, "-mapping", m}, {"-data-dir", dir, "-source", s}, {"-data-dir", dir, "-source", s, "-mapping", m, "extra"}, {"-bogus"}}
	for _, args := range usage {
		if code, _, errOut := impRun(t, d, args...); code != exitUsage || !strings.Contains(errOut, ingestImportUsage) {
			t.Errorf("%v: %d %s", args, code, errOut)
		}
	}
	fails := map[string][]string{
		"no such file":   {"-data-dir", dir, "-source", filepath.Join(dir, "none.json"), "-mapping", m},
		"no such map":    {"-data-dir", dir, "-source", s, "-mapping", filepath.Join(dir, "none.json")},
		"too large":      {"-data-dir", dir, "-source", big, "-mapping", m},
		"not json":       {"-data-dir", dir, "-source", notJSON, "-mapping", m},
		"a file as data": {"-data-dir", s, "-source", s, "-mapping", m},
	}
	for name, args := range fails {
		if code, out, errOut := impRun(t, d, args...); code != exitFailure || !strings.HasPrefix(errOut, "praxium: ") {
			t.Errorf("%s: %d %s%s", name, code, out, errOut)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := execute([]string{"ingest", "import", "--help"}, &stdout, &stderr, d); code != exitOK || !strings.HasPrefix(stdout.String(), ingestImportUsage) ||
		!strings.Contains(stdout.String(), "example") || !strings.Contains(stdout.String(), "-message") {
		t.Errorf("help: %d %s%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := execute([]string{"ingest", "nothing"}, &stdout, &stderr, d); code != exitUsage || !strings.Contains(stderr.String(), "run or import") {
		t.Errorf("ingest: %d %s", code, stderr.String())
	}
	if got := withID([]byte("[]"), "x"); string(got) != "[]" {
		t.Errorf("a text that is no object is changed: %s", got)
	}
	plain := fmt.Errorf("plain")
	if importLocked(dir, plain) != plain || namedErrors("x", plain) != plain {
		t.Errorf("an error that is no lock and no definition error is passed on as it is")
	}
}

// [REQ-704]
// What cannot be stored is said: a key written twice, a message longer than the store keeps, a folder whose store is
// damaged, a folder given as a file.
func TestImpSaysWhatCannotBeStored(t *testing.T) {
	d := modelWiring(t)
	dir := filepath.Join(t.TempDir(), "data")
	source, mapping := impSource("src_a", "ds_a"), impMapping("m_a", impFeed("f_a", "ds_a"))
	twice := strings.Replace(source, `"version":1,`, `"version":1,"version":1,`, 1)
	s, m := impFiles(t, twice, mapping)
	if code, out, errOut := impRun(t, d, "-data-dir", dir, "-source", s, "-mapping", m); code != exitFailure || !strings.Contains(errOut, "the source definition cannot be stored") {
		t.Errorf("a key twice: %d %s%s", code, out, errOut)
	}
	s, m = impFiles(t, source, mapping)
	if code, out, errOut := impRun(t, d, "-data-dir", dir, "-source", s, "-mapping", m, "-message", strings.Repeat("x", definitions.DefaultMaxMessageLen+1)); code != exitFailure ||
		!strings.Contains(errOut, "max_message_len") {
		t.Errorf("a long message: %d %s%s", code, out, errOut)
	}
	if code, out, errOut := impRun(t, d, "-data-dir", dir, "-source", filepath.Dir(s), "-mapping", m); code != exitFailure || !strings.Contains(errOut, "cannot read") {
		t.Errorf("a folder as the source: %d %s%s", code, out, errOut)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, definitionsfile.FileName), []byte(`{"format":"praxium-definitions","vers`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := impRun(t, d, "-data-dir", dir, "-source", s, "-mapping", m); code != exitFailure || !strings.Contains(errOut, definitionsfile.FileName) {
		t.Errorf("a damaged store: %d %s%s", code, out, errOut)
	}
}
