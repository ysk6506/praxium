package worldgen

// Layout is the structure of the estate as pure functions of the parameters
// and the layout key: how many halls a site has, how many servers a rack has,
// which server is a hypervisor host, how many VMs it runs. Nothing below the
// site level is stored. Any rack can be made again on its own, and the
// service layer and the mutation plan find their targets by arithmetic.
type Layout struct {
	P     Params
	key   uint64
	Sites []SiteShape
}

// SiteShape is what is stored per site.
type SiteShape struct {
	Racks    int
	Halls    int
	Servers  int
	Hosts    int
	VMs      int
	Services int
}

// Layout tags: each kind of random decision has its own stream.
const (
	tagServers = iota + 1
	tagHost
	tagVMs
	tagNode
	tagPods
)

func newLayout(p Params, key uint64, racksPerSite []int) *Layout {
	l := &Layout{P: p, key: key, Sites: make([]SiteShape, len(racksPerSite))}
	for s, racks := range racksPerSite {
		l.Sites[s] = SiteShape{Racks: racks, Halls: max(1, ceilDiv(racks, p.RacksPerHall))}
	}
	for s := range l.Sites {
		l.tally(s)
	}
	return l
}

// tally counts the servers, hosts, VMs and services of a site.
func (l *Layout) tally(s int) {
	sh := &l.Sites[s]
	for h := 0; h < sh.Halls; h++ {
		for rk := 0; rk < l.hallRacks(s, h); rk++ {
			n := l.serversIn(s, h, rk)
			sh.Servers += n
			for srv := 0; srv < n; srv++ {
				if l.isHost(s, h, rk, srv) {
					sh.Hosts++
					sh.VMs += l.vmsOn(s, h, rk, srv)
				}
			}
		}
	}
	sh.Services = int(float64(sh.VMs)*l.P.ServiceDensity + 0.5)
	if sh.Services == 0 && sh.VMs >= 8 {
		sh.Services = 1
	}
}

func (l *Layout) h(tag, s, hall, rk, srv int) uint64 {
	return mix(l.key^uint64(tag)*0x9e3779b97f4a7c15, uint64(s)<<44|uint64(hall)<<32|uint64(rk)<<16|uint64(srv))
}

// Sizes of the levels.

func (l *Layout) hallRacks(s, hall int) int {
	sh := l.Sites[s]
	return splitEven(sh.Racks, sh.Halls, hall)
}

func (l *Layout) rowsIn(s, hall int) int {
	return max(1, ceilDiv(l.hallRacks(s, hall), l.P.RacksPerRow))
}

func (l *Layout) rowRacks(s, hall, row int) int {
	return splitEven(l.hallRacks(s, hall), l.rowsIn(s, hall), row)
}

// rackIndex is the index of rack k of a row inside its hall.
func (l *Layout) rackIndex(s, hall, row, k int) int {
	return splitStart(l.hallRacks(s, hall), l.rowsIn(s, hall), row) + k
}

func (l *Layout) zonesIn(s, hall int) int { return ceilDiv(l.rowsIn(s, hall), l.P.RowsPerZone) }

func (l *Layout) clustersIn(s, hall int) int {
	return max(1, ceilDiv(l.hallRacks(s, hall), l.P.RacksPerCluster))
}

func (l *Layout) clusterOf(rk int) int { return rk / l.P.RacksPerCluster }

// isStorageRack says whether the rack holds a storage array.
func (l *Layout) isStorageRack(rk int) bool {
	return l.P.StorageRackEvery > 0 && rk%l.P.StorageRackEvery == 0
}

// serversIn is the number of servers of a rack: the parameter with a jitter
// of one up or down, and fewer in a rack that holds an array.
func (l *Layout) serversIn(s, hall, rk int) int {
	n := l.P.ServersPerRack
	switch l.h(tagServers, s, hall, rk, 0) % 4 {
	case 0:
		n--
	case 1:
		n++
	}
	if l.isStorageRack(rk) {
		n = n * 2 / 3
	}
	return min(max(n, 1), 40)
}

func (l *Layout) isHost(s, hall, rk, srv int) bool {
	return float64(l.h(tagHost, s, hall, rk, srv)>>11)/(1<<53) < l.P.HostShare
}

// vmsOn is the number of VMs on a host: half to one and a half times the parameter.
func (l *Layout) vmsOn(s, hall, rk, srv int) int {
	if l.P.VMsPerHost == 0 {
		return 0
	}
	lo, hi := max(1, l.P.VMsPerHost/2), max(1, l.P.VMsPerHost*3/2)
	return lo + int(l.h(tagVMs, s, hall, rk, srv)%uint64(hi-lo+1))
}

// hasNode says whether the first VM of a host is a node of a container cluster.
func (l *Layout) hasNode(s, hall, rk, srv int) bool {
	return l.P.PodsPerNode > 0 && l.isHost(s, hall, rk, srv) && l.vmsOn(s, hall, rk, srv) > 0 &&
		float64(l.h(tagNode, s, hall, rk, srv)>>11)/(1<<53) < l.P.NodeHostShare
}

// hostAddr is the address of a server in its site.
type hostAddr struct{ s, hall, row, rk, srv int }

// hostsOfRack lists the servers of a rack that are hypervisor hosts.
func (l *Layout) hostsOfRack(s, hall, rk int) []int {
	var out []int
	for srv := 0; srv < l.serversIn(s, hall, rk); srv++ {
		if l.isHost(s, hall, rk, srv) {
			out = append(out, srv)
		}
	}
	return out
}

// rowOfRack finds the row and the place in the row of a rack of the hall.
func (l *Layout) rowOfRack(s, hall, rk int) (row, k int) {
	for row = 0; row < l.rowsIn(s, hall); row++ {
		start := splitStart(l.hallRacks(s, hall), l.rowsIn(s, hall), row)
		if rk < start+l.rowRacks(s, hall, row) {
			return row, rk - start
		}
	}
	return row - 1, 0
}

// rackAt turns an index inside a site into (hall, rack in hall).
func (l *Layout) rackAt(s, n int) (hall, rk int) {
	for hall = 0; hall < l.Sites[s].Halls; hall++ {
		if n < l.hallRacks(s, hall) {
			return hall, n
		}
		n -= l.hallRacks(s, hall)
	}
	return hall - 1, 0
}

// Totals of the layout.

// Counts returns racks, servers, hosts, VMs and services of the whole world.
func (l *Layout) Counts() (racks, servers, hosts, vms, services int) {
	for _, s := range l.Sites {
		racks += s.Racks
		servers += s.Servers
		hosts += s.Hosts
		vms += s.VMs
		services += s.Services
	}
	return
}

// clusterHosts is the number of hypervisor hosts of a cluster of a hall.
func (l *Layout) clusterHosts(s, hall, cl int) int {
	n := 0
	for rk := cl * l.P.RacksPerCluster; rk < min((cl+1)*l.P.RacksPerCluster, l.hallRacks(s, hall)); rk++ {
		n += len(l.hostsOfRack(s, hall, rk))
	}
	return n
}

// nodesIn is the number of container cluster nodes of a hall.
func (l *Layout) nodesIn(s, hall int) int {
	n := 0
	for rk := 0; rk < l.hallRacks(s, hall); rk++ {
		for srv := 0; srv < l.serversIn(s, hall, rk); srv++ {
			if l.hasNode(s, hall, rk, srv) {
				n++
			}
		}
	}
	return n
}
