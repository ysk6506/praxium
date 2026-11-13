package worldgen

import (
	"strconv"
	"strings"
)

// grp is a LAG or an ECMP group as a device lists it.
type grp struct {
	id, name string
	max      string
	members  []string
	ecmp     bool
}

type ifRec struct{ id, name, speed string }

// devBuf collects a network device and its interfaces and groups, until the
// device is complete (its rack, hall or site ends).
type devBuf struct {
	id, host, role string
	ifs            []ifRec
	groups         []*grp
}

// netState holds the devices of the current rack, hall and site.
type netState struct {
	devs     map[string]*devBuf
	pending  [3][]string
	depth    int
	mismatch bool
	order    int
}

func (n *netState) addDevice(v *Views, o *Object) {
	n.devs[o.ID] = &devBuf{id: o.ID, host: o.str("hostname"), role: o.str("fabric_role")}
	// depth 2 is a rack, 1 a hall, 0 the site
	n.pending[n.depth] = append(n.pending[n.depth], o.ID)
}

func (n *netState) addPart(o *Object) {
	dev := n.devs[deviceOfIface(o.ID)]
	if o.T.idx != tInterface {
		dev = n.devs[parentID(o.ID)]
	}
	if dev == nil {
		return
	}
	switch o.T.idx {
	case tInterface:
		dev.ifs = append(dev.ifs, ifRec{o.ID, o.str("if_name"), o.str("speed_mbps")})
	default:
		dev.groups = append(dev.groups, &grp{id: o.ID, name: o.str("name"), max: o.str("configured_max_paths"), ecmp: o.T.idx == tEcmp})
	}
}

// member puts an interface into the group it belongs to.
func (n *netState) member(l *Link) {
	dev := n.devs[deviceOfIface(l.From.id)]
	if dev == nil {
		return
	}
	name := ""
	for _, i := range dev.ifs {
		if i.id == l.From.id {
			name = i.name
		}
	}
	for _, g := range dev.groups {
		if g.id == l.To.id {
			g.members = append(g.members, name)
		}
	}
}

// flush writes the devices that a rack (2), hall (1) or site (0) closes.
func (n *netState) flush(v *Views, depth int) {
	for _, id := range n.pending[depth] {
		if dev := n.devs[id]; dev != nil {
			n.write(v, dev)
			delete(n.devs, id)
		}
	}
	n.pending[depth] = n.pending[depth][:0]
}

func siteFile(prefix string, id, suffix string) string {
	return prefix + "-site-" + strconv.Itoa(siteOf(id)) + suffix
}

// write renders one device in three ways: its configuration (hierarchical
// text), its rows in the command-output table, and its declared groups.
func (n *netState) write(v *Views, dev *devBuf) {
	cfg := v.fs.open(siteFile("netconf/config", dev.id, ".cfg"), SrcNetconf, "device-config")
	b := append(v.buf[:0], "hostname "+dev.host+"\n! role "+dev.role+"\n"...)
	garbage := v.draw(SrcNetconf, dev.id)
	for _, i := range dev.ifs {
		b = append(b, "interface "+i.name+"\n  speed "+i.speed+"\n  description "+tagOf(i.id)+"\n"...)
	}
	for _, g := range dev.groups {
		kind := "lag"
		if g.ecmp {
			kind = "ecmp"
		}
		b = append(b, kind+" "+g.name+"\n"...)
		if g.ecmp {
			b = append(b, "  max-paths "+g.max+"\n"...)
		}
		for _, m := range g.members {
			b = append(b, "  member "+m+"\n"...)
		}
	}
	if garbage.chance(v.p.Imperfections.UnparseableRatio) {
		b = append(b, "%%## garbled "+tagOf(dev.id)+" ##%%\n"...)
		v.log.add(Imperfection{"unparseable_line", SrcNetconf, cfg.info.Name, dev.id, "the line is rejected and counted; the rest of the device block is still read"})
	}
	b = append(b, "!\n"...)
	cfg.write(b)
	v.Rows["netconf/devices"]++
	n.groupViews(v, dev)
}

// groupViews writes the ECMP groups of a device to the table and the declared list.
func (n *netState) groupViews(v *Views, dev *devBuf) {
	for _, g := range dev.groups {
		if !g.ecmp {
			continue
		}
		tbl := v.fs.open(siteFile("show/ecmp", dev.id, ".txt"), SrcShow, "fixed-table")
		if tbl.info.Lines == 0 {
			tbl.write([]byte(fixedRow("DEVICE", "GROUP", "PATHS", "MEMBERS") + "\n"))
		}
		members := g.members
		if v.p.Imperfections.EcmpMismatch && !n.mismatch && len(members) > 1 {
			n.mismatch = true
			members = members[:len(members)-1]
			v.log.add(Imperfection{"ecmp_mismatch", SrcShow, tbl.info.Name, g.id, "the table lists one member less than the configuration and the declared list; the group is derived from the two that agree and flagged as inconsistent"})
		}
		tbl.write([]byte(fixedRow(dev.host, g.name, strconv.Itoa(len(members)), strings.Join(members, ",")) + "\n"))
		decl := v.fs.open(siteFile("declared/ecmp", dev.id, ".jsonl"), SrcDeclared, "jsonl")
		b := append(v.buf[:0], `{"device":`...)
		b = appendJSONString(b, dev.host)
		b = append(b, `,"group":`...)
		b = appendJSONString(b, g.name)
		b = append(b, `,"members":[`...)
		for i, m := range g.members {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSONString(b, m)
		}
		decl.write(append(b, "]}\n"...))
		v.Rows["show/ecmp"]++
	}
}

// fixedRow lays out a row of the command-output table in fixed columns:
// device 0-35, group 36-71, paths 72-77, members from 78.
func fixedRow(device, group, paths, members string) string {
	return pad(device, 36) + pad(group, 36) + padLeft(paths, 5) + " " + members
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s[:n-1] + " "
	}
	return s + strings.Repeat(" ", n-len(s))
}

func padLeft(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}
