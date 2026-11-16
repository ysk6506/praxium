package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"time"

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
	return kernel.Errorf(CodeIntegrity, "the stored secret does not authenticate: the file was changed or mixed from different states; restore it from a copy, or put the secret in again")
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
	return aeadSeal(src, master.bytes(), d.key, aad)
}

// unwrapDataKey returns a data key for decrypting only: it is marked as used, so it never encrypts.
func unwrapDataKey(master *MasterKey, w sealed, aad []byte) (*dataKey, error) {
	key, err := aeadOpen(master.bytes(), w, aad)
	if err != nil {
		return nil, err
	}
	return &dataKey{key: key, sealed: true}, nil
}

func (d *dataKey) wipe() { wipe(d.key) }

// The authenticated data are built from length-prefixed parts (lengths and numbers are 64 bits), so no two different
// inputs give the same bytes.
type aadBuilder struct{ b []byte }

func newAAD(label string) *aadBuilder { return (&aadBuilder{}).text(label) }

func (a *aadBuilder) u64(n uint64) *aadBuilder {
	a.b = binary.BigEndian.AppendUint64(a.b, n)
	return a
}

func (a *aadBuilder) text(s string) *aadBuilder {
	a.u64(uint64(len(s)))
	a.b = append(a.b, s...)
	return a
}

func (a *aadBuilder) bytes(b []byte) *aadBuilder {
	a.u64(uint64(len(b)))
	a.b = append(a.b, b...)
	return a
}

func (a *aadBuilder) number(n int) *aadBuilder { return a.u64(uint64(n)) }

func (a *aadBuilder) stamp(t time.Time) *aadBuilder { return a.u64(uint64(t.UTC().UnixNano())) }

func (a *aadBuilder) flag(set bool) *aadBuilder {
	if set {
		a.b = append(a.b, 1)
	} else {
		a.b = append(a.b, 0)
	}
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

func (a *aadBuilder) sealed(s *Sealed) *aadBuilder {
	a.flag(s != nil)
	if s != nil {
		a.text(s.Method).text(s.KeyID).bytes(s.Nonce).bytes(s.Data)
	}
	return a
}

// valueAAD is what the encryption of the fields of one version authenticates: the purpose and format, the method, the
// secret, its instance and the version.
func valueAAD(id string, instance []byte, version int) []byte {
	return newAAD(labelValue).text(MethodAES256GCM).text(id).bytes(instance).number(version).b
}

// dataKeyAAD is what the wrapping of the data key of one version authenticates: the same and the id of the master key.
func dataKeyAAD(id string, instance []byte, version int, keyID string) []byte {
	return newAAD(labelDataKey).text(MethodAES256GCM).text(id).bytes(instance).number(version).text(keyID).b
}

// bindingAAD is what the tag of the binding authenticates: the whole state of the record except the tag itself. That
// is the purpose and format, the method and master key of the binding, the secret, its instance, kind, creation time,
// scope and allowed addresses, and the list of all versions, each with its number, time, retirement and the whole
// ciphertext it has. A version that was retired and lost its ciphertext is bound as retired.
func bindingAAD(r Record) []byte {
	a := newAAD(labelBinding).text(r.Binding.Method).text(r.Binding.KeyID).text(r.ID).bytes(r.Instance).
		text(string(r.Kind)).stamp(r.CreatedAt).list(r.Scope).addresses(r.AllowedHosts).number(len(r.Versions))
	for _, v := range r.Versions {
		a.number(v.Version).stamp(v.CreatedAt).flag(v.RetiredAt != nil)
		if v.RetiredAt != nil {
			a.stamp(*v.RetiredAt)
		}
		a.sealed(v.Value).sealed(v.WrappedKey)
	}
	return a.b
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
