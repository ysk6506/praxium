package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
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
	// maxMasterKeyFile is the most that is read of a key file (the unit refuses a larger one anyway).
	maxMasterKeyFile = 4096
	keyFileSuffix    = ".key"
	folderIDPrefix   = "pfid-"
)

// secretLocation says which data folder holds the secrets and, when the person named one, which file holds the key.
type secretLocation struct{ dataDir, keyFile string }

// fileKeys is the secrets.KeyProvider of the master key file: it finds the file (the one named, else the one of the
// data folder under the user's configuration folder), refuses a place inside the data folder and a file others can
// read, and, when it is told that the key may be made, makes it the first time.
type fileKeys struct {
	loc       secretLocation
	disk      secretDisk
	configDir func() (string, error)
	bytes     kernel.ByteSource
	// mayCreate says whether a key that is not there may be made now: the secrets are being put in for the first time,
	// so the store holds none. nil means never.
	mayCreate func(ctx context.Context) (bool, error)
	// made is the path of the key file this provider made, for the answer of the command ("" when it made none).
	made string
}

var _ secrets.KeyProvider = (*fileKeys)(nil)

// MasterKey gives the master key.
func (k *fileKeys) MasterKey() (*secrets.MasterKey, error) {
	ctx := context.Background()
	create, err := k.creationAllowed(ctx)
	if err != nil {
		return nil, err
	}
	path, err := k.locate(create)
	if err != nil {
		return nil, err
	}
	if err := k.checkOutside(path); err != nil {
		return nil, err
	}
	key, err := readMasterKey(path)
	if err == nil || !isNotExist(err) {
		return key, err
	}
	if !create {
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

// folderID reads the identifier of the data folder, and makes it when it is not there and the key may be made (a
// folder that holds secrets and has no identifier has lost its key's name: the key has to be named).
func (k *fileKeys) folderID(create bool) (string, error) {
	path := k.disk.path(folderIDFile)
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		id := strings.TrimSpace(string(raw))
		if !validFolderID(id) {
			return "", kernel.Errorf(secrets.CodeMasterKeyInvalid, "%s is damaged; name the key file with -secrets-key-file <file>", path)
		}
		return id, nil
	case !isNotExist(err):
		return "", kernel.Wrap(secrets.CodeMasterKeyInvalid, err, "cannot read "+path)
	case !create:
		return "", kernel.Errorf(secrets.CodeMasterKeyMissing,
			"the data folder has secrets but no %s to find their key by; name the key file with -secrets-key-file <file>, or delete the secrets and put them in again", folderIDFile)
	}
	return k.makeFolderID(path)
}

func (k *fileKeys) makeFolderID(path string) (string, error) {
	raw := make([]byte, 16)
	k.bytes.Fill(raw)
	id := folderIDPrefix + hexOf(raw)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return k.folderID(false)
	}
	if err != nil {
		return "", kernel.Wrap(kernel.CodeInternal, err, "cannot make "+path)
	}
	_, err = f.WriteString(id + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", kernel.Wrap(kernel.CodeInternal, err, "cannot write "+path)
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

// checkOutside refuses a key file in the data folder or below it (the paths are cleaned and the links resolved
// before they are compared): a copy or a backup of the folder would carry the key with the secrets it opens.
func (k *fileKeys) checkOutside(keyPath string) error {
	dir, err := realPath(k.loc.dataDir)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot resolve the data folder")
	}
	key, err := realPath(keyPath)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot resolve the key file")
	}
	rel, err := filepath.Rel(dir, key)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return kernel.Errorf(kernel.CodeInvalidValue,
			"the master key file %s is inside the data folder %s; keep the key outside of it, so that a copy of the folder does not carry its key (name another place with -secrets-key-file <file>)", keyPath, k.loc.dataDir)
	}
	return nil
}

// realPath is an absolute path with the links resolved as far as the path exists; a part that does not exist yet is
// kept as it is.
func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rest, cur := "", abs
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		parent := filepath.Dir(cur)
		if !isNotExist(err) || parent == cur {
			return "", err
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// readMasterKey reads a key file that only its owner may read. A file that is not there is an error that is
// fs.ErrNotExist; every other trouble is the code of a key that cannot be used.
func readMasterKey(path string) (*secrets.MasterKey, error) {
	raw, err := readPrivateFile(path, maxMasterKeyFile)
	if err != nil {
		if isNotExist(err) {
			return nil, err
		}
		return nil, kernel.Wrap(secrets.CodeMasterKeyInvalid, err, "the master key file cannot be used")
	}
	defer zero(raw)
	return secrets.ParseMasterKey(raw)
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// createMasterKey makes a new key and puts it at path with the mode 0600, and never over a file that is there. The
// key is written whole to a temporary file of the same folder first and then linked to its name, which fails when the
// name exists, so a key file is never seen half written and a key that is there is never replaced: when another
// process made it first, its key is the one used.
func createMasterKey(path string, src kernel.ByteSource) (key *secrets.MasterKey, made bool, err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, kernel.Wrap(kernel.CodeInternal, err, "cannot make the folder of the master key file")
	}
	key = secrets.GenerateMasterKey(src)
	tmp, err := writeTempKey(dir, key.Encode())
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			key, err = readMasterKey(path)
			return key, false, err
		}
		return nil, false, kernel.Wrap(kernel.CodeInternal, err, "cannot put the new master key file in place")
	}
	syncFolder(dir)
	return key, true, nil
}

func writeTempKey(dir string, encoded []byte) (string, error) {
	defer zero(encoded)
	f, err := os.CreateTemp(dir, ".new-key-*")
	if err != nil {
		return "", kernel.Wrap(kernel.CodeInternal, err, "cannot make the master key file")
	}
	_, err = f.Write(encoded)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", kernel.Wrap(kernel.CodeInternal, err, "cannot write the master key file")
	}
	return f.Name(), nil
}

// syncFolder makes the new name durable. A system that cannot sync a folder is not an error: the file is synced.
func syncFolder(dir string) {
	if f, err := os.Open(dir); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
}
