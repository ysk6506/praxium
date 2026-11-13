package worldgen

// level is the depth of a service in the call graph: callers have a lower
// level than callees, so the graph is acyclic until cycles are added on purpose.
func (g *gen) level(s, i int) int {
	return int(mix(g.k.service, uint64(s)<<32|uint64(i)) % uint64(g.p.ServiceDepth))
}

// vmAt is the address of a VM picked from the draw.
type vmAt struct {
	a      rackAddr
	srv, v int
	ok     bool
}

// pickVM picks a VM of a site: a random rack, a random server, and if that
// is not a host with VMs, a few more tries.
func (g *gen) pickVM(s int, d *draw) vmAt {
	sh := g.lay.Sites[s]
	for try := 0; try < 40 && sh.Racks > 0; try++ {
		hall, rk := g.lay.rackAt(s, d.intn(sh.Racks))
		srv := d.intn(g.lay.serversIn(s, hall, rk))
		if g.lay.isHost(s, hall, rk, srv) {
			if n := g.lay.vmsOn(s, hall, rk, srv); n > 0 {
				return vmAt{a: g.lay.rackAddrAt(s, hall, rk), srv: srv, v: d.intn(n), ok: true}
			}
		}
	}
	return vmAt{}
}

// services makes the service layer of a site.
func (g *gen) services(c *siteCtx) {
	n := g.lay.Sites[c.s].Services
	g.scope(ScopeServices, c.s, 0, true)
	for i := 0; i < n; i++ {
		g.service(c, i)
	}
	for i := 0; i < n; i += 8 {
		g.businessService(c, i, min(i+8, n))
	}
	g.scope(ScopeServices, c.s, 0, false)
}

func (g *gen) businessService(c *siteCtx, from, to int) {
	bs := g.obj(tBizService, mkid("bsvc", c.s, from/8)).set(kS("name", "biz-"+itoa(c.s)+"-"+itoa(from/8))).emit()
	g.link(lOwnedBy, bs, c.teams[(from/8)%len(c.teams)])
	g.link(lRealizedBy, bs, refOf(tTechService, mkid("tsvc", c.s, 0)))
	for i := from; i < to; i++ {
		g.link(lRealizedBy, bs, refOf(tAppService, mkid("asvc", c.s, i)))
	}
}

// service makes an application service with its member software instances
// (a k-of-n group), its owner, and its dependencies.
func (g *gen) service(c *siteCtx, i int) {
	id := mkid("asvc", c.s, i)
	d := g.k.forID(id)
	app := g.obj(tAppService, id).set(kS("service_code", "svc-"+itoa(c.s)+"-"+itoa(i)), kS("name", "service-"+itoa(c.s)+"-"+itoa(i)),
		kB("user_facing", i%3 == 0)).emit()
	g.link(lOwnedBy, app, c.teams[d.intn(len(c.teams))])
	g.link(lMemberOf, app, c.envs[d.intn(len(c.envs))])
	members := 2 + d.intn(3)
	k := members - 1
	if d.chance(g.p.StrictServiceShare) {
		k = members
	}
	g.members = g.members[:0]
	var firstVM vmAt
	for m := 0; m < members; m++ {
		at := g.pickVM(c.s, &d)
		if !at.ok {
			continue
		}
		vm := vmID(at.a, at.srv, at.v)
		sw := g.obj(tSoftware, mkid("swi", c.s, i, m)).set(kS("software_kind", "app_server")).in(lRunsOn, refOf(tOS, osID(vm))).emit()
		g.link(lRealizedBy, app, sw)
		if len(g.members) == 0 {
			firstVM = at
		}
		g.members = append(g.members, sw)
	}
	if len(g.members) > 0 {
		g.balancer(c, i, app, k)
		if d.chance(g.p.DatabaseShare) {
			g.database(c, i, app, firstVM, &d)
		}
	}
	g.dependencies(c, i, app, &d)
}

// balancer makes the pool that needs k of the members of a service (the k-of-n
// group the standard model can key) and, for every third service, a virtual server.
func (g *gen) balancer(c *siteCtx, i int, app ref, k int) {
	pool := g.obj(tLbPool, mkid("lbp", c.s, i)).set(kS("name", "pool-"+app.id), kI("min_alive", min(k, len(g.members))),
		kS("mode", "n_plus_k"), kS("threshold_kind", "count")).in(lPartOf, g.members[0]).emit()
	g.link(lRealizedBy, app, pool)
	if i%3 == 0 {
		vs := g.obj(tLbServer, mkid("lbv", c.s, i)).set(kS("name", "vip-"+app.id)).in(lRunsOn, g.members[0]).emit()
		g.link(lForwardsTo, vs, pool)
		g.link(lRealizedBy, app, vs)
	}
	for m, sw := range g.members {
		pm := g.obj(tLbMember, mkid("lbm", c.s, i, m)).set(kS("name", "m"+itoa(m))).in(lMemberOf, pool).emit()
		g.link(lForwardsTo, pm, sw)
	}
}

// database makes a two-instance database cluster behind a service.
func (g *gen) database(c *siteCtx, i int, app ref, at vmAt, d *draw) {
	cl := g.obj(tDbCluster, mkid("dbc", c.s, i)).set(kS("name", "dbc-"+app.id), kI("min_alive", 1), kS("mode", "active_standby"),
		kS("threshold_kind", "count")).emit()
	for m := 0; m < 2; m++ {
		w := at
		if m == 1 {
			if w = g.pickVM(c.s, d); !w.ok {
				w = at
			}
		}
		db := g.obj(tDbInstance, mkid("dbi", c.s, i, m)).set(kS("name", "dbi"+itoa(m))).in(lRunsOn, refOf(tOS, osID(vmID(w.a, w.srv, w.v)))).emit()
		g.link(lMemberOf, db, cl)
		g.link(lStoresDataOn, db, refOf(tDatastore, datastoreID(w.a.s, w.a.hall, g.lay.clusterOf(w.a.rk))))
		g.link(lDependsOn, app, db)
	}
}

// dependencies makes the calls of a service to services of a deeper level,
// some in another site, and now and then a call back that closes a cycle.
func (g *gen) dependencies(c *siteCtx, i int, app ref, d *draw) {
	n := int(g.p.DependencyDegree)
	if d.chance(g.p.DependencyDegree - float64(n)) {
		n++
	}
	var done [8]string
	for k := 0; k < n && k < len(done); k++ {
		t, j, ok := g.callee(c.s, i, d)
		if !ok {
			continue
		}
		to := refOf(tAppService, mkid("asvc", t, j))
		if containsString(done[:k], to.id) {
			continue
		}
		done[k] = to.id
		g.link(lCalls, app, to)
		if d.chance(g.p.CycleRatio) {
			g.link(lCalls, to, app)
		}
	}
}

// callee picks a deeper service, in this site or (by CrossSiteRatio) in another.
func (g *gen) callee(s, i int, d *draw) (t, j int, ok bool) {
	t = s
	if len(g.lay.Sites) > 1 && d.chance(g.p.CrossSiteRatio) {
		t = (s + 1 + d.intn(len(g.lay.Sites)-1)) % len(g.lay.Sites)
	}
	n := g.lay.Sites[t].Services
	for try := 0; try < 8 && n > 0; try++ {
		j = d.intn(n)
		if g.level(t, j) > g.level(s, i) {
			return t, j, true
		}
	}
	return 0, 0, false
}
