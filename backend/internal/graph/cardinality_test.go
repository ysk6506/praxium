package graph

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// cluster builds a container cluster, a control plane and namespaces inside it, each with its PART_OF link.
type clusterWorld struct {
	u        *unit
	cluster  ObjectID
	partOf   model.LinkTypeID
	upserts  []ObjectUpsert
	links    []LinkUpsert
	nameAttr int
}

func newClusterWorld(t *testing.T, uid string) *clusterWorld {
	t.Helper()
	u := newUnit(t, nil, Options{})
	cw := &clusterWorld{u: u, partOf: u.fx.linkID("PART_OF")}
	ct := u.fx.typeID("ContainerCluster")
	for _, p := range u.fx.Reg.EffectiveProperties(ct) {
		if p.Name == "name" {
			cw.nameAttr = p.AttrNo
		}
	}
	cluster := ObjectUpsert{Type: ct, Name: "cluster " + uid, Attrs: map[int]Attr{cw.nameAttr: fact("cluster " + uid)},
		Key: []KeyPart{{Kind: PartScheme, Ref: "is.k8s_cluster_uid", Value: uid}}}
	id, err := cluster.id(u.fx)
	if err != nil {
		t.Fatal(err)
	}
	cw.cluster = id
	cw.upserts = append(cw.upserts, cluster)
	return cw
}

// inside adds an object of a type that is identified inside the cluster and its link to the cluster.
func (cw *clusterWorld) placeInside(t *testing.T, typ, name string) {
	t.Helper()
	up := ObjectUpsert{Type: cw.u.fx.typeID(typ), Name: name, Attrs: map[int]Attr{cw.nameAttr: fact(name)},
		Key: []KeyPart{{Kind: PartContext, Ref: "lt.part_of", Objects: []ObjectID{cw.cluster}},
			{Kind: PartProperty, Ref: "pr.common.name", Value: name}}}
	id, err := up.id(cw.u.fx)
	if err != nil {
		t.Fatal(err)
	}
	cw.upserts = append(cw.upserts, up)
	cw.links = append(cw.links, LinkUpsert{Type: cw.partOf, Src: id, Dst: cw.cluster, Grade: model.GradeFact, Basis: model.BasisConfigured})
}

func (cw *clusterWorld) applyAll() error {
	_, err := cw.u.a.Apply(cw.u.ctx, cw.u.kg.ID, cw.u.lease, Batch{Kind: BatchSync, Objects: cw.upserts, Links: cw.links})
	return err
}

// [REQ-261]
func TestACardinalityBelongsToABindingNotToALinkType(t *testing.T) {
	// A cluster holds one control plane (a 1:1 binding of PART_OF) and any number of namespaces (an N:1 binding of
	// the same link type): the namespaces do not count against the control plane.
	cw := newClusterWorld(t, "k8s-A")
	cw.placeInside(t, "ControlPlane", "cp")
	cw.placeInside(t, "Namespace", "ns-one")
	cw.placeInside(t, "Namespace", "ns-two")
	if err := cw.applyAll(); err != nil {
		t.Fatalf("a cluster with one control plane and two namespaces: %v", err)
	}
	// A second control plane breaks the 1:1 binding, in the same batch as in a later one.
	again := newClusterWorld(t, "k8s-B")
	again.placeInside(t, "ControlPlane", "cp-one")
	again.placeInside(t, "ControlPlane", "cp-two")
	if err := again.applyAll(); kernel.CodeOf(err) != CodeCardinality {
		t.Fatalf("two control planes in one batch: %v", err)
	}
	later := newClusterWorld(t, "k8s-A")
	later.u = cw.u
	later.upserts, later.links = nil, nil
	later.placeInside(t, "ControlPlane", "cp-extra")
	if err := later.applyAll(); kernel.CodeOf(err) != CodeCardinality {
		t.Fatalf("a second control plane in a later batch: %v", err)
	}
	// A namespace has one cluster: the N:1 binding limits its outgoing links.
	two := newClusterWorld(t, "k8s-C")
	two.placeInside(t, "Namespace", "ns")
	extra := two.links[0]
	other := ObjectUpsert{Type: two.upserts[0].Type, Name: "other", Attrs: two.upserts[0].Attrs,
		Key: []KeyPart{{Kind: PartScheme, Ref: "is.k8s_cluster_uid", Value: "k8s-D"}}}
	otherID, err := other.id(two.u.fx)
	if err != nil {
		t.Fatal(err)
	}
	extra.Dst = otherID
	two.upserts = append(two.upserts, other)
	two.links = append(two.links, extra)
	if err := two.applyAll(); kernel.CodeOf(err) != CodeCardinality {
		t.Fatalf("a namespace in two clusters: %v", err)
	}
}
