package secrets

import (
	"slices"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Sealed is an AES-256-GCM ciphertext as stored: the method (the name of the wrapping or encrypting), the id of the
// master key when a master key made it, the nonce, and the ciphertext with its tag. Bytes are base64 in JSON.
type Sealed struct {
	Method string `json:"method"`
	KeyID  string `json:"key_id,omitempty"`
	Nonce  []byte `json:"nonce"`
	Data   []byte `json:"data"`
}

// VersionRecord is one version as stored. A retired version has neither Value nor WrappedKey: its ciphertext is gone.
type VersionRecord struct {
	Version    int        `json:"version"`
	CreatedAt  time.Time  `json:"created_at"`
	RetiredAt  *time.Time `json:"retired_at,omitempty"`
	Value      *Sealed    `json:"value,omitempty"`       // the fields under the data key
	WrappedKey *Sealed    `json:"wrapped_key,omitempty"` // the data key under the master key
}

// Record is a secret as stored: nothing in it is a value in the clear. Binding is the tag under the master key over
// the whole state of the record (bindingAAD).
type Record struct {
	ID           string          `json:"id"`
	Kind         Kind            `json:"kind"`
	Instance     []byte          `json:"instance"` // 16 random bytes made with the secret, in every authenticated data
	CreatedAt    time.Time       `json:"created_at"`
	Scope        []string        `json:"scope"`
	AllowedHosts []Address       `json:"allowed_hosts"`
	Binding      Sealed          `json:"binding"`
	Versions     []VersionRecord `json:"versions"`
}

func (s Sealed) clone() Sealed {
	s.Nonce, s.Data = slices.Clone(s.Nonce), slices.Clone(s.Data)
	return s
}

func cloneSealed(s *Sealed) *Sealed {
	if s == nil {
		return nil
	}
	c := s.clone()
	return &c
}

// Clone returns a copy that shares nothing with r.
func (r Record) Clone() Record {
	r.Instance = slices.Clone(r.Instance)
	r.Scope = slices.Clone(r.Scope)
	r.AllowedHosts = slices.Clone(r.AllowedHosts)
	r.Binding = r.Binding.clone()
	vs := make([]VersionRecord, len(r.Versions))
	for i, v := range r.Versions {
		if v.RetiredAt != nil {
			t := *v.RetiredAt
			v.RetiredAt = &t
		}
		v.Value, v.WrappedKey = cloneSealed(v.Value), cloneSealed(v.WrappedKey)
		vs[i] = v
	}
	r.Versions = vs
	return r
}

// current returns the version an open gives: the highest one that is not retired.
func (r Record) current() (VersionRecord, bool) {
	for i := len(r.Versions) - 1; i >= 0; i-- {
		if r.Versions[i].RetiredAt == nil {
			return r.Versions[i], true
		}
	}
	return VersionRecord{}, false
}

// Validate checks the shape of a stored record: syntax and structure, not the cryptography (that is checked when the
// secret is opened). A record that fails is a file that cannot be used.
func (r Record) Validate() error {
	bad := func(why string) error {
		return kernel.Errorf(kernel.CodeInvalidValue, "the stored secret %q %s", r.ID, why)
	}
	if err := ValidateID(r.ID); err != nil {
		return kernel.Errorf(kernel.CodeInvalidValue, "a stored secret has an invalid id")
	}
	if len(r.Instance) != instanceLen {
		return bad("has no instance id of the right length")
	}
	if _, ok := kindFieldNames(r.Kind); !ok {
		return bad("has an unknown kind")
	}
	if scope, err := normalizeScope(slices.Clone(r.Scope)); err != nil || !slices.Equal(scope, r.Scope) {
		return bad("has a scope that is not in the stored form")
	}
	if hosts, err := normalizeAddresses(slices.Clone(r.AllowedHosts)); err != nil || !slices.Equal(hosts, r.AllowedHosts) {
		return bad("has allowed addresses that are not in the stored form")
	}
	if !nanoSafe(r.CreatedAt) {
		return bad("has a creation time out of the range that the binding can hold")
	}
	if !validSealed(r.Binding, true) {
		return bad("has a binding that is not in the stored form")
	}
	return r.validateVersions(bad)
}

func (r Record) validateVersions(bad func(string) error) error {
	if len(r.Versions) == 0 || len(r.Versions) > maxVersions {
		return bad("has no versions or too many")
	}
	last := 0
	for _, v := range r.Versions {
		if v.Version <= last || v.Version > maxVersionNumber {
			return bad("has versions that are not in rising order or have a number out of range")
		}
		last = v.Version
		if !nanoSafe(v.CreatedAt) || v.RetiredAt != nil && !nanoSafe(*v.RetiredAt) {
			return bad("has a version time out of the range that the binding can hold")
		}
		if why := v.shapeProblem(); why != "" {
			return bad(why)
		}
	}
	return nil
}

// shapeProblem says what is wrong with the form of a version, or "" when nothing is.
func (v VersionRecord) shapeProblem() string {
	live := v.Value != nil && v.WrappedKey != nil
	if v.RetiredAt == nil && !live || v.RetiredAt != nil && (v.Value != nil || v.WrappedKey != nil) {
		return "has a version whose retirement and ciphertext disagree"
	}
	if live && (!validSealed(*v.Value, false) || !validSealed(*v.WrappedKey, true)) {
		return "has a version that is not in the stored form"
	}
	return ""
}

func validSealed(s Sealed, withKey bool) bool {
	if s.Method != MethodAES256GCM || len(s.Nonce) != nonceLen || len(s.Data) < 16 {
		return false
	}
	if withKey {
		return validName(s.KeyID)
	}
	return s.KeyID == ""
}

func (r Record) meta() Meta {
	names, _ := kindFieldNames(r.Kind)
	m := Meta{
		ID: r.ID, Kind: r.Kind, FieldNames: names, CreatedAt: r.CreatedAt,
		Scope: slices.Clone(r.Scope), AllowedHosts: slices.Clone(r.AllowedHosts),
	}
	if m.Scope == nil {
		m.Scope = []string{}
	}
	if m.AllowedHosts == nil {
		m.AllowedHosts = []Address{}
	}
	for _, v := range r.Versions {
		info := VersionInfo{Version: v.Version, CreatedAt: v.CreatedAt}
		if v.RetiredAt != nil {
			t := *v.RetiredAt
			info.RetiredAt = &t
		}
		m.Versions = append(m.Versions, info)
	}
	if cur, ok := r.current(); ok {
		m.Version = cur.Version
	}
	return m
}

// nanoSafe says whether a time is the same instant when counted in nanoseconds since 1970 (years 1678 to 2262). The
// binding writes times that way, so a time outside would be the same bytes as one 2^64 nanoseconds away.
func nanoSafe(t time.Time) bool { return time.Unix(0, t.UnixNano()).Equal(t) }
