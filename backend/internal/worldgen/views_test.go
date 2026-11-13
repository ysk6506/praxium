package worldgen

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func viewParams(p *Params) {
	p.Sites = 2
	p.Imperfections.MissingRowRatio = 0.05
	p.Imperfections.DuplicateKeyRatio = 0.03
}

func csvRows(t *testing.T, st *MemStore, name string) [][]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(st.Text(name))).ReadAll()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return rows
}

// A table source has the rows of the truth, minus the rows it lost, plus the
// rows it repeated; the manifest says how many of each.
func TestCSVTablesMatchTruthAndRecordedFlaws(t *testing.T) {
	w := smallWorld(t, 21, 30000, viewParams)
	m, st := runAll(t, w)
	for _, c := range []struct {
		file  string
		types []string
	}{{"racks.csv", []string{"Rack"}}, {"servers.csv", []string{"PhysicalServer"}}, {"switches.csv", []string{"Switch", "Router"}}} {
		want := 0
		for _, ty := range c.types {
			want += int(m.Counts.ObjectsByType[ty])
		}
		rows := csvRows(t, st, c.file)
		got := len(rows) - 1
		lost := int(m.Imperfections.ByFile["missing_row "+c.file])
		dup := int(m.Imperfections.ByFile["duplicate_key "+c.file])
		if got != want-lost+dup {
			t.Errorf("%s: %d rows, want %d - %d + %d", c.file, got, want, lost, dup)
		}
		if c.file == "servers.csv" && (lost == 0 || dup == 0) {
			t.Errorf("%s: no flaw was made (lost %d, dup %d)", c.file, lost, dup)
		}
	}
	if m.Imperfections.Counts["shape_drift"] < 2 {
		t.Fatalf("shape drift not recorded: %v", m.Imperfections.Counts)
	}
	if !strings.HasPrefix(st.Text("servers.csv"), "asset_id,hostname,serial_no,") {
		t.Fatal("the serial column should be renamed")
	}
}

type vmPage struct {
	Page     int                          `json:"page"`
	Items    []map[string]json.RawMessage `json:"items"`
	Next     *string                      `json:"next"`
	Complete *bool                        `json:"complete"`
	Total    *int                         `json:"total"`
}

func readPages(t *testing.T, st *MemStore, base string) []vmPage {
	var out []vmPage
	for n := 1; ; n++ {
		raw, ok := st.Files["virt/"+base+"-"+pad4(n)+".json"]
		if !ok {
			return out
		}
		var p vmPage
		if err := json.Unmarshal(raw.Bytes(), &p); err != nil {
			t.Fatalf("page %d of %s: %v", n, base, err)
		}
		out = append(out, p)
	}
}

// The JSON pages nest guests, NICs and disks, and the last page claims a
// complete delivery with a total that counts the rows it lost.
func TestVirtPagesNestAndClaimFalseCompleteness(t *testing.T) {
	w := smallWorld(t, 22, 40000, viewParams)
	m, st := runAll(t, w)
	pages := readPages(t, st, "vms")
	if len(pages) < 2 {
		t.Fatalf("%d pages", len(pages))
	}
	items := 0
	for i, p := range pages {
		items += len(p.Items)
		if (i == len(pages)-1) != (p.Next == nil) {
			t.Fatalf("page %d: next = %v", i+1, p.Next)
		}
	}
	vms := int(m.Counts.ObjectsByType["VirtualMachine"])
	lost := int(m.Imperfections.ByFile["missing_row vms-*.json"]) + int(m.Imperfections.ByFile["false_completeness vms-*.json"])
	if items < vms-lost || items > vms-lost+int(m.Imperfections.Counts["duplicate_key"]) {
		t.Fatalf("%d items for %d VMs, %d lost", items, vms, lost)
	}
	last := pages[len(pages)-1]
	if last.Complete == nil || !*last.Complete || last.Total == nil || *last.Total <= items-int(m.Imperfections.Counts["duplicate_key"]) {
		t.Fatalf("the last page should claim completeness and a larger total: %+v", last)
	}
	first := pages[0].Items[0]
	var nics, disks []any
	if json.Unmarshal(first["nics"], &nics) != nil || json.Unmarshal(first["disks"], &disks) != nil || len(nics) == 0 || len(disks) == 0 {
		t.Fatalf("nested arrays missing: %s", first["nics"])
	}
	if _, ok := pages[1].Items[0]["hypervisor"]; !ok {
		t.Fatal("shape drift: page 2 should call the host field hypervisor")
	}
}

// A source that is honest says so when rows are missing.
func TestHonestSourceSaysIncomplete(t *testing.T) {
	w := smallWorld(t, 22, 40000, func(p *Params) {
		viewParams(p)
		p.Imperfections.FalseCompleteness = false
	})
	_, st := runAll(t, w)
	pages := readPages(t, st, "vms")
	last := pages[len(pages)-1]
	if last.Complete == nil || *last.Complete {
		t.Fatalf("an honest source with missing rows says complete=false: %+v", last)
	}
}

// The same ECMP group is derivable from the configuration, the table and the
// declared list; they agree except for the one group the manifest names.
func TestECMPThreeWays(t *testing.T) {
	w := smallWorld(t, 23, 60000, func(p *Params) { p.Sites = 2; p.Imperfections.UnparseableRatio = 0.2 })
	m, st := runAll(t, w)
	conf := map[string]string{}
	garbled := 0
	for _, name := range st.Names() {
		if !strings.HasPrefix(name, "netconf/") {
			continue
		}
		cur := ""
		for _, line := range strings.Split(st.Text(name), "\n") {
			switch {
			case strings.HasPrefix(line, "%%##"):
				garbled++
			case strings.HasPrefix(line, "ecmp "):
				cur = strings.TrimPrefix(line, "ecmp ")
				conf[cur] = ""
			case strings.HasPrefix(line, "  member ") && cur != "":
				conf[cur] += strings.TrimPrefix(line, "  member ") + ","
			case !strings.HasPrefix(line, "  "):
				cur = ""
			}
		}
	}
	if len(conf) == 0 || int64(len(conf)) != m.Counts.ObjectsByType["EcmpGroup"] {
		t.Fatalf("%d groups in config, %d in truth", len(conf), m.Counts.ObjectsByType["EcmpGroup"])
	}
	if int64(garbled) != m.Imperfections.Counts["unparseable_line"]-m.Imperfections.ByFile["unparseable_line "+ChangesFile] {
		t.Fatalf("%d garbled lines, log says %v", garbled, m.Imperfections.Counts)
	}
	declared := map[string]string{}
	for _, name := range st.Names() {
		if strings.HasPrefix(name, "declared/") {
			for _, line := range strings.Split(strings.TrimSpace(st.Text(name)), "\n") {
				var d struct {
					Group   string   `json:"group"`
					Members []string `json:"members"`
				}
				if err := json.Unmarshal([]byte(line), &d); err != nil {
					t.Fatal(err)
				}
				declared[d.Group] = strings.Join(d.Members, ",") + ","
			}
		}
	}
	table := map[string]string{}
	for _, name := range st.Names() {
		if strings.HasPrefix(name, "show/") {
			for _, line := range strings.Split(strings.TrimSpace(st.Text(name)), "\n")[1:] {
				group := strings.TrimSpace(line[36:72])
				paths, _ := strconv.Atoi(strings.TrimSpace(line[72:77]))
				members := line[78:]
				if len(strings.Split(members, ",")) != paths {
					t.Fatalf("paths column disagrees with members: %q", line)
				}
				table[group] = members + ","
			}
		}
	}
	odd := 0
	for g, c := range conf {
		if declared[g] != c {
			t.Errorf("%s: config %q, declared %q", g, c, declared[g])
		}
		if table[g] != c {
			odd++
		}
	}
	if odd != int(m.Imperfections.Counts["ecmp_mismatch"]) || odd != 1 {
		t.Fatalf("table differs for %d groups, manifest records %d", odd, m.Imperfections.Counts["ecmp_mismatch"])
	}
}

// The same server is in the asset register and in the virtualization
// inventory, with different identifiers and the serial number in common.
func TestOverlapBetweenSources(t *testing.T) {
	w := smallWorld(t, 24, 30000, func(p *Params) { p.Imperfections.MissingRowRatio = 0 })
	_, st := runAll(t, w)
	serial := map[string]string{} // serial -> asset id
	for _, r := range csvRows(t, st, "servers.csv")[1:] {
		serial[r[2]] = r[0]
	}
	matched := 0
	for _, p := range readPages(t, st, "hosts") {
		for _, it := range p.Items {
			var hw struct{ Serial *string }
			var id string
			_ = json.Unmarshal(it["host_id"], &id)
			if err := json.Unmarshal(it["hardware"], &hw); err != nil {
				t.Fatal(err)
			}
			if hw.Serial != nil {
				if asset, ok := serial[*hw.Serial]; ok {
					matched++
					if asset == id {
						t.Fatal("the two sources should name the server differently")
					}
				}
			}
		}
	}
	if matched == 0 {
		t.Fatal("no host matched a server by serial number")
	}
}

// [REQ-001]
func TestViewsAreDeterministicAndOptional(t *testing.T) {
	w := smallWorld(t, 25, 10000, nil)
	_, a := runAll(t, w)
	_, b := runAll(t, w)
	for _, n := range a.Names() {
		if !bytes.Equal(a.Files[n].Bytes(), b.Files[n].Bytes()) {
			t.Fatalf("%s differs between runs", n)
		}
	}
	st := NewMemStore()
	m, err := w.Run(st, RunOptions{})
	if err != nil || len(st.Files) != 1 || m.ContentHash == "" || m.Truth != nil {
		t.Fatalf("a counting run writes only the manifest: %v %v", err, st.Names())
	}
}
