package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
	"github.com/ysk6506/praxium/backend/internal/secretsfile"
)

// `praxium secret webhook ...` (ADR-0107): the secrets of the receiving webhooks. The server makes them, a person never
// chooses one, and the value is shown once: it is the only time it can be read, because the store keeps the signing key
// encrypted under the key of the webhooks and a token only as its hash. They are in their own commands because they
// differ from the secrets of the sources: their key is another file (the webhook key), and no command takes a value.

// The commands of the webhooks are commands of `praxium secret` like the others (webhook-issue, webhook-rotate,
// webhook-retire, webhook-delete), so the test that no flag of a secret command takes a value covers them too. The id
// they take is <source-id>/<webhook-id>; -kind is the type of the proof (hmac, header_token, basic, url_token) and
// -secrets-key-file names the webhook key file (not the master key).
const secretWebhookBody = `The secret of a webhook is what a product that calls a webhook of a source proves itself with. The server makes it, a person never
chooses one, and its value is shown once on the standard output: give it to the product that calls the webhook and keep it nowhere else.
The id is <source-id>/<webhook-id>. -secrets-key-file names the webhook key file, which is not the master key: it is made the first
time a secret of a webhook is issued, and by default it is <your configuration folder>/praxium/keys/<identifier of the data folder>.webhook.key.
Keep a copy of it; a server that checks the calls needs this key and no other.
The secrets of the webhooks are kept in the file webhook-secrets.json of the data folder, apart from secrets.json, which has none of them.
`

// hookIDs splits the id of a command of the webhooks into the source and the webhook.
func hookIDs(id string) (source, webhook string, err error) {
	source, webhook, ok := strings.Cut(id, "/")
	if !ok || !ingest.ValidID(source) || !ingest.ValidID(webhook) {
		return "", "", kernel.Errorf(kernel.CodeInvalidValue, "the id of a webhook is <source-id>/<webhook-id>, each of lower-case letters, digits, _ and -, a letter first, up to 64 characters")
	}
	return source, webhook, nil
}

// hookStore is the open store for the commands of the webhooks: the file of the webhooks (webhook-secrets.json, another
// file than secrets.json), and the service that has the webhook key and no master key and no secret of a source.
type hookStore struct {
	svc    *secrets.Service
	keys   *fileHookKeys
	unlock func() error
}

func (s *hookStore) Close() error {
	return s.unlock()
}

func openHookStore(w wiring, loc secretLocation, access secretAccess) (*hookStore, error) {
	if access != secretCreate {
		if err := requireFolder(loc.dataDir); err != nil {
			return nil, err
		}
	}
	disk, err := openSecretDisk(loc.dataDir, false)
	if err != nil {
		return nil, err
	}
	unlock, err := disk.Lock()
	if err != nil {
		return nil, secretLocked(loc.dataDir, err)
	}
	out := &hookStore{unlock: unlock}
	store, err := secretsfile.OpenWebhooks(disk)
	if err != nil {
		_ = out.Close()
		return nil, err
	}
	keys := secrets.KeyProvider(nil)
	if access != secretDelete {
		out.keys = &fileHookKeys{fileKeys: newFileKeys(w, loc, disk, &secretStore{store: store, bytes: productBytes()}, false), store: store, create: access == secretCreate}
		keys = out.keys
	}
	out.svc, err = secrets.New(secrets.Config{Store: secrets.NewMemoryStore(), WebhookStore: store, Keys: secrets.NoKey(), WebhookKeys: keys, Clock: w.clock, Bytes: productBytes()})
	if err != nil {
		_ = out.Close()
		return nil, err
	}
	return out, nil
}

// fileHookKeys is the secrets.KeyProvider of the webhook key file. It finds the data folder's identifier and refuses a
// place inside the data folder as the master key does, but the file is another one (<identifier>.webhook.key) and it is
// made when the data folder has no secret of a webhook yet.
type fileHookKeys struct {
	*fileKeys
	store  *secretsfile.Store
	create bool
	made   string
}

var _ secrets.KeyProvider = (*fileHookKeys)(nil)

const webhookKeyFileSuffix = ".webhook.key"

func (k *fileHookKeys) MasterKey() (*secrets.MasterKey, error) {
	recs, err := k.store.List(context.Background())
	if err != nil {
		return nil, err
	}
	stored := ""
	if len(recs) > 0 {
		stored = recs[0].Binding.KeyID // every record of this file was sealed by the one webhook key
	}
	path, err := k.path(stored == "")
	if err != nil {
		return nil, err
	}
	key, err := readHookKey(path)
	switch {
	case err == nil:
		if stored != "" && stored != key.ID() {
			return nil, kernel.Errorf(secrets.CodeMasterKeyMismatch, "the secrets of the webhooks were sealed by the webhook key %s, but the key given is %s; give the key file they were made with (-secrets-key-file)", stored, key.ID())
		}
		return key, nil
	case !isNotExist(err):
		return nil, err
	case !k.create || stored != "":
		return nil, kernel.Errorf(secrets.CodeWebhookKeyMissing, "the webhook key file %s is not there; put it back in its place or give the file the secrets were made with (-secrets-key-file)", path)
	}
	return k.makeKey(path)
}

// makeKey makes the webhook key file the first time a secret of a webhook is issued.
func (k *fileHookKeys) makeKey(path string) (*secrets.MasterKey, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot make the folder of the webhook key file")
	}
	key := secrets.GenerateWebhookKey(k.bytes)
	made, err := putFileOnce(path, ".new-key-*", key.Encode())
	switch {
	case err != nil:
		return nil, err
	case !made:
		return readHookKey(path)
	}
	k.made = path
	return key, nil
}

// path is the one clean absolute path of the key file, outside the data folder.
func (k *fileHookKeys) path(create bool) (string, error) {
	raw := k.loc.keyFile
	if raw == "" {
		cfg, err := k.configDir()
		if err != nil || cfg == "" {
			return "", kernel.Errorf(secrets.CodeWebhookKeyMissing, "this system has no user configuration folder to keep the webhook key in; name the key file with -secrets-key-file <file>")
		}
		id, err := k.folderID(create)
		if err != nil {
			return "", err
		}
		raw = filepath.Join(cfg, "praxium", "keys", id+webhookKeyFileSuffix)
	}
	path, err := normalizePath(raw)
	if err != nil {
		return "", err
	}
	return path, k.checkOutside(path)
}

func readHookKey(path string) (*secrets.MasterKey, error) {
	raw, err := readPrivateFile(path, maxMasterKeyFile)
	if err != nil {
		if isNotExist(err) {
			return nil, err
		}
		return nil, kernel.Wrap(secrets.CodeMasterKeyInvalid, err, "the webhook key file cannot be used")
	}
	defer zero(raw)
	return secrets.ParseWebhookKey(raw)
}

func runSecretHookCommand(name string, f secretFlags, out io.Writer, w wiring) error {
	source, webhook, err := hookIDs(f.id)
	if err != nil {
		return err
	}
	if name == "webhook-issue" {
		if _, ok := secrets.KindOfWebhookAuth(f.kind); !ok {
			return kernel.Errorf(kernel.CodeInvalidValue, "-kind is the type of the proof: hmac, header_token, basic or url_token")
		}
	}
	spec := secretCommands[name]
	st, err := openHookStore(w, secretLocation{dataDir: f.dataDir, keyFile: f.keyFile}, spec.access)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	switch name {
	case "webhook-issue":
		issued, err := st.svc.IssueWebhookSecret(ctx, source, webhook, f.kind)
		return printHookIssued(out, st, f, "issued", issued, err)
	case "webhook-rotate":
		issued, err := st.svc.RotateWebhookSecret(ctx, source, webhook)
		return printHookIssued(out, st, f, "rotated", issued, err)
	case "webhook-retire":
		meta, err := st.svc.RetireWebhookVersion(ctx, source, webhook, f.version)
		if err != nil {
			return err
		}
		return printSecretDone(out, f.asJSON, map[string]any{"secret_id": meta.ID, "retired_version": f.version, "version_in_use": meta.Version},
			fmt.Sprintf("retired version %d of the secret of the webhook %s of the source %s", f.version, webhook, source))
	}
	if err := st.svc.DeleteWebhookSecret(ctx, source, webhook); err != nil {
		return err
	}
	return printSecretDone(out, f.asJSON, map[string]any{"secret_id": secrets.SecretIDFor(source, webhook), "deleted": true},
		fmt.Sprintf("deleted the secret of the webhook %s of the source %s with all its versions", webhook, source))
}

// printHookIssued prints the value of a new version: the one time it is shown.
func printHookIssued(out io.Writer, st *hookStore, f secretFlags, verb string, issued secrets.Issued, err error) error {
	if err != nil {
		return err
	}
	made := ""
	if st.keys != nil {
		made = st.keys.made
	}
	if f.asJSON {
		view := map[string]any{"secret_id": issued.Meta.ID, "kind": issued.Meta.Kind, "version": issued.Meta.Version, "value": issued.Value}
		if made != "" {
			view["webhook_key_file_made"] = made
		}
		return printJSON(out, view)
	}
	fmt.Fprintf(out, "%s the secret %s (%s): version %d\n", verb, issued.Meta.ID, issued.Meta.Kind, issued.Meta.Version)
	fmt.Fprintln(out, "the value follows; it is shown this once and is kept nowhere else:")
	fmt.Fprintln(out, issued.Value)
	if made != "" {
		fmt.Fprintf(out, "made the webhook key file %s: keep a copy of it somewhere safe; the secrets of the webhooks cannot be checked without it\n", made)
	}
	return nil
}
