package secrets_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/secrets"
)

func methodNames[T any]() []string {
	typ := reflect.TypeFor[T]()
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	sort.Strings(names)
	return names
}

// [REQ-406]
func TestTheWritingTheOpeningAndTheDescribingArePortsApart(t *testing.T) {
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"Writer", methodNames[secrets.Writer](), []string{"AddVersion", "Create", "Delete", "RetireVersion", "SetAllowedHosts", "SetScope"}},
		{"Opener", methodNames[secrets.Opener](), []string{"Open"}},
		{"Describer", methodNames[secrets.Describer](), []string{"Get", "List"}},
	}
	for _, c := range cases {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s has the methods %v, want %v", c.name, c.got, c.want)
		}
	}
	// The service has all three, so the composition root can hand each role the one it needs.
	r := newRig(t)
	var (
		w secrets.Writer    = r.svc
		o secrets.Opener    = r.svc
		d secrets.Describer = r.svc
	)
	if _, err := w.Create(ctx, secrets.CreateInput{ID: "one", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": testToken}),
		Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}}); err != nil {
		t.Fatal(err)
	}
	if opened, err := o.Open(ctx, "one", okUse); err != nil {
		t.Fatal(err)
	} else {
		opened.Close()
	}
	if list, err := d.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("list: %v", err)
	}
}

// walk says whether a type holds, anywhere in its fields, a type that can carry a value in the clear.
func walk(typ reflect.Type, path string, seen map[reflect.Type]bool, bad func(string)) {
	if seen[typ] {
		return
	}
	seen[typ] = true
	switch typ {
	case reflect.TypeFor[secrets.Value](), reflect.TypeFor[secrets.Fields](), reflect.TypeFor[secrets.Opened](), reflect.TypeFor[[]byte]():
		bad(path)
		return
	}
	switch typ.Kind() {
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			walk(typ.Field(i).Type, path+"."+typ.Field(i).Name, seen, bad)
		}
	case reflect.Slice, reflect.Array, reflect.Pointer:
		walk(typ.Elem(), path+"[]", seen, bad)
	case reflect.Map:
		walk(typ.Key(), path+"{key}", seen, bad)
		walk(typ.Elem(), path+"{}", seen, bad)
	}
}

// [REQ-404] [REQ-406]
func TestWhatTheDescribingPortReturnsHasNoPlaceForAValue(t *testing.T) {
	describer := reflect.TypeFor[secrets.Describer]()
	for i := 0; i < describer.NumMethod(); i++ {
		method := describer.Method(i)
		for j := 0; j < method.Type.NumOut(); j++ {
			walk(method.Type.Out(j), method.Name, map[reflect.Type]bool{}, func(path string) {
				t.Errorf("%s can return a value holder at %s", method.Name, path)
			})
		}
	}
}
