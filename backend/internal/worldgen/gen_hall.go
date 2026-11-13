package worldgen

// hallCtx holds what the racks of a hall refer to.
type hallCtx struct {
	h     int
	room  ref
	zones []ref
	panel [2]ref
	rpp   [2]ref
	agg   [2][]ref
	crahs [][]ref
	loop  ref
}

// hall makes a hall: the room, its zones with the cooling units (N+k), the
// two power sides down to the rack feeds, and the aggregation switches of two
// planes, each with an ECMP group over its uplinks to the spines.
func (g *gen) hall(c *siteCtx, h int) *hallCtx {
	hc := &hallCtx{h: h}
	g.scope(ScopeHall, c.s, h, true)
	hc.room = g.obj(tRoom, mkid("hall", c.s, h)).set(kS("name", "hall-"+itoa(c.s)+"-"+itoa(h))).in(lLocatedIn, c.building).emit()
	g.hallPower(c, hc)
	g.hallCooling(c, hc)
	g.hallNetwork(c, hc)
	return hc
}

func (g *gen) hallPower(c *siteCtx, hc *hallCtx) {
	for side := 0; side < 2; side++ {
		sn := sideName(side)
		hc.panel[side] = g.fac(tPowerPanel, mkid("ppnl", c.s, hc.h, side), "panel-"+itoa(c.s)+"-"+itoa(hc.h)+sn, hc.room).emit()
		g.link(lPoweredBy, hc.panel[side], c.ups[side][hc.h%len(c.ups[side])], kS("feed_side", feedSide[side]), kS("path_role", "primary"))
		hc.rpp[side] = g.fac(tRpp, mkid("rpp", c.s, hc.h, side), "rpp-"+itoa(c.s)+"-"+itoa(hc.h)+sn, hc.room).emit()
		g.link(lPoweredBy, hc.rpp[side], hc.panel[side], kS("feed_side", feedSide[side]), kS("path_role", "primary"))
	}
}

// hallCooling makes, per zone, CoolingUnitsN+1 units (N+1, or N+2 for some
// zones); the units are cooled by the loop of the hall, which a chiller feeds.
func (g *gen) hallCooling(c *siteCtx, hc *hallCtx) {
	hc.loop = g.obj(tCoolingLoop, mkid("cloop", c.s, hc.h)).set(kS("name", "loop-"+itoa(c.s)+"-"+itoa(hc.h))).emit()
	for i, ch := range c.chillers {
		g.link(lCooledBy, hc.loop, ch, kD("capacity_kw", 4000+10*i))
	}
	zones := g.lay.zonesIn(c.s, hc.h)
	for z := 0; z < zones; z++ {
		zone := g.obj(tRoomZone, mkid("zone", c.s, hc.h, z)).set(kS("name", "zone-"+itoa(c.s)+"-"+itoa(hc.h)+"-"+itoa(z))).in(lLocatedIn, hc.room).emit()
		hc.zones = append(hc.zones, zone)
		k := 1
		if d := g.k.forID(zone.id); d.chance(g.p.CoolingN2Share) {
			k = 2
		}
		units := make([]ref, 0, g.p.CoolingUnitsN+k)
		for u := 0; u < g.p.CoolingUnitsN+k; u++ {
			cr := g.fac(tCrah, child(zone.id, "crah", u), "crah-"+itoa(c.s)+"-"+itoa(hc.h)+"-"+itoa(z)+"-"+itoa(u), hc.room).emit()
			g.link(lCooledBy, cr, hc.loop, kD("capacity_kw", 300))
			g.link(lPoweredBy, cr, hc.rpp[u%2], kS("feed_side", feedSide[u%2]), kS("path_role", "primary"))
			units = append(units, cr)
		}
		hc.crahs = append(hc.crahs, units)
	}
}

// hallNetwork makes the aggregation switches: two planes of AggPerPlane,
// each with uplinks to every spine and one ECMP group over them.
func (g *gen) hallNetwork(c *siteCtx, hc *hallCtx) {
	for plane := 0; plane < 2; plane++ {
		for a := 0; a < g.p.AggPerPlane; a++ {
			sw := g.netDevice(tSwitch, mkid("sw", c.s, 1+hc.h, plane*16+a), "agg-"+itoa(c.s)+"-"+itoa(hc.h)+sideName(plane)+itoa(a), hc.room, "aggregation")
			hc.agg[plane] = append(hc.agg[plane], sw)
			g.link(lPoweredBy, sw, hc.rpp[plane], kS("feed_side", feedSide[plane]), kS("path_role", "primary"))
			g.aggUplinks(c, sw)
		}
	}
}

// aggUplinks cables an aggregation switch to every spine and groups the
// uplinks in an ECMP group.
func (g *gen) aggUplinks(c *siteCtx, sw ref) {
	grp := g.obj(tEcmp, child(sw.id, "ecmp", 0)).set(kS("name", "up-"+sw.id), kI("min_alive", max(1, len(c.spines)-1)),
		kS("mode", "load_shared"), kI("configured_max_paths", len(c.spines)), kS("derived_from", "config")).in(lPartOf, sw).emit()
	for i, sp := range c.spines {
		mine := g.iface(sw, i, "up"+itoa(i), 100000)
		theirs := g.ifaceFor(sp, sw.id, i, "dn"+itoa(i)+"-"+sw.id, 100000)
		g.link(lCabledTo, mine, theirs)
		g.link(lMemberOf, mine, grp)
	}
}
