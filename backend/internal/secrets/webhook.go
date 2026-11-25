package secrets

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The secrets of the receiving webhooks (ADR-0107). They are the proof that the product which calls a webhook is the
// one that was given the secret, and they are made by the server: a person never chooses one, so a guess has no help
// from a habit. Two kinds, both receive-only:
//
//   - KindHMACKey keeps the signing key of a webhook of the type hmac. The Verifier needs the key itself, so it is kept
//     (encrypted, under the key of the webhooks).
//   - KindWebhookToken keeps only the SHA-256 of the token of a webhook of the type header_token, basic or url_token.
//     The token has the entropy of a key, so a fast hash is enough and the token cannot be had from the store.
const (
	// KindHMACKey is the signing key of a webhook that is signed.
	KindHMACKey Kind = "hmac_key"
	// KindWebhookToken is the hash of the token of a webhook that is proven by a token.
	KindWebhookToken Kind = "webhook_token"
)

var webhookKindFields = map[Kind][]string{
	KindHMACKey:      {"key"},
	KindWebhookToken: {"hash"},
}

// IsReceiveOnly says whether a kind is one of the receiving webhooks: Open never opens it and the Verifier checks it.
func IsReceiveOnly(kind Kind) bool {
	_, ok := webhookKindFields[kind]
	return ok
}

// kindFieldNames is FieldNames for the kinds of the sources and of the webhooks both.
func kindFieldNames(kind Kind) ([]string, bool) {
	if names, ok := webhookKindFields[kind]; ok {
		return slices.Clone(names), true
	}
	return FieldNames(kind)
}

func errReceiveOnly(id string) error {
	return kernel.Errorf(CodeReceiveOnly, "the secret %q is the secret of a receiving webhook: it is checked by the verifier and never opened; change it with the webhook commands", id)
}

// The types of the proof of a webhook, as the definition names them (core.ingest knows the same names).
const (
	WebhookAuthHMAC        = "hmac"
	WebhookAuthHeaderToken = "header_token"
	WebhookAuthBasic       = "basic"
	WebhookAuthURLToken    = "url_token"
)

// KindOfWebhookAuth is the kind of secret that a type of proof keeps.
func KindOfWebhookAuth(authType string) (Kind, bool) {
	switch authType {
	case WebhookAuthHMAC:
		return KindHMACKey, true
	case WebhookAuthHeaderToken, WebhookAuthBasic, WebhookAuthURLToken:
		return KindWebhookToken, true
	}
	return "", false
}

// SecretIDFor is the id of the secret of one webhook of one source. The definition names no secret: the webhook has
// exactly one secret, and it is this one, so two webhooks can never share a key and a source can never be pointed at
// the secret of another.
func SecretIDFor(sourceID, webhookID string) string {
	sum := sha256.Sum256([]byte("praxium-webhook-secret/1\x00" + sourceID + "\x00" + webhookID))
	return WebhookIDPrefix + hex.EncodeToString(sum[:15])
}

// WebhookIDPrefix begins the id of every secret of a webhook. A secret of a source cannot have an id that begins with it
// (Create refuses it), so the two never share a name even if the stores were joined one day.
const WebhookIDPrefix = "wh_"

// hookStore is the store of the secrets of the webhooks; a program that has none gets an error that says so.
func (s *Service) hookStore() (Store, error) {
	if s.cfg.WebhookStore == nil {
		return nil, kernel.Errorf(CodeWebhookStoreMissing, "there is no store of the secrets of the webhooks; give the file of the receiving webhooks")
	}
	return s.cfg.WebhookStore, nil
}

// Issued is what an issue or a rotation gives back: the value is shown this once and is not kept anywhere. It does not
// print its value: %v, %+v, %#v, a log record and a JSON encoding show the metadata and a mark in place of the value, so
// that the value is only in the program's output where a caller reads Value on purpose.
type Issued struct {
	Meta Meta
	// Value is what the sender is given: for a signed webhook whsec_ and the key in base64, else the token.
	Value string
}

const issuedHidden = "(shown once, not printed)"

// String shows the secret without its value.
func (i Issued) String() string {
	return fmt.Sprintf("issued secret %s version %d: %s", i.Meta.ID, i.Meta.Version, issuedHidden)
}

// GoString is the same for %#v.
func (i Issued) GoString() string { return i.String() }

// LogValue is the same for a log record.
func (i Issued) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", i.Meta.ID), slog.Int("version", i.Meta.Version), slog.String("value", issuedHidden))
}

// MarshalJSON is the same for an encoding; a command that shows the value on purpose builds its own output.
func (i Issued) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": i.Meta.ID, "version": i.Meta.Version, "value": issuedHidden})
}

const (
	hmacKeyBytes  = 32 // the bytes of a signing key and of a token
	hmacKeyPrefix = "whsec_"
)

// maxWebhookVersions is how many versions of a webhook secret the Verifier looks at: the newest that are not retired.
const maxWebhookVersions = 16

// IssueWebhookSecret makes the secret of a webhook, with its first version, and gives the value once. A webhook that
// has a secret is a conflict: rotate it.
func (s *Service) IssueWebhookSecret(ctx context.Context, sourceID, webhookID, authType string) (Issued, error) {
	kind, ok := KindOfWebhookAuth(authType)
	if !ok {
		return Issued{}, kernel.Errorf(kernel.CodeInvalidValue, "%s: use hmac, header_token, basic or url_token", badText("type of the proof of a webhook", authType))
	}
	if err := checkWebhookNames(sourceID, webhookID); err != nil {
		return Issued{}, err
	}
	store, err := s.hookStore()
	if err != nil {
		return Issued{}, err
	}
	key, err := s.webhookKey()
	if err != nil {
		return Issued{}, err
	}
	id := SecretIDFor(sourceID, webhookID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found, err := store.Get(ctx, id); err != nil {
		return Issued{}, kernel.Wrap(kernel.CodeInternal, err, "cannot read the secret store")
	} else if found {
		return Issued{}, kernel.Errorf(kernel.CodeConflict, "the webhook %q of the source %q has a secret already; rotate it", webhookID, sourceID)
	}
	fields, shown := s.webhookValue(kind)
	instance := make([]byte, instanceLen)
	s.cfg.Bytes.Fill(instance)
	rec := Record{ID: id, Instance: instance, Kind: kind, CreatedAt: s.cfg.Clock.Now().UTC(), Scope: []string{sourceID}}
	v, err := s.newVersion(id, instance, 1, fields, key)
	if err != nil {
		return Issued{}, err
	}
	rec.Versions = []VersionRecord{v}
	if err := s.bind(&rec, key); err != nil {
		return Issued{}, err
	}
	if err := s.putIn(ctx, store, rec); err != nil {
		return Issued{}, err
	}
	return Issued{Meta: rec.meta(), Value: shown}, nil
}

// RotateWebhookSecret adds a new version with a new value and gives it once. The older versions are checked too until
// they are retired, so the sender can be changed over without a gap.
func (s *Service) RotateWebhookSecret(ctx context.Context, sourceID, webhookID string) (Issued, error) {
	if err := checkWebhookNames(sourceID, webhookID); err != nil {
		return Issued{}, err
	}
	key, err := s.webhookKey()
	if err != nil {
		return Issued{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	store, rec, err := s.loadWebhook(ctx, sourceID, webhookID, key)
	if err != nil {
		return Issued{}, err
	}
	if len(rec.Versions) >= maxVersions {
		return Issued{}, kernel.Errorf(kernel.CodeInvalidValue, "a secret has at most %d versions; delete it and issue it again", maxVersions)
	}
	fields, shown := s.webhookValue(rec.Kind)
	v, err := s.newVersion(rec.ID, rec.Instance, rec.Versions[len(rec.Versions)-1].Version+1, fields, key)
	if err != nil {
		return Issued{}, err
	}
	rec.Versions = append(rec.Versions, v)
	if err := s.bind(&rec, key); err != nil {
		return Issued{}, err
	}
	if err := s.putIn(ctx, store, rec); err != nil {
		return Issued{}, err
	}
	return Issued{Meta: rec.meta(), Value: shown}, nil
}

// RetireWebhookVersion retires a version of the secret of a webhook: from then on it proves nothing.
func (s *Service) RetireWebhookVersion(ctx context.Context, sourceID, webhookID string, version int) (Meta, error) {
	if err := checkWebhookNames(sourceID, webhookID); err != nil {
		return Meta{}, err
	}
	key, err := s.webhookKey()
	if err != nil {
		return Meta{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	store, rec, err := s.loadWebhook(ctx, sourceID, webhookID, key)
	if err != nil {
		return Meta{}, err
	}
	i := slices.IndexFunc(rec.Versions, func(v VersionRecord) bool { return v.Version == version })
	if i < 0 {
		return Meta{}, kernel.Errorf(kernel.CodeNotFound, "the secret of the webhook %q has no version %d", webhookID, version)
	}
	if rec.Versions[i].RetiredAt != nil {
		return Meta{}, kernel.Errorf(kernel.CodeConflict, "version %d of the secret of the webhook %q is already retired", version, webhookID)
	}
	now := s.cfg.Clock.Now().UTC()
	rec.Versions[i].RetiredAt, rec.Versions[i].Value, rec.Versions[i].WrappedKey = &now, nil, nil
	if err := s.bind(&rec, key); err != nil {
		return Meta{}, err
	}
	if err := s.putIn(ctx, store, rec); err != nil {
		return Meta{}, err
	}
	return rec.meta(), nil
}

// DeleteWebhookSecret removes the secret of a webhook with all its versions. It needs no key.
func (s *Service) DeleteWebhookSecret(ctx context.Context, sourceID, webhookID string) error {
	if err := checkWebhookNames(sourceID, webhookID); err != nil {
		return err
	}
	store, err := s.hookStore()
	if err != nil {
		return err
	}
	id := SecretIDFor(sourceID, webhookID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, found, err := store.Get(ctx, id)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot read the secret store")
	}
	if !found {
		return notFound(id)
	}
	if !IsReceiveOnly(rec.Kind) {
		return kernel.Errorf(CodeIntegrity, "the stored secret %q is not the secret of a webhook; it is not deleted with a webhook command", id)
	}
	if _, err := store.Delete(ctx, id); err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot write the secret store")
	}
	return nil
}

// ListWebhookSecrets returns the metadata of the secrets of the webhooks, in the order of the ids. No answer holds a value.
func (s *Service) ListWebhookSecrets(ctx context.Context) ([]Meta, error) {
	store, err := s.hookStore()
	if err != nil {
		return nil, err
	}
	recs, err := store.List(ctx)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot read the secret store")
	}
	out := make([]Meta, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.meta())
	}
	return out, nil
}

func checkWebhookNames(sourceID, webhookID string) error {
	if !validName(sourceID) || !validName(webhookID) {
		return kernel.Errorf(kernel.CodeInvalidValue, "the id of a source and of a webhook start with a lower-case letter and have lower-case letters, digits, _ and - (up to %d characters)", maxIDLen)
	}
	return nil
}

// webhookKey is the key of the webhooks, which a program that handles them must have.
func (s *Service) webhookKey() (*MasterKey, error) {
	if s.cfg.WebhookKeys == nil {
		return nil, kernel.Errorf(CodeWebhookKeyMissing, "there is no webhook key; give the key file of the receiving webhooks")
	}
	key, err := s.cfg.WebhookKeys.MasterKey()
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, kernel.Errorf(CodeWebhookKeyMissing, "there is no webhook key; give the key file of the receiving webhooks")
	}
	if !key.IsWebhookKey() {
		return nil, kernel.Errorf(CodeMasterKeyInvalid, "the key given as the webhook key is not a webhook key; give the key file of the receiving webhooks")
	}
	return key, nil
}

// loadWebhook reads the record of the secret of a webhook that must authenticate under the key of the webhooks.
func (s *Service) loadWebhook(ctx context.Context, sourceID, webhookID string, key *MasterKey) (Store, Record, error) {
	store, err := s.hookStore()
	if err != nil {
		return nil, Record{}, err
	}
	rec, found, err := store.Get(ctx, SecretIDFor(sourceID, webhookID))
	if err != nil {
		return nil, Record{}, kernel.Wrap(kernel.CodeInternal, err, "cannot read the secret store")
	}
	if !found {
		return nil, Record{}, kernel.Errorf(kernel.CodeNotFound, "the webhook %q of the source %q has no secret; issue it first", webhookID, sourceID)
	}
	if !IsReceiveOnly(rec.Kind) || !slices.Equal(rec.Scope, []string{sourceID}) {
		return nil, Record{}, kernel.Errorf(CodeIntegrity, "the stored secret does not authenticate: the file was changed or mixed from different states")
	}
	if err := verify(rec, key); err != nil {
		return nil, Record{}, err
	}
	return store, rec, nil
}

// webhookValue makes the value of a version: the fields to keep and the text to show once.
func (s *Service) webhookValue(kind Kind) (Fields, string) {
	raw := make([]byte, hmacKeyBytes)
	s.cfg.Bytes.Fill(raw)
	defer wipe(raw)
	if kind == KindHMACKey {
		text := base64.StdEncoding.EncodeToString(raw)
		return NewFields(map[string]string{"key": text}), hmacKeyPrefix + text
	}
	token := strings.TrimRight(base64.URLEncoding.EncodeToString(raw), "=")
	sum := sha256.Sum256([]byte(token))
	return NewFields(map[string]string{"hash": hex.EncodeToString(sum[:])}), token
}
