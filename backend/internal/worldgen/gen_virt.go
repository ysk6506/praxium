package worldgen

func clusterID(s, hall, cl int) string   { return mkid("clu", s, hall, cl) }
func datastoreID(s, hall, cl int) string { return mkid("dstr", s, hall, cl) }
func hostID(a rackAddr, srv int) string  { return child(serverID(a, srv), "hyp", 0) }
func osID(vm string) string              { return child(vm, "os", 0) }

// cluster makes the compute cluster that starts at a rack of a hall: its
// capacity redundancy is N+1 or N+2 over the hosts, and its datastore is
// backed by a volume of the array of the storage rack before it.
func (g *gen) cluster(c *siteCtx, hc *hallCtx, cl int) {
	hosts := g.lay.clusterHosts(c.s, hc.h, cl)
	if hosts == 0 {
		return
	}
	g.scope(ScopeCluster, c.s, cl, true)
	first := cl * g.p.RacksPerCluster
	spare, mode := 1, "n_plus_1"
	if d := g.k.forID(clusterID(c.s, hc.h, cl)); d.chance(g.p.ClusterSpare2Share) {
		spare, mode = 2, "n_plus_k"
	}
	cr := g.obj(tCluster, clusterID(c.s, hc.h, cl)).set(kS("name", "cluster-"+itoa(c.s)+"-"+itoa(hc.h)+"-"+itoa(cl)),
		kI("min_alive", max(1, hosts-spare)), kS("mode", mode), kS("threshold_kind", "count"), kB("ha_enabled", true),
		kI("failover_capacity_hosts", spare)).in(lPartOf, c.vdc).emit()
	g.link(lManagedBy, cr, c.vmgr)
	ds := g.obj(tDatastore, datastoreID(c.s, hc.h, cl)).set(kS("name", "ds-"+itoa(hc.h)+"-"+itoa(cl)), kS("datastore_kind", "block_fs")).in(lPartOf, c.vdc).emit()
	g.link(lManagedBy, ds, c.vmgr)
	if g.lay.P.StorageRackEvery > 0 {
		st := (first / g.p.StorageRackEvery) * g.p.StorageRackEvery
		a := g.lay.rackAddrAt(c.s, hc.h, st)
		g.link(lBackedBy, ds, refOf(tVolume, volumeID(a, cl%volumesPerArray)))
	}
	g.scope(ScopeCluster, c.s, cl, false)
}

// hostOf turns a server into a hypervisor host (when the layout says so),
// with the VMs that run on it.
func (g *gen) hostOf(c *siteCtx, a rackAddr, sv ref, srv int) {
	if !g.lay.isHost(a.s, a.hall, a.rk, srv) {
		return
	}
	cl := g.lay.clusterOf(a.rk)
	hp := g.obj(tHost, hostID(a, srv)).set(kS("name", "hyp-"+sv.id)).in(lRunsOn, sv).emit()
	g.link(lMemberOf, hp, refOf(tCluster, clusterID(a.s, a.hall, cl)))
	g.link(lManagedBy, hp, c.vmgr)
	g.link(lMounts, hp, refOf(tDatastore, datastoreID(a.s, a.hall, cl)))
	n := g.lay.vmsOn(a.s, a.hall, a.rk, srv)
	for v := 0; v < n; v++ {
		g.vm(c, a, hp, srv, v, cl)
	}
}

func (g *gen) vm(c *siteCtx, a rackAddr, hp ref, srv, v, cl int) {
	id := vmID(a, srv, v)
	vm := g.obj(tVM, id).set(kS("name", "vm-"+id)).in(lRunsOn, hp).emit()
	g.link(lManagedBy, vm, c.vmgr)
	g.link(lMemberOf, vm, c.envs[int(hash64(id)%3)])
	os := g.obj(tOS, osID(id)).set(kS("name", "os-"+id)).in(lRunsOn, vm).emit()
	g.obj(tVNic, child(id, "vnic", 0)).set(kS("name", "eth0")).in(lPartOf, vm).emit()
	vd := g.obj(tVDisk, child(id, "vdsk", 0)).set(kS("name", "disk0")).in(lPartOf, vm).emit()
	g.link(lBackedBy, vd, refOf(tDatastore, datastoreID(a.s, a.hall, cl)))
	if v == 0 && g.lay.hasNode(a.s, a.hall, a.rk, srv) {
		g.node(c, a, vm, os)
	}
}

// node makes a container cluster node on a VM, with its pods.
func (g *gen) node(c *siteCtx, a rackAddr, vm, os ref) {
	nd := g.obj(tNode, child(vm.id, "node", 0)).set(kS("name", "node-"+vm.id)).in(lPartOf, refOf(tK8s, mkid("k8s", a.s, a.hall))).emit()
	g.link(lRealizedBy, nd, vm)
	g.link(lMemberOf, nd, refOf(tControlPlane, mkid("cp", a.s, a.hall)))
	for i := 0; i < g.p.PodsPerNode; i++ {
		pd := g.obj(tPod, child(nd.id, "pod", i)).set(kS("name", "pod-"+nd.id+itoa(i))).in(lPartOf, refOf(tNamespace, mkid("ns", a.s, a.hall))).emit()
		g.link(lRunsOn, pd, nd)
		g.link(lMemberOf, pd, g.workload(a))
		g.obj(tContainer, child(pd.id, "ctr", 0)).set(kS("name", "app")).in(lPartOf, pd).emit()
		g.link(lRunsOn, refOf(tContainer, child(pd.id, "ctr", 0)), pd)
	}
}

// hallPlatform makes the container cluster of a hall (control plane and
// namespace) when the hall has nodes; the control plane is a quorum group.
func (g *gen) hallPlatform(c *siteCtx, hc *hallCtx) {
	g.podSeq = 0
	n := g.lay.nodesIn(c.s, hc.h)
	if n == 0 {
		return
	}
	k := g.obj(tK8s, mkid("k8s", c.s, hc.h)).set(kS("name", "k8s-"+itoa(c.s)+"-"+itoa(hc.h))).emit()
	g.obj(tControlPlane, mkid("cp", c.s, hc.h)).set(kS("name", "cp-"+itoa(c.s)+"-"+itoa(hc.h)), kI("min_alive", n/2+1),
		kS("mode", "quorum"), kS("threshold_kind", "count")).in(lPartOf, k).emit()
	g.obj(tNamespace, mkid("ns", c.s, hc.h)).set(kS("name", "apps")).in(lPartOf, k).emit()
}

// workload gives the pod a workload; a workload is made with its first pod, so none is empty.
func (g *gen) workload(a rackAddr) ref {
	w := g.podSeq % g.p.Workloads
	id := mkid("wl", a.s, a.hall, w)
	if g.podSeq < g.p.Workloads {
		g.obj(tWorkload, id).set(kS("name", "wl"+itoa(w)), kS("workload_kind", "deployment"), kI("min_alive", 1), kS("mode", "n_plus_k"),
			kI("desired_replicas", 3)).in(lPartOf, refOf(tNamespace, mkid("ns", a.s, a.hall))).emit()
		g.link(lRunsOn, refOf(tWorkload, id), refOf(tControlPlane, mkid("cp", a.s, a.hall)))
		g.link(lRealizedBy, refOf(tTechService, mkid("tsvc", a.s, a.hall)), refOf(tWorkload, id))
	}
	g.podSeq++
	return refOf(tWorkload, id)
}
