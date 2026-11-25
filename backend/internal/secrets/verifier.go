package secrets

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"sync"
	"time"
)

// The check of what a product that calls a webhook presents (ADR-0107). The Verifier is the only port that reaches the
// secrets of the webhooks, and it gives no key and no hash back: it answers whether a proof is good. It answers false,
// and no reason, for everything that is not a good proof (no such secret, a secret of a source's connection, another
// source, a retired version, a wrong value), so that a caller cannot tell the cases apart.

// Verifier is the port that checks the proof of a call of a receiving webhook.
type Verifier interface {
	// VerifyHMAC says whether one of the signatures is the HMAC-SHA256 of signed under a version of the key of the
	// webhook that is not retired.
	VerifyHMAC(ctx context.Context, sourceID, webhookID string, signed []byte, signatures [][]byte) (bool, error)
	// VerifyToken says whether the presented token is one of the tokens of the webhook that are not retired.
	VerifyToken(ctx context.Context, sourceID, webhookID string, presented []byte) (bool, error)
	// Dummy does the work of a check for a webhook that is not known, so that the time does not tell it.
	Dummy(signed []byte, signatures [][]byte, presented []byte)
}

// Observer is told what the Verifier does. SecretOpened is the audit line of a fill of the cache; the others are counted.
type Observer interface {
	Event(name, secretID string)
}

// The names of the events the Observer is told.
const (
	// EventSecretOpened is the audit line: a key was opened into the memory of the process.
	EventSecretOpened = "secret_opened"
	// EventVerifyAccepted and EventVerifyRefused count the checks.
	EventVerifyAccepted = "webhook_verify_accepted"
	EventVerifyRefused  = "webhook_verify_refused"
)

// DefaultVerifyCacheTTL is the longest time an opened key stays in memory.
const DefaultVerifyCacheTTL = 60 * time.Second

type verifierPort struct{ s *Service }

func (p verifierPort) VerifyHMAC(ctx context.Context, sourceID, webhookID string, signed []byte, signatures [][]byte) (bool, error) {
	return p.s.verifyHMAC(ctx, sourceID, webhookID, signed, signatures)
}

func (p verifierPort) VerifyToken(ctx context.Context, sourceID, webhookID string, presented []byte) (bool, error) {
	return p.s.verifyToken(ctx, sourceID, webhookID, presented)
}

func (p verifierPort) Dummy(signed []byte, signatures [][]byte, presented []byte) {
	p.s.dummyVerify(signed, signatures, presented)
}

// Verifier is the port that checks the proof of a webhook, as a value with that port's methods only.
func (s *Service) Verifier() Verifier { return verifierPort{s: s} }

// cached is what is known about one secret between two fills: the open key material of its versions that are not
// retired, and the revision of the record it was opened from.
type cached struct {
	revision []byte
	kind     Kind
	scope    []string
	material [][]byte
	until    time.Time
}

type verifyState struct {
	mu      sync.Mutex
	entries map[string]*cached
	probe   []byte // the key of the process that makes the two sides of a comparison of tokens look alike
}

const maxCachedSecrets = 4096

func (s *Service) ttl() time.Duration {
	if s.cfg.VerifyCacheTTL <= 0 || s.cfg.VerifyCacheTTL > DefaultVerifyCacheTTL {
		return DefaultVerifyCacheTTL
	}
	return s.cfg.VerifyCacheTTL
}

func (s *Service) tell(name, id string) {
	if s.cfg.Observer != nil {
		s.cfg.Observer.Event(name, id)
	}
}

// revisionOf is the revision of a record: the tag of its binding, which every write makes anew.
func revisionOf(rec Record) []byte {
	return append(slices.Clone(rec.Binding.Nonce), rec.Binding.Data...)
}

// material returns the key material of the secret of a webhook, from the cache when the record is the one it was
// opened from and the minute has not passed, else opened anew. A nil result is "no good secret".
func (s *Service) material(ctx context.Context, sourceID, webhookID string, want Kind) (*cached, error) {
	id := SecretIDFor(sourceID, webhookID)
	rec, found, err := s.cfg.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.cfg.Clock.Now()
	s.vf.mu.Lock()
	defer s.vf.mu.Unlock()
	if !found {
		delete(s.vf.entries, id)
		return nil, nil
	}
	if c := s.vf.entries[id]; c != nil && now.Before(c.until) && hmac.Equal(c.revision, revisionOf(rec)) {
		return usable(c, sourceID, want), nil
	}
	delete(s.vf.entries, id)
	c, err := s.fill(id, rec, sourceID, now)
	if err != nil || c == nil {
		return nil, err
	}
	return usable(c, sourceID, want), nil
}

// fill opens the record that the cache lacks, puts it in the cache and tells the audit line. A record that does not
// authenticate, or is not a secret of this source's webhook, is nil.
func (s *Service) fill(id string, rec Record, sourceID string, now time.Time) (*cached, error) {
	key, err := s.webhookKey()
	if err != nil {
		return nil, err
	}
	if verify(rec, key) != nil || !IsReceiveOnly(rec.Kind) || !slices.Equal(rec.Scope, []string{sourceID}) {
		return nil, nil
	}
	c, err := openMaterial(rec, key)
	if err != nil {
		return nil, err
	}
	c.until = now.Add(s.ttl())
	if s.vf.entries == nil || len(s.vf.entries) >= maxCachedSecrets {
		s.vf.entries = map[string]*cached{}
	}
	s.vf.entries[id] = c
	s.tell(EventSecretOpened, id)
	return c, nil
}

func usable(c *cached, sourceID string, want Kind) *cached {
	if c.kind != want || !slices.Contains(c.scope, sourceID) || len(c.material) == 0 {
		return nil
	}
	return c
}

// openMaterial opens the versions that are not retired (the newest few) of an authenticated record.
func openMaterial(rec Record, key *MasterKey) (*cached, error) {
	c := &cached{revision: revisionOf(rec), kind: rec.Kind, scope: slices.Clone(rec.Scope)}
	for i := len(rec.Versions) - 1; i >= 0 && len(c.material) < maxWebhookVersions; i-- {
		v := rec.Versions[i]
		if v.RetiredAt != nil {
			continue
		}
		values, wipeFn, err := decryptVersion(rec, v, key)
		if err != nil {
			return nil, err
		}
		name := "key"
		if rec.Kind == KindWebhookToken {
			name = "hash"
		}
		text := values[name].Reveal()
		raw, ok := decodeMaterial(rec.Kind, text)
		wipeFn()
		if !ok {
			return nil, errIntegrity()
		}
		c.material = append(c.material, raw)
	}
	return c, nil
}

func decodeMaterial(kind Kind, text string) ([]byte, bool) {
	var raw []byte
	var err error
	if kind == KindHMACKey {
		raw, err = base64.StdEncoding.DecodeString(text)
	} else {
		raw, err = hex.DecodeString(text)
	}
	return raw, err == nil && len(raw) == hmacKeyBytes
}

func (s *Service) verifyHMAC(ctx context.Context, sourceID, webhookID string, signed []byte, signatures [][]byte) (bool, error) {
	c, err := s.material(ctx, sourceID, webhookID, KindHMACKey)
	if err != nil || c == nil {
		s.dummyVerify(signed, signatures, nil)
		return false, err
	}
	ok := false
	for _, key := range c.material {
		mac := hmac.New(sha256.New, key)
		mac.Write(signed)
		sum := mac.Sum(nil)
		for _, sig := range signatures {
			ok = subtle.ConstantTimeCompare(sum, sig) == 1 || ok
		}
	}
	s.count(ok, SecretIDFor(sourceID, webhookID))
	return ok, nil
}

func (s *Service) verifyToken(ctx context.Context, sourceID, webhookID string, presented []byte) (bool, error) {
	c, err := s.material(ctx, sourceID, webhookID, KindWebhookToken)
	if err != nil || c == nil {
		s.dummyVerify(nil, nil, presented)
		return false, err
	}
	probe := s.probeKey()
	sum := sha256.Sum256(presented)
	got := probeMAC(probe, sum[:])
	ok := false
	for _, stored := range c.material {
		ok = subtle.ConstantTimeCompare(got, probeMAC(probe, stored)) == 1 || ok
	}
	s.count(ok, SecretIDFor(sourceID, webhookID))
	return ok, nil
}

func (s *Service) count(ok bool, id string) {
	if ok {
		s.tell(EventVerifyAccepted, id)
		return
	}
	s.tell(EventVerifyRefused, id)
}

// probeKey is the random key of this process for the comparison of tokens: both sides are MACed under it before they
// are compared, so what is compared has one length whatever was presented.
func (s *Service) probeKey() []byte {
	s.vf.mu.Lock()
	defer s.vf.mu.Unlock()
	if s.vf.probe == nil {
		s.vf.probe = make([]byte, keyLen)
		s.cfg.Bytes.Fill(s.vf.probe)
	}
	return s.vf.probe
}

func probeMAC(probe, data []byte) []byte {
	mac := hmac.New(sha256.New, probe)
	mac.Write(data)
	return mac.Sum(nil)
}

func (s *Service) dummyVerify(signed []byte, signatures [][]byte, presented []byte) {
	probe := s.probeKey()
	zero := make([]byte, keyLen)
	mac := hmac.New(sha256.New, zero)
	mac.Write(signed)
	sum := mac.Sum(nil)
	for _, sig := range signatures {
		_ = subtle.ConstantTimeCompare(sum, sig)
	}
	h := sha256.Sum256(presented)
	_ = subtle.ConstantTimeCompare(probeMAC(probe, h[:]), probeMAC(probe, zero))
}
