package query

import "time"

// Modes of impact (kg-queries.md 3): which readings of the evidence are answered.
const (
	ModeBoth            = "both"
	ModeFactsOnly       = "facts_only"
	ModeWithAssumptions = "with_assumptions"
)

// Directions of impact. Downstream asks what a failure reaches; upstream asks
// what a target depends on (ADR-0043: the design has no query of its own for it).
const (
	DirectionDownstream = "downstream"
	DirectionUpstream   = "upstream"
)

// Results of an object (kg-queries.md 5.4), in the order of their severity.
const (
	ResultDown             = "down"
	ResultInterrupted      = "interrupted"
	ResultUnknown          = "unknown"
	ResultExposedDirect    = "exposed_direct"
	ResultExposedInherited = "exposed_inherited"
	ResultDegraded         = "degraded"
	ResultUnmanageable     = "unmanageable"
)

// The limits of an impact question. All are guesses until measured (defaults.md).
const (
	MaxSeeds          = 1000
	DefaultMaxItems   = 5000
	MaxItemsCap       = 100000
	DefaultTimeBudget = 30 * time.Second
	// MaxCandidates is how many upstream objects are tried one by one.
	MaxCandidates = 2000
)

// StoppedBy values of the computation, which are also the reasons of result_complete.
const (
	StopTimeBudget = "time_budget"
	StopMaxHops    = "max_hops"
)

// ImpactInput is the input of impact (kg-queries.md 3). Downstream, the seeds
// are Dead, DeadLinks and SiteFailure; upstream, TargetObjectID. Zero values
// mean the default.
type ImpactInput struct {
	Common
	Direction string
	// Dead are the objects that have failed; SiteFailure are objects that fail
	// with everything that is contained in them; DeadLinks are links that are cut.
	Dead        []string
	DeadLinks   []string
	SiteFailure []string
	// DeadScopeID needs stored scopes, which do not exist yet; it is refused.
	DeadScopeID string
	// TargetObjectID is the object whose dependencies upstream lists.
	TargetObjectID string
	Mode           string
	// MaxHops stops the spread after that many rounds (0: no limit).
	MaxHops int
	// MaxItems caps how many items are kept per reading (0: DefaultMaxItems).
	MaxItems int
	// TimeBudget stops the computation (0: DefaultTimeBudget).
	TimeBudget time.Duration
}

// PathLink is the link that carried a failure on to the next object of a path.
type PathLink struct {
	LinkID     string `json:"link_id"`
	LinkType   string `json:"link_type"`
	Meaning    string `json:"meaning"`
	Role       string `json:"role"`
	Grade      string `json:"grade"`
	Basis      string `json:"basis"`
	DelayClass string `json:"delay_class,omitempty"`
	Note       string `json:"note,omitempty"`
}

// PathStep is one object of a path. Link is how the failure reached it from the
// step before; the first step has none. An object the caller may not read is
// shown as Hidden, without an id.
type PathStep struct {
	ObjectID    *string    `json:"object_id"`
	ObjectType  string     `json:"object_type,omitempty"`
	DisplayName string     `json:"display_name,omitempty"`
	Hidden      bool       `json:"hidden,omitempty"`
	Link        *PathLink  `json:"link"`
	ViaGroup    *ObjectRef `json:"via_group,omitempty"`
}

// ImpactItem is one affected object with why (kg-queries.md 5.7).
type ImpactItem struct {
	ObjectID         string     `json:"object_id"`
	ObjectType       string     `json:"object_type"`
	DisplayName      string     `json:"display_name"`
	Result           string     `json:"result"`
	Also             []string   `json:"also"`
	LostChannels     []string   `json:"lost_channels"`
	UnfilledChannels []string   `json:"unfilled_channels,omitempty"`
	DelayClass       string     `json:"delay_class,omitempty"`
	Via              *string    `json:"via"`
	ViaHidden        bool       `json:"via_hidden,omitempty"`
	Hops             int        `json:"hops"`
	AssumptionBased  bool       `json:"assumption_based"`
	WeakestGrade     string     `json:"weakest_grade"`
	Path             []PathStep `json:"path"`
}

// ParentCount counts affected objects by the place they are contained in.
type ParentCount struct {
	ObjectID    string `json:"object_id"`
	DisplayName string `json:"display_name"`
	Count       int    `json:"count"`
}

// GroupTouched is a redundancy group whose members were hit (kg-queries.md 5.7).
// MinAlive and Slack are absent when min_alive is not known.
type GroupTouched struct {
	GroupID     string `json:"group_id"`
	GroupType   string `json:"group_type"`
	DisplayName string `json:"display_name"`
	Mode        string `json:"mode"`
	Value       string `json:"value"`
	Alive       int    `json:"alive"`
	Unknown     int    `json:"unknown"`
	N           int    `json:"n"`
	MinAlive    *int   `json:"min_alive"`
	Slack       *int   `json:"slack"`
}

// ImpactSection is the answer of one reading: the counts first, then the items.
type ImpactSection struct {
	CountsByResult map[string]int `json:"counts_by_result"`
	CountsByType   []TypeCount    `json:"counts_by_type"`
	CountsByParent []ParentCount  `json:"counts_by_parent"`
	GroupsTouched  []GroupTouched `json:"groups_touched"`
	Items          []ImpactItem   `json:"items,omitempty"`
}

// UpstreamItem is one object the target depends on, with what happens to the
// target when that object alone fails.
type UpstreamItem struct {
	ObjectID             string            `json:"object_id"`
	ObjectType           string            `json:"object_type"`
	DisplayName          string            `json:"display_name"`
	Hops                 int               `json:"hops"`
	Effects              map[string]string `json:"effects"`
	SinglePointOfFailure bool              `json:"single_point_of_failure"`
	AssumptionBased      bool              `json:"assumption_based"`
	Path                 []PathStep        `json:"path"`
}

// UpstreamSection is what the target depends on: the counts first, then the items.
type UpstreamSection struct {
	Total          int            `json:"total"`
	SinglePoints   int            `json:"single_points_of_failure"`
	CountsByType   []TypeCount    `json:"counts_by_type"`
	CountsByEffect map[string]int `json:"counts_by_effect"`
	Items          []UpstreamItem `json:"items,omitempty"`
}

// Computation says whether the calculation ran to its end and what it was limited by.
type Computation struct {
	Complete          bool    `json:"complete"`
	StoppedBy         *string `json:"stopped_by"`
	Rounds            int     `json:"rounds"`
	MaxHops           int     `json:"max_hops"`
	MaxItems          int     `json:"max_items"`
	TimeBudgetSeconds float64 `json:"time_budget_seconds"`
	WeakLinks         int     `json:"weak_links"`
	StaleInputs       int     `json:"stale_inputs"`
	ItemsOverCap      bool    `json:"items_over_cap"`
}

// ImpactData is the body of the answer of impact.
type ImpactData struct {
	Direction       string           `json:"direction"`
	Seeds           []ObjectRef      `json:"seeds,omitempty"`
	Target          *ObjectRef       `json:"target,omitempty"`
	FactsOnly       *ImpactSection   `json:"facts_only,omitempty"`
	WithAssumptions *ImpactSection   `json:"with_assumptions,omitempty"`
	Upstream        *UpstreamSection `json:"upstream,omitempty"`
	Computation     Computation      `json:"computation"`
}
