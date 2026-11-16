package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The labels name what an authenticated message is for and the format it belongs to, so a ciphertext of one purpose
// never authenticates as another. They are the first thing in every authenticated data.
const (
	labelValue   = "praxium-secrets/1/value"
	labelDataKey = "praxium-secrets/1/data-key"
	labelBinding = "praxium-secrets/1/binding"
	nonceLen     = 12
)

// sealed is the output of one AES-256-GCM encryption, as stored.
type sealed struct {
	nonce []byte
	data  []byte
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != keyLen {
		return nil, kernel.Errorf(kernel.CodeInternal, "an encryption key is %d bytes, not %d", len(key), keyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot start the cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot start the cipher")
	}
	return gcm, nil
}

// aeadSeal encrypts plain under key with a new random nonce of 96 bits. Nonce uniqueness is kept by the callers:
// a data key is used here once (dataKey), and the master key makes a random nonce for each of its few messages
// (one wrapped key per version, one binding tag per change), far below the 2^32 messages that NIST SP 800-38D allows
// for random nonces under one key.
func aeadSeal(src kernel.ByteSource, key, plain, aad []byte) (sealed, error) {
	gcm, err := newAEAD(key)
	if err != nil {
		return sealed{}, err
	}
	nonce := make([]byte, nonceLen)
	src.Fill(nonce)
	return sealed{nonce: nonce, data: gcm.Seal(nil, nonce, plain, aad)}, nil
}

// aeadOpen decrypts and authenticates. Any failure is the same error: it must not tell why.
func aeadOpen(key []byte, s sealed, aad []byte) ([]byte, error) {
	gcm, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(s.nonce) != nonceLen {
		return nil, errIntegrity()
	}
	plain, err := gcm.Open(nil, s.nonce, s.data, aad)
	if err != nil {
		return nil, errIntegrity()
	}
	return plain, nil
}

func errIntegrity() error {
	return kernel.Errorf(CodeIntegrity, "the stored secret does not authenticate: the file was changed, or it was written under another master key with the same id; put the secret in again or give the key file it was made with")
}

// dataKey is a 32-byte key for one encryption. It encrypts at most once (a second seal is an error), which keeps a
// nonce from ever repeating under it, and it leaves the process only wrapped by the master key.
type dataKey struct {
	key    []byte
	sealed bool
}

func newDataKey(src kernel.ByteSource) *dataKey {
	k := make([]byte, keyLen)
	src.Fill(k)
	return &dataKey{key: k}
}

func (d *dataKey) seal(src kernel.ByteSource, plain, aad []byte) (sealed, error) {
	if d.sealed {
		return sealed{}, kernel.Errorf(kernel.CodeInternal, "a data key encrypts once")
	}
	d.sealed = true
	return aeadSeal(src, d.key, plain, aad)
}

func (d *dataKey) wrap(src kernel.ByteSource, master *MasterKey, aad []byte) (sealed, error) {
	return aeadSeal(src, master.key, d.key, aad)
}

// unwrapDataKey returns a data key for decrypting only: it is marked as used, so it never encrypts.
func unwrapDataKey(master *MasterKey, w sealed, aad []byte) (*dataKey, error) {
	key, err := aeadOpen(master.key, w, aad)
	if err != nil {
		return nil, err
	}
	return &dataKey{key: key, sealed: true}, nil
}

func (d *dataKey) wipe() { wipe(d.key) }

// The authenticated data are built from length-prefixed parts, so no two different inputs give the same bytes.
type aadBuilder struct{ b []byte }

func newAAD(label string) *aadBuilder { return (&aadBuilder{}).text(label) }

func (a *aadBuilder) text(s string) *aadBuilder {
	a.b = binary.BigEndian.AppendUint32(a.b, uint32(len(s)))
	a.b = append(a.b, s...)
	return a
}

func (a *aadBuilder) number(n int) *aadBuilder {
	a.b = binary.BigEndian.AppendUint32(a.b, uint32(n))
	return a
}

func (a *aadBuilder) list(items []string) *aadBuilder {
	a.number(len(items))
	for _, it := range items {
		a.text(it)
	}
	return a
}

func (a *aadBuilder) addresses(list []Address) *aadBuilder {
	a.number(len(list))
	for _, ad := range list {
		a.text(ad.Host).number(ad.Port)
	}
	return a
}

// valueAAD is what the encryption of the fields of one version authenticates: the format, the secret and the version.
func valueAAD(id string, version int) []byte {
	return newAAD(labelValue).text(id).number(version).b
}

// dataKeyAAD is what the wrapping of the data key of one version authenticates: the format, the secret, the version
// and the id of the master key.
func dataKeyAAD(id string, version int, keyID string) []byte {
	return newAAD(labelDataKey).text(id).number(version).text(keyID).b
}

// bindingAAD is what the tag of the binding authenticates: the secret, its kind, the master key, the scope and the
// allowed addresses, in the order stored.
func bindingAAD(r Record, keyID string) []byte {
	return newAAD(labelBinding).text(r.ID).text(string(r.Kind)).text(keyID).list(r.Scope).addresses(r.AllowedHosts).b
}

// encodeFields writes the fields as the plaintext of the encryption: a count, then each name and value with
// lengths, in the order of the names.
func encodeFields(fields Fields) []byte {
	names := fields.Names()
	b := binary.BigEndian.AppendUint32(nil, uint32(len(names)))
	for _, n := range names {
		b = binary.BigEndian.AppendUint32(b, uint32(len(n)))
		b = append(b, n...)
		value := fields.values()[n]
		b = binary.BigEndian.AppendUint32(b, uint32(len(value)))
		b = append(b, value...)
	}
	return b
}

// decodeFields reads what encodeFields wrote. The values are slices of their own, so they can be overwritten.
func decodeFields(b []byte) (map[string][]byte, bool) {
	r := reader{b: b}
	count, ok := r.u32()
	if !ok || count > 64 {
		return nil, false
	}
	out := make(map[string][]byte, count)
	for i := uint32(0); i < count; i++ {
		name, ok1 := r.chunk()
		value, ok2 := r.chunk()
		if !ok1 || !ok2 {
			return nil, false
		}
		if _, dup := out[string(name)]; dup {
			return nil, false
		}
		out[string(name)] = append([]byte(nil), value...)
	}
	return out, len(r.b) == 0
}

type reader struct{ b []byte }

func (r *reader) u32() (uint32, bool) {
	if len(r.b) < 4 {
		return 0, false
	}
	n := binary.BigEndian.Uint32(r.b)
	r.b = r.b[4:]
	return n, true
}

func (r *reader) chunk() ([]byte, bool) {
	n, ok := r.u32()
	if !ok || uint64(n) > uint64(len(r.b)) {
		return nil, false
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out, true
}
