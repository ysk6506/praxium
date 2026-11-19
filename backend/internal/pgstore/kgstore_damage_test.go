package pgstore

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of what the new storage does when its tables are damaged or hold what it did not write: every
// operation fails with an error and none answers wrongly. Their names start with v2d.

type v2dOps map[string]func() error

// v2dReads are the reads of a KG on the newest state and on a past one.
func v2dReads(t *testing.T, w *v2rWorld) v2dOps {
	t.Helper()
	ctx := context.Background()
	latest := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest})
	past := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadSeq, Seq: 2})
	both := map[string]graph.Snapshot{"latest": latest, "past": past}
	ops := v2dOps{}
	for name, sn := range both {
		sn := sn
		bulk := sn.(graph.BulkReader)
		ops[name+" object"] = func() error { _, _, err := sn.Object(ctx, w.srv); return err }
		ops[name+" objects"] = func() error { _, err := bulk.ObjectsByID(ctx, []graph.ObjectID{w.srv, w.site}); return err }
		ops[name+" scan"] = func() error { _, err := sn.ScanObjects(ctx, graph.ObjectID{}, 5); return err }
		ops[name+" link"] = func() error { _, _, err := sn.Link(ctx, w.link); return err }
		ops[name+" scan links"] = func() error { _, err := sn.ScanLinks(ctx, graph.LinkID{}, 5); return err }
		ops[name+" links of"] = func() error { _, err := sn.LinksOf(ctx, w.srv, graph.Both); return err }
		ops[name+" identifiers"] = func() error { _, err := sn.Identifiers(ctx, "serial", "S1"); return err }
		ops[name+" identifier history"] = func() error { _, err := sn.IdentifierHistory(ctx, "serial", "S1"); return err }
		ops[name+" identifiers of"] = func() error { _, err := sn.(graph.IdentifierLister).IdentifiersOf(ctx, w.srv); return err }
		ops[name+" events"] = func() error { _, err := sn.IdentityEvents(ctx); return err }
	}
	ops["volatile"] = func() error { _, _, err := latest.Volatile(ctx, w.srv, graph.AttrServerPower); return err }
	ops["versions"] = func() error { _, err := w.st.Versions(ctx, w.kg.ID); return err }
	ops["head"] = func() error { _, err := w.st.Head(ctx, w.kg.ID); return err }
	ops["lease"] = func() error { _, _, err := w.st.Lease(ctx, w.kg.ID); return err }
	ops["snapshot"] = func() error {
		_, err := w.st.Snapshot(ctx, graph.ReadPoint{KG: w.kg.ID, Kind: graph.ReadLatest})
		return err
	}
	return ops
}

func v2dDamage(t *testing.T, w *v2rWorld, table string, ops v2dOps, names ...string) {
	t.Helper()
	q := w.a.t(table)
	v2rExec(t, w.admin, `ALTER TABLE `+q+` RENAME TO `+pgschemaQuote(table+"_away"))
	defer v2rExec(t, w.admin, `ALTER TABLE `+w.a.t(table+"_away")+` RENAME TO `+pgschemaQuote(table))
	for _, name := range names {
		if err := ops[name](); err == nil {
			t.Errorf("%s with %s missing: no error", name, table)
		}
	}
}

func pgschemaQuote(name string) string { return pgschema.QuoteIdent(name) }

func TestV2dReadsFailWhenATableTheyNeedIsMissing(t *testing.T) {
	w := v2rWorldOf(t)
	w.v2rSeed(t)
	ops := v2dReads(t, w)
	v2dDamage(t, w, pgschema.TableObject, ops, "latest object", "latest objects", "latest scan", "past scan", "latest links of", "latest identifiers", "latest identifiers of", "latest events",
		"latest link", "latest scan links", "past link", "past links of", "volatile")
	v2dDamage(t, w, pgschema.TypeTable(w.serverNo), ops, "latest object", "latest objects", "latest scan", "past object", "past objects", "past scan")
	v2dDamage(t, w, pgschema.TableObjectHist, ops, "past object", "past objects", "past scan")
	v2dDamage(t, w, pgschema.TableLink, ops, "latest link", "latest scan links", "latest links of", "past link", "past scan links", "past links of")
	v2dDamage(t, w, pgschema.TableLinkHist, ops, "past link", "past scan links", "past links of")
	v2dDamage(t, w, pgschema.TableObjectIdentifier, ops, "latest identifiers", "latest identifier history", "latest identifiers of")
	v2dDamage(t, w, pgschema.TableIdentityEvent, ops, "latest events")
	v2dDamage(t, w, pgschema.TableVersion, ops, "versions")
	v2dDamage(t, w, pgschema.TableVersionHashPart, ops, "versions")
	v2dDamage(t, w, w.volatileTable(), ops, "volatile")
	v2dDamage(t, w, pgschema.TableLease, ops, "lease")
	v2dDamage(t, w, pgschema.TableHead, ops, "head", "snapshot", "latest object", "versions")
}

func TestV2dReadsFailOnRowsThatHoldWhatTheStoreDidNotWrite(t *testing.T) {
	w := v2rWorldOf(t)
	w.v2rSeed(t)
	ops := v2dReads(t, w)
	obj, hist := w.a.typeTable(w.serverNo), w.a.t(pgschema.TableObjectHist)
	bad := []struct {
		name, undo, do string
		ops            []string
	}{
		{"a hash of the wrong length", `UPDATE ` + obj + ` SET content_hash = '\x0102'`, `UPDATE ` + obj + ` SET content_hash = '\x` + zeros(32) + `'`, []string{"latest object", "latest scan"}},
		{"a grade that is no grade", `UPDATE ` + obj + ` SET min_grade = 99`, `UPDATE ` + obj + ` SET min_grade = 0`, []string{"latest object", "latest objects"}},
		{"an attribute that has no number", `UPDATE ` + obj + ` SET doc = '{"x":{"v":1,"g":"fact","b":"configured"}}'`, `UPDATE ` + obj + ` SET doc = '{}'`, []string{"latest object"}},
		{"source refs that are not a list", `UPDATE ` + obj + ` SET source_refs = '{"a":1}'`, `UPDATE ` + obj + ` SET source_refs = '[]'`, []string{"latest object"}},
		{"a history hash of the wrong length", `UPDATE ` + hist + ` SET content_hash = '\x01'`, `UPDATE ` + hist + ` SET content_hash = '\x` + zeros(32) + `'`, []string{"past object", "past scan"}},
		{"a ref that is no id", `UPDATE ` + w.a.t("object") + ` SET ref = 'not an id' WHERE obj_no = 2`, `UPDATE ` + w.a.t("object") + ` SET ref = '` + w.srv.String() + `' WHERE obj_no = 2`, []string{"latest scan"}},
		{"a link end that is no id", `UPDATE ` + w.a.t("object") + ` SET ref = 'not an id' WHERE obj_no = 1`, `UPDATE ` + w.a.t("object") + ` SET ref = '` + w.site.String() + `' WHERE obj_no = 1`, []string{"latest scan links", "past scan links"}},
		{"a link ref that is no id", `UPDATE ` + w.a.t("link") + ` SET ref = 'not an id'`, `UPDATE ` + w.a.t("link") + ` SET ref = '` + w.link.String() + `'`, []string{"latest scan links", "latest links of"}},
		{"a link hash of the wrong length", `UPDATE ` + w.a.t("link") + ` SET content_hash = '\x01'`, `UPDATE ` + w.a.t("link") + ` SET content_hash = '\x` + zeros(32) + `'`, []string{"latest scan links"}},
		{"a link document that has no numbers", `UPDATE ` + w.a.t("link") + ` SET doc = '{"x":{"v":1,"g":"fact","b":"configured"}}'`, `UPDATE ` + w.a.t("link") + ` SET doc = '{}'`, []string{"latest scan links"}},
		{"a link source refs that are no list", `UPDATE ` + w.a.t("link") + ` SET source_refs = '{"a":1}'`, `UPDATE ` + w.a.t("link") + ` SET source_refs = '[]'`, []string{"latest scan links"}},
		{"an identifier of an object whose ref is no id", `UPDATE ` + w.a.t("object") + ` SET ref = 'x y' WHERE obj_no = 2`, `UPDATE ` + w.a.t("object") + ` SET ref = '` + w.srv.String() + `' WHERE obj_no = 2`, []string{"latest identifiers"}},
		{"a volatile value that is not JSON of the kind", `UPDATE ` + w.a.t(w.volatileTable()) + ` SET prov = '{"203":"x"}'`, `UPDATE ` + w.a.t(w.volatileTable()) + ` SET prov = '{}'`, []string{"volatile"}},
	}
	for _, c := range bad {
		v2rExec(t, w.admin, c.undo)
		for _, name := range c.ops {
			if err := ops[name](); err == nil {
				t.Errorf("%s: %s: no error", c.name, name)
			}
		}
		v2rExec(t, w.admin, c.do)
	}
}

func zeros(n int) string {
	out := make([]byte, 2*n)
	for i := range out {
		out[i] = '0'
	}
	return string(out)
}

func TestV2dEveryOperationFailsOnAClosedPool(t *testing.T) {
	w := v2rWorldOf(t)
	w.v2rSeed(t)
	ctx, kg := context.Background(), w.kg.ID
	sn := w.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest})
	ghost, _ := kernel.ParseID[graph.KGKind]("kg-0123456789abcdef")
	if err := w.st.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(0, 0)
	ops := v2dOps{
		"create": func() error {
			_, err := w.st.CreateKG(ctx, graph.KG{ID: ghost, Name: "n", Role: graph.RoleTest, DefinitionSetHash: w.kg.DefinitionSetHash})
			return err
		},
		"get":      func() error { _, err := w.st.GetKG(ctx, "v2r"); return err },
		"list":     func() error { _, err := w.st.ListKGs(ctx); return err },
		"delete":   func() error { return w.st.DeleteKG(ctx, kg) },
		"gens":     func() error { _, err := w.st.Generations(ctx, kg); return err },
		"head":     func() error { _, err := w.st.Head(ctx, kg); return err },
		"lease":    func() error { _, _, err := w.st.Lease(ctx, kg); return err },
		"versions": func() error { _, err := w.st.Versions(ctx, kg); return err },
		"snapshot": func() error {
			_, err := w.st.Snapshot(ctx, graph.ReadPoint{KG: kg, Kind: graph.ReadLatest})
			return err
		},
		"acquire": func() error {
			_, err := w.st.AcquireLease(ctx, graph.LeaseRequest{KG: kg, Owner: "x", TTL: time.Second, Now: now})
			return err
		},
		"renew": func() error {
			_, err := w.st.RenewLease(ctx, graph.Lease{KG: kg, Owner: "x", Attempt: 1}, time.Second, now)
			return err
		},
		"release":  func() error { return w.st.ReleaseLease(ctx, graph.Lease{KG: kg, Owner: "x", Attempt: 1}) },
		"object":   func() error { _, _, err := sn.Object(ctx, w.srv); return err },
		"volatile": func() error { _, _, err := sn.Volatile(ctx, w.srv, graph.AttrServerPower); return err },
		"events":   func() error { _, err := sn.IdentityEvents(ctx); return err },
	}
	for name, op := range ops {
		if err := op(); err == nil || kernel.CodeOf(err) == graph.CodeKGNotFound {
			t.Errorf("%s on a closed pool: %v", name, err)
		}
	}
	other := &StoreV2{rt: w.st.rt, models: w.st.models}
	if _, err := other.Head(ctx, kg); err == nil || kernel.CodeOf(err) == graph.CodeKGNotFound {
		t.Errorf("a KG that is not known yet, on a closed pool: %v", err)
	}
	if _, err := other.Head(ctx, graph.KGID{}); kernel.CodeOf(err) != graph.CodeKGNotFound {
		t.Errorf("no KG: %v", err)
	}
}

func (w *v2rWorld) volatileTable() string {
	if _, ok := w.a.state[w.serverNo][graph.AttrServerPower]; ok {
		return pgschema.StateTable(w.serverNo)
	}
	return pgschema.TableObjectState
}
