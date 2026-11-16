package secrets

import (
	"context"
	"slices"
	"sync"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Config is what a Service is made of.
type Config struct {
	Store Store
	Keys  KeyProvider
	Clock kernel.Clock
	// Bytes is where keys and nonces come from: kernel.SystemBytes{} in a real run, kernel.NewSeededBytes in a test
	// that must repeat.
	Bytes kernel.ByteSource
}

// Service is the secret store. It implements the three ports, which the composition root hands out one by one:
// Writer to the roles that put secrets in, Opener to the roles that connect to sources, Describer to everyone.
type Service struct {
	cfg Config
	mu  sync.Mutex // serializes the writes (read, change, put) of this process, the only writer of its store
}

var (
	_ Writer    = (*Service)(nil)
	_ Opener    = (*Service)(nil)
	_ Describer = (*Service)(nil)
)

// New makes a Service. All four parts of the config are needed.
func New(cfg Config) (*Service, error) {
	if cfg.Store == nil || cfg.Keys == nil || cfg.Clock == nil || cfg.Bytes == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a secret service needs a store, a key provider, a clock and a byte source")
	}
	return &Service{cfg: cfg}, nil
}

func notFound(id string) error {
	return kernel.Errorf(kernel.CodeNotFound, "there is no secret %q", id)
}

// load reads a record that must be there.
func (s *Service) load(ctx context.Context, id string) (Record, error) {
	if err := ValidateID(id); err != nil {
		return Record{}, err
	}
	rec, ok, err := s.cfg.Store.Get(ctx, id)
	if err != nil {
		return Record{}, kernel.Wrap(kernel.CodeInternal, err, "cannot read the secret store")
	}
	if !ok {
		return Record{}, notFound(id)
	}
	return rec, nil
}

func (s *Service) put(ctx context.Context, rec Record) error {
	if err := s.cfg.Store.Put(ctx, rec); err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot write the secret store")
	}
	return nil
}

// bind seals the binding of the record anew: the tag under the master key over the whole state of the record. Every
// write ends with it, so a state that was not written by this store never authenticates.
func (s *Service) bind(rec *Record, master *MasterKey) error {
	rec.Binding = Sealed{Method: MethodAES256GCM, KeyID: master.id}
	tag, err := aeadSeal(s.cfg.Bytes, master.bytes(), nil, bindingAAD(*rec))
	if err != nil {
		return err
	}
	rec.Binding.Nonce, rec.Binding.Data = tag.nonce, tag.data
	return nil
}

// verify checks that the record was sealed by this master key and that its binding authenticates: what the file says
// about the secret (scope, addresses, which versions there are and which of them are retired) is what was written, as
// one state. Every read of the state and every write begins with it.
func verify(rec Record, master *MasterKey) error {
	if rec.Binding.KeyID != master.id {
		return mismatch(rec.ID, rec.Binding.KeyID, master.id)
	}
	_, err := aeadOpen(master.bytes(), sealed{nonce: rec.Binding.Nonce, data: rec.Binding.Data}, bindingAAD(rec))
	return err
}

func mismatch(id, stored, given string) error {
	return kernel.Errorf(CodeMasterKeyMismatch, "the secret %q was sealed by the master key %q, not %q; put the secret in again or give the key file it was made with", id, stored, given)
}

func (s *Service) master() (*MasterKey, error) {
	key, err := s.cfg.Keys.MasterKey()
	if err != nil {
		return nil, err
	}
	if key == nil {
		return NoKey().MasterKey()
	}
	return key, nil
}

// newVersion makes version number n of the secret: a new data key encrypts the fields once and the master key wraps
// the data key. The data key is overwritten before this returns.
func (s *Service) newVersion(id string, instance []byte, n int, fields Fields, master *MasterKey) (VersionRecord, error) {
	plain := encodeFields(fields)
	defer wipe(plain)
	dk := newDataKey(s.cfg.Bytes)
	defer dk.wipe()
	value, err := dk.seal(s.cfg.Bytes, plain, valueAAD(id, instance, n))
	if err != nil {
		return VersionRecord{}, err
	}
	wrapped, err := dk.wrap(s.cfg.Bytes, master, dataKeyAAD(id, instance, n, master.id))
	if err != nil {
		return VersionRecord{}, err
	}
	return VersionRecord{
		Version:    n,
		CreatedAt:  s.cfg.Clock.Now().UTC(),
		Value:      &Sealed{Method: MethodAES256GCM, Nonce: value.nonce, Data: value.data},
		WrappedKey: &Sealed{Method: MethodAES256GCM, KeyID: master.id, Nonce: wrapped.nonce, Data: wrapped.data},
	}, nil
}

// Create makes a secret with version 1.
func (s *Service) Create(ctx context.Context, in CreateInput) (Meta, error) {
	if err := ValidateID(in.ID); err != nil {
		return Meta{}, err
	}
	if err := checkFields(in.Kind, in.Fields); err != nil {
		return Meta{}, err
	}
	scope, err := normalizeScope(slices.Clone(in.Scope))
	if err != nil {
		return Meta{}, err
	}
	hosts, err := normalizeAddresses(in.AllowedHosts)
	if err != nil {
		return Meta{}, err
	}
	master, err := s.master()
	if err != nil {
		return Meta{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok, err := s.cfg.Store.Get(ctx, in.ID); err != nil {
		return Meta{}, kernel.Wrap(kernel.CodeInternal, err, "cannot read the secret store")
	} else if ok {
		return Meta{}, kernel.Errorf(kernel.CodeConflict, "there is already a secret %q", in.ID)
	}
	instance := make([]byte, instanceLen)
	s.cfg.Bytes.Fill(instance)
	rec := Record{ID: in.ID, Instance: instance, Kind: in.Kind, CreatedAt: s.cfg.Clock.Now().UTC(), Scope: scope, AllowedHosts: hosts}
	v, err := s.newVersion(in.ID, instance, 1, in.Fields, master)
	if err != nil {
		return Meta{}, err
	}
	rec.Versions = []VersionRecord{v}
	if err := s.bind(&rec, master); err != nil {
		return Meta{}, err
	}
	if err := s.put(ctx, rec); err != nil {
		return Meta{}, err
	}
	return rec.meta(), nil
}

// AddVersion adds the next version to a secret whose stored form authenticates.
func (s *Service) AddVersion(ctx context.Context, id string, fields Fields) (Meta, error) {
	master, err := s.master()
	if err != nil {
		return Meta{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	if err := checkFields(rec.Kind, fields); err != nil {
		return Meta{}, err
	}
	if err := verify(rec, master); err != nil {
		return Meta{}, err
	}
	if len(rec.Versions) >= maxVersions {
		return Meta{}, kernel.Errorf(kernel.CodeInvalidValue, "a secret has at most %d versions; delete it and put it in again", maxVersions)
	}
	v, err := s.newVersion(id, rec.Instance, rec.Versions[len(rec.Versions)-1].Version+1, fields, master)
	if err != nil {
		return Meta{}, err
	}
	rec.Versions = append(rec.Versions, v)
	if err := s.bind(&rec, master); err != nil {
		return Meta{}, err
	}
	if err := s.put(ctx, rec); err != nil {
		return Meta{}, err
	}
	return rec.meta(), nil
}

// RetireVersion retires a version and deletes its ciphertext. It needs the master key: the retirement is part of the
// state that the binding covers, and a store that could retire without the key could not tell a retirement that was
// undone by hand. Delete needs no key and is the way to stop a secret when the key is lost.
func (s *Service) RetireVersion(ctx context.Context, id string, version int) (Meta, error) {
	master, err := s.master()
	if err != nil {
		return Meta{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	if err := verify(rec, master); err != nil {
		return Meta{}, err
	}
	i := slices.IndexFunc(rec.Versions, func(v VersionRecord) bool { return v.Version == version })
	if i < 0 {
		return Meta{}, kernel.Errorf(kernel.CodeNotFound, "the secret %q has no version %d", id, version)
	}
	if rec.Versions[i].RetiredAt != nil {
		return Meta{}, kernel.Errorf(kernel.CodeConflict, "version %d of the secret %q is already retired", version, id)
	}
	now := s.cfg.Clock.Now().UTC()
	rec.Versions[i].RetiredAt, rec.Versions[i].Value, rec.Versions[i].WrappedKey = &now, nil, nil
	if err := s.bind(&rec, master); err != nil {
		return Meta{}, err
	}
	if err := s.put(ctx, rec); err != nil {
		return Meta{}, err
	}
	return rec.meta(), nil
}

// Delete removes the secret. It needs no master key. A secret made again under the same id is another secret (it has
// another instance id), so nothing of the deleted one fits it.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found, err := s.cfg.Store.Delete(ctx, id)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot write the secret store")
	}
	if !found {
		return notFound(id)
	}
	return nil
}

// SetScope replaces the scope. The stored state must authenticate first: sealing a new binding over a record that was
// changed by hand would make the change genuine.
func (s *Service) SetScope(ctx context.Context, id string, sourceIDs []string) (Meta, error) {
	scope, err := normalizeScope(slices.Clone(sourceIDs))
	if err != nil {
		return Meta{}, err
	}
	return s.rebind(ctx, id, func(r *Record) { r.Scope = scope })
}

// SetAllowedHosts replaces the allowed addresses, under the same rule as SetScope.
func (s *Service) SetAllowedHosts(ctx context.Context, id string, hosts []Address) (Meta, error) {
	list, err := normalizeAddresses(hosts)
	if err != nil {
		return Meta{}, err
	}
	return s.rebind(ctx, id, func(r *Record) { r.AllowedHosts = list })
}

func (s *Service) rebind(ctx context.Context, id string, change func(*Record)) (Meta, error) {
	master, err := s.master()
	if err != nil {
		return Meta{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	if err := verify(rec, master); err != nil {
		return Meta{}, err
	}
	change(&rec)
	if err := s.bind(&rec, master); err != nil {
		return Meta{}, err
	}
	if err := s.put(ctx, rec); err != nil {
		return Meta{}, err
	}
	return rec.meta(), nil
}

// List returns the metadata of every secret.
func (s *Service) List(ctx context.Context) ([]Meta, error) {
	recs, err := s.cfg.Store.List(ctx)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot read the secret store")
	}
	out := make([]Meta, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.meta())
	}
	return out, nil
}

// Get returns the metadata of one secret.
func (s *Service) Get(ctx context.Context, id string) (Meta, error) {
	rec, err := s.load(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	return rec.meta(), nil
}
