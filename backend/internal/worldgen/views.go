package worldgen

import (
	"strconv"
	"strings"
)

// Views renders the world as several invented source systems while it is
// generated (docs/design-docs/worldgen.md). It is an Observer; it keeps only
// what one rack, one hall or one VM needs, so its memory does not grow with
// the world.
type Views struct {
	p    Params
	k    worldKeys
	fs   *fileSet
	log  *ImperfectionLog
	buf  []byte
	srv  srvInfo
	virt virtState
	net  netState
	err  error

	// Rows holds what was delivered per source file, for the manifest.
	Rows map[string]int64
}

type srvInfo struct{ id, serial string }

// NewViews makes the view writer. The files go to the store.
func NewViews(p Params, w *World, st Store) *Views {
	v := &Views{p: p, k: w.keys, fs: newFileSet(st), log: newImperfectionLog(), Rows: map[string]int64{}}
	v.net.devs = map[string]*devBuf{}
	v.virt.vms.base, v.virt.hosts.base = "vms", "hosts"
	return v
}

// Log returns the flaws that were made.
func (v *Views) Log() *ImperfectionLog { return v.log }

// Close ends the files and returns them.
func (v *Views) Close() ([]FileInfo, error) {
	v.virt.finish(v)
	return v.fs.closeAll()
}

// draw gives the flaw draw of an object: independent of everything else.
func (v *Views) draw(tag, id string) draw { return newDraw(mix(v.k.view^hash64(tag), hash64(id))) }

// Scope flushes what a scope closes.
func (v *Views) Scope(kind Scope, site, index int, begin bool) error {
	v.virt.flushVM(v)
	switch {
	case kind == ScopeRack && begin:
		v.net.depth = 2
	case kind == ScopeRack:
		v.net.flush(v, 2)
		v.net.depth = 1
	case kind == ScopeHall && begin:
		v.net.depth = 1
	case kind == ScopeHall:
		v.net.flush(v, 1)
		v.net.depth = 0
	case kind == ScopeSite && !begin:
		v.net.flush(v, 0)
	}
	return v.err
}

// Object routes an object to the sources that carry its type.
func (v *Views) Object(o *Object) error {
	if v.virt.vm != nil && !strings.HasPrefix(o.ID, v.virt.vm.id+"/") {
		v.virt.flushVM(v)
	}
	switch o.T.idx {
	case tRack:
		v.rackRow(o)
	case tServer:
		v.serverRow(o)
	case tSwitch, tRouter:
		v.deviceRow(o)
		v.net.addDevice(v, o)
	case tHost:
		v.virt.host(v, o, v.srv)
	case tVM:
		v.virt.startVM(v, o)
	case tOS, tVNic, tVDisk:
		v.virt.child(o)
	case tInterface, tLag, tEcmp:
		v.net.addPart(o)
	}
	return v.err
}

// Link collects what the configs need: which interface is in which group.
func (v *Views) Link(l *Link) error {
	if l.T.idx == lMemberOf && l.From.t == tInterface && (l.To.t == tLag || l.To.t == tEcmp) {
		v.net.member(l)
	}
	return v.err
}

func (v *Views) csv(name, header string, f func(b []byte) []byte) {
	o := v.fs.open(name, SrcRegister, "csv")
	if o.info.Lines == 0 {
		o.write([]byte(header + "\n"))
	}
	o.write(f(v.buf[:0]))
}

// dropRow decides the flaws of a row of a table source: it returns how many
// times the row is written (0 missing, 1 normal, 2 duplicated).
func (v *Views) dropRow(source, file string, o *Object) int {
	d := v.draw(source, o.ID)
	if d.chance(v.p.Imperfections.MissingRowRatio) {
		v.log.add(Imperfection{"missing_row", source, file, o.ID, "the object is absent from this source only; it must not be treated as deleted"})
		return 0
	}
	if d.chance(v.p.Imperfections.DuplicateKeyRatio) {
		v.log.add(Imperfection{"duplicate_key", source, file, o.ID, "two rows with one key give one object; the properties of the last row win"})
		return 2
	}
	return 1
}

func (v *Views) rackRow(o *Object) {
	n := v.dropRow(SrcRegister, "racks.csv", o)
	for i := 0; i < n; i++ {
		v.csv("racks.csv", "asset_id,rack_name,site_no,total_u", func(b []byte) []byte {
			return appendCSV(b, srcID(SrcRegister, "RK", o.ID), o.str("name"), strconv.Itoa(siteOf(o.ID)), o.str("total_u"))
		})
		v.Rows["racks.csv"]++
	}
}

func (v *Views) serverRow(o *Object) {
	v.srv = srvInfo{o.ID, o.str("serial_number")}
	n := v.dropRow(SrcRegister, "servers.csv", o)
	head := "asset_id,hostname,serial_number,manufacturer,model,rack_asset_id,power_state"
	if v.p.Imperfections.ShapeDrift {
		head = strings.Replace(head, "serial_number", "serial_no", 1)
	}
	for i := 0; i < n; i++ {
		v.csv("servers.csv", head, func(b []byte) []byte {
			state := o.str("power_state")
			if i == 1 {
				state = "off"
			}
			return appendCSV(b, srcID(SrcRegister, "SV", o.ID), o.str("hostname"), o.str("serial_number"), o.str("manufacturer"),
				o.str("model"), srcID(SrcRegister, "RK", parentID(o.ID)), state)
		})
		v.Rows["servers.csv"]++
	}
	if v.p.Imperfections.ShapeDrift && v.Rows["servers.csv"] == 1 {
		v.log.add(Imperfection{"shape_drift", SrcRegister, "servers.csv", "serial_number", "the column is named serial_no; a reader maps it by alias or quarantines the column, and does not read serial numbers as empty"})
	}
}

func (v *Views) deviceRow(o *Object) {
	n := v.dropRow(SrcRegister, "switches.csv", o)
	rack := ""
	if strings.Contains(o.ID, "/tor") {
		rack = srcID(SrcRegister, "RK", parentID(o.ID))
	}
	for i := 0; i < n; i++ {
		v.csv("switches.csv", "asset_id,hostname,serial_number,manufacturer,device_role,rack_asset_id,management_address", func(b []byte) []byte {
			return appendCSV(b, srcID(SrcRegister, "NW", o.ID), o.str("hostname"), o.str("serial_number"), o.str("manufacturer"),
				o.str("fabric_role"), rack, o.str("management_address"))
		})
		v.Rows["switches.csv"]++
	}
}
