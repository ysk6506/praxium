package main

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// Where the master key of a data folder is, how it is made, and how the folder is told from another (ADR-0076).

const (
	// folderIDFile is the file of the data folder that holds its identifier: made the first time a master key has to be
	// found for the folder, never from the path (a folder that is moved or copied keeps its identifier, so it finds its
	// key again). The identifier is not a secret.
	folderIDFile = "FOLDER_ID"
	// maxFolderIDFile is the most that is read of the identifier file (it holds 38 bytes).
	maxFolderIDFile = 128
	// maxMasterKeyFile is the most that is read of a key file (the unit refuses a larger one anyway).
	maxMasterKeyFile = 4096
	keyFileSuffix    = ".key"
	folderIDPrefix   = "pfid-"
)

// secretLocation says which data folder holds the secrets and, when the person named one, which file holds the key.
type secretLocation struct{ dataDir, keyFile string }

// fileKeys is the secrets.KeyProvider of the master key file: it finds the file (the one named, else the one of the
// data folder under the user's configuration folder), refuses a place inside the data folder and a file others can
// read, refuses a key that is not the one the stored secrets were sealed by, and, when it is told that the key may be
// made, makes it the first time.
type fileKeys struct {
	loc       secretLocation
	disk      secretDisk
	configDir func() (string, error)
	bytes     kernel.ByteSource
	// mayCreate says whether a key that is not there may be made now: the secrets are being put in for the first time,
	// so the store holds none. nil means never.
	mayCreate func(ctx context.Context) (bool, error)
	// storedKeyIDs gives the ids of the master keys that sealed the secrets that are stored; nil means none are asked.
	storedKeyIDs func(ctx context.Context) ([]string, error)
	// made is the path of the key file this provider made, for the answer of the command ("" when it made none).
	made string
}

var _ secrets.KeyProvider = (*fileKeys)(nil)

// MasterKey gives the master key. The path of the key file is made one clean absolute path (the links resolved) once,
// and that path is the one that is checked, read, made and printed (keyPath).
func (k *fileKeys) MasterKey() (*secrets.MasterKey, error) {
	ctx := context.Background()
	create, err := k.creationAllowed(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := k.locate(create)
	if err != nil {
		return nil, err
	}
	path, err := k.keyPath(raw)
	if err != nil {
		return nil, err
	}
	key, err := readMasterKey(path)
	switch {
	case err == nil:
		return key, k.checkSameKey(ctx, key)
	case !isNotExist(err):
		return nil, err
	case !create:
		return nil, missingKey(path)
	}
	key, made, err := createMasterKey(path, k.bytes)
	if made {
		k.made = path
	}
	return key, err
}

func (k *fileKeys) creationAllowed(ctx context.Context) (bool, error) {
	if k.mayCreate == nil {
		return false, nil
	}
	return k.mayCreate(ctx)
}

// checkSameKey refuses a key that is not the one that sealed the secrets that are stored: a store with two master keys
// would be opened by neither. The message holds the two key ids and nothing else.
func (k *fileKeys) checkSameKey(ctx context.Context, key *secrets.MasterKey) error {
	if k.storedKeyIDs == nil {
		return nil
	}
	ids, err := k.storedKeyIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id != key.ID() {
			return kernel.Errorf(secrets.CodeMasterKeyMismatch,
				"the secrets of this data folder were sealed by the master key %s, but the key given is %s; give the key file they were made with (-secrets-key-file), or delete the secrets and put them in again", id, key.ID())
		}
	}
	return nil
}

func missingKey(path string) error {
	return kernel.Errorf(secrets.CodeMasterKeyMissing,
		"the master key file %s is not there but secrets are stored; put the key file back in its place, give the file the secrets were made with (-secrets-key-file), or delete the secrets and put them in again", path)
}

// locate is the path of the key file. The one named comes first. Otherwise it is <user configuration folder>/praxium/
// keys/<identifier of the data folder>.key; a system that has no configuration folder is told to name the file.
func (k *fileKeys) locate(create bool) (string, error) {
	if k.loc.keyFile != "" {
		return k.loc.keyFile, nil
	}
	cfg, err := k.configDir()
	if err != nil || cfg == "" {
		return "", kernel.Errorf(secrets.CodeMasterKeyMissing,
			"this system has no user configuration folder to keep the master key in; name the key file with -secrets-key-file <file>")
	}
	id, err := k.folderID(create)
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "praxium", "keys", id+keyFileSuffix), nil
}

// folderIDAdvice is what to do when the identifier of the data folder cannot be had. It names no value.
func (k *fileKeys) folderIDAdvice() string {
	where := "the folder praxium/keys of your configuration folder"
	if cfg, err := k.configDir(); err == nil && cfg != "" {
		where = filepath.Join(cfg, "praxium", "keys")
	}
	return "name the key file with -secrets-key-file <file>; the keys are kept in " + where + " (a key file is named by the identifier of its data folder, and the file " + folderIDFile +
		" of the data folder holds that name without .key; the key id that praxium secret show gives is the key_id inside the right key file). If the data folder holds no secrets, remove " +
		folderIDFile + " and put the secret in again"
}

// folderID reads the identifier of the data folder, and makes it when it is not there and the key may be made (a
// folder that holds secrets and has no identifier has lost its key's name: the key has to be named).
func (k *fileKeys) folderID(create bool) (string, error) {
	path := k.disk.path(folderIDFile)
	raw, err := readFileChecked(path, maxFolderIDFile, false, path)
	switch {
	case err == nil:
		id := strings.TrimSpace(string(raw))
		if !validFolderID(id) {
			return "", kernel.Errorf(secrets.CodeMasterKeyInvalid, "%s is damaged; %s", path, k.folderIDAdvice())
		}
		return id, nil
	case !isNotExist(err):
		return "", kernel.Wrap(secrets.CodeMasterKeyInvalid, err, "cannot read "+path+"; "+k.folderIDAdvice())
	case !create:
		return "", kernel.Errorf(secrets.CodeMasterKeyMissing, "the data folder has secrets but no %s to find their key by; %s", folderIDFile, k.folderIDAdvice())
	}
	return k.makeFolderID(path)
}

// makeFolderID makes the identifier file as the key file is made (a temporary file, synced, then linked to its name),
// so a half written identifier is never there; one that is there already is the one that is used.
func (k *fileKeys) makeFolderID(path string) (string, error) {
	raw := make([]byte, 16)
	k.bytes.Fill(raw)
	id := folderIDPrefix + hexOf(raw)
	made, err := putFileOnce(path, ".new-id-*", []byte(id+"\n"))
	if err != nil {
		return "", err
	}
	if !made {
		return k.folderID(false)
	}
	return id, nil
}

func validFolderID(id string) bool {
	rest, ok := strings.CutPrefix(id, folderIDPrefix)
	if !ok || len(rest) != 32 {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func hexOf(raw []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(raw)*2)
	for _, b := range raw {
		out = append(out, digits[b>>4], digits[b&15])
	}
	return string(out)
}
