package pgcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

// pgxExports are source files made from a generated world: racks and servers (CSV), hosts and virtual machines
// (JSON Lines) and a power report (CSV).
type pgxExports map[string]string

func pgxWorld(t testing.TB, reg *model.Registry, seed uint64, scale int64) (pgxExports, []byte) {
	t.Helper()
	p := worldgen.DefaultParams(seed, scale)
	p.Imperfections = worldgen.Imperfections{}
	w, err := worldgen.Build(reg, p)
	if err != nil {
		t.Fatal(err)
	}
	st := worldgen.NewMemStore()
	if _, err := w.Run(st, worldgen.RunOptions{Truth: true, Views: true}); err != nil {
		t.Fatal(err)
	}
	truth := st.Files[worldgen.TruthFile].Bytes()
	return pgxExports{
		"ds_racks":    st.Files["racks.csv"].String(),
		"ds_servers":  st.Files["servers.csv"].String(),
		"ds_hosts":    pgxLines(t, st, "virt/hosts-"),
		"ds_vms":      pgxLines(t, st, "virt/vms-"),
		"ds_vm_power": pgxPower(truth),
	}, truth
}

func pgxLines(t testing.TB, st *worldgen.MemStore, prefix string) string {
	t.Helper()
	var names []string
	for n := range st.Files {
		if strings.HasPrefix(n, prefix) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var out strings.Builder
	for _, n := range names {
		var page struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(st.Files[n].Bytes(), &page); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		for _, it := range page.Items {
			out.Write(it)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func pgxPower(truth []byte) string {
	var out strings.Builder
	out.WriteString("instance_uuid,power_state\n")
	for _, line := range bytes.Split(truth, []byte("\n")) {
		var rec struct {
			Type  string            `json:"type"`
			Props map[string]any    `json:"props"`
			Ids   map[string]string `json:"identifiers"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "ot.virtual_machine" {
			continue
		}
		if id, ok := rec.Ids["is.vm_instance_uuid"]; ok {
			fmt.Fprintf(&out, "%s,%v\n", id, rec.Props["power_state"])
		}
	}
	return out.String()
}
