package pgingest

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The test of the copies the tests of this unit start from (ADR-0120). Every helper it adds starts with tpl, so that none of them
// meets a name of another test file of this folder.

// tplDump is what a test can see of the database of e.
func tplDump(t *testing.T, e *pgiEnv) []string {
	t.Helper()
	var name string
	if err := e.admin.QueryRowContext(e.ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	base, _ := kernel.LookupEnv(pgconn.TestDSNEnv)
	lines, err := pgconn.DumpTestDatabase(e.ctx, pgconn.WithDatabase(base, name))
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

// [REQ-270]
func TestTplTheDatabaseATestOfTheReceivingStartsFromIsTheOneOpenCreateKGAndAttachSourceMakeTheSlowWay(t *testing.T) {
	for _, c := range [][2]string{{pgiSourceJSON, pgiMappingJSON}, {rdfParitySource, rdfParityMapping}} {
		plan := pgiPlan(t, c[0], c[1])
		e := pgiOpenOn(t, pgconn.NewTestDatabase(t))
		kg := e.kg(t, 1)
		ref := e.attach(t, kg, plan)
		s := rdfNew(t, plan)
		if !reflect.DeepEqual(ref, s.ref) || !reflect.DeepEqual(kg, s.kg) {
			t.Fatalf("%s: the copy gives %+v %+v, the slow way %+v %+v", plan.Source.ID, s.ref, s.kg, ref, kg)
		}
		slow, copied := tplDump(t, e), tplDump(t, s.e)
		if strings.Join(slow, "\n") != strings.Join(copied, "\n") {
			t.Fatalf("%s: the copy differs from the database made the slow way (%d and %d lines)", plan.Source.ID, len(slow), len(copied))
		}
		if !strings.Contains(strings.Join(slow, "\n"), "row public."+pgschema.TableSourceDataset) {
			t.Errorf("%s: the dump does not show the datasets of the source", plan.Source.ID)
		}
		// what the test does next takes the numbers the slow way would: a second source gets the same numbers on both
		other := pgiPlan(t, `{"id":"src_tpl","adapter":"file","datasets":[{"id":"ds_one","parser":"csv","natural_key":["k"],"columns":[{"name":"k"}]}]}`,
			`{"feeds":[{"id":"f_one","dataset":"ds_one","object_type":"Rack","creates_objects":true,"basis":"declared","attributes":[{"attribute":"name","column":"k"}],"identity":[{"components":[{"property":"asset_tag","column":"k"}]}]}]}`)
		if a, b := e.attach(t, kg, other), s.e.attach(t, s.kg, other); !reflect.DeepEqual(a, b) {
			t.Errorf("%s: a second source is %+v on the copy and %+v the slow way", plan.Source.ID, b, a)
		}
	}
}
