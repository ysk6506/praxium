package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// normalizePath makes a path absolute and clean with its links resolved, in the order the operating system would use
// them: a ".." comes after the link before it is followed, not before (the clean of filepath.Abs would take
// "link/.." away as text). The last part is left as it is when it is a link: a key file that is a link is refused later,
// and a link is never followed to make one. A part that does not exist yet is kept as written.
func normalizePath(p string) (string, error) {
	if p == "" {
		return "", kernel.Errorf(kernel.CodeInvalidValue, "the path of the key file is empty")
	}
	cur, rest, err := pathStart(p)
	if err != nil {
		return "", err
	}
	parts := strings.FieldsFunc(rest, func(r rune) bool { return r < 128 && os.IsPathSeparator(uint8(r)) })
	for i, part := range parts {
		if cur, err = stepPath(cur, part, i == len(parts)-1); err != nil {
			return "", err
		}
	}
	return cur, nil
}

// pathStart is the folder a path starts from (the root, or the real working folder) and the rest of the path.
func pathStart(p string) (start, rest string, err error) {
	if filepath.IsAbs(p) {
		vol := filepath.VolumeName(p)
		return vol + string(filepath.Separator), p[len(vol):], nil
	}
	cwd, err := os.Getwd()
	if err == nil {
		cwd, err = filepath.EvalSymlinks(cwd)
	}
	if err != nil {
		return "", "", kernel.Wrap(kernel.CodeInternal, err, "cannot find the working folder")
	}
	return cwd, p, nil
}

func stepPath(cur, part string, last bool) (string, error) {
	switch part {
	case ".":
		return cur, nil
	case "..":
		return filepath.Dir(cur), nil
	}
	next := filepath.Join(cur, part)
	if last {
		return next, nil
	}
	info, err := os.Lstat(next)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		// a part that is not there yet; the operation that follows says what is wrong with it
		return next, nil
	}
	resolved, err := filepath.EvalSymlinks(next)
	if err != nil {
		return "", kernel.Errorf(kernel.CodeInvalidValue, "a link in the path of the key file points nowhere (%s)", next)
	}
	return resolved, nil
}

// checkOutside refuses a key file in the data folder or below it. It asks the file system whether a folder above the
// key file is the data folder (os.SameFile), so a link, a bind of the same folder twice and a file system that does
// not tell the case of letters apart do not pass. (A hard link to a file elsewhere is a file of the folder, and is not
// told by this: the guide says so.) A copy or a backup of the folder would carry a key that is kept inside it.
func (k *fileKeys) checkOutside(keyPath string) error {
	data, err := os.Stat(k.loc.dataDir)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "cannot look at the data folder")
	}
	for dir := keyPath; ; {
		if st, err := os.Stat(dir); err == nil && os.SameFile(data, st) {
			return kernel.Errorf(kernel.CodeInvalidValue,
				"the master key file %s is inside the data folder %s; keep the key outside of it, so that a copy of the folder does not carry its key (name another place with -secrets-key-file <file>)", keyPath, k.loc.dataDir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// keyPath is the one clean absolute path of a key file, outside the data folder. A key file the person named
// (-secrets-key-file) may be a link, and that one link is followed (followNamedLink, ADR-0132); a key file in its
// default place is never followed, so a link there is refused when it is read.
func (k *fileKeys) keyPath(raw string) (string, error) {
	path, err := normalizePath(raw)
	if err != nil {
		return "", err
	}
	if err := k.checkOutside(path); err != nil {
		return "", err
	}
	if k.loc.keyFile == "" {
		return path, nil
	}
	return k.followNamedLink(path)
}

// followNamedLink follows a named key file that is a link, as the volume of secrets of an orchestrator gives every file:
// one link, whose target is outside the data folder and is not a link itself (the folders on its way are resolved as
// in any path). The target is then read as every key file is: a plain file that only its owner can read. A key is never
// made through a link, so a link whose target is not there is refused.
func (k *fileKeys) followNamedLink(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	// A link that is gone since it was looked at reads as "", which is the folder of the link: no key file, refused
	// when it is read.
	dest, _ := os.Readlink(path)
	if !filepath.IsAbs(dest) {
		dest = filepath.Dir(path) + string(filepath.Separator) + dest
	}
	target, err := normalizePath(dest)
	if err != nil {
		return "", err
	}
	if err := k.checkOutside(target); err != nil {
		return "", err
	}
	tinfo, err := os.Lstat(target)
	switch {
	case err != nil && isNotExist(err):
		return "", kernel.Errorf(secrets.CodeMasterKeyMissing,
			"the key file %s is a link to %s, which is not there; a key is never made through a link: put the key file there, or name the file itself with -secrets-key-file", path, target)
	case err != nil:
		return "", kernel.Wrap(secrets.CodeMasterKeyInvalid, err, "cannot look at "+target)
	case tinfo.Mode()&os.ModeSymlink != 0:
		return "", kernel.Errorf(secrets.CodeMasterKeyInvalid,
			"the key file %s is a link to another link (%s); only one link is followed: name the key file itself, or one link to it", path, target)
	}
	return target, nil
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

// createMasterKey makes a new key and puts it at path with the mode 0600, and never over a file that is there: when
// another process made it first, its key is the one used (made is false).
func createMasterKey(path string, src kernel.ByteSource) (key *secrets.MasterKey, made bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, kernel.Wrap(kernel.CodeInternal, err, "cannot make the folder of the master key file")
	}
	key = secrets.GenerateMasterKey(src)
	made, err = putFileOnce(path, ".new-key-*", key.Encode())
	switch {
	case err != nil:
		return nil, false, err
	case !made:
		key, err = readMasterKey(path)
		return key, false, err
	}
	return key, true, nil
}

// putFileOnce puts content at path, for its owner only, and never over a file that is there. The content is written
// whole to a temporary file of the same folder and synced, and then linked to its name, which fails when the name
// exists: the file is never seen half written, and one that is there is never replaced (made is false then). The
// content is overwritten before this returns.
func putFileOnce(path, pattern string, content []byte) (made bool, err error) {
	defer zero(content)
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot make "+path)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	_, err = f.Write(content)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot write "+path)
	}
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, kernel.Wrap(kernel.CodeInternal, err, "cannot put "+path+" in place")
	}
	disk := fileDisk{dir: dir}
	if err := disk.SyncDir(); err != nil {
		return false, kernel.Wrap(kernel.CodeInternal, err, path+" is in place but its folder could not be synced; run the command again")
	}
	return true, nil
}
