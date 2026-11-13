package worldgen

const volumesPerArray = 4

func arrayID(a rackAddr) string { return child(a.id(), "arr", 0) }

func volumeID(a rackAddr, v int) string { return child(arrayID(a), "vol", v) }

// storageArray makes a block array in a storage rack: two controllers in an
// HA pair, a pool over a RAID group of drives, and volumes that the clusters
// of the hall use as datastores.
func (g *gen) storageArray(c *siteCtx, hc *hallCtx, a rackAddr, rk ref, pdu [2]ref) {
	arr := g.obj(tArray, arrayID(a)).set(kS("name", "arr-"+a.id()), kS("manufacturer", pickOf(storMakers, hash64(a.id()))),
		kS("array_kind", "block"), kI("controller_count", 2)).emit()
	g.link(lMountedIn, arr, rk, kS("face", "front"), kS("side", "full"))
	for side := 0; side < 2; side++ {
		g.link(lPoweredBy, arr, pdu[side], kS("feed_side", feedSide[side]), kS("path_role", "primary"))
	}
	pair := g.obj(tHaPair, child(arr.id, "ha", 0)).set(kS("name", "ha-"+arr.id), kI("min_alive", 1), kS("mode", "active_active")).in(lPartOf, arr).emit()
	for i := 0; i < 2; i++ {
		ctl := g.obj(tController, child(arr.id, "ctl", i)).set(kS("name", "ctl-"+arr.id+itoa(i))).in(lPartOf, arr).emit()
		g.link(lMemberOf, ctl, pair)
		port := g.iface(ctl, 0, "fc0", 32000)
		far := g.ifaceFor(tor0(c, a, i), arr.id, i, "st-"+arr.id, 25000)
		g.link(lCabledTo, port, far)
	}
	pool := g.obj(tPool, child(arr.id, "pool", 0)).set(kS("name", "pool-"+arr.id)).in(lRunsOn, arr).emit()
	g.arrayDisks(arr, pool)
	for v := 0; v < volumesPerArray; v++ {
		vol := g.obj(tVolume, volumeID(a, v)).set(kS("name", "vol-"+itoa(v)+"-"+arr.id)).in(lRunsOn, arr).emit()
		g.link(lBackedBy, vol, pool)
	}
}

// tor0 is the ToR switch of a side of the rack (made before the array).
func tor0(c *siteCtx, a rackAddr, side int) ref { return refOf(tSwitch, child(a.id(), "tor", side)) }

// arrayDisks makes a RAID group of drives (parity: one drive may fail).
func (g *gen) arrayDisks(arr, pool ref) {
	const n = 6
	rg := g.obj(tRaid, child(arr.id, "raid", 0)).set(kS("name", "rg-"+arr.id), kI("min_alive", n-1), kS("mode", "raid_parity"),
		kS("threshold_kind", "count")).in(lPartOf, arr).emit()
	g.link(lBackedBy, pool, rg)
	for i := 0; i < n; i++ {
		d := g.obj(tDrive, child(arr.id, "drv", i)).set(kS("slot_name", "slot"+itoa(i))).in(lPartOf, arr).emit()
		g.link(lMemberOf, d, rg)
	}
}
