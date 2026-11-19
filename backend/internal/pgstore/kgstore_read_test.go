package pgstore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the catalog and the reads of the new storage. Their names start with v2r so that they never meet a
// name of another test file of this folder. The rows are written by hand: the writer of the new storage is
// tested with the conformance cases.

type v2rWorld struct {
	st         *StoreV2
	admin      *sql.DB
	dsn        string
	kg         graph.KG
	a          *kgAccess
	siteNo     int
	serverNo   int
	linkNo     int
	site, srv  graph.ObjectID
	gone       graph.ObjectID
	link       graph.LinkID
	identifier string
}

func v2rExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func v2rWorldOf(t *testing.T) *v2rWorld { return v2rWorldWith(t, LeaseClockDB) }

func v2rWorldWith(t *testing.T, clock LeaseClock) *v2rWorld {
	t.Helper()
	ctx := context.Background()
	dsn := NewTestDatabase(t)
	models, err := StandardModels(false)
	if err != nil {
		t.Fatal(err)
	}
	st, err := OpenV2(ctx, Config{DSN: dsn}, V2Options{Models: models, LeaseClock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	id, err := kernel.NewID[graph.KGKind](kernel.NewRandomIDs(kernel.NewSeededRng(7)), "kg")
	if err != nil {
		t.Fatal(err)
	}
	kg, err := st.CreateKG(ctx, graph.KG{ID: id, Name: "v2r", Role: graph.RoleTest, State: graph.StateBuilding,
		DefinitionSetHash: fx.Reg.Hash(), CreatedAt: time.Unix(1000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.access(ctx, kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := &v2rWorld{st: st, admin: admin, dsn: dsn, kg: kg, a: a, site: fx.SiteID("A"), srv: fx.ServerID("S1"), gone: fx.ServerID("S2"), identifier: "serial"}
	site, _ := fx.Reg.ObjectTypeByName("Site")
	server, _ := fx.Reg.ObjectTypeByName("PhysicalServer")
	mount, _ := fx.Reg.LinkTypeByName("MOUNTED_IN")
	w.siteNo, w.serverNo, w.linkNo = site.TypeNo, server.TypeNo, mount.LinkTypeNo
	w.link = fx.MountedInID(w.srv, w.site)
	return w
}

// v2rSeed writes: a site (from change 1), a server (version 1 from change 1 to 3, version 2 from change 3), a server
// that was retired at change 4, a link from change 2 and an identifier of the server.
func (w *v2rWorld) v2rSeed(t *testing.T) {
	t.Helper()
	a := w.a
	tn := func(table string) string { return a.t(table) }
	obj := func(no int, id graph.ObjectID, typeNo int, from int64, retired any) {
		v2rExec(t, w.admin, `INSERT INTO `+tn("object")+` (obj_no, ref, type_no, name, min_grade, seq_from, retired_seq) VALUES ($1,$2,$3,'n',0,$4,$5)`,
			no, id.String(), typeNo, from, retired)
	}
	row := func(table string, no int, name string, from int64, extra ...any) {
		cols, vals := `, ref`, `, (SELECT ref FROM `+tn("object")+` WHERE obj_no = $1)`
		if len(extra) > 0 {
			cols, vals = `, type_no, seq_to`, `, $5, $6`
		}
		v2rExec(t, w.admin, `INSERT INTO `+table+` (obj_no, name, min_grade, doc, content_hash, row_hash, seq_from`+cols+`) VALUES ($1,$2,0,'{"1":{"v":"x","g":"fact","b":"configured"}}',$3,$3,$4`+vals+`)`,
			append([]any{no, name, make([]byte, 32), from}, extra...)...)
	}
	obj(1, w.site, w.siteNo, 1, nil)
	obj(2, w.srv, w.serverNo, 1, nil)
	obj(3, w.gone, w.serverNo, 1, 4)
	row(a.typeTable(w.siteNo), 1, "site", 1)
	row(a.typeTable(w.serverNo), 2, "server v2", 3)
	row(tn("object_hist"), 2, "server v1", 1, w.serverNo, 3)
	row(tn("object_hist"), 3, "gone", 1, w.serverNo, 4)
	v2rExec(t, w.admin, `INSERT INTO `+tn("link")+` (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from) VALUES (1,$1,$2,2,1,'fact','configured',$3,$3,2)`,
		w.link.String(), w.linkNo, make([]byte, 32))
	v2rExec(t, w.admin, `INSERT INTO `+tn("object_identifier")+` (obj_no, scheme, value, scope, seq_from, seq_to) VALUES (2,'serial','S1','',1,NULL),(2,'serial','OLD','',1,3)`)
	v2rExec(t, w.admin, `UPDATE `+tn("head")+` SET applied_seq = 4, readable_through = 4, applied_through = 4`)
	prov := `{"203":{"v":"on","g":"fact","b":"observed","s":"2025-01-01T00:00:00Z","r":"src"}}`
	if _, ok := a.state[w.serverNo][graph.AttrServerPower]; ok {
		v2rExec(t, w.admin, `INSERT INTO `+tn(pgschema.StateTable(w.serverNo))+` (obj_no, prov) VALUES (2, $1)`, prov)
	} else {
		v2rExec(t, w.admin, `INSERT INTO `+tn("object_state")+` (obj_no, type_no, prov) VALUES (2, $1, $2)`, w.serverNo, prov)
	}
}

func (w *v2rWorld) v2rSnap(t *testing.T, rp graph.ReadPoint) graph.Snapshot {
	t.Helper()
	rp.KG = w.kg.ID
	sn, err := w.st.Snapshot(context.Background(), rp)
	if err != nil {
		t.Fatal(err)
	}
	return sn
}

func v2rIDs(rows []graph.ObjectRow) []graph.ObjectID {
	var out []graph.ObjectID
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func TestV2rReadsTheNewestAndThePastStates(t *testing.T) {
	w := v2rWorldOf(t)
	w.v2rSeed(t)
	ctx := context.Background()
	latest := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest})
	rows, err := latest.ScanObjects(ctx, graph.ObjectID{}, 10)
	noErr(t, "scan", err)
	if len(rows) != 2 {
		t.Fatalf("the newest state has %d objects, wanted 2", len(rows))
	}
	got, ok, err := latest.Object(ctx, w.srv)
	noErr(t, "object", err)
	if !ok || got.Name != "server v2" || got.SeqFrom != 3 || got.TypeNo != w.serverNo {
		t.Fatalf("server %+v %v", got, ok)
	}
	if _, ok, _ := latest.Object(ctx, w.gone); ok {
		t.Fatal("a retired object is in the newest state")
	}
	past := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadSeq, Seq: 2})
	got, ok, err = past.Object(ctx, w.srv)
	noErr(t, "past object", err)
	if !ok || got.Name != "server v1" || got.SeqTo != 3 {
		t.Fatalf("past server %+v %v", got, ok)
	}
	rows, err = past.ScanObjects(ctx, graph.ObjectID{}, 2)
	noErr(t, "past scan", err)
	if len(rows) != 2 {
		t.Fatalf("the page of the past state has %d, wanted 2", len(v2rIDs(rows)))
	}
	first := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadSeq, Seq: 1})
	all, err := first.ScanObjects(ctx, graph.ObjectID{}, 10)
	noErr(t, "scan 1", err)
	if len(all) != 3 {
		t.Fatalf("the state of change 1 has %d objects, wanted 3", len(all))
	}
	if rows, err = first.ScanObjects(ctx, all[2].ID, 10); err != nil || len(rows) != 0 {
		t.Fatalf("after the last: %v %v", rows, err)
	}
	if rows, err = first.ScanObjects(ctx, graph.ObjectID{}, 0); err != nil || len(rows) != 0 {
		t.Fatalf("a page of 0: %v %v", rows, err)
	}
	bulk, err := latest.(graph.BulkReader).ObjectsByID(ctx, []graph.ObjectID{w.srv, w.site, w.gone})
	noErr(t, "bulk", err)
	if len(bulk) != 2 {
		t.Fatalf("bulk read %d, wanted 2", len(bulk))
	}
}

func TestV2rReadsLinksIdentifiersAndVolatileValues(t *testing.T) {
	w := v2rWorldOf(t)
	w.v2rSeed(t)
	ctx := context.Background()
	latest := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest})
	l, ok, err := latest.Link(ctx, w.link)
	noErr(t, "link", err)
	if !ok || l.Src != w.srv || l.Dst != w.site || l.TypeNo != w.linkNo || l.SeqFrom != 2 {
		t.Fatalf("link %+v %v", l, ok)
	}
	links, err := latest.ScanLinks(ctx, graph.LinkID{}, 5)
	noErr(t, "scan links", err)
	if len(links) != 1 {
		t.Fatalf("%d links", len(links))
	}
	for _, d := range []graph.Direction{graph.Out, graph.In, graph.Both} {
		got, err := latest.LinksOf(ctx, w.srv, d)
		noErr(t, "links of", err)
		want := 0
		if d != graph.In {
			want = 1
		}
		if len(got) != want {
			t.Fatalf("direction %d: %d links, wanted %d", d, len(got), want)
		}
	}
	_, err = latest.LinksOf(ctx, w.srv, graph.Direction(99))
	wantCode(t, "direction", err, kernel.CodeInvalidValue)
	both, err := latest.(graph.BulkReader).LinksOfMany(ctx, []graph.ObjectID{w.srv, w.site}, graph.Both)
	noErr(t, "links of many", err)
	if len(both[w.srv]) != 1 || len(both[w.site]) != 1 {
		t.Fatalf("both ends: %v", both)
	}
	byID, err := latest.(graph.BulkReader).LinksByID(ctx, []graph.LinkID{w.link})
	noErr(t, "links by id", err)
	if len(byID) != 1 {
		t.Fatal("links by id")
	}
	before := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadSeq, Seq: 1})
	if _, ok, _ := before.Link(ctx, w.link); ok {
		t.Fatal("a link is in the state before it began")
	}
	if got, err := before.LinksOf(ctx, w.srv, graph.Both); err != nil || len(got) != 0 {
		t.Fatalf("links before: %v %v", got, err)
	}
	cur, err := latest.Identifiers(ctx, "serial", "S1")
	noErr(t, "identifiers", err)
	hist, err := latest.IdentifierHistory(ctx, "serial", "OLD")
	noErr(t, "identifier history", err)
	of, err := latest.(graph.IdentifierLister).IdentifiersOf(ctx, w.srv)
	noErr(t, "identifiers of", err)
	if len(cur) != 1 || cur[0].Object != w.srv || len(hist) != 1 || hist[0].SeqTo != 3 || len(of) != 1 {
		t.Fatalf("identifiers %v %v %v", cur, hist, of)
	}
	vol, ok, err := latest.Volatile(ctx, w.srv, graph.AttrServerPower)
	noErr(t, "volatile", err)
	if !ok || vol.Value != "on" || vol.SeenAt == "" || vol.Ref != "src" {
		t.Fatalf("volatile %+v %v", vol, ok)
	}
	if _, ok, _ := latest.Volatile(ctx, w.site, graph.AttrServerPower); ok {
		t.Fatal("a value of an object that has none")
	}
	if _, ok, _ := latest.Volatile(ctx, w.gone, graph.AttrServerPower); ok {
		t.Fatal("a value of a retired object")
	}
	_, _, err = w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadSeq, Seq: 2}).Volatile(ctx, w.srv, graph.AttrServerPower)
	wantCode(t, "volatile of a past state", err, graph.CodeVolatileOmitted)
	events, err := latest.IdentityEvents(ctx)
	if err != nil || len(events) != 0 {
		t.Fatalf("events %v %v", events, err)
	}
}

func TestV2rRefusesReadsThatCannotBeAnswered(t *testing.T) {
	w := v2rWorldOf(t)
	w.v2rSeed(t)
	ctx := context.Background()
	_, err := w.st.Snapshot(ctx, graph.ReadPoint{KG: w.kg.ID, Kind: graph.ReadSeq, Seq: 9})
	wantCode(t, "past the head", err, graph.CodeNotReadable)
	_, err = w.st.Snapshot(ctx, graph.ReadPoint{KG: w.kg.ID, Kind: graph.ReadKind(99)})
	wantCode(t, "unknown kind", err, kernel.CodeInvalidValue)
	_, err = w.st.Snapshot(ctx, graph.ReadPoint{KG: w.kg.ID, Kind: graph.ReadLatest, Generation: 5})
	wantCode(t, "generation", err, graph.CodeGenerationChanged)
	v2rExec(t, w.admin, `UPDATE `+w.a.t("head")+` SET history_floor = 3`)
	_, err = w.st.Snapshot(ctx, graph.ReadPoint{KG: w.kg.ID, Kind: graph.ReadSeq, Seq: 2})
	wantCode(t, "under the floor", err, graph.CodeNotReadable)
	v2rExec(t, w.admin, `UPDATE `+w.a.t("head")+` SET history_floor = 0, work = 'restoring'`)
	_, err = w.st.Snapshot(ctx, graph.ReadPoint{KG: w.kg.ID, Kind: graph.ReadLatest})
	wantCode(t, "restoring", err, CodeKGRestoring)
}

func TestV2rCatalogListsGetsAndDeletesAKG(t *testing.T) {
	w := v2rWorldOf(t)
	ctx := context.Background()
	got, err := w.st.GetKG(ctx, "v2r")
	noErr(t, "get", err)
	list, err := w.st.ListKGs(ctx)
	noErr(t, "list", err)
	gens, err := w.st.Generations(ctx, w.kg.ID)
	noErr(t, "generations", err)
	if got.ID != w.kg.ID || len(list) != 1 || len(gens) != 1 || got.Generation != 1 {
		t.Fatalf("%+v %v %v", got, list, gens)
	}
	_, err = w.st.CreateKG(ctx, graph.KG{})
	wantCode(t, "empty", err, kernel.CodeInvalidValue)
	_, err = w.st.CreateKG(ctx, w.kg)
	wantCode(t, "again", err, kernel.CodeConflict)
	vs, err := w.st.Versions(ctx, w.kg.ID)
	if err != nil || len(vs) != 0 {
		t.Fatalf("versions %v %v", vs, err)
	}
	noErr(t, "delete", w.st.DeleteKG(ctx, w.kg.ID))
	wantCode(t, "delete again", w.st.DeleteKG(ctx, w.kg.ID), graph.CodeKGNotFound)
	_, err = w.st.Head(ctx, w.kg.ID)
	wantCode(t, "head of a deleted KG", err, graph.CodeKGNotFound)
}
