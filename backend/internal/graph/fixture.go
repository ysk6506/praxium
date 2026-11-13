package graph

import (
	"context"
	"fmt"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Fixture builds small invented worlds on the standard model for tests of
// this unit, of adapters and of the units above it: sites that hold racks that
// hold servers, with the links between them and a volatile power state. All
// values are made up.
type Fixture struct {
	Reg *model.Registry
}

// NewFixture loads the standard model.
func NewFixture() (*Fixture, error) {
	reg, err := model.LoadStandard()
	if err != nil {
		return nil, err
	}
	return &Fixture{Reg: reg}, nil
}

// Attribute numbers of the standard model that the fixture uses.
const (
	AttrSiteAddress   = 40
	AttrServerModel   = 9
	AttrServerPower   = 203
	AttrMountPosition = 672
)

func (f *Fixture) typeID(name string) model.ObjectTypeID {
	t, ok := f.Reg.ObjectTypeByName(name)
	if !ok {
		panic(fmt.Sprintf("graph fixture: no type %s in the standard model", name))
	}
	return t.ID
}

func (f *Fixture) linkID(name string) model.LinkTypeID {
	t, ok := f.Reg.LinkTypeByName(name)
	if !ok {
		panic(fmt.Sprintf("graph fixture: no link type %s in the standard model", name))
	}
	return t.ID
}

func (f *Fixture) must(id ObjectID, err error) ObjectID {
	if err != nil {
		panic(err)
	}
	return id
}

func fact(v any) Attr { return Attr{Value: v, Grade: model.GradeFact, Basis: model.BasisConfigured} }

// Site is the upsert of a site.
func (f *Fixture) Site(code, address string) ObjectUpsert {
	return ObjectUpsert{Type: f.typeID("Site"), Name: "Site " + code,
		Key:   []KeyPart{{Kind: PartProperty, Ref: "pr.site.site_code", Value: code}},
		Attrs: map[int]Attr{AttrSiteAddress: fact(address)}, SourceRefs: []string{"fixture/site/" + code}}
}

// SiteID is the id of a site.
func (f *Fixture) SiteID(code string) ObjectID { return f.must(f.Site(code, "").id(f)) }

// Rack is the upsert of a rack inside a site.
func (f *Fixture) Rack(site ObjectID, name string) ObjectUpsert {
	return ObjectUpsert{Type: f.typeID("Rack"), Name: "Rack " + name,
		Key: []KeyPart{{Kind: PartContext, Ref: "lt.located_in", Objects: []ObjectID{site}},
			{Kind: PartProperty, Ref: "pr.common.name", Value: name}}}
}

// RackID is the id of a rack.
func (f *Fixture) RackID(site ObjectID, name string) ObjectID {
	return f.must(f.Rack(site, name).id(f))
}

// Server is the upsert of a server with a serial number.
func (f *Fixture) Server(serial, model string) ObjectUpsert {
	u := ObjectUpsert{Type: f.typeID("PhysicalServer"), Name: "Server " + serial,
		Key: []KeyPart{{Kind: PartProperty, Ref: "pr.serialized.manufacturer", Value: "ACME"},
			{Kind: PartProperty, Ref: "pr.serialized.serial_number", Value: serial}}}
	if model != "" {
		u.Attrs = map[int]Attr{AttrServerModel: fact(model)}
	}
	return u
}

// ServerID is the id of a server.
func (f *Fixture) ServerID(serial string) ObjectID { return f.must(f.Server(serial, "").id(f)) }

// LocatedIn is the link from a rack to its site.
func (f *Fixture) LocatedIn(rack, site ObjectID) LinkUpsert {
	return LinkUpsert{Type: f.linkID("LOCATED_IN"), Src: rack, Dst: site, Grade: model.GradeFact, Basis: model.BasisConfigured}
}

// MountedIn is the link from a server to its rack with the position in units.
func (f *Fixture) MountedIn(server, rack ObjectID, position int64) LinkUpsert {
	return LinkUpsert{Type: f.linkID("MOUNTED_IN"), Src: server, Dst: rack, Grade: model.GradeFact, Basis: model.BasisConfigured,
		Attrs: map[int]Attr{AttrMountPosition: fact(position)}}
}

// MountedInID is the id of the mounted-in link of a server in a rack.
func (f *Fixture) MountedInID(server, rack ObjectID) LinkID {
	return f.mustLink(NewLinkID(f.linkID("MOUNTED_IN"), server, rack, ""))
}

func (f *Fixture) mustLink(id LinkID, err error) LinkID {
	if err != nil {
		panic(err)
	}
	return id
}

// Power is a volatile write of a server's power state.
func (f *Fixture) Power(server ObjectID, state, seenAt string) VolatileWrite {
	return VolatileWrite{Object: server, AttrNo: AttrServerPower, Value: state, Grade: model.GradeFact, Basis: model.BasisObserved, SeenAt: seenAt}
}

func (u ObjectUpsert) id(f *Fixture) (ObjectID, error) { return NewObjectID(u.Type, u.Key) }

// World is a batch that adds sites sites with racks racks each and servers
// servers in each rack: one object, one link and one volatile value per thing.
func (f *Fixture) World(sites, racks, servers int) Batch {
	b := Batch{Kind: BatchSync, SourceID: "fixture"}
	for s := 0; s < sites; s++ {
		code := fmt.Sprintf("S%02d", s)
		site := f.SiteID(code)
		b.Objects = append(b.Objects, f.Site(code, "Fixture Road "+code))
		for r := 0; r < racks; r++ {
			rackName := fmt.Sprintf("R%02d", r)
			rack := f.RackID(site, rackName)
			b.Objects = append(b.Objects, f.Rack(site, rackName))
			b.Links = append(b.Links, f.LocatedIn(rack, site))
			for k := 0; k < servers; k++ {
				serial := fmt.Sprintf("SN-%02d%02d%02d", s, r, k)
				server := f.ServerID(serial)
				b.Objects = append(b.Objects, f.Server(serial, "M1"))
				b.Links = append(b.Links, f.MountedIn(server, rack, int64(k+1)))
				b.Volatile = append(b.Volatile, f.Power(server, "on", "2030-01-01T00:00:00Z"))
			}
		}
	}
	return b
}

// KGSpec is the name and role of a KG that NewTestKG creates.
type KGSpec struct {
	Name string
	Role Role
}

// NewTestKG creates a KG on a store the way a loader would: the id from the
// generator, the definition set hash of the registry.
func NewTestKG(ctx context.Context, store Catalog, reg *model.Registry, ids kernel.IDGenerator, clock kernel.Clock, spec KGSpec) (KG, error) {
	id, err := kernel.NewID[KGKind](ids, "kg")
	if err != nil {
		return KG{}, err
	}
	return store.CreateKG(ctx, KG{ID: id, Name: spec.Name, Role: spec.Role, State: StateBuilding,
		DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
}
