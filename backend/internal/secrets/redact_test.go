package secrets_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"time"

	"encoding"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"log/slog"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// leaks says whether text shows the secret value in any plain form: as it is, in hex, in base64, or as a list of bytes.
func leaks(text, secret string) bool {
	forms := []string{
		secret,
		hex.EncodeToString([]byte(secret)),
		strings.ToUpper(hex.EncodeToString([]byte(secret))),
		base64.StdEncoding.EncodeToString([]byte(secret)),
		base64.RawURLEncoding.EncodeToString([]byte(secret)),
		fmt.Sprint([]byte(secret)),
		fmt.Sprintf("%d", []byte(secret)),
	}
	for _, f := range forms {
		if strings.Contains(text, f) {
			return true
		}
	}
	return false
}

type holder struct {
	Name  string
	Value secrets.Value
	List  []secrets.Value
	Map   map[string]secrets.Value
	Ptr   *secrets.Value
	Input secrets.Fields
}

type hidden struct {
	name   string
	value  secrets.Value
	input  secrets.Fields
	opened *secrets.Opened
}

var verbs = []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%t", "%5.2v", "%-20s", "%+q", "%08x", "%p", "%e"}

// [REQ-404]
func TestAnOpenedValueIsNeverPrintedByFmt(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	o := r.open(t, "one")
	pw, _ := o.Value("password")
	user, _ := o.Value("username")
	in := passwordFields()
	h := holder{Name: "x", Value: pw, List: []secrets.Value{pw, user}, Map: map[string]secrets.Value{"p": pw}, Ptr: &pw, Input: in}
	hid := hidden{name: "x", value: pw, input: in, opened: o}
	things := map[string]any{
		"value": pw, "value pointer": &pw, "list": []secrets.Value{pw}, "map": map[string]secrets.Value{"p": pw},
		"opened": o, "opened copy": *o, "holder": h, "holder pointer": &h, "unexported fields": hid, "unexported pointer": &hid,
		"fields": in, "fields pointer": &in, "create input": secrets.CreateInput{ID: "one", Fields: in},
		"key": r.key, "key value": *r.key, "any list": []any{pw, in, o},
	}
	for name, thing := range things {
		for _, verb := range verbs {
			out := fmt.Sprintf(verb, thing)
			if leaks(out, testPassword) || leaks(out, testUser) || leaks(out, rawKey(t, r.key)) {
				t.Errorf("%s printed with %s shows a value", name, verb)
			}
		}
		for _, out := range []string{fmt.Sprint(thing), fmt.Sprintln(thing)} {
			if leaks(out, testPassword) || leaks(out, testUser) {
				t.Errorf("%s printed plainly shows a value", name)
			}
		}
	}
	if got := fmt.Sprintf("%v|%s|%+v|%#v", pw, pw, pw, pw); got != strings.Repeat(secrets.Redacted+"|", 3)+secrets.Redacted {
		t.Fatalf("a value prints as %q", got)
	}
	if pw.String() != secrets.Redacted || o.String() != secrets.Redacted || in.String() != secrets.Redacted {
		t.Fatal("String shows a value")
	}
}

// [REQ-404]
func TestAnOpenedValueIsNeverEncodedByJSONOrTextEncoders(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	o := r.open(t, "one")
	pw, _ := o.Value("password")
	in := passwordFields()
	things := []any{pw, &pw, o, in, secrets.CreateInput{ID: "one", Fields: in}, []secrets.Value{pw}, map[string]secrets.Value{"p": pw},
		holder{Value: pw, Input: in}, map[string]any{"v": pw, "f": in}, r.key}
	for _, thing := range things {
		for _, enc := range []func(any) ([]byte, error){json.Marshal, func(v any) ([]byte, error) { return json.MarshalIndent(v, "", " ") }} {
			out, err := enc(thing)
			if err != nil {
				t.Fatal(err)
			}
			if leaks(string(out), testPassword) || leaks(string(out), testUser) || leaks(string(out), rawKey(t, r.key)) {
				t.Errorf("JSON of %T shows a value", thing)
			}
		}
	}
	if out, _ := json.Marshal(pw); string(out) != `"`+secrets.Redacted+`"` {
		t.Fatalf("JSON of a value: %s", out)
	}
	for _, m := range []encoding.TextMarshaler{pw, o, in, r.key} {
		text, err := m.MarshalText()
		if err != nil || string(text) != secrets.Redacted {
			t.Fatalf("MarshalText of %T: %q %v", m, text, err)
		}
	}
	if gs, ok := any(pw).(fmt.GoStringer); !ok || gs.GoString() != secrets.Redacted {
		t.Fatal("GoString shows a value")
	}
}

// [REQ-404]
func TestAnOpenedValueIsNeverWrittenByTheLogger(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	o := r.open(t, "one")
	pw, _ := o.Value("password")
	in := passwordFields()
	var text, js bytes.Buffer
	loggers := []*slog.Logger{
		slog.New(slog.NewTextHandler(&text, &slog.HandlerOptions{Level: slog.LevelDebug})),
		slog.New(slog.NewJSONHandler(&js, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	for _, l := range loggers {
		l.Info("opened", "value", pw, "opened", o, "fields", in, "key", r.key, "again", &pw, slog.Any("any", pw),
			slog.Group("g", slog.Any("v", pw)), "list", []secrets.Value{pw}, "input", secrets.CreateInput{ID: "one", Fields: in})
		l.With("value", pw).Warn("with", "id", "one")
		l.WithGroup("grp").Error("group", "value", pw)
	}
	for _, out := range []string{text.String(), js.String()} {
		if leaks(out, testPassword) || leaks(out, testUser) || leaks(out, rawKey(t, r.key)) {
			t.Fatalf("the log shows a value:\n%s", redactOnly(out))
		}
		if !strings.Contains(out, secrets.Redacted) {
			t.Fatal("the log shows nothing where the value was")
		}
	}
	if pw.LogValue().String() != secrets.Redacted || in.LogValue().String() != secrets.Redacted || o.LogValue().String() != secrets.Redacted {
		t.Fatal("LogValue shows a value")
	}
}

// redactOnly is for a failure message: it must not show what it complains about.
func redactOnly(s string) string { return fmt.Sprintf("%d bytes of log", len(s)) }

// [REQ-404]
func TestTheValueComesOutOnlyThroughRevealAndCloseOverwritesIt(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	o, err := r.svc.Open(ctx, "one", okUse)
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := o.Value("password")
	sameText(t, pw.Reveal(), testPassword, "reveal")
	o.Close()
	o.Close()
	if got := pw.Reveal(); got == testPassword || strings.Trim(got, "\x00") != "" {
		t.Fatal("Close left the value in memory")
	}
	var zero secrets.Value
	if zero.Reveal() != "" || zero.String() != secrets.Redacted {
		t.Fatal("a zero Value is not empty and redacted")
	}
}

// [REQ-404]
func TestNothingThatDescribesASecretHoldsAValue(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	if _, err := r.svc.AddVersion(ctx, "one", secrets.NewFields(map[string]string{"username": testUser, "password": "not-a-real-password-v2"})); err != nil {
		t.Fatal(err)
	}
	list, err := r.svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := r.svc.Get(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	for _, thing := range []any{list, meta, fmt.Sprintf("%+v %#v", meta, list)} {
		out, _ := json.Marshal(thing)
		text := string(out) + fmt.Sprintf("%v %+v %#v", thing, thing, thing)
		for _, v := range []string{testPassword, testUser, "not-a-real-password-v2"} {
			if leaks(text, v) {
				t.Fatal("a description shows a value")
			}
		}
	}
	// What is stored holds no value in the clear either: the records as JSON, in every form of the value.
	recs, err := r.store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := json.Marshal(recs)
	for _, v := range []string{testPassword, testUser, "not-a-real-password-v2", rawKey(t, r.key)} {
		if leaks(string(stored), v) {
			t.Fatal("a stored record shows a value")
		}
	}
}

// [REQ-404]
func TestFieldsReadFromJSONAreAsHiddenAsFieldsMadeInCode(t *testing.T) {
	var req struct {
		ID     string         `json:"id"`
		Fields secrets.Fields `json:"fields"`
	}
	body := `{"id":"one","fields":{"username":"` + testUser + `","password":"` + testPassword + `"}}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	if names := req.Fields.Names(); len(names) != 2 || names[0] != "password" || names[1] != "username" {
		t.Fatalf("names: %v", names)
	}
	for _, out := range []string{fmt.Sprintf("%v %+v %#v", req, req, req), fmt.Sprintf(verbs[len(verbs)-2], req), fmt.Sprint(&req)} {
		if leaks(out, testPassword) || leaks(out, testUser) {
			t.Fatal("a request read from JSON shows a value")
		}
	}
	// The request puts the secret in as the values say.
	r := newRig(t)
	if _, err := r.svc.Create(ctx, secrets.CreateInput{ID: req.ID, Kind: secrets.KindPassword, Fields: req.Fields,
		Scope: []string{"crm"}, AllowedHosts: []secrets.Address{dbAddress}}); err != nil {
		t.Fatal(err)
	}
	sameText(t, field(t, r.open(t, "one"), "password"), testPassword, "from JSON")
	var bad secrets.Fields
	for _, text := range []string{`[]`, `"x"`, `{"a":1}`, `{"a":null,"b":[]}`} {
		if err := bad.UnmarshalJSON([]byte(text)); err == nil {
			t.Errorf("%s was accepted", text)
		}
	}
}

// rawKey is the bytes of the master key themselves. The searches below look for these bytes in their plain, hex and
// base64 forms, not for the text of the key file.
func rawKey(t *testing.T, k *secrets.MasterKey) string {
	t.Helper()
	var f struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(k.Encode(), &f); err != nil {
		t.Fatal(err)
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := enc.DecodeString(f.Key); err == nil && len(raw) == 32 {
			return string(raw)
		}
	}
	t.Fatal("the key file holds no 32 bytes")
	return ""
}

type keyHolder struct {
	K  secrets.MasterKey
	P  *secrets.MasterKey
	L  []secrets.MasterKey
	M  map[string]*secrets.MasterKey
	hk secrets.MasterKey
	pk *secrets.MasterKey
}

// [REQ-404]
func TestAMasterKeyIsNeverShownByAnyOutputOfAnythingThatHoldsIt(t *testing.T) {
	r := newRig(t)
	r.createPassword(t, "one")
	key := r.key
	secret := rawKey(t, key)
	cfg := secrets.Config{Store: r.store, Keys: secrets.StaticKey(key), Clock: kernel.NewManualClock(time.Unix(1, 0)), Bytes: kernel.NewSeededBytes(3)}
	svc, err := secrets.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	kh := keyHolder{K: *key, P: key, L: []secrets.MasterKey{*key}, M: map[string]*secrets.MasterKey{"a": key}, hk: *key, pk: key}
	things := map[string]any{
		"key": key, "key value": *key, "struct": struct{ k secrets.MasterKey }{*key}, "struct pointer": struct{ k *secrets.MasterKey }{key},
		"holder": kh, "holder pointer": &kh, "list": []*secrets.MasterKey{key}, "map": map[string]secrets.MasterKey{"a": *key},
		"static key": secrets.StaticKey(key), "config": cfg, "config pointer": &cfg, "service": svc,
		"writer": svc.Writer(), "opener": svc.Opener(), "describer": svc.Describer(), "ports": []any{svc.Writer(), svc.Opener(), svc.Describer()},
		"wrapped error": fmt.Errorf("wrap: %w", errors.Join(errors.New("x"), kernel.Errorf(secrets.CodeMasterKeyInvalid, "bad"))),
	}
	verbs := append(append([]string{}, verbs...), "%T", "%10v", "%+#v")
	for name, thing := range things {
		for _, verb := range verbs {
			if out := fmt.Sprintf(verb, thing); leaks(out, secret) {
				t.Errorf("%s printed with %s shows the key", name, verb)
			}
		}
		if out, err := json.Marshal(thing); err == nil && leaks(string(out), secret) {
			t.Errorf("JSON of %s shows the key", name)
		}
		var buf bytes.Buffer
		slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "thing", thing)
		slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "thing", thing)
		if leaks(buf.String(), secret) {
			t.Errorf("the log of %s shows the key", name)
		}
	}
	// A walk over everything reachable by reflection finds no byte of the key in any field, exported or not.
	for name, thing := range things {
		if found := walkFor(reflect.ValueOf(thing), []byte(secret), map[uintptr]bool{}, 0); found {
			t.Errorf("%s holds the bytes of the key in a field", name)
		}
	}
	if _, err := svc.Open(context.Background(), "one", okUse); err != nil {
		t.Fatal(err)
	}
}

// walkFor looks through v, unexported fields included, for text or bytes that hold needle. Function values hide their
// captured variables from reflection, which is what the types under test rely on.
func walkFor(v reflect.Value, needle []byte, seen map[uintptr]bool, depth int) bool {
	if depth > 12 || !v.IsValid() {
		return false
	}
	switch v.Kind() {
	case reflect.String:
		return strings.Contains(v.String(), string(needle))
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, v.Len())
			for i := range b {
				b[i] = byte(v.Index(i).Uint())
			}
			return bytes.Contains(b, needle)
		}
		for i := 0; i < v.Len(); i++ {
			if walkFor(v.Index(i), needle, seen, depth+1) {
				return true
			}
		}
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			if walkFor(it.Key(), needle, seen, depth+1) || walkFor(it.Value(), needle, seen, depth+1) {
				return true
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if walkFor(v.Field(i), needle, seen, depth+1) {
				return true
			}
		}
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return false
		}
		if v.Kind() == reflect.Pointer {
			if seen[v.Pointer()] {
				return false
			}
			seen[v.Pointer()] = true
		}
		return walkFor(v.Elem(), needle, seen, depth+1)
	}
	return false
}
