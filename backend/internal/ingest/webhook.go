package ingest

import (
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strings"
)

// The receiving webhooks of a source (ADR-0107): the addresses at which a product tells Praxium that something changed.
// The definition says what a webhook is and how a call of it is proven; it holds no secret and no reference to one, because
// the server makes the secret of a webhook and keeps it apart (secrets.SecretIDFor names it by the source and the webhook).

// The kinds of a webhook.
const (
	// WebhookSignal says "the source changed, read it again". It carries no data: its body is thrown away.
	WebhookSignal = "signal"
	// WebhookRecords and WebhookAlarms are named in the design and have no way to be handled yet: a definition that
	// has one is refused until the change that handles them.
	WebhookRecords = "records"
	WebhookAlarms  = "alarms"
)

// The types of the proof of a webhook, and the profiles of a signature.
const (
	WebhookAuthHMAC        = "hmac"
	WebhookAuthHeaderToken = "header_token"
	WebhookAuthBasic       = "basic"
	WebhookAuthURLToken    = "url_token"

	// ProfileStandardWebhooks signs id.timestamp.body with HMAC-SHA256, and the id and the time are in the signature.
	ProfileStandardWebhooks = "standard_webhooks"
	// ProfileBodyOnly signs the body alone: it has no time, so a call that was caught can be sent again. It is the weak form.
	ProfileBodyOnly = "body_only"
)

// The limits of a webhook: the defaults and the highest values (ADR-0107, the table of the limits).
const (
	DefaultWebhookRate      = 50
	DefaultWebhookBurst     = 500
	DefaultWebhookBodyBytes = 256 << 10
	MaxWebhookRate          = 10000
	MaxWebhookBurst         = 5000
	MaxWebhookBodyBytes     = 4 << 20
	// MaxWebhooksPerSource is the most webhooks a source has.
	MaxWebhooksPerSource = 32
	maxAllowIPs          = 256
)

// Webhook is one receiving webhook of a source.
type Webhook struct {
	ID   string      `json:"id"`
	Kind string      `json:"kind"`
	Auth WebhookAuth `json:"auth"`
	// AllowIPs are the addresses or networks a call may come from; none means from anywhere.
	AllowIPs []string       `json:"allow_ips,omitempty"`
	Limits   *WebhookLimits `json:"limits,omitempty"`
}

// WebhookAuth is how a call is proven.
type WebhookAuth struct {
	Type string `json:"type"`
	// Profile is the form of the signature of a webhook of the type hmac; the others have none.
	Profile string `json:"profile,omitempty"`
}

// WebhookLimits are what a webhook takes: calls a second, a burst and the size of a body.
type WebhookLimits struct {
	RatePerSecond int `json:"rate_per_second,omitempty"`
	Burst         int `json:"burst,omitempty"`
	BodyBytes     int `json:"body_bytes,omitempty"`
}

// The class of each field of the types of a webhook for ClassifyChange, as "Type.Field" (see webhookClassOf). The table is
// apart from changeClasses, whose fixed test reads the types of the older definitions; webhookTypes lists these and a test
// reads every field of them, so a field that is added is a change of the connection until it is here.
var webhookClasses = map[string]string{
	"Webhook.ID": classKept, "Webhook.Kind": classKept, "Webhook.Auth": classKept, "Webhook.AllowIPs": classKept, "Webhook.Limits": classLimit,
	"WebhookAuth.Type": classKept, "WebhookAuth.Profile": classKept,
	"WebhookLimits.RatePerSecond": classLimit, "WebhookLimits.Burst": classLimit, "WebhookLimits.BodyBytes": classLimit,
}

// webhookLimitCompared lists the limit fields of a webhook that a comparison of "is it raised" is written for.
var webhookLimitCompared = map[string]bool{
	"Webhook.Limits": true, "WebhookLimits.RatePerSecond": true, "WebhookLimits.Burst": true, "WebhookLimits.BodyBytes": true,
}

// webhookTypes are the types the table of the webhooks covers, for the test that reads their fields.
func webhookTypes() map[string]reflect.Type {
	return map[string]reflect.Type{"Webhook": reflect.TypeOf(Webhook{}), "WebhookAuth": reflect.TypeOf(WebhookAuth{}), "WebhookLimits": reflect.TypeOf(WebhookLimits{})}
}

var (
	ecWhAdapter  = regAdapter("source.webhook.not_for_adapter", inv, "A source of the file adapter has webhooks; only the http and postgres adapters have them.", "source,adapter")
	ecWhCount    = regAdapter("source.webhook.too_many", inv, "The source has more webhooks than a source may have.", "source,count,max")
	ecWhID       = regAdapter("source.webhook.id_invalid", inv, "A webhook has no id or an id that does not follow the grammar of an id.", "source,webhook,id,pattern,max_length")
	ecWhDup      = regAdapter("source.webhook.id_repeated", inv, "Two webhooks of the source have the same id.", "source,webhook,first")
	ecWhKind     = regAdapter("source.webhook.kind_unsupported", unsp, "The kind of the webhook is not one of signal, records and alarms.", "source,webhook,kind,supported")
	ecWhNotOpen  = regAdapter("source.webhook.kind_not_handled_yet", unsp, "The kind of the webhook is in the design and has no way to be handled yet.", "source,webhook,kind")
	ecWhAuth     = regAdapter("source.webhook.auth_unsupported", unsp, "The type of the proof of the webhook is not one of hmac, header_token, basic and url_token.", "source,webhook,type,supported")
	ecWhProfile  = regAdapter("source.webhook.profile_invalid", inv, "The profile of the signature is not one of standard_webhooks and body_only, or is given to a type that has no signature.", "source,webhook,profile,reason")
	ecWhAuthKind = regAdapter("source.webhook.auth_not_for_kind", inv, "The type or the profile of the proof is not allowed for the kind of the webhook (url_token and body_only are for signal only).", "source,webhook,kind,type,profile")
	ecWhIPs      = regAdapter("source.webhook.allow_ips_invalid", inv, "An entry of allow_ips is not an IP address or a network in CIDR form, or the list is too long.", "source,webhook,reason")
	ecWhLimit    = regAdapter("source.webhook.limit_out_of_range", inv, "A limit of the webhook is outside the range it may have.", "source,webhook,field,value,min,max")
)

// normalizeWebhooks checks the webhooks of a source and fills the defaults of their limits.
func (s Source) normalizeWebhooks(c *collector) []Webhook {
	if len(s.Webhooks) == 0 {
		return nil
	}
	if s.Adapter == AdapterFile {
		c.add(ecWhAdapter, ptr("webhooks"), Params{"source": s.ID, "adapter": s.Adapter}, "source %s: a source of the file adapter has no webhooks", s.ID)
		return s.Webhooks
	}
	if len(s.Webhooks) > MaxWebhooksPerSource {
		c.add(ecWhCount, ptr("webhooks"), Params{"source": s.ID, "count": len(s.Webhooks), "max": MaxWebhooksPerSource},
			"source %s has %d webhooks, and a source has at most %d", s.ID, len(s.Webhooks), MaxWebhooksPerSource)
	}
	out := make([]Webhook, len(s.Webhooks))
	first := map[string]int{}
	for i, w := range s.Webhooks {
		out[i] = w.normalize(c, s.ID, i)
		if at, dup := first[w.ID]; dup && w.ID != "" {
			c.add(ecWhDup, ptr("webhooks", i, "id"), Params{"source": s.ID, "webhook": w.ID, "first": at}, "source %s: webhook %s appears twice", s.ID, w.ID)
		} else if w.ID != "" {
			first[w.ID] = i
		}
	}
	return out
}

func (w Webhook) normalize(c *collector, source string, i int) Webhook {
	at := ptr("webhooks", i)
	if w.ID == "" {
		c.add(ecWhID, at+ptr("id"), Params{"source": source, "webhook": ""}, "source %s: a webhook needs an id", source)
	}
	c.checkID(ecWhID, at+ptr("id"), "webhook", w.ID, Params{"source": source, "webhook": clip(w.ID, idClipAt)})
	w.checkKind(c, source, at)
	w.checkAuth(c, source, at)
	w.checkAllowIPs(c, source, at)
	l := WebhookLimits{}
	if w.Limits != nil {
		l = *w.Limits
	}
	l = l.withDefaults()
	l.check(c, source, w.ID, at+ptr("limits"))
	w.Limits = &l
	return w
}

func (l WebhookLimits) withDefaults() WebhookLimits {
	if l.RatePerSecond == 0 {
		l.RatePerSecond = DefaultWebhookRate
	}
	if l.Burst == 0 {
		l.Burst = DefaultWebhookBurst
	}
	if l.BodyBytes == 0 {
		l.BodyBytes = DefaultWebhookBodyBytes
	}
	return l
}

func (l WebhookLimits) check(c *collector, source, webhook, at string) {
	for _, f := range []struct {
		name  string
		v, hi int
	}{{"rate_per_second", l.RatePerSecond, MaxWebhookRate}, {"burst", l.Burst, MaxWebhookBurst}, {"body_bytes", l.BodyBytes, MaxWebhookBodyBytes}} {
		if f.v < 1 || f.v > f.hi {
			c.add(ecWhLimit, at+ptr(f.name), Params{"source": source, "webhook": webhook, "field": f.name, "value": f.v, "min": 1, "max": f.hi},
				"source %s, webhook %s: %s is %d, and it has to be from 1 to %d", source, webhook, f.name, f.v, f.hi)
		}
	}
}

func (w Webhook) checkKind(c *collector, source, at string) {
	switch w.Kind {
	case WebhookSignal:
	case WebhookRecords, WebhookAlarms:
		c.add(ecWhNotOpen, at+ptr("kind"), Params{"source": source, "webhook": w.ID, "kind": w.Kind},
			"source %s, webhook %s: the kind %s has no way to be handled yet; only signal is", source, w.ID, w.Kind)
	default:
		c.add(ecWhKind, at+ptr("kind"), Params{"source": source, "webhook": w.ID, "kind": clip(w.Kind, idClipAt), "supported": []string{WebhookSignal, WebhookRecords, WebhookAlarms}},
			"source %s, webhook %s: the kind %q is none of signal, records, alarms", source, w.ID, clip(w.Kind, idClipAt))
	}
}

func (w Webhook) checkAuth(c *collector, source, at string) {
	supported := []string{WebhookAuthHMAC, WebhookAuthHeaderToken, WebhookAuthBasic, WebhookAuthURLToken}
	if !slices.Contains(supported, w.Auth.Type) {
		c.add(ecWhAuth, at+ptr("auth", "type"), Params{"source": source, "webhook": w.ID, "type": clip(w.Auth.Type, idClipAt), "supported": supported},
			"source %s, webhook %s: the type of the proof %q is none of hmac, header_token, basic, url_token", source, w.ID, clip(w.Auth.Type, idClipAt))
		return
	}
	prof := w.Auth.Profile
	switch {
	case w.Auth.Type != WebhookAuthHMAC && prof != "":
		c.add(ecWhProfile, at+ptr("auth", "profile"), Params{"source": source, "webhook": w.ID, "profile": clip(prof, idClipAt), "reason": "only a signed webhook has a profile"},
			"source %s, webhook %s: a profile belongs to the type hmac", source, w.ID)
	case w.Auth.Type == WebhookAuthHMAC && prof != "" && prof != ProfileStandardWebhooks && prof != ProfileBodyOnly:
		c.add(ecWhProfile, at+ptr("auth", "profile"), Params{"source": source, "webhook": w.ID, "profile": clip(prof, idClipAt), "reason": "unknown profile"},
			"source %s, webhook %s: the profile %q is none of standard_webhooks, body_only", source, w.ID, clip(prof, idClipAt))
	}
	if w.Kind != WebhookSignal && (w.Auth.Type == WebhookAuthURLToken || prof == ProfileBodyOnly) {
		c.add(ecWhAuthKind, at+ptr("auth"), Params{"source": source, "webhook": w.ID, "kind": w.Kind, "type": w.Auth.Type, "profile": prof},
			"source %s, webhook %s: url_token and body_only are for the kind signal only", source, w.ID)
	}
}

func (w Webhook) checkAllowIPs(c *collector, source, at string) {
	if len(w.AllowIPs) > maxAllowIPs {
		c.add(ecWhIPs, at+ptr("allow_ips"), Params{"source": source, "webhook": w.ID, "reason": "too many entries"}, "source %s, webhook %s: allow_ips has more than %d entries", source, w.ID, maxAllowIPs)
		return
	}
	for j, e := range w.AllowIPs {
		if _, err := ParseAllowEntry(e); err != nil {
			c.add(ecWhIPs, at+ptr("allow_ips", j), Params{"source": source, "webhook": w.ID, "reason": "not an IP address or a network in CIDR form"},
				"source %s, webhook %s: allow_ips entry %d is not an IP address or a network in CIDR form", source, w.ID, j)
		}
	}
}

// ParseAllowEntry reads one entry of allow_ips: an address, or a network written as address/length.
func ParseAllowEntry(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, err
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// CheckWebhookEventID says whether the place an event id is taken from is covered by the signature of the webhook. A
// signature covers the body and, with the profile standard_webhooks, the id of the message; an event id that is read from
// another header can be changed by whoever sends the call again, so a webhook that is signed refuses it. A webhook that is
// proven by a token has no signature to cover anything and is not asked.
func CheckWebhookEventID(a WebhookAuth, reference string) error {
	if a.Type != WebhookAuthHMAC {
		return nil
	}
	switch {
	case strings.HasPrefix(reference, "$"):
		return nil
	case reference == "webhook-id" && a.Profile != ProfileBodyOnly:
		return nil
	}
	return fmt.Errorf("the event id %q is not in the signed body or in the id of the signed message", clip(reference, idClipAt))
}

// webhookReasons names the webhooks whose change is a change of the connection: one that is new, taken away or changed, and
// a limit that went up. A limit that is lowered is not one.
func webhookReasons(before, after []Webhook) []string {
	old := map[string]Webhook{}
	for _, w := range before {
		old[w.ID] = w
	}
	var out []string
	seen := map[string]bool{}
	for _, w := range after {
		seen[w.ID] = true
		o, existed := old[w.ID]
		if !existed {
			out = append(out, "webhooks."+w.ID)
			continue
		}
		bl, al := o.Limits, w.Limits
		o.Limits, w.Limits = nil, nil
		if !reflect.DeepEqual(o, w) {
			out = append(out, "webhooks."+w.ID)
		}
		if webhookLimitsRaised(bl, al) {
			out = append(out, "webhooks."+w.ID+".limits")
		}
	}
	for _, w := range before {
		if !seen[w.ID] {
			out = append(out, "webhooks."+w.ID)
		}
	}
	return out
}

func webhookLimitsRaised(before, after *WebhookLimits) bool {
	var b, a WebhookLimits
	if before != nil {
		b = *before
	}
	if after != nil {
		a = *after
	}
	b, a = b.withDefaults(), a.withDefaults()
	return a.RatePerSecond > b.RatePerSecond || a.Burst > b.Burst || a.BodyBytes > b.BodyBytes
}
