package main

import (
	"context"
	"io"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// runSecretCommand opens the store as the command needs it, does the command and prints its answer.
func runSecretCommand(name string, f secretFlags, out io.Writer, w wiring) error {
	spec := secretCommands[name]
	st, err := openSecretStore(w, secretLocation{dataDir: f.dataDir, keyFile: f.keyFile}, spec.access)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	switch name {
	case "list":
		return secretList(ctx, st, f, out)
	case "show":
		return secretShow(ctx, st, f, out)
	case "delete":
		return secretDeleteCmd(ctx, st, f, out)
	case "retire":
		return secretRetire(ctx, st, f, out)
	case "set-scope", "set-hosts":
		return secretSetWho(ctx, name, st, f, out)
	}
	return secretPutOrRotate(ctx, name, st, f, out, w)
}

func secretList(ctx context.Context, st *secretStore, f secretFlags, out io.Writer) error {
	metas, err := st.svc.Describer().List(ctx)
	if err != nil {
		return err
	}
	if f.asJSON {
		return printJSON(out, map[string]any{"secrets": nonNil(metas)})
	}
	printSecretTable(out, metas)
	return nil
}

func nonNil(m []secrets.Meta) []secrets.Meta {
	if m == nil {
		return []secrets.Meta{}
	}
	return m
}

func secretShow(ctx context.Context, st *secretStore, f secretFlags, out io.Writer) error {
	meta, err := st.svc.Describer().Get(ctx, f.id)
	if err != nil {
		return err
	}
	view := st.viewOf(ctx, meta)
	if f.asJSON {
		return printJSON(out, view)
	}
	printSecretDetail(out, view)
	return nil
}

func secretDeleteCmd(ctx context.Context, st *secretStore, f secretFlags, out io.Writer) error {
	if err := st.svc.Writer().Delete(ctx, f.id); err != nil {
		return err
	}
	return printSecretDone(out, f.asJSON, map[string]any{"id": f.id, "deleted": true}, "deleted the secret "+f.id+" with all its versions")
}

func secretRetire(ctx context.Context, st *secretStore, f secretFlags, out io.Writer) error {
	meta, err := st.svc.Writer().RetireVersion(ctx, f.id, f.version)
	if err != nil {
		return err
	}
	return printSecretChange(ctx, out, st, f, meta, retireSentence(f, meta))
}

func secretSetWho(ctx context.Context, name string, st *secretStore, f secretFlags, out io.Writer) error {
	var meta secrets.Meta
	var err error
	if name == "set-scope" {
		meta, err = st.svc.Writer().SetScope(ctx, f.id, f.scope)
	} else {
		var hosts []secrets.Address
		if hosts, err = parseAddresses(f.hosts); err != nil {
			return err
		}
		meta, err = st.svc.Writer().SetAllowedHosts(ctx, f.id, hosts)
	}
	if err != nil {
		return err
	}
	return printSecretChange(ctx, out, st, f, meta, "changed the "+whoName(name)+" of the secret "+f.id)
}

func whoName(name string) string {
	if name == "set-scope" {
		return "scope"
	}
	return "allowed addresses"
}

// secretPutOrRotate takes the values, then puts a new secret in or adds a version to the one that is there.
func secretPutOrRotate(ctx context.Context, name string, st *secretStore, f secretFlags, out io.Writer, w wiring) error {
	kind := secrets.Kind(f.kind)
	if name == "rotate" {
		meta, err := st.svc.Describer().Get(ctx, f.id)
		if err != nil {
			return err
		}
		kind = meta.Kind
	}
	names, ok := secrets.FieldNames(kind)
	if !ok {
		return kernel.Errorf(kernel.CodeInvalidValue, "unknown -kind: use password or token")
	}
	fields, err := readSecretFields(f.fields, names, w)
	if err != nil {
		return err
	}
	var meta secrets.Meta
	if name == "rotate" {
		meta, err = st.svc.Writer().AddVersion(ctx, f.id, fields)
		if err != nil {
			return err
		}
		return printSecretChange(ctx, out, st, f, meta, rotateSentence(meta))
	}
	hosts, err := parseAddresses(f.hosts)
	if err != nil {
		return err
	}
	meta, err = st.svc.Writer().Create(ctx, secrets.CreateInput{ID: f.id, Kind: kind, Fields: fields, Scope: f.scope, AllowedHosts: hosts})
	if err != nil {
		return putFailure(err, f.id)
	}
	return printSecretChange(ctx, out, st, f, meta, putSentence(meta))
}

// putFailure adds to the conflict of a put that the secret is there what to do: put never changes a secret.
func putFailure(err error, id string) error {
	if kernel.CodeOf(err) != kernel.CodeConflict {
		return err
	}
	return kernel.Wrap(kernel.CodeConflict, err, "praxium secret put does not change a secret; to add a new version of "+id+" run praxium secret rotate "+id+", to start over run praxium secret delete "+id+" first")
}
