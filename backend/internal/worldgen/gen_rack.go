package worldgen

// rack makes a rack with its two feeds, its two ToR switches, its servers
// and, in a storage rack, an array.
func (g *gen) rack(c *siteCtx, hc *hallCtx, a rackAddr) {
	g.scope(ScopeRack, c.s, a.rk, true)
	row := refOf(tRow, mkid("row", c.s, a.hall, a.row))
	if a.k == 0 {
		g.obj(tRow, row.id).set(kS("name", "row-"+itoa(a.row))).in(lLocatedIn, hc.zones[a.row/g.p.RowsPerZone]).emit()
	}
	rk := g.obj(tRack, a.id()).set(kS("name", "rack-"+itoa(a.s)+"-"+itoa(a.hall)+"-"+itoa(a.rk)), kI("total_u", 42)).in(lLocatedIn, row).emit()
	units := hc.crahs[a.row/g.p.RowsPerZone]
	for i := 0; i < 2; i++ {
		g.link(lCooledBy, rk, units[(a.rk+i)%len(units)], kD("capacity_kw", 80))
	}
	var pdu, tor [2]ref
	for side := 0; side < 2; side++ {
		pdu[side] = g.fac(tRackPdu, child(rk.id, "pdu", side), "pdu-"+rk.id+sideName(side), rk).emit()
		g.link(lMountedIn, pdu[side], rk, kS("face", "rear"), kS("side", sideSide[side]))
		g.link(lPoweredBy, pdu[side], hc.rpp[side], kS("feed_side", feedSide[side]), kS("path_role", "primary"))
		tor[side] = g.torSwitch(c, hc, rk, side, pdu[side])
	}
	n := g.lay.serversIn(a.s, a.hall, a.rk)
	for srv := 0; srv < n; srv++ {
		g.server(c, a, rk, srv, pdu, tor)
	}
	if g.lay.isStorageRack(a.rk) {
		g.storageArray(c, hc, a, rk, pdu)
	}
	g.scope(ScopeRack, c.s, a.rk, false)
}

// torSwitch makes the top-of-rack switch of one side. Its two uplinks go to
// two aggregation switches of the plane and form a LAG.
func (g *gen) torSwitch(c *siteCtx, hc *hallCtx, rk ref, side int, pdu ref) ref {
	sw := g.netDevice(tSwitch, child(rk.id, "tor", side), "tor-"+rk.id+sideName(side), rk, "access")
	g.link(lMountedIn, sw, rk, kS("face", "rear"), kS("side", "full"))
	g.link(lPoweredBy, sw, pdu, kS("feed_side", feedSide[side]), kS("path_role", "primary"))
	lag := g.obj(tLag, child(sw.id, "lag", 0)).set(kS("name", "po1"), kI("min_alive", 1), kS("mode", "active_active"),
		kI("configured_min_links", 1), kS("lag_kind", "lacp")).in(lPartOf, sw).emit()
	aggs := hc.agg[side]
	for i := 0; i < 2; i++ {
		up := g.iface(sw, i, "up"+itoa(i), 25000)
		ag := aggs[(g.rackSeed(rk)+i)%len(aggs)]
		down := g.ifaceFor(ag, sw.id, i, "dn"+itoa(i)+"-"+sw.id, 25000)
		g.link(lCabledTo, up, down)
		g.link(lMemberOf, up, lag)
	}
	return sw
}

func (g *gen) rackSeed(rk ref) int { return int(hash64(rk.id) >> 40) }
