package hookin

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The headers a webhook is proven by.
const (
	HeaderToken     = "X-Webhook-Token"
	HeaderMessageID = "Webhook-Id"
	HeaderTimestamp = "Webhook-Timestamp"
	HeaderSignature = "Webhook-Signature"
	queryToken      = "token"
)

// Every way a call is not proven ends in fail: the same 401 and the same body, whatever the reason, for a webhook that is
// not known and for one that is, so that the answer tells nothing. The reason is counted and logged, in groups.

// proveByHeader is step 4: the proofs that end in the headers or the query are checked before the body is read.
func (q *request) proveByHeader() bool {
	var presented string
	switch q.hook.Auth.Type {
	case ingest.WebhookAuthHeaderToken:
		presented = q.r.Header.Get(HeaderToken)
	case ingest.WebhookAuthBasic:
		presented = q.basicPassword()
	case ingest.WebhookAuthURLToken:
		presented = q.queryToken()
	}
	if presented == "" || len(presented) > maxHeaderValue {
		q.h.cfg.Verifier.Dummy(nil, nil, []byte(presented))
		q.fail("token")
		return false
	}
	ok, err := q.h.cfg.Verifier.VerifyToken(q.r.Context(), q.source, q.webhook, []byte(presented))
	return q.judge(ok, err, "token")
}

// basicPassword is the password of a Basic header whose user is the id of the webhook, and "" for anything else.
func (q *request) basicPassword() string {
	raw := q.r.Header.Get("Authorization")
	if len(raw) > maxHeaderValue || len(raw) < 7 || !strings.EqualFold(raw[:6], "Basic ") {
		return ""
	}
	dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw[6:]))
	if err != nil {
		return ""
	}
	user, pass, ok := strings.Cut(string(dec), ":")
	if !ok || user != q.webhook {
		return ""
	}
	return pass
}

// queryToken is the value of ?token=, and "" when there is none or more than one. It is read from the query only.
func (q *request) queryToken() string {
	raw := q.r.URL.RawQuery
	if len(raw) > maxHeaderValue {
		return ""
	}
	vals, err := url.ParseQuery(raw)
	if err != nil || len(vals[queryToken]) != 1 {
		return ""
	}
	return vals[queryToken][0]
}

// proveBySignature is step 6, and the dummy of a webhook that is not known: a signature needs the body.
func (q *request) proveBySignature() bool {
	if !q.known {
		q.h.cfg.Verifier.Dummy(q.body, nil, nil)
		q.fail("unknown")
		return false
	}
	signed, sigs, why := q.signedMessage()
	if why != "" {
		q.h.cfg.Verifier.Dummy(q.body, nil, nil)
		q.fail(why)
		return false
	}
	ok, err := q.h.cfg.Verifier.VerifyHMAC(q.r.Context(), q.source, q.webhook, signed, sigs)
	return q.judge(ok, err, "signature")
}

// signedMessage is what the signature is over, and the signatures that were sent, by the profile of the webhook.
func (q *request) signedMessage() (signed []byte, sigs [][]byte, why string) {
	sigs = parseSignatures(q.r.Header.Get(HeaderSignature))
	if len(sigs) == 0 {
		return nil, nil, "signature_form"
	}
	if q.hook.Auth.Profile == ingest.ProfileBodyOnly {
		return q.body, sigs, ""
	}
	id, ts := q.r.Header.Get(HeaderMessageID), q.r.Header.Get(HeaderTimestamp)
	if id == "" || len(id) > 256 {
		return nil, nil, "signature_form"
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(ts) > 20 {
		return nil, nil, "signature_form"
	}
	d := q.h.cfg.Clock.Now().Sub(time.Unix(sec, 0))
	if d > SignatureTolerance || d < -SignatureTolerance {
		return nil, nil, "timestamp"
	}
	msg := make([]byte, 0, len(id)+len(ts)+len(q.body)+2)
	msg = append(append(append(append(append(msg, id...), '.'), ts...), '.'), q.body...)
	return msg, sigs, ""
}

// parseSignatures reads the header: space-separated "v1,<base64>" entries; other versions are skipped, at most
// maxSignatures are kept, and a header that is too long is none.
func parseSignatures(header string) [][]byte {
	if header == "" || len(header) > maxHeaderValue {
		return nil
	}
	var out [][]byte
	for _, part := range strings.Fields(header) {
		ver, val, ok := strings.Cut(part, ",")
		if !ok || ver != "v1" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(val)
		if err != nil || len(raw) != 32 {
			continue
		}
		if out = append(out, raw); len(out) >= maxSignatures {
			break
		}
	}
	return out
}

// judge turns the answer of the verifier into the end of the step. A fault of the verifier is a failure too, and it is
// logged: the caller learns nothing of the secrets from it.
func (q *request) judge(ok bool, err error, kind string) bool {
	if err != nil {
		q.h.cfg.Logger.Error("the proof of a webhook could not be checked", "webhook", q.key)
		q.fail("verifier")
		return false
	}
	if !ok {
		q.fail(kind)
		return false
	}
	return true
}

// fail ends a call that is not proven: the failure is charged to the bucket of (webhook, address), and the answer is
// the 401, or, when the bucket is empty, the 429. A proof that is right never comes here, so a flood of failures from an
// address does not stop a caller that is right.
func (q *request) fail(kind string) {
	h := q.h
	h.counts.add("fail_" + kind)
	h.logFailure(q.key, q.addr.String(), kind)
	if wait, ok := h.takeFailure(q.key+"|"+q.addr.String(), h.cfg.Clock.Now()); !ok {
		h.counts.add("fail_limited")
		h.problem(q.w, "rate_limited.webhook_rate", nil, wait)
		return
	}
	h.problem(q.w, "auth.webhook_invalid", nil, 0)
}
