package worldgen

import (
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// World is a resolved world: the effective parameters, the layout and the
// registry schema. Generate streams it; it can be streamed any number of times.
type World struct {
	P     Params
	Lay   *Layout
	sch   *schema
	keys  worldKeys
	Racks []int
}

// NewWorld resolves the parameters (the scale becomes a number of sites and
// racks by probing the generator) and draws the keys from the rng.
func NewWorld(rng kernel.Rng, reg *model.Registry, p Params) (*World, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	sch, err := newSchemaFor(reg)
	if err != nil {
		return nil, err
	}
	w := &World{sch: sch, keys: drawKeys(rng)}
	racks, err := w.calibrate(&p)
	if err != nil {
		return nil, err
	}
	w.P, w.Racks = p, racks
	w.Lay = newLayout(p, w.keys.layout, racks)
	return w, nil
}

// racksPerSiteDefault is the size of a site when the number of sites is not set.
const racksPerSiteDefault = 500

type counter struct{ n int64 }

func (c *counter) Scope(Scope, int, int, bool) error { return nil }
func (c *counter) Object(*Object) error              { c.n++; return nil }
func (c *counter) Link(*Link) error                  { c.n++; return nil }

// probe counts the objects and links of a one-site world of r racks.
func (w *World) probe(p Params, r int) (int64, error) {
	lay := newLayout(p, w.keys.layout, []int{r})
	var c counter
	g := newGen(p, w.sch, w.keys, lay, []Observer{&c})
	g.run()
	return c.n, g.err
}

// calibrate turns the scale into racks per site: two probe runs give the
// cost of a rack and of a site, and the rest is arithmetic.
func (w *World) calibrate(p *Params) ([]int, error) {
	t1, err := w.probe(*p, 48)
	if err != nil {
		return nil, err
	}
	t2, err := w.probe(*p, 96)
	if err != nil {
		return nil, err
	}
	per := float64(t2-t1) / 48
	fixed := max(0, float64(t1)-48*per)
	sites := p.Sites
	if sites == 0 {
		sites = max(1, ceilDiv(int(float64(p.Scale)/per), racksPerSiteDefault))
	}
	total := max(sites, int((float64(p.Scale)-float64(sites)*fixed)/per))
	p.Sites = sites
	if total/sites <= 120 {
		total = w.refine(*p, sites, total)
	}
	out := make([]int, sites)
	for s := range out {
		out[s] = splitEven(total, sites, s)
	}
	return out, nil
}

// refine corrects the estimate for small sites, where the cost of a rack is
// not linear: it measures a site of the estimated size and scales the racks.
func (w *World) refine(p Params, sites, total int) int {
	for i := 0; i < 3; i++ {
		got, err := w.probe(p, max(1, total/sites))
		if err != nil || got == 0 {
			return total
		}
		next := max(sites, int(float64(total)*float64(p.Scale)/(float64(got)*float64(sites))+0.5))
		if next == total {
			break
		}
		total = next
	}
	return total
}

// Generate streams the world to the observers, in the order objects and
// links are made. The same World gives the same stream.
func (w *World) Generate(obs ...Observer) error {
	g := newGen(w.P, w.sch, w.keys, w.Lay, obs)
	g.run()
	return g.err
}

func (g *gen) run() {
	for s := range g.lay.Sites {
		g.site(s)
		if g.err != nil {
			return
		}
	}
}

func (g *gen) site(s int) {
	c := g.siteInfra(s)
	sh := g.lay.Sites[s]
	for h := 0; h < sh.Halls && g.err == nil; h++ {
		g.hallRun(c, h)
	}
	g.services(c)
	g.scope(ScopeSite, s, s, false)
}

func (g *gen) hallRun(c *siteCtx, h int) {
	hc := g.hall(c, h)
	g.hallPlatform(c, hc)
	for row := 0; row < g.lay.rowsIn(c.s, h); row++ {
		for k := 0; k < g.lay.rowRacks(c.s, h, row); k++ {
			rk := g.lay.rackIndex(c.s, h, row, k)
			if rk%g.p.RacksPerCluster == 0 {
				g.cluster(c, hc, g.lay.clusterOf(rk))
			}
			g.rack(c, hc, rackAddr{c.s, h, row, k, rk})
		}
	}
	ts := g.obj(tTechService, mkid("tsvc", c.s, h)).set(kS("name", "compute-"+itoa(c.s)+"-"+itoa(h))).emit()
	for cl := 0; cl < g.lay.clustersIn(c.s, h); cl++ {
		if g.lay.clusterHosts(c.s, h, cl) == 0 {
			continue
		}
		g.link(lRealizedBy, ts, refOf(tCluster, clusterID(c.s, h, cl)))
	}
	g.scope(ScopeHall, c.s, h, false)
}

// Build makes a world from parameters alone: the rng is the seeded one of Params.Seed.
func Build(reg *model.Registry, p Params) (*World, error) {
	return NewWorld(kernel.NewSeededRng(p.Seed), reg, p)
}
