package secrets

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The key of the receiving webhooks (ADR-0107). The API role checks what the products that call a webhook present, so
// it must open the keys of the webhooks and no secret of a source. The secrets of the webhooks are therefore wrapped by
// a key of their own (purpose webhook_verify), which is another file than the master key: a process that has only
// this key opens the receiving webhooks and nothing else, and the process that has only the master key cannot open them.
const (
	purposeWebhook   = "webhook_verify"
	webhookKeyFormat = "praxium-webhook-verify-key"
	labelWebhookKey  = "praxium-secrets/1/webhook-verify-key-id"
)

// webhookKeyID is the id of a key of the webhooks: "wk-" and the first 16 hex digits of the SHA-256 of its own purpose
// and the key, so the id of a webhook key is never that of a master key and the two cannot be taken for each other.
func webhookKeyID(raw []byte) string {
	sum := sha256.Sum256(append([]byte(labelWebhookKey), raw...))
	return "wk-" + hex.EncodeToString(sum[:8])
}

func newWebhookKey(raw []byte) *MasterKey {
	key := append([]byte(nil), raw...)
	return &MasterKey{id: webhookKeyID(key), get: func() []byte { return key }, purpose: purposeWebhook}
}

// GenerateWebhookKey makes a new key of 32 bytes from src for the secrets of the receiving webhooks.
func GenerateWebhookKey(src kernel.ByteSource) *MasterKey {
	raw := make([]byte, keyLen)
	src.Fill(raw)
	k := newWebhookKey(raw)
	wipe(raw)
	return k
}

// ParseWebhookKey reads the key file of the receiving webhooks. A master key file is not one, and the other way round.
func ParseWebhookKey(data []byte) (*MasterKey, error) {
	return parseKeyFile(data, webhookKeyFormat, "webhook verify key", newWebhookKey)
}

// IsWebhookKey says whether the key is one of the receiving webhooks.
func (k *MasterKey) IsWebhookKey() bool { return k != nil && k.purpose == purposeWebhook }
