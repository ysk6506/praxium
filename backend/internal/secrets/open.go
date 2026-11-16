package secrets

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Open decrypts the current version of a secret for one use. The order matters: the master key and the binding come
// first, because the scope and the addresses that are checked next are only to be believed once they authenticate.
// Then the use is checked in one place (authorize), then the version is unwrapped and decrypted.
func (s *Service) Open(ctx context.Context, id string, use Use) (*Opened, error) {
	addr, err := checkUse(use)
	if err != nil {
		return nil, err
	}
	rec, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	master, err := s.master()
	if err != nil {
		return nil, err
	}
	if err := verify(rec, master); err != nil {
		return nil, err
	}
	if err := authorize(rec, use.SourceID, addr); err != nil {
		return nil, err
	}
	ver, ok := rec.current()
	if !ok {
		return nil, kernel.Errorf(CodeVersionRetired, "every version of the secret %q is retired; put in a new version", id)
	}
	values, raw, err := decryptVersion(rec, ver, master)
	if err != nil {
		return nil, err
	}
	return &Opened{d: &openedData{id: rec.ID, kind: rec.Kind, version: ver.Version, values: values, raw: raw}}, nil
}

// authorize is the one place that decides whether a use is allowed: the source must be in the scope and the address
// among the allowed addresses. Both lists were authenticated by verify.
func authorize(rec Record, sourceID string, addr Address) error {
	if !contains(rec.Scope, sourceID) {
		return kernel.Errorf(CodeScopeDenied, "the source %q may not use the secret %q", sourceID, rec.ID)
	}
	for _, a := range rec.AllowedHosts {
		if a == addr {
			return nil
		}
	}
	return kernel.Errorf(CodeAddressDenied, "the secret %q may not be used for %s port %d", rec.ID, addr.Host, addr.Port)
}

// decryptVersion unwraps the data key of the version, decrypts the fields and checks that they are the fields of the
// kind. Whatever is wrong gives the integrity error: the stored form is not what this store wrote.
func decryptVersion(rec Record, ver VersionRecord, master *MasterKey) (map[string]Value, [][]byte, error) {
	if ver.WrappedKey.KeyID != master.id {
		return nil, nil, mismatch(rec.ID, ver.WrappedKey.KeyID, master.id)
	}
	dk, err := unwrapDataKey(master, sealed{nonce: ver.WrappedKey.Nonce, data: ver.WrappedKey.Data}, dataKeyAAD(rec.ID, ver.Version, master.id))
	if err != nil {
		return nil, nil, err
	}
	defer dk.wipe()
	plain, err := aeadOpen(dk.key, sealed{nonce: ver.Value.Nonce, data: ver.Value.Data}, valueAAD(rec.ID, ver.Version))
	if err != nil {
		return nil, nil, err
	}
	defer wipe(plain)
	raw, ok := decodeFields(plain)
	names, _ := FieldNames(rec.Kind)
	if !ok || len(raw) != len(names) {
		wipeAll(raw)
		return nil, nil, errIntegrity()
	}
	values := make(map[string]Value, len(names))
	held := make([][]byte, 0, len(names))
	for _, n := range names {
		b, present := raw[n]
		if !present || len(b) == 0 {
			wipeAll(raw)
			return nil, nil, errIntegrity()
		}
		values[n] = newValue(b)
		held = append(held, b)
	}
	return values, held, nil
}

func wipeAll(m map[string][]byte) {
	for _, b := range m {
		wipe(b)
	}
}
