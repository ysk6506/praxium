package worldgen

import "github.com/ysk6506/praxium/backend/internal/model"

// siteCtx holds what the lower levels of a site need to refer to.
type siteCtx struct {
	s        int
	site     ref
	building ref
	ups      [2][]ref
	upsGroup [2]ref
	chillerG ref
	chillers []ref
	spines   []ref
	routers  []ref
	vmgr     ref
	vdc      ref
	vrf      ref
	envs     []ref
	teams    []ref
}

func (g *gen) siteName(s int) string { return "site-" + itoa(s) }

// siteInfra makes the site, its building, the utility power chain, the
// chillers, the spine switches, the edge routers, the management of the
// virtualization and the owners (teams and environments).
func (g *gen) siteInfra(s int) *siteCtx {
	c := &siteCtx{s: s}
	var region ref
	g.scope(ScopeSite, s, s, true)
	if r := s / g.p.SitesPerRegion; s%g.p.SitesPerRegion == 0 {
		region = g.obj(tRegion, mkid("reg", 0, r)).set(kS("name", "region-"+itoa(r)), kS("code", "rg"+itoa(r))).emit()
	} else {
		region = refOf(tRegion, mkid("reg", 0, s/g.p.SitesPerRegion))
	}
	c.site = g.obj(tSite, mkid("site", s)).set(kS("name", g.siteName(s)), kS("site_code", "sx"+itoa(100+s)),
		kS("tier_rating", "tier_3")).in(lLocatedIn, region).emit()
	c.building = g.obj(tBuilding, mkid("bld", s, 0)).set(kS("name", "building-"+itoa(s))).in(lLocatedIn, c.site).emit()
	g.sitePower(c)
	g.siteCooling(c)
	g.siteNetwork(c)
	g.siteVirt(c)
	g.siteOwners(c)
	return c
}

func (g *gen) fac(t int, id string, name string, parent ref) *builder {
	b := g.obj(t, id).set(kS("name", name)).opt(kS("manufacturer", pickOf(facMakers, hash64(id))))
	return g.locate(b, parent)
}

// locate says where an object is: in a place, or, for rack-mounted equipment
// (the caller makes the MOUNTED_IN link), nowhere else.
func (g *gen) locate(b *builder, parent ref) *builder {
	if parent.t == tRack {
		return b
	}
	return b.in(lLocatedIn, parent)
}

// sitePower makes the utility feeds, transformers, switchgear and UPS of the two sides.
func (g *gen) sitePower(c *siteCtx) {
	for side := 0; side < 2; side++ {
		sn := sideName(side)
		feed := g.obj(tUtilityFeed, mkid("ufeed", c.s, side)).set(kS("name", "feed-"+itoa(c.s)+sn)).in(lLocatedIn, c.site).emit()
		xf := g.fac(tTransformer, mkid("xfmr", c.s, side), "xfmr-"+itoa(c.s)+sn, c.site).emit()
		g.link(lPoweredBy, xf, feed, kS("feed_side", feedSide[side]), kS("path_role", "primary"))
		gear := g.fac(tSwitchgear, mkid("swgr", c.s, side), "swgr-"+itoa(c.s)+sn, c.site).emit()
		g.link(lPoweredBy, gear, xf, kS("feed_side", feedSide[side]), kS("path_role", "primary"))
		for u := 0; u < g.p.UpsPerSide; u++ {
			ups := g.fac(tUps, mkid("ups", c.s, side, u), "ups-"+itoa(c.s)+sn+itoa(u), c.site).emit()
			g.link(lPoweredBy, ups, gear, kS("feed_side", feedSide[side]), kS("path_role", "primary"))
			c.ups[side] = append(c.ups[side], ups)
		}
		g.upsBank(c, side)
	}
}

// upsBank groups the UPS of one side: with more than one, one may fail (N+1).
// The power panels of the side depend on the bank, not on a single UPS.
func (g *gen) upsBank(c *siteCtx, side int) {
	mode := "load_shared"
	if len(c.ups[side]) > 1 {
		mode = "n_plus_1"
	}
	c.upsGroup[side] = g.obj(tPowerGroup, mkid("pgrp", c.s, side)).set(kS("name", "ups-bank-"+itoa(c.s)+sideName(side)),
		kI("min_alive", max(1, len(c.ups[side])-1)), kS("mode", mode), kS("threshold_kind", "count")).in(lPartOf, c.building).emit()
	for _, ups := range c.ups[side] {
		g.link(lMemberOf, ups, c.upsGroup[side])
	}
}

// siteCooling makes the chillers: two that carry the load and a spare (N+1),
// each on the power of one side.
func (g *gen) siteCooling(c *siteCtx) {
	for i := 0; i < 3; i++ {
		ch := g.fac(tChiller, mkid("chil", c.s, i), "chiller-"+itoa(c.s)+"-"+itoa(i), c.site).
			set(kI("cooling_capacity_kw", 400)).emit()
		gear := refOf(tSwitchgear, mkid("swgr", c.s, i%2))
		g.link(lPoweredBy, ch, gear, kS("feed_side", feedSide[i%2]), kS("path_role", "primary"))
		c.chillers = append(c.chillers, ch)
	}
	c.chillerG = g.obj(tCoolGroup, mkid("cgrp", c.s)).set(kS("name", "chillers-"+itoa(c.s)), kI("min_alive", 2), kS("mode", "n_plus_1"),
		kS("threshold_kind", "count")).in(lPartOf, c.building).emit()
	for _, ch := range c.chillers {
		g.link(lMemberOf, ch, c.chillerG)
	}
}

// siteNetwork makes the spine switches, two edge routers, the routing
// instance, and the links from site to site (the sites form a ring).
func (g *gen) siteNetwork(c *siteCtx) {
	for i := 0; i < g.p.SpinesPerSite; i++ {
		sp := g.netDevice(tSwitch, mkid("sw", c.s, 0, i), "spine-"+itoa(c.s)+"-"+itoa(i), c.site, "spine")
		c.spines = append(c.spines, sp)
	}
	for i := 0; i < 2; i++ {
		rt := g.netDevice(tRouter, mkid("rtr", c.s, i), "edge-"+itoa(c.s)+"-"+itoa(i), c.site, "edge")
		c.routers = append(c.routers, rt)
	}
	pair := g.obj(tHaPair, child(c.routers[0].id, "ha", 0)).set(kS("name", "edge-pair-"+itoa(c.s)),
		kI("min_alive", 1), kS("mode", "active_active")).in(lPartOf, c.routers[0]).emit()
	for _, rt := range c.routers {
		g.link(lMemberOf, rt, pair)
	}
	for _, sp := range c.spines {
		g.link(lUplinkedVia, sp, pair, kS("network_role", "data"))
	}
	c.vrf = g.obj(tNetInstance, mkid("vrf", c.s, 0)).set(kS("name", "vrf-"+itoa(c.s)), kS("instance_type", "l3_vrf"),
		kS("route_distinguisher", "65000:"+itoa(c.s))).emit()
	g.siteRing(c)
}

// siteRing cables the edge routers of this site to the ones of the previous
// site: a cross-site network link that needs both sites.
func (g *gen) siteRing(c *siteCtx) {
	if c.s == 0 {
		return
	}
	prev := c.s - 1
	for i := 0; i < 2; i++ {
		mine := g.iface(c.routers[i], 100, "wan0", 100000)
		theirs := g.iface(refOf(tRouter, mkid("rtr", prev, i)), 101, "wan1", 100000)
		g.link(lCabledTo, mine, theirs)
	}
}

// netDevice makes a switch or router. role is the fabric role.
func (g *gen) netDevice(t int, id, name string, where ref, role string) ref {
	b := g.obj(t, id).set(kS("name", name), kS("hostname", name+".dc.example.test"), kS("fabric_role", role)).opt(kS("manufacturer", pickOf(netMakers, hash64(id))))
	return g.locate(b, where).emit()
}

// iface makes a physical interface of a device or an adapter.
func (g *gen) iface(dev ref, n int, name string, speed int) ref {
	return g.obj(tInterface, child(dev.id, "if", n)).set(kS("name", name), kS("if_name", name),
		kS("interface_kind", "physical"), kI("speed_mbps", speed)).in(lPartOf, dev).emit()
}

// siteVirt makes the virtualization manager and its datacenter.
func (g *gen) siteVirt(c *siteCtx) {
	c.vmgr = g.obj(tVirtMgr, mkid("vmgr", c.s, 0)).set(kS("name", "vmgr-"+itoa(c.s)), kS("api_endpoint", "vmgr-"+itoa(c.s)+".dc.example.test")).emit()
	c.vdc = g.obj(tVdc, mkid("vdc", c.s, 0)).set(kS("name", "vdc-"+itoa(c.s))).in(lPartOf, c.vmgr).emit()
}

var envKinds = []string{"production", "staging", "development"}

// siteOwners makes the environments and the teams of the site.
func (g *gen) siteOwners(c *siteCtx) {
	for i, k := range envKinds {
		c.envs = append(c.envs, g.obj(tEnv, mkid("env", c.s, i)).set(kS("name", k+"-"+itoa(c.s)), kS("env_kind", k)).emit())
	}
	for i := 0; i < 3; i++ {
		c.teams = append(c.teams, g.obj(tTeam, mkid("team", c.s, i)).set(kS("name", "team-"+itoa(c.s)+"-"+itoa(i)),
			kS("team_code", "tm"+itoa(c.s)+"x"+itoa(i))).emit())
	}
}

var _ = model.GradeFact
