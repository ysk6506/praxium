package secrets

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

const (
	masterFormat  = "praxium-master-key"
	masterVersion = 1
	// MethodAES256GCM is the name of the one method of wrapping and of encrypting: AES-256 in Galois/Counter Mode
	// (NIST SP 800-38D) with a 96-bit nonce and a 128-bit tag.
	MethodAES256GCM = "aes-256-gcm"
	keyLen          = 32
	maxKeyFileLen   = 4096
)

// MasterKey is the key that wraps the data keys. Its id is derived from the key (keyID), and is stored beside
// everything the key sealed, so a store knows which key to ask for. The key bytes are held by a function value, like
// the bytes of a Value: fmt prints the methods of a field it can see only, and for a wrong verb it prints what a
// pointer points to, so a key in a slice field would show in a Config or a Service that holds it. Printing the key
// itself gives Redacted.
type MasterKey struct {
	redacted
	id  string
	get func() []byte
}

// KeyProvider is the port that gives the master key. Where the key comes from (a file, a key service) is the
// business of whoever implements it; a failure is one of the master key codes.
type KeyProvider interface {
	MasterKey() (*MasterKey, error)
}

// ID is the id of the key.
func (k *MasterKey) ID() string { return k.id }

// masterFile is the key file: one JSON object.
type masterFile struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	KeyID   string `json:"key_id"`
	Method  string `json:"method"`
	Key     string `json:"key"` // base64 of the 32 key bytes
}

const labelKeyID = "praxium-secrets/1/master-key-id"

// keyID is the id of a key: "mk-" and the first 16 hex digits of the SHA-256 of the purpose and the key. The id comes
// from the key, so a file whose id is not that of its key is not what it says it is.
func keyID(raw []byte) string {
	sum := sha256.Sum256(append([]byte(labelKeyID), raw...))
	return "mk-" + hex.EncodeToString(sum[:8])
}

func newMasterKey(raw []byte) *MasterKey {
	key := append([]byte(nil), raw...)
	return &MasterKey{id: keyID(key), get: func() []byte { return key }}
}

func (k *MasterKey) bytes() []byte { return k.get() }

// GenerateMasterKey makes a new key of 32 bytes from src.
func GenerateMasterKey(src kernel.ByteSource) *MasterKey {
	raw := make([]byte, keyLen)
	src.Fill(raw)
	k := newMasterKey(raw)
	wipe(raw)
	return k
}

// Encode returns the key file for the key. The file holds the key itself: the caller writes it where only its
// owner can read it.
func (k *MasterKey) Encode() []byte {
	out, _ := json.MarshalIndent(masterFile{
		Format: masterFormat, Version: masterVersion, KeyID: k.id, Method: MethodAES256GCM,
		Key: base64.StdEncoding.EncodeToString(k.bytes()),
	}, "", " ")
	return append(out, '\n')
}

// ParseMasterKey reads a key file. The errors have the code CodeMasterKeyInvalid and never repeat what the bytes
// hold (not even the text of a syntax error, which names a character of the input).
func ParseMasterKey(data []byte) (*MasterKey, error) {
	invalid := func(why string) (*MasterKey, error) {
		return nil, kernel.Errorf(CodeMasterKeyInvalid, "the master key file %s; give the key file this store was made with", why)
	}
	if len(data) > maxKeyFileLen {
		return invalid("is too large")
	}
	var f masterFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return invalid("is not a praxium master key file")
	}
	if _, err := dec.Token(); err != io.EOF {
		return invalid("has text after the key")
	}
	if f.Format != masterFormat {
		return invalid("is not a praxium master key file")
	}
	if f.Version != masterVersion {
		return invalid("has a version this program does not read")
	}
	if f.Method != MethodAES256GCM {
		return invalid("uses a method this program does not know")
	}
	if !validName(f.KeyID) {
		return invalid("has an invalid key id")
	}
	raw, err := base64.StdEncoding.DecodeString(f.Key)
	defer wipe(raw)
	if err != nil || len(raw) != keyLen || subtle.ConstantTimeCompare(raw, make([]byte, keyLen)) == 1 {
		return invalid("has a key that is not 32 bytes of base64")
	}
	key := newMasterKey(raw)
	if key.id != f.KeyID {
		return invalid("has a key id that is not the id of its key")
	}
	return key, nil
}

type staticKey struct{ key *MasterKey }

func (s staticKey) MasterKey() (*MasterKey, error) { return s.key, nil }

// StaticKey is the KeyProvider of a key that is already read.
func StaticKey(key *MasterKey) KeyProvider { return staticKey{key: key} }

type noKey struct{}

func (noKey) MasterKey() (*MasterKey, error) {
	return nil, kernel.Errorf(CodeMasterKeyMissing, "there is no master key; give the key file this store was made with")
}

// NoKey is the KeyProvider of a program that has no master key. Describing secrets works without a key; writing
// and opening them fail with CodeMasterKeyMissing.
func NoKey() KeyProvider { return noKey{} }
