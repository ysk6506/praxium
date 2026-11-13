package worldgen

import "strconv"

// child is the id of a part of an object: "<parent>/<tag><n>".
func child(parent, tag string, n int) string {
	var buf [64]byte
	b := append(buf[:0], parent...)
	b = append(b, '/')
	b = append(b, tag...)
	b = strconv.AppendInt(b, int64(n), 10)
	return string(b)
}

func itoa(n int) string { return strconv.Itoa(n) }

// pickOf chooses from a list by hash.
func pickOf(list []string, h uint64) string { return list[h%uint64(len(list))] }

// sideName is "a" or "b".
func sideName(side int) string { return string(rune('a' + side)) }

// side letters for feed_side values.
var feedSide = [2]string{"a", "b"}

// rackAddr is the address of a rack: site, hall, row, place in the row, and
// its index in the hall.
type rackAddr struct{ s, hall, row, k, rk int }

func (a rackAddr) id() string { return mkid("rack", a.s, a.hall, a.row, a.k) }

// rackAddrAt finds the address of rack rk of a hall.
func (l *Layout) rackAddrAt(s, hall, rk int) rackAddr {
	row, k := l.rowOfRack(s, hall, rk)
	return rackAddr{s, hall, row, k, rk}
}

func serverID(a rackAddr, srv int) string { return child(a.id(), "srv", srv) }

func vmID(a rackAddr, srv, v int) string { return child(serverID(a, srv), "vm", v) }

// refOf makes the ref of an object of a type with the id the layout computes.
func refOf(t int, id string) ref { return ref{id: id, t: t} }

var sideSide = [2]string{"left", "right"}

// ifaceFor makes an interface of a device that is named after the peer it
// is cabled to, so two peers never make the same interface.
func (g *gen) ifaceFor(dev ref, peer string, i int, name string, speed int) ref {
	return g.obj(tInterface, dev.id+"/if@"+peer+"."+itoa(i)).set(kS("name", name), kS("if_name", name),
		kS("interface_kind", "physical"), kI("speed_mbps", speed)).in(lPartOf, dev).emit()
}
