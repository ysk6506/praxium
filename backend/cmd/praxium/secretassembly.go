package main

import (
	"context"
	"os"

	"github.com/ysk6506/praxium/backend/internal/filejournal"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
	"github.com/ysk6506/praxium/backend/internal/secretsfile"
)

// The assembly of the secret store (ADR-0076): the one function that joins the file store, the disk of the operating
// system, the lock of the data folder, the master key file, the clock and the system's randomness into a
// secrets.Service. The commands of `praxium secret` use it, and so will whatever else opens the store (the ingestion
// service), so that there is one place that decides how a store is made and no second one.

// secretAccess says what the caller does with the store, which decides what the assembly takes and checks.
type secretAccess int

const (
	// secretRead lists and shows. It takes no lock and needs no key: a folder that a server has open can be looked at.
	secretRead secretAccess = iota
	// secretDelete changes the store without the key (a deletion needs none): the lock, no key. The data folder has to be
	// there: only secretCreate makes one.
	secretDelete
	// secretChange changes a secret that is there: the lock and the key, which is never made here.
	secretChange
	// secretCreate puts a secret in: the lock and the key, which is made when there is none and the store is empty.
	secretCreate
)

// secretStore is an open store with what has to be let go of.
type secretStore struct {
	svc   *secrets.Service
	store *secretsfile.Store
	// keys is the master key file's provider; nil for a store that is only read or has its secrets deleted.
	keys *fileKeys
	// bytes is the source of the randomness of the keys and nonces: always the system's (see productBytes).
	bytes  kernel.ByteSource
	unlock func() error
}

// Close lets go of the lock of the data folder.
func (s *secretStore) Close() error {
	if s.unlock == nil {
		return nil
	}
	return s.unlock()
}

// productBytes is the randomness of the secret store of the program: the system's. A deterministic source
// (kernel.NewSeededBytes) is for tests of the units and is never given here; a test of the assembly checks both.
func productBytes() kernel.ByteSource { return kernel.SystemBytes{} }

// openSecretStore opens the store of the data folder for what the caller does with it.
func openSecretStore(w wiring, loc secretLocation, access secretAccess) (*secretStore, error) {
	if access == secretDelete || access == secretChange {
		if err := requireFolder(loc.dataDir); err != nil {
			return nil, err
		}
	}
	disk, err := openSecretDisk(loc.dataDir, access == secretRead)
	if err != nil {
		return nil, err
	}
	out := &secretStore{bytes: productBytes()}
	if access != secretRead {
		unlock, err := disk.Lock()
		if err != nil {
			return nil, secretLocked(loc.dataDir, err)
		}
		out.unlock = unlock
	}
	if out.store, err = secretsfile.Open(disk); err != nil {
		_ = out.Close()
		return nil, err
	}
	keys := secrets.NoKey()
	if access == secretChange || access == secretCreate {
		out.keys = newFileKeys(w, loc, disk, out, access == secretCreate)
		keys = out.keys
	}
	out.svc, err = secrets.New(secrets.Config{Store: out.store, Keys: keys, Clock: w.clock, Bytes: out.bytes})
	if err != nil {
		_ = out.Close()
		return nil, err
	}
	return out, nil
}

func newFileKeys(w wiring, loc secretLocation, disk secretDisk, s *secretStore, mayCreate bool) *fileKeys {
	k := &fileKeys{loc: loc, disk: disk, configDir: w.userConfigDir(), bytes: s.bytes}
	k.storedKeyIDs = func(ctx context.Context) ([]string, error) {
		all, err := s.store.List(ctx)
		ids := make([]string, 0, len(all))
		for _, rec := range all {
			if !secrets.IsReceiveOnly(rec.Kind) { // the secrets of the webhooks have a key of their own
				ids = append(ids, rec.Binding.KeyID)
			}
		}
		return ids, err
	}
	if mayCreate {
		k.mayCreate = func(ctx context.Context) (bool, error) {
			all, err := s.store.List(ctx)
			for _, rec := range all {
				if !secrets.IsReceiveOnly(rec.Kind) {
					return false, err
				}
			}
			return true, err
		}
	}
	return k
}

// userConfigDir is where the user's configuration folder is: the system's unless a test says another.
func (w wiring) userConfigDir() func() (string, error) {
	if w.configDir != nil {
		return w.configDir
	}
	return os.UserConfigDir
}

// secretLocked says that a data folder that another process holds cannot be changed, and what to do.
func secretLocked(dir string, err error) error {
	if kernel.CodeOf(err) != filejournal.CodeLocked {
		return err
	}
	return kernel.Errorf(filejournal.CodeLocked, "the data folder %s is in use by another praxium (a server such as praxium local or api serve, or an ingest run). "+
		"Stop it, run the praxium secret command again, and start it again", dir)
}
