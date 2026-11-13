package worldgen

// rackFeeds is what the servers of a rack depend on: the PDUs and ToR
// switches of the two sides, and the MLAG domain of the ToR pair.
type rackFeeds struct {
	pdu, tor [2]ref
	mlag     ref
}

// rack makes a rack with its two feeds, its two ToR switches, its servers
// and, in a storage rack, an array.
func (g *gen) rack(c *siteCtx, hc *hallCtx, a rackAddr) {
	g.scope(ScopeRack, c.s, a.rk, true)
	row := refOf(tRow, mkid("row", c.s, a.hall, a.row))
	if a.k == 0 {
		g.obj(tRow, row.id).set(kS("name", "row-"+itoa(a.row))).in(lLocatedIn, hc.zones[a.row/g.p.RowsPerZone]).emit()
	}
	rk := g.obj(tRack, a.id()).set(kS("name", "rack-"+itoa(a.s)+"-"+itoa(a.hall)+"-"+itoa(a.rk)), kI("total_u", 42)).in(lLocatedIn, row).emit()
	g.link(lCooledBy, rk, hc.cgrps[a.row/g.p.RowsPerZone], kD("capacity_kw", 160))
	var pdu, tor [2]ref
	for side := 0; side < 2; side++ {
		pdu[side] = g.fac(tRackPdu, child(rk.id, "pdu", side), "pdu-"+rk.id+sideName(side), rk).emit()
		g.link(lMountedIn, pdu[side], rk, kS("face", "rear"), kS("side", sideSide[side]))
		g.link(lPoweredBy, pdu[side], hc.rpp[side], kS("feed_side", feedSide[side]), kS("path_role", "primary"))
		tor[side] = g.torSwitch(c, hc, rk, side, pdu[side])
	}
	g.mlagPair(rk, tor)
	mlag := refOf(tMlag, child(rk.id, "mlag", 0))
	n := g.lay.serversIn(a.s, a.hall, a.rk)
	for srv := 0; srv < n; srv++ {
		g.server(c, a, rk, srv, rackFeeds{pdu, tor, mlag})
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
	g.link(lUplinkedVia, sw, lag, kS("network_role", "data"))
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

// mlagPair makes the MLAG domain of the two ToR switches of a rack: the pair
// is named by its two members, and the servers depend on the pair.
func (g *gen) mlagPair(rk ref, tor [2]ref) {
	dom := g.obj(tMlag, child(rk.id, "mlag", 0)).set(kS("name", "mlag-"+rk.id), kI("min_alive", 1), kS("mode", "active_active"),
		kS("threshold_kind", "count"), kS("domain_id", itoa(1+int(g.rackSeed(rk)%4000)))).members(lMemberOf).emit()
	for side := 0; side < 2; side++ {
		g.link(lMemberOf, tor[side], dom)
	}
}

func (g *gen) rackSeed(rk ref) int { return int(hash64(rk.id) >> 40) }
