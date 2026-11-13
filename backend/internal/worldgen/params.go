package worldgen

import (
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// FormatVersion is the version of the ground-truth format and of the
// manifest, the source views and the shape card (docs/design-docs/worldgen.md).
const FormatVersion = 1

// Imperfections are the controllable flaws of the source views. Every flaw
// that is made is recorded in the manifest with its expected outcome.
type Imperfections struct {
	// MissingRowRatio drops that share of rows from every table source.
	MissingRowRatio float64 `json:"missing_row_ratio"`
	// DuplicateKeyRatio repeats that share of rows with the same key.
	DuplicateKeyRatio float64 `json:"duplicate_key_ratio"`
	// FalseCompleteness makes the JSON pages claim a complete delivery while rows are missing.
	FalseCompleteness bool `json:"false_completeness"`
	// ShapeDrift renames a column in one table and one JSON field.
	ShapeDrift bool `json:"shape_drift"`
	// UnparseableRatio inserts that share of garbage lines into the text and JSON Lines sources.
	UnparseableRatio float64 `json:"unparseable_ratio"`
	// LateEventRatio delivers that share of change events late (after newer ones).
	LateEventRatio float64 `json:"late_event_ratio"`
	// DuplicateEventRatio delivers that share of change events twice.
	DuplicateEventRatio float64 `json:"duplicate_event_ratio"`
	// EcmpMismatch makes one of the three derivations of an ECMP group disagree.
	EcmpMismatch bool `json:"ecmp_mismatch"`
}

// ChangePlan sets the mutation plan behind the change stream.
type ChangePlan struct {
	// Events is the number of change events to plan (0 means none).
	Events int `json:"events"`
	// Mix is the share of each kind in the plan; it is normalised.
	VMMoveShare     float64 `json:"vm_move_share"`
	ReplaceShare    float64 `json:"replace_share"`
	LinkChangeShare float64 `json:"link_change_share"`
	RetireShare     float64 `json:"retire_share"`
	FlapShare       float64 `json:"flap_share"`
	AddShare        float64 `json:"add_share"`
	FlapsPerObject  int     `json:"flaps_per_object"`
}

// Params are all the knobs of a world. A zero value means "derive or use the
// default" (Normalize); the manifest records the effective values.
type Params struct {
	Seed  uint64 `json:"seed"`
	Scale int64  `json:"scale"`

	// Structure: fan-outs per level.
	Sites            int `json:"sites"`
	SitesPerRegion   int `json:"sites_per_region"`
	RacksPerHall     int `json:"racks_per_hall"`
	RacksPerRow      int `json:"racks_per_row"`
	RowsPerZone      int `json:"rows_per_zone"`
	ServersPerRack   int `json:"servers_per_rack"`
	StorageRackEvery int `json:"storage_rack_every"`
	RacksPerCluster  int `json:"racks_per_cluster"`
	AggPerPlane      int `json:"agg_per_plane"`
	SpinesPerSite    int `json:"spines_per_site"`
	UpsPerSide       int `json:"ups_per_side"`
	CoolingUnitsN    int `json:"cooling_units_n"`

	// Server detail.
	MemoryModules int `json:"memory_modules"`
	Drives        int `json:"drives"`
	Fans          int `json:"fans"`

	// Virtualization and platform.
	HostShare     float64 `json:"host_share"`
	VMsPerHost    int     `json:"vms_per_host"`
	NodeHostShare float64 `json:"node_host_share"`
	PodsPerNode   int     `json:"pods_per_node"`
	Workloads     int     `json:"workloads_per_cluster"`

	// Services.
	ServiceDensity   float64 `json:"service_density"`
	ServiceDepth     int     `json:"service_depth"`
	DependencyDegree float64 `json:"dependency_degree"`
	CrossSiteRatio   float64 `json:"cross_site_ratio"`
	CycleRatio       float64 `json:"cycle_ratio"`
	DatabaseShare    float64 `json:"database_share"`

	// Redundancy mix.
	CoolingN2Share     float64 `json:"cooling_n2_share"`
	ClusterSpare2Share float64 `json:"cluster_spare2_share"`
	StrictServiceShare float64 `json:"strict_service_share"`

	// Facts.
	UnknownRatio float64 `json:"unknown_ratio"`
	AssumedRatio float64 `json:"assumed_ratio"`
	FillRate     float64 `json:"fill_rate"`

	Imperfections Imperfections `json:"imperfections"`
	Changes       ChangePlan    `json:"changes"`
}

// DefaultParams returns the defaults for a scale. The defaults are choices of
// this generator, not facts about any real estate (docs/design-docs/worldgen.md).
func DefaultParams(seed uint64, scale int64) Params {
	return Params{
		Seed: seed, Scale: scale,
		SitesPerRegion: 4, RacksPerHall: 24, RacksPerRow: 8, RowsPerZone: 2, ServersPerRack: 14,
		StorageRackEvery: 8, RacksPerCluster: 3, AggPerPlane: 2, SpinesPerSite: 4, UpsPerSide: 1, CoolingUnitsN: 3,
		MemoryModules: 4, Drives: 2, Fans: 3,
		HostShare: 0.7, VMsPerHost: 8, NodeHostShare: 0.25, PodsPerNode: 6, Workloads: 6,
		ServiceDensity: 0.04, ServiceDepth: 4, DependencyDegree: 1.6, CrossSiteRatio: 0.03, CycleRatio: 0.02, DatabaseShare: 0.3,
		CoolingN2Share: 0.3, ClusterSpare2Share: 0.3, StrictServiceShare: 0.3,
		UnknownRatio: 0.03, AssumedRatio: 0.02, FillRate: 0.35,
		Imperfections: Imperfections{
			MissingRowRatio: 0.01, DuplicateKeyRatio: 0.005, FalseCompleteness: true, ShapeDrift: true,
			UnparseableRatio: 0.002, LateEventRatio: 0.05, DuplicateEventRatio: 0.03, EcmpMismatch: true,
		},
		Changes: ChangePlan{
			Events: 0, VMMoveShare: 0.25, ReplaceShare: 0.1, LinkChangeShare: 0.1, RetireShare: 0.05,
			FlapShare: 0.4, AddShare: 0.1, FlapsPerObject: 4,
		},
	}
}

// intField, floatField and the others bind a name to a parameter.
type field struct {
	name string
	ptr  any
}

func (p *Params) fields() []field {
	im, ch := &p.Imperfections, &p.Changes
	return []field{
		{"seed", &p.Seed}, {"scale", &p.Scale}, {"sites", &p.Sites}, {"sites_per_region", &p.SitesPerRegion},
		{"racks_per_hall", &p.RacksPerHall}, {"racks_per_row", &p.RacksPerRow}, {"rows_per_zone", &p.RowsPerZone},
		{"servers_per_rack", &p.ServersPerRack}, {"storage_rack_every", &p.StorageRackEvery},
		{"racks_per_cluster", &p.RacksPerCluster}, {"agg_per_plane", &p.AggPerPlane},
		{"spines_per_site", &p.SpinesPerSite}, {"ups_per_side", &p.UpsPerSide}, {"cooling_units_n", &p.CoolingUnitsN},
		{"memory_modules", &p.MemoryModules}, {"drives", &p.Drives}, {"fans", &p.Fans},
		{"host_share", &p.HostShare}, {"vms_per_host", &p.VMsPerHost}, {"node_host_share", &p.NodeHostShare},
		{"pods_per_node", &p.PodsPerNode}, {"workloads_per_cluster", &p.Workloads},
		{"service_density", &p.ServiceDensity}, {"service_depth", &p.ServiceDepth},
		{"dependency_degree", &p.DependencyDegree}, {"cross_site_ratio", &p.CrossSiteRatio},
		{"cycle_ratio", &p.CycleRatio}, {"database_share", &p.DatabaseShare},
		{"cooling_n2_share", &p.CoolingN2Share}, {"cluster_spare2_share", &p.ClusterSpare2Share},
		{"strict_service_share", &p.StrictServiceShare},
		{"unknown_ratio", &p.UnknownRatio}, {"assumed_ratio", &p.AssumedRatio}, {"fill_rate", &p.FillRate},
		{"missing_row_ratio", &im.MissingRowRatio}, {"duplicate_key_ratio", &im.DuplicateKeyRatio},
		{"false_completeness", &im.FalseCompleteness}, {"shape_drift", &im.ShapeDrift},
		{"unparseable_ratio", &im.UnparseableRatio}, {"late_event_ratio", &im.LateEventRatio},
		{"duplicate_event_ratio", &im.DuplicateEventRatio}, {"ecmp_mismatch", &im.EcmpMismatch},
		{"change_events", &ch.Events}, {"vm_move_share", &ch.VMMoveShare}, {"replace_share", &ch.ReplaceShare},
		{"link_change_share", &ch.LinkChangeShare}, {"retire_share", &ch.RetireShare},
		{"flap_share", &ch.FlapShare}, {"add_share", &ch.AddShare}, {"flaps_per_object", &ch.FlapsPerObject},
	}
}

// ParamNames lists the names Set accepts.
func ParamNames() []string {
	var p Params
	var out []string
	for _, f := range p.fields() {
		out = append(out, f.name)
	}
	return out
}

// Set sets one parameter by name from text.
func (p *Params) Set(name, value string) error {
	for _, f := range p.fields() {
		if f.name != name {
			continue
		}
		return setField(f.ptr, name, value)
	}
	return kernel.Errorf(kernel.CodeInvalidValue, "unknown parameter %q", name)
}

func setField(ptr any, name, value string) error {
	var err error
	switch v := ptr.(type) {
	case *int:
		var n int64
		n, err = strconv.ParseInt(value, 10, 64)
		*v = int(n)
	case *int64:
		*v, err = strconv.ParseInt(value, 10, 64)
	case *uint64:
		*v, err = strconv.ParseUint(value, 10, 64)
	case *float64:
		*v, err = strconv.ParseFloat(value, 64)
	case *bool:
		*v, err = strconv.ParseBool(value)
	}
	if err != nil {
		return kernel.Errorf(kernel.CodeInvalidValue, "parameter %s: %q is not a valid value", name, value)
	}
	return nil
}

type bound struct {
	name   string
	v      float64
	lo, hi float64
}

// Validate checks the ranges of the parameters.
func (p Params) Validate() error {
	checks := []bound{
		{"scale", float64(p.Scale), 500, 2e9}, {"sites", float64(p.Sites), 0, 100000},
		{"sites_per_region", float64(p.SitesPerRegion), 1, 1000}, {"racks_per_hall", float64(p.RacksPerHall), 1, 1000},
		{"racks_per_row", float64(p.RacksPerRow), 1, 100}, {"rows_per_zone", float64(p.RowsPerZone), 1, 20},
		{"servers_per_rack", float64(p.ServersPerRack), 1, 42}, {"storage_rack_every", float64(p.StorageRackEvery), 0, 1000},
		{"racks_per_cluster", float64(p.RacksPerCluster), 1, 20}, {"agg_per_plane", float64(p.AggPerPlane), 1, 8},
		{"spines_per_site", float64(p.SpinesPerSite), 1, 16}, {"ups_per_side", float64(p.UpsPerSide), 1, 4},
		{"cooling_units_n", float64(p.CoolingUnitsN), 1, 12}, {"memory_modules", float64(p.MemoryModules), 1, 32},
		{"drives", float64(p.Drives), 0, 24}, {"fans", float64(p.Fans), 2, 12}, {"host_share", p.HostShare, 0, 1},
		{"vms_per_host", float64(p.VMsPerHost), 0, 200}, {"node_host_share", p.NodeHostShare, 0, 1},
		{"pods_per_node", float64(p.PodsPerNode), 0, 100}, {"workloads_per_cluster", float64(p.Workloads), 1, 100},
		{"service_density", p.ServiceDensity, 0, 1}, {"service_depth", float64(p.ServiceDepth), 1, 12},
		{"dependency_degree", p.DependencyDegree, 0, 20}, {"cross_site_ratio", p.CrossSiteRatio, 0, 1},
		{"cycle_ratio", p.CycleRatio, 0, 1}, {"database_share", p.DatabaseShare, 0, 1},
		{"cooling_n2_share", p.CoolingN2Share, 0, 1}, {"cluster_spare2_share", p.ClusterSpare2Share, 0, 1},
		{"strict_service_share", p.StrictServiceShare, 0, 1}, {"unknown_ratio", p.UnknownRatio, 0, 1},
		{"assumed_ratio", p.AssumedRatio, 0, 1}, {"fill_rate", p.FillRate, 0, 1},
		{"missing_row_ratio", p.Imperfections.MissingRowRatio, 0, 0.5}, {"duplicate_key_ratio", p.Imperfections.DuplicateKeyRatio, 0, 0.5},
		{"unparseable_ratio", p.Imperfections.UnparseableRatio, 0, 0.2}, {"late_event_ratio", p.Imperfections.LateEventRatio, 0, 0.9},
		{"duplicate_event_ratio", p.Imperfections.DuplicateEventRatio, 0, 0.9}, {"change_events", float64(p.Changes.Events), 0, 1e7},
		{"flaps_per_object", float64(p.Changes.FlapsPerObject), 1, 50},
	}
	for _, c := range checks {
		if c.v < c.lo || c.v > c.hi {
			return kernel.Errorf(kernel.CodeInvalidValue, "parameter %s = %v is outside %v..%v", c.name, c.v, c.lo, c.hi)
		}
	}
	return nil
}
