package worldgen

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The standard-model types the generator makes. The names are the registry's
// names; every one is looked up when the schema is built, so a rename in the
// standard model fails fast instead of producing a wrong world.
const (
	tRegion = iota
	tSite
	tBuilding
	tRoom
	tRoomZone
	tRow
	tRack
	tUtilityFeed
	tTransformer
	tSwitchgear
	tUps
	tPowerPanel
	tRpp
	tBusway
	tRackPdu
	tPduOutlet
	tChiller
	tCrah
	tCoolingLoop
	tServer
	tBmc
	tCpu
	tMemory
	tDrive
	tPsu
	tFan
	tNic
	tSwitch
	tRouter
	tInterface
	tLag
	tEcmp
	tNetInstance
	tPrefix
	tArray
	tController
	tPool
	tRaid
	tVolume
	tVirtMgr
	tVdc
	tCluster
	tHost
	tResPool
	tVM
	tVDisk
	tVNic
	tDatastore
	tK8s
	tControlPlane
	tNodePool
	tNode
	tNamespace
	tWorkload
	tPod
	tContainer
	tOS
	tSoftware
	tDbCluster
	tDbInstance
	tDatabase
	tLbServer
	tLbPool
	tLbMember
	tBizService
	tTechService
	tAppService
	tTeam
	tEnv
	tPowerGroup
	tRedundancy
	tHaPair
	tCoolGroup
	tMlag
	nTypes
)

var typeNames = [nTypes]string{
	tRegion: "Region", tSite: "Site", tBuilding: "Building", tRoom: "Room", tRoomZone: "RoomZone",
	tRow: "Row", tRack: "Rack", tUtilityFeed: "UtilityFeed", tTransformer: "Transformer",
	tSwitchgear: "Switchgear", tUps: "Ups", tPowerPanel: "PowerPanel", tRpp: "Rpp", tBusway: "Busway",
	tRackPdu: "RackPdu", tPduOutlet: "PduOutlet", tChiller: "Chiller", tCrah: "Crah",
	tCoolingLoop: "CoolingLoop", tServer: "PhysicalServer", tBmc: "Bmc", tCpu: "Cpu",
	tMemory: "MemoryModule", tDrive: "Drive", tPsu: "PowerSupply", tFan: "Fan", tNic: "NetworkAdapter",
	tSwitch: "Switch", tRouter: "Router", tInterface: "Interface", tLag: "Lag", tEcmp: "EcmpGroup",
	tNetInstance: "NetworkInstance", tPrefix: "Prefix", tArray: "StorageArray",
	tController: "ArrayController", tPool: "StoragePool", tRaid: "RaidGroup", tVolume: "StorageVolume",
	tVirtMgr: "VirtualizationManager", tVdc: "VirtualDatacenter", tCluster: "ComputeCluster",
	tHost: "HypervisorHost", tResPool: "ResourcePool", tVM: "VirtualMachine", tVDisk: "VirtualDisk",
	tVNic: "VirtualNic", tDatastore: "Datastore", tK8s: "ContainerCluster", tControlPlane: "ControlPlane",
	tNodePool: "NodePool", tNode: "ClusterNode", tNamespace: "Namespace", tWorkload: "Workload",
	tPod: "Pod", tContainer: "Container", tOS: "OperatingSystem", tSoftware: "SoftwareInstance",
	tDbCluster: "DatabaseCluster", tDbInstance: "DatabaseInstance", tDatabase: "Database",
	tLbServer: "LbVirtualServer", tLbPool: "LbPool", tLbMember: "LbPoolMember",
	tBizService: "BusinessService", tTechService: "TechnicalService", tAppService: "ApplicationService",
	tTeam: "Team", tEnv: "Environment", tPowerGroup: "PowerRedundancyGroup",
	tRedundancy: "RedundancyGroup", tHaPair: "HaPair", tCoolGroup: "CoolingRedundancyGroup", tMlag: "MlagDomain",
}

// typePrefix gives the short id prefix of each type.
var typePrefix = [nTypes]string{
	tRegion: "reg", tSite: "site", tBuilding: "bld", tRoom: "hall", tRoomZone: "zone", tRow: "row",
	tRack: "rack", tUtilityFeed: "ufeed", tTransformer: "xfmr", tSwitchgear: "swgr", tUps: "ups",
	tPowerPanel: "ppnl", tRpp: "rpp", tBusway: "bus", tRackPdu: "pdu", tPduOutlet: "outl",
	tChiller: "chil", tCrah: "crah", tCoolingLoop: "cloop", tServer: "srv", tBmc: "bmc", tCpu: "cpu",
	tMemory: "mem", tDrive: "drv", tPsu: "psu", tFan: "fan", tNic: "nic", tSwitch: "sw",
	tRouter: "rtr", tInterface: "if", tLag: "lag", tEcmp: "ecmp", tNetInstance: "vrf", tPrefix: "pfx",
	tArray: "arr", tController: "ctl", tPool: "pool", tRaid: "raid", tVolume: "vol", tVirtMgr: "vmgr",
	tVdc: "vdc", tCluster: "clu", tHost: "hyp", tResPool: "rpool", tVM: "vm", tVDisk: "vdsk",
	tVNic: "vnic", tDatastore: "dstr", tK8s: "k8s", tControlPlane: "cp", tNodePool: "npool",
	tNode: "node", tNamespace: "ns", tWorkload: "wl", tPod: "pod", tContainer: "ctr", tOS: "os",
	tSoftware: "swi", tDbCluster: "dbc", tDbInstance: "dbi", tDatabase: "db", tLbServer: "lbv",
	tLbPool: "lbp", tLbMember: "lbm", tBizService: "bsvc", tTechService: "tsvc", tAppService: "asvc",
	tTeam: "team", tEnv: "env", tPowerGroup: "pgrp", tRedundancy: "rgrp", tHaPair: "hapr", tCoolGroup: "cgrp", tMlag: "mlag",
}

// The link types the generator makes.
const (
	lLocatedIn = iota
	lMountedIn
	lRunsOn
	lPoweredBy
	lCooledBy
	lPartOf
	lMemberOf
	lManagedBy
	lCabledTo
	lUplinkedVia
	lBackedBy
	lMounts
	lRealizedBy
	lDependsOn
	lCalls
	lStoresDataOn
	lOwnedBy
	lForwardsTo
	nLinks
)

var linkNames = [nLinks]string{
	lLocatedIn: "LOCATED_IN", lMountedIn: "MOUNTED_IN", lRunsOn: "RUNS_ON", lPoweredBy: "POWERED_BY",
	lCooledBy: "COOLED_BY", lPartOf: "PART_OF", lMemberOf: "MEMBER_OF", lManagedBy: "MANAGED_BY",
	lCabledTo: "CABLED_TO", lUplinkedVia: "UPLINKED_VIA", lBackedBy: "BACKED_BY", lMounts: "MOUNTS",
	lRealizedBy: "REALIZED_BY", lDependsOn: "DEPENDS_ON", lCalls: "CALLS", lStoresDataOn: "STORES_DATA_ON",
	lOwnedBy: "OWNED_BY", lForwardsTo: "FORWARDS_TO",
}

// hint is how a property without an explicit value gets one.
type hint uint8

const (
	hString hint = iota
	hIPv4
	hMAC
	hSerial
	hFQDN
	hUUID
	hInt
	hNumber
	hBool
	hTime
	hEnum
	hQty
	hList
	hMap
	hSkip
)

// propSpec is one property of a type as the generator uses it.
type propSpec struct {
	name     string
	hint     hint
	required bool
	enum     []string
	class    model.ChangeClass
	lo, hi   int  // range of numbers
	keyPart  bool // a component of an identity key
	key      []byte
	vt       model.ValueTypeID
}

// keyComp is one component of an identity key: a property (index into the
// type's properties, -1 for a context) or a context (a link type of the
// generator).
type keyComp struct {
	prop int
	ctx  int
	// set is a set component over the link type ctx: the sorted ids of the
	// objects at the other end, incoming when the links arrive at the
	// object, with size members when size is more than 0.
	set, incoming bool
	size          int
}

// keySpec is an identity key as the generator can satisfy it. A key that
// names a context the generator never makes is left out.
type keySpec struct {
	comps  []keyComp
	scheme string // identifier scheme id; with no components it is the whole key
	strong bool
}

type typeSpec struct {
	idx    int
	name   string
	id     string
	otid   model.ObjectTypeID
	prefix string
	layer  model.Layer
	props  []propSpec
	byName map[string]int
	keys   []keySpec
	ingest bool // an identity key exists in the standard model
	// setOut and setIn are the link types some key reads as a set, by the
	// links that leave the object and the links that arrive at it.
	setOut, setIn uint32
	basis         model.Basis
}

type linkSpec struct {
	idx     int
	name    string
	id      string
	ltid    model.LinkTypeID
	meaning model.Meaning
	props   []propSpec
	byName  map[string]int
}

// schema is the registry seen by the generator.
type schema struct {
	reg    *model.Registry
	types  [nTypes]*typeSpec
	links  [nLinks]*linkSpec
	bound  map[uint32]bool // checked (from, to, link) triples
	byID   map[string]*typeSpec
	linkBy map[string]*linkSpec
}

var layerBasis = map[model.Layer]model.Basis{
	model.LayerFacility: model.BasisDeclared, model.LayerHardware: model.BasisObserved,
	model.LayerNetwork: model.BasisConfigured, model.LayerStorage: model.BasisConfigured,
	model.LayerVirtualization: model.BasisObserved, model.LayerContainer: model.BasisObserved,
	model.LayerPlatform: model.BasisObserved, model.LayerService: model.BasisDeclared,
	model.LayerCrossCutting: model.BasisDerived,
}

func newSchema(reg *model.Registry) (*schema, error) {
	s := &schema{reg: reg, bound: map[uint32]bool{}, byID: map[string]*typeSpec{}, linkBy: map[string]*linkSpec{}}
	for i := range s.links {
		if err := s.addLink(i); err != nil {
			return nil, err
		}
	}
	for i := range s.types {
		if err := s.addType(i); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *schema) addLink(i int) error {
	lt, ok := s.reg.LinkTypeByName(linkNames[i])
	if !ok {
		return kernel.Errorf(kernel.CodeNotFound, "the standard model has no link type %s", linkNames[i])
	}
	ls := &linkSpec{idx: i, name: lt.Name, id: lt.ID.String(), ltid: lt.ID, meaning: lt.Meaning, byName: map[string]int{}}
	for _, p := range lt.Properties {
		ls.byName[p.Name] = len(ls.props)
		ls.props = append(ls.props, s.propSpecOf(p))
	}
	s.links[i] = ls
	s.linkBy[ls.id] = ls
	return nil
}

func (s *schema) addType(i int) error {
	ot, ok := s.reg.ObjectTypeByName(typeNames[i])
	if !ok {
		return kernel.Errorf(kernel.CodeNotFound, "the standard model has no object type %s", typeNames[i])
	}
	ts := &typeSpec{idx: i, name: ot.Name, id: ot.ID.String(), otid: ot.ID, prefix: typePrefix[i], layer: ot.Layer, byName: map[string]int{}}
	ts.basis = layerBasis[ot.Layer]
	names := map[model.PropertyID]string{}
	for _, p := range s.reg.EffectiveProperties(ot.ID) {
		ts.byName[p.Name] = len(ts.props)
		ts.props = append(ts.props, s.propSpecOf(p))
		names[p.ID] = p.Name
	}
	keys := s.reg.IdentityKeys(ot.ID)
	ts.ingest = len(keys) > 0
	for _, k := range keys {
		if ks, ok := s.keySpecOf(ts, k, names); ok {
			ts.keys = append(ts.keys, ks)
		}
	}
	ts.markKeyParts()
	s.types[i] = ts
	s.byID[ts.id] = ts
	return nil
}

// markKeyParts marks the properties some key uses and the link types some key
// reads as a set, from the keys of the type.
func (ts *typeSpec) markKeyParts() {
	for _, k := range ts.keys {
		for _, c := range k.comps {
			switch {
			case c.prop >= 0:
				ts.props[c.prop].keyPart = true
			case c.set && c.incoming:
				ts.setIn |= 1 << uint(c.ctx)
			case c.set:
				ts.setOut |= 1 << uint(c.ctx)
			}
		}
	}
}

func (s *schema) keySpecOf(ts *typeSpec, k model.IdentityKey, names map[model.PropertyID]string) (keySpec, bool) {
	ks := keySpec{scheme: k.Scheme.String(), strong: k.Strength == model.IdentityStrong}
	for _, c := range k.Components {
		if c.Context.IsZero() {
			idx, ok := ts.byName[names[c.Property]]
			if !ok {
				return ks, false
			}
			ks.comps = append(ks.comps, keyComp{prop: idx, ctx: -1})
			continue
		}
		lt, ok := s.reg.LinkType(c.Context)
		if !ok {
			return ks, false
		}
		found := -1
		for li, n := range linkNames {
			if n == lt.Name {
				found = li
			}
		}
		if found < 0 {
			return ks, false
		}
		ks.comps = append(ks.comps, keyComp{prop: -1, ctx: found, set: c.Set, incoming: c.Incoming, size: c.Size})
	}
	return ks, true
}

// numeric ranges by the end of a property name, first match wins.
var rangeBySuffix = []struct {
	suffix string
	lo, hi int
}{
	{"_pct", 1, 99}, {"_c", 16, 32}, {"_kw", 5, 900}, {"_kva", 5, 900}, {"_va", 500, 9000},
	{"_w", 80, 3500}, {"_mhz", 1800, 3600}, {"_gib", 8, 1024}, {"_gb", 8, 1024}, {"_mm", 300, 1200},
	{"_kg", 3, 90}, {"_u", 1, 4}, {"_s", 5, 600}, {"_a", 8, 63}, {"_v", 200, 240}, {"_mbps", 1000, 100000},
	{"_count", 1, 64}, {"_hosts", 1, 3}, {"_cfm", 800, 8000}, {"_m2", 40, 2000}, {"_year", 2008, 2024},
	{"_index", 0, 63}, {"_id", 1, 4094}, {"_sockets", 1, 2}, {"_cores", 8, 128},
}

func (s *schema) propSpecOf(p model.Property) propSpec {
	ps := propSpec{vt: p.ValueType, name: p.Name, required: p.Required, enum: p.EnumValues, class: p.ChangeClass, lo: 1, hi: 64}
	ps.key = []byte(`"` + p.Name + `":`)
	base := model.BaseString
	if v, ok := s.reg.ValueType(p.ValueType); ok {
		base = v.Base
	}
	ps.hint = hintOf(p.Name, p.ValueType.String(), base)
	for _, r := range rangeBySuffix {
		if strings.HasSuffix(p.Name, r.suffix) {
			ps.lo, ps.hi = r.lo, r.hi
			break
		}
	}
	return ps
}

var baseHint = map[model.BaseKind]hint{
	model.BaseString: hString, model.BaseInteger: hInt, model.BaseNumber: hNumber, model.BaseBoolean: hBool,
	model.BaseTime: hTime, model.BaseEnum: hEnum, model.BaseQuantity: hQty, model.BaseRef: hSkip,
	model.BaseList: hList, model.BaseMap: hMap,
}

var namedHint = map[string]hint{
	"vt.ipv4_address": hIPv4, "vt.mac_address": hMAC, "vt.serial_number": hSerial, "vt.fqdn": hFQDN,
}

func hintOf(name, vt string, base model.BaseKind) hint {
	if h, ok := namedHint[vt]; ok {
		return h
	}
	h := baseHint[base]
	if h == hString && (strings.HasSuffix(name, "uuid") || name == "machine_id") {
		return hUUID
	}
	if h == hString && (name == "primary_address" || name == "management_address") {
		return hIPv4
	}
	return h
}

// bind checks, once per (from, to, link), that the standard model has a
// binding for the triple.
func (s *schema) bind(from, to, link int) bool {
	key := uint32(from)<<16 | uint32(to)<<8 | uint32(link)
	if ok, seen := s.bound[key]; seen {
		return ok
	}
	ok := false
	for _, b := range s.reg.BindingsFor(s.types[from].otid, s.types[to].otid) {
		if b.LinkType == s.links[link].ltid {
			ok = true
			break
		}
	}
	s.bound[key] = ok
	return ok
}
