package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/devauth"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/identity"
	"github.com/ysk6506/praxium/backend/internal/identityfile"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// The built-in authentication of a production server (ADR-0044): which store holds the users and tokens, the file
// of it on the operating system, and the checks that a production start passes before anything is opened.

// authDisk is the data folder as the identity file needs it: the same folder the journal of the file store lives in.
// Everything it makes is readable by its owner only (0600), and it refuses to read an identity file that others can
// read: a file that was copied or restored with a loose mode is a thing the operator must look at.
type authDisk struct{ fileDisk }

var _ identityfile.Disk = authDisk{}

// ReadWhole returns the whole file, after checking who may read it.
func (d authDisk) ReadWhole(name string) ([]byte, error) {
	info, err := os.Stat(d.path(name))
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is readable by other users (mode %04o): run chmod 600 %s, then start again",
			name, info.Mode().Perm(), d.path(name))
	}
	return os.ReadFile(d.path(name))
}

// Create makes a new file for writing, for its owner only, and fails when the name exists.
func (d authDisk) Create(name string) (identityfile.File, error) {
	return os.OpenFile(d.path(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

// randomOf is the source of the secrets of tokens: the kernel's, which reads the entropy of the kernel of the
// operating system, unless a test passes a fixed one.
func randomOf(d wiring) func(int) string {
	if d.random != nil {
		return d.random
	}
	return kernel.NewToken
}

// authKit is what a server needs to know who calls it: in development one fixed authenticator, in production the
// built-in service, which is also the directory of users and tokens, and the limiter of failures.
type authKit struct {
	authn   access.Authenticator
	dir     access.Directory
	limiter access.FailureLimiter
	// name says which one it is, for the start log.
	name  string
	close func() error
}

// checkMode is the fail-closed guard of the server (ADR-0024, ADR-0027, ADR-0044). Development mode starts with its
// one token; production mode starts only when everything it needs is there. Every refusal comes before the store is
// opened and before the address is taken.
func checkMode(o serveOptions, d wiring) error {
	switch strings.ToLower(strings.TrimSpace(o.mode)) {
	case devauth.ModeDevelopment:
		return nil
	case devauth.ModeProduction:
		return checkProduction(o, d)
	}
	return kernel.Errorf(kernel.CodeInvalidValue, "unknown -mode %q: use development or production", o.mode)
}

// elsewhere is the sentence that sends a person who only wants to look around to the commands for that.
const elsewhere = "To look around run `praxium demo`; for your own data run `praxium local`; for development use `-mode development`."

func checkProduction(o serveOptions, d wiring) error {
	if o.devFlag != "" {
		return kernel.Errorf(kernel.CodeInvalidValue, "-%s belongs to -mode development: a production server signs people in with the access tokens of praxium admin, never with a development token", o.devFlag)
	}
	if token, _ := d.env.Lookup(envDevToken); strings.TrimSpace(token) != "" {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s is for -mode development only: unset it to start in production", envDevToken)
	}
	if o.store != storeFile && o.store != storePostgres {
		return kernel.Errorf(kernel.CodeInvalidValue,
			"production mode (-mode production) keeps its users and tokens in a store that survives a restart, and the memory store does not: "+
				"use -store file -store-data-dir DIR (or -store postgres -store-dsn-file FILE), make the first administrator with `praxium admin bootstrap` "+
				"with the same store flags, then start again. %s", elsewhere)
	}
	if (o.tlsCert == "") != (o.tlsKey == "") {
		return kernel.Errorf(kernel.CodeInvalidValue, "-tls-cert and -tls-key go together")
	}
	if o.tlsCert == "" && !o.behindProxy && !isLoopbackAddr(o.addr) {
		return kernel.Errorf(kernel.CodeInvalidValue,
			"-addr %s is not a loopback address, and access tokens must not cross a network in plain text: give -tls-cert and -tls-key, "+
				"or put a reverse proxy that ends TLS in front and say so with -behind-proxy, or listen on 127.0.0.1", o.addr)
	}
	return nil
}

// tlsConfigFor reads the certificate and key of -tls-cert and -tls-key; no flags, no TLS.
func tlsConfigFor(o serveOptions, d wiring) (*tls.Config, error) {
	if o.tlsCert == "" {
		return nil, nil
	}
	certPEM, err := d.env.ReadFile(o.tlsCert)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the TLS certificate (-tls-cert)")
	}
	keyPEM, err := d.env.ReadFile(o.tlsKey)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the TLS key (-tls-key)")
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "the TLS certificate and key do not make a pair")
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}, nil
}

// productionAuth builds the authentication of a production server over the store it has opened: the identity data
// lives beside the KG, in the data folder (which this process holds the lock of) or in the PostgreSQL database. A
// server with no administrator would have no one to manage it, so it does not start.
func productionAuth(o serveOptions, d wiring) func(context.Context, graph.Store, *slog.Logger) (authKit, error) {
	return func(ctx context.Context, store graph.Store, log *slog.Logger) (authKit, error) {
		ids, err := serverIdentityStore(o, store)
		if err != nil {
			return authKit{}, err
		}
		svc, err := identity.New(identity.Config{Store: ids, Clock: d.clock, Random: randomOf(d), Log: log})
		if err != nil {
			return authKit{}, err
		}
		has, err := svc.HasAdministrator(ctx)
		if err != nil {
			return authKit{}, err
		}
		if !has {
			return authKit{}, kernel.Errorf(kernel.CodeInvalidValue, "there is no administrator in this store, so nobody could manage the server: %s", bootstrapHint(o))
		}
		return authKit{authn: svc, dir: svc, limiter: identity.NewLimiter(d.clock, identity.LimiterConfig{}), name: "built-in", close: func() error { return nil }}, nil
	}
}

// bootstrapHint is the command that makes the first administrator, with the store flags of this start.
func bootstrapHint(o serveOptions) string {
	flags := "-store file -store-data-dir " + o.storeDataDir
	if o.store == storePostgres {
		flags = "-store postgres -store-dsn-file " + o.storeDSNFile
	}
	return fmt.Sprintf("make the first one at this machine with `praxium admin bootstrap -name NAME %s` (with the server stopped for a data folder), then start again", flags)
}

// recordsOf is what the PostgreSQL store gives for the identity data.
type recordsOf interface{ Records() *pgstore.Records }

// serverIdentityStore is the identity store of a running server. The data folder is already locked by the journal of
// the file store, so the identity file needs no lock of its own; the PostgreSQL store hands out its records.
func serverIdentityStore(o serveOptions, store graph.Store) (identity.Store, error) {
	switch o.store {
	case storeFile:
		disk, err := openDataDir(o.storeDataDir)
		if err != nil {
			return nil, err
		}
		return identityfile.Open(authDisk{disk})
	case storePostgres:
		if pg, ok := store.(recordsOf); ok {
			return pg.Records(), nil
		}
	}
	return nil, kernel.Errorf(kernel.CodeInternal, "the %s store has no place for users and tokens", o.store)
}

// withIdentity opens the identity store for a command that runs at the server with no API (the bootstrap), and
// calls fn with it. A data folder is locked for the time of the call, so the command cannot run beside a server that
// has the folder open; the PostgreSQL database needs no lock, the store itself makes two bootstraps exclusive.
func withIdentity(ctx context.Context, o serveOptions, d wiring, fn func(identity.Store) error) error {
	switch o.store {
	case storeFile:
		if o.storeDataDir == "" || o.storeDSNFile != "" {
			return kernel.Errorf(kernel.CodeInvalidValue, "-store file needs -store-data-dir (and not -store-dsn-file)")
		}
		disk, err := openDataDir(o.storeDataDir)
		if err != nil {
			return err
		}
		unlock, err := disk.Lock()
		if err != nil {
			return lockHint(err)
		}
		defer func() { _ = unlock() }()
		st, err := identityfile.Open(authDisk{disk})
		if err != nil {
			return err
		}
		return fn(st)
	case storePostgres:
		if o.storeDSNFile == "" || o.storeDataDir != "" {
			return kernel.Errorf(kernel.CodeInvalidValue, "-store postgres needs -store-dsn-file (and not -store-data-dir)")
		}
		store, closeStore, err := openPostgres(ctx, o, d)
		if err != nil {
			return err
		}
		defer func() { _ = closeStore() }()
		pg, ok := store.(recordsOf)
		if !ok {
			return kernel.Errorf(kernel.CodeInternal, "the postgres store has no place for users and tokens")
		}
		return fn(pg.Records())
	}
	return kernel.Errorf(kernel.CodeInvalidValue, "the users and tokens are kept in -store file or -store postgres, not %q", o.store)
}
