package ingest

import (
	"strconv"
	"strings"
	"testing"
)

const hkaBase = `{"version":1,"id":"src_inventory_api","adapter":"http",
"connection":{"base_url":"https://inventory.example.test/api/v2"},
"webhooks":[{"id":"changed","kind":"signal","auth":{"type":"hmac","profile":"standard_webhooks"},"allow_ips":["192.0.2.0/24","2001:db8::1"],"limits":{"rate_per_second":20,"burst":100,"body_bytes":65536}}],
"datasets":[{"id":"ds_servers","request":{"method":"GET","path":"/servers"},"records_path":"items","natural_key":["serial"],"columns":[{"name":"serial"}]}]}`

func hkaParse(t *testing.T, doc string) Source {
	t.Helper()
	s, err := ParseAdapterSource(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("%v\n%s", err, doc)
	}
	return s
}

func hkaWith(t *testing.T, from, to string) Source { return hkaParse(t, adpSwap(t, hkaBase, from, to)) }

// [REQ-861]
func TestHkaAWebhookIsReadWithItsDefaultsAndHoldsNoSecret(t *testing.T) {
	s := hkaParse(t, hkaBase)
	w := s.Webhooks[0]
	if w.ID != "changed" || w.Kind != WebhookSignal || w.Auth.Type != WebhookAuthHMAC || w.Limits.BodyBytes != 65536 || len(w.AllowIPs) != 2 {
		t.Errorf("webhook %+v", w)
	}
	d := hkaParse(t, adpSwap(t, hkaBase, `,"limits":{"rate_per_second":20,"burst":100,"body_bytes":65536}`, ""))
	if l := d.Webhooks[0].Limits; l.RatePerSecond != DefaultWebhookRate || l.Burst != DefaultWebhookBurst || l.BodyBytes != DefaultWebhookBodyBytes {
		t.Errorf("defaults %+v", l)
	}
	for _, field := range []string{`"secret":"x"`, `"secret_id":"s"`, `"token":"x"`, `"password":"x"`} {
		_, err := ParseAdapterSource(strings.NewReader(adpSwap(t, hkaBase, `"type":"hmac",`, `"type":"hmac",`+field+`,`)))
		if err == nil {
			t.Errorf("a webhook that holds %s was read: a definition has no secret and no reference to one", field)
		}
	}
	for _, kind := range []string{"header_token", "basic", "url_token"} {
		hkaWith(t, `"type":"hmac","profile":"standard_webhooks"`, `"type":"`+kind+`"`)
	}
	hkaWith(t, `"profile":"standard_webhooks"`, `"profile":"body_only"`)
}

// [REQ-861]
func TestHkaEveryFieldOfAWebhookHasItsClassAndALimitHasItsComparison(t *testing.T) {
	seen := map[string]bool{}
	for name, rt := range webhookTypes() {
		for i := 0; i < rt.NumField(); i++ {
			key := name + "." + rt.Field(i).Name
			seen[key] = true
			switch webhookClasses[key] {
			case classMapping, classLimit, classKept:
			default:
				t.Errorf("the field %s of a webhook has no class for the change of the connection", key)
			}
		}
	}
	for key, class := range webhookClasses {
		if !seen[key] {
			t.Errorf("the table has %s, which is no field of a webhook", key)
		}
		if class == classLimit && !webhookLimitCompared[key] {
			t.Errorf("%s is a limit with no comparison of a raise", key)
		}
	}
	for key := range webhookLimitCompared {
		if webhookClasses[key] != classLimit {
			t.Errorf("%s has a comparison and is not a limit", key)
		}
	}
	if webhookClasses["Webhook.Auth"] != classKept || webhookClasses["Webhook.AllowIPs"] != classKept || webhookClasses["Webhook.Kind"] != classKept {
		t.Error("the proof, the addresses and the kind of a webhook are a change of the connection")
	}
}

// [REQ-861]
func TestHkaAChangeOfAWebhookIsAChangeOfTheConnectionButALowerLimit(t *testing.T) {
	before := hkaParse(t, hkaBase)
	connection := map[string]Source{
		"a new webhook":     hkaWith(t, `"webhooks":[`, `"webhooks":[{"id":"second","kind":"signal","auth":{"type":"basic"}},`),
		"another proof":     hkaWith(t, `"type":"hmac","profile":"standard_webhooks"`, `"type":"header_token"`),
		"another profile":   hkaWith(t, `"profile":"standard_webhooks"`, `"profile":"body_only"`),
		"another network":   hkaWith(t, `"192.0.2.0/24"`, `"198.51.100.0/24"`),
		"no network at all": hkaWith(t, `"allow_ips":["192.0.2.0/24","2001:db8::1"],`, ""),
		"a raised body":     hkaWith(t, `"body_bytes":65536`, `"body_bytes":131072`),
		"a raised rate":     hkaWith(t, `"rate_per_second":20`, `"rate_per_second":21`),
		"a raised burst":    hkaWith(t, `"burst":100`, `"burst":101`),
		"a limit left to its default, which is more": hkaWith(t, `,"limits":{"rate_per_second":20,"burst":100,"body_bytes":65536}`, ""),
		"the webhook taken away":                     hkaWith(t, `"webhooks":[{"id":"changed","kind":"signal","auth":{"type":"hmac","profile":"standard_webhooks"},"allow_ips":["192.0.2.0/24","2001:db8::1"],"limits":{"rate_per_second":20,"burst":100,"body_bytes":65536}}],`, ""),
		"the id":                                     hkaWith(t, `"id":"changed"`, `"id":"renamed"`),
	}
	for name, after := range connection {
		if got := ClassifyChange(before, after); got.Kind != ChangeConnection || len(got.Reasons) == 0 {
			t.Errorf("%s: %s %v", name, got.Kind, got.Reasons)
		}
	}
	mapping := map[string]Source{
		"a lower body": hkaWith(t, `"body_bytes":65536`, `"body_bytes":1024`),
		"a lower rate": hkaWith(t, `"rate_per_second":20`, `"rate_per_second":1`),
		"the same":     hkaParse(t, hkaBase),
	}
	for name, after := range mapping {
		if got := ClassifyChange(before, after); got.Kind != ChangeMapping {
			t.Errorf("%s: %s %v", name, got.Kind, got.Reasons)
		}
	}
	// A source that never had a webhook gets one: the first is a change of the connection too.
	bare := hkaParse(t, adpSwap(t, hkaBase, hkaBase[strings.Index(hkaBase, `"webhooks"`):strings.Index(hkaBase, `"datasets"`)], ""))
	if got := ClassifyChange(bare, before); got.Kind != ChangeConnection {
		t.Errorf("the first webhook: %s", got.Kind)
	}
}

// [REQ-861]
func TestHkaTheAddressesOfAWebhookAreReadAsAddressesAndNetworks(t *testing.T) {
	for _, ok := range []string{"192.0.2.7", "192.0.2.0/24", "2001:db8::/32", "2001:db8::1", "::/0", "0.0.0.0/0"} {
		if _, err := ParseAllowEntry(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	if p, _ := ParseAllowEntry("192.0.2.77/24"); p.String() != "192.0.2.0/24" {
		t.Errorf("a network is its network: %s", p)
	}
	for _, bad := range []string{"", "example.test", "192.0.2.0/33", "300.1.1.1", "10.0.0.0/8/9", "::ffff:192.0.2.7", "::ffff:192.0.2.0/120", "::ffff:0:0/96", "0:0:0:0:0:ffff:c000:207"} {
		if _, err := ParseAllowEntry(bad); err == nil {
			t.Errorf("%q was taken for an address", bad)
		}
	}
	// In a definition the refusal says what to write instead.
	if _, err := ParseAdapterSource(strings.NewReader(adpSwap(t, hkaBase, `"allow_ips":["192.0.2.0/24","2001:db8::1"]`, `"allow_ips":["::ffff:192.0.2.0/120"]`))); err == nil || !strings.Contains(err.Error(), "write it as IPv4") {
		t.Errorf("a network mapped into IPv6 in a definition: %v", err)
	}
	many := `"allow_ips":[` + strings.TrimSuffix(strings.Repeat(`"192.0.2.1",`, 257), ",") + `]`
	if _, err := ParseAdapterSource(strings.NewReader(adpSwap(t, hkaBase, `"allow_ips":["192.0.2.0/24","2001:db8::1"]`, many))); err == nil {
		t.Error("a list of 257 entries was read")
	}
}

// [REQ-861]
func TestHkaASignedWebhookTakesItsEventIDOnlyFromWhatTheSignatureCovers(t *testing.T) {
	signed := WebhookAuth{Type: WebhookAuthHMAC, Profile: ProfileStandardWebhooks}
	for _, ok := range []string{"$.event_id", "$.items[0].id", "webhook-id"} {
		if err := CheckWebhookEventID(signed, ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"header:X-Event-Id", "X-Event-Id", "webhook-timestamp", ""} {
		if err := CheckWebhookEventID(signed, bad); err == nil {
			t.Errorf("%q was taken: a header outside the signature can be changed by whoever sends the call again", bad)
		}
	}
	if err := CheckWebhookEventID(WebhookAuth{Type: WebhookAuthHMAC, Profile: ProfileBodyOnly}, "webhook-id"); err == nil {
		t.Error("a body-only signature covers no message id")
	}
	if err := CheckWebhookEventID(WebhookAuth{Type: WebhookAuthBasic}, "header:X-Event-Id"); err != nil {
		t.Errorf("a token has no signature to ask: %v", err)
	}
}

// [REQ-861]
// The highest values of the limits are what the entrance holds a body and a rate to before a caller is proven, so each is
// held on both sides: the value itself is read, one more is refused, and the highest values are the ones of the ADR.
func TestHkaTheHighestLimitsOfAWebhookAreHeldOnBothSides(t *testing.T) {
	if MaxWebhookBodyBytes != 4<<20 || MaxWebhookRate != 10000 || MaxWebhookBurst != 5000 {
		t.Fatalf("the highest limits are %d, %d and %d bytes, rate, burst", MaxWebhookBodyBytes, MaxWebhookRate, MaxWebhookBurst)
	}
	for field, max := range map[string]int{"body_bytes": MaxWebhookBodyBytes, "rate_per_second": MaxWebhookRate, "burst": MaxWebhookBurst} {
		from := map[string]string{"body_bytes": `"body_bytes":65536`, "rate_per_second": `"rate_per_second":20`, "burst": `"burst":100`}[field]
		at := hkaWith(t, from, `"`+field+`":`+strconv.Itoa(max))
		if at.Webhooks[0].Limits == nil {
			t.Errorf("%s at its highest value was not read", field)
		}
		for _, over := range []int{max + 1, 64 << 20, 99999999} {
			if _, err := ParseAdapterSource(strings.NewReader(adpSwap(t, hkaBase, from, `"`+field+`":`+strconv.Itoa(over)))); err == nil {
				t.Errorf("%s of %d was read; the highest is %d", field, over, max)
			}
		}
		if _, err := ParseAdapterSource(strings.NewReader(adpSwap(t, hkaBase, from, `"`+field+`":-1`))); err == nil {
			t.Errorf("%s of -1 was read", field)
		}
	}
}
