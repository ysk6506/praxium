package worldgen

// server makes a physical server in a rack with its components: a BMC, CPUs,
// memory, drives, two power supplies (a 2N group), fans (an N+1 group) and a
// network adapter with a port on each ToR switch.
func (g *gen) server(c *siteCtx, a rackAddr, rk ref, srv int, f rackFeeds) {
	sv := g.obj(tServer, serverID(a, srv)).set(kS("name", "srv-"+a.id()+"-"+itoa(srv)),
		kS("manufacturer", pickOf(serverMakers, hash64(a.id())+uint64(srv)))).emit()
	g.link(lMountedIn, sv, rk, kS("face", "front"), kS("side", "full"))
	bmc := g.obj(tBmc, child(sv.id, "bmc", 0)).set(kS("name", "bmc-"+sv.id)).in(lPartOf, sv).emit()
	g.link(lManagedBy, sv, bmc)
	for i := 0; i < 2; i++ {
		g.obj(tCpu, child(sv.id, "cpu", i)).set(kS("socket_id", "cpu"+itoa(i))).in(lPartOf, sv).emit()
	}
	for i := 0; i < g.p.MemoryModules; i++ {
		g.obj(tMemory, child(sv.id, "mem", i)).set(kS("slot_name", "slot"+itoa(i))).in(lPartOf, sv).emit()
	}
	for i := 0; i < g.p.Drives; i++ {
		g.obj(tDrive, child(sv.id, "drv", i)).set(kS("slot_name", "slot"+itoa(i))).in(lPartOf, sv).emit()
	}
	g.dualPower(sv, f.pdu, true)
	g.serverFans(sv)
	g.serverNetwork(sv, f.tor, f.mlag)
	g.hostOf(c, a, sv, srv)
}

// dualPower gives a device two power supplies, one on each PDU of its rack,
// in a 2N group (one is enough); the device depends on the group, not on a
// PDU, so one PDU going down costs the device its redundancy and no more.
// The supplies of a server are parts of it; the standard model has no
// PART_OF from a power supply to a storage array, so there they stand alone
// and are known by the group and by the PDU that feeds them.
func (g *gen) dualPower(dev ref, pdu [2]ref, holdsPsu bool) {
	grp := g.obj(tPowerGroup, child(dev.id, "pg", 0)).set(kS("name", "psu-"+dev.id), kI("min_alive", 1), kS("mode", "two_n"),
		kS("threshold_kind", "count")).in(lPartOf, dev).emit()
	for side := 0; side < 2; side++ {
		b := g.obj(tPsu, child(dev.id, "psu", side)).set(kS("slot_name", "psu"+itoa(side)))
		if holdsPsu {
			b = b.in(lPartOf, dev)
		}
		psu := b.emit()
		g.link(lPoweredBy, psu, pdu[side], kS("feed_side", feedSide[side]), kS("path_role", "primary"))
		g.link(lMemberOf, psu, grp)
	}
	g.link(lPoweredBy, dev, grp)
}

func (g *gen) serverFans(sv ref) {
	grp := g.obj(tCoolGroup, child(sv.id, "rg", 0)).set(kS("name", "fans-"+sv.id), kI("min_alive", g.p.Fans-1),
		kS("mode", "n_plus_1"), kS("threshold_kind", "count")).in(lPartOf, sv).emit()
	for i := 0; i < g.p.Fans; i++ {
		fan := g.obj(tFan, child(sv.id, "fan", i)).set(kS("slot_name", "slot"+itoa(i))).in(lPartOf, sv).emit()
		g.link(lMemberOf, fan, grp)
	}
	g.link(lCooledBy, sv, grp)
}

// serverNetwork makes the adapter and its two ports, one cabled to each ToR.
func (g *gen) serverNetwork(sv ref, tor [2]ref, mlag ref) {
	nic := g.obj(tNic, child(sv.id, "nic", 0)).set(kS("name", "nic-"+sv.id)).in(lPartOf, sv).emit()
	for side := 0; side < 2; side++ {
		port := g.iface(nic, side, "p"+itoa(side), 25000)
		far := g.ifaceFor(tor[side], sv.id, side, "dn-"+sv.id, 25000)
		g.link(lCabledTo, port, far)
	}
	g.link(lUplinkedVia, sv, mlag, kS("network_role", "data"))
}
