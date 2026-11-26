package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// `praxium secret keygen` (ADR-0132): makes the master key file alone, before any secret is put in, so that the key can
// be put where the server will read it (a secret of an orchestrator, say) and named with -secrets-key-file.

const secretKeygenUsage = "usage: praxium secret keygen -secrets-key-file <file> [-data-dir <folder>] [--json]"

const secretKeygenBody = `Makes a new master key file, and nothing else: the file the secrets are sealed with (see "praxium secret --help").
It is for a key that has to be there before the secrets: make it, put a copy where the server reads it (for one, a secret
of your orchestrator), and give that place to the commands with -secrets-key-file. The commands that put secrets in use
the key that is there and never make another.

The file is made for its owner only (mode 0600), and never over a file or a link that is there. With -data-dir, a place
inside that data folder is refused, as every command refuses it: a copy of the folder would carry its key. The key is
made from the randomness of the system. Keep a copy of it somewhere safe: secrets cannot be opened without it.
The output names the file and the key id (the id is no secret; praxium secret show gives the id that sealed a secret).
Exit codes: 0 made, 1 failed (the file is there, the place cannot be used), 2 the command line is wrong.

example:
  praxium secret keygen -secrets-key-file /secure/praxium/master.key
`

// keygenFlags is what the command line of keygen says.
type keygenFlags struct {
	keyFile, dataDir string
	asJSON           bool
}

func newKeygenFlags(f *keygenFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("secret keygen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&f.keyFile, "secrets-key-file", "", "the master key file to make; it must not be there")
	fs.StringVar(&f.dataDir, "data-dir", "", "a data folder the key file must be outside of (optional)")
	fs.BoolVar(&f.asJSON, "json", false, "print JSON instead of text")
	return fs
}

func keygenHelpEntry(out map[string]helpEntry) {
	out["secret keygen"] = helpEntry{usage: secretKeygenUsage, body: secretKeygenBody, flags: func() *flag.FlagSet { return newKeygenFlags(&keygenFlags{}) }}
}

func parseKeygen(args []string) (keygenFlags, error) {
	var f keygenFlags
	fs := newKeygenFlags(&f)
	if err := fs.Parse(args); err != nil {
		return f, secretFlagProblem(err, "keygen")
	}
	if fs.NArg() > 0 {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "unexpected argument: secret keygen takes only flags")
	}
	if strings.TrimSpace(f.keyFile) == "" {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "secret keygen needs -secrets-key-file <file>: the master key file to make")
	}
	return f, nil
}

// runSecretKeygen is `praxium secret keygen`.
func runSecretKeygen(args []string, stdout, stderr io.Writer) int {
	f, err := parseKeygen(args)
	if err != nil {
		return usageFail(stderr, err, "secret keygen")
	}
	if err := secretKeygen(f, stdout); err != nil {
		printSecretFailure(stderr, err)
		return exitFailure
	}
	return exitOK
}

// secretKeygen makes the key file and says where it is and the id of its key; never the key.
func secretKeygen(f keygenFlags, out io.Writer) error {
	path, key, err := makeKeyFile(f)
	if err != nil {
		return err
	}
	if f.asJSON {
		return printJSON(out, map[string]any{"key_file": path, "key_id": key.ID(), "made": true})
	}
	fmt.Fprintf(out, "made the master key file %s (key id %s)\nKeep a copy of it somewhere safe: secrets cannot be opened without it. "+
		"Give it to the secret commands and to the server with -secrets-key-file.\n", path, key.ID())
	return nil
}

// makeKeyFile makes the key at the one clean path of the file, outside the data folder when one is given, and never
// over a file or a link that is there, also one that another process makes at the same moment (putFileOnce links the
// new file to its name, which fails when the name is taken).
func makeKeyFile(f keygenFlags) (string, *secrets.MasterKey, error) {
	path, err := normalizePath(f.keyFile)
	if err != nil {
		return "", nil, err
	}
	if f.dataDir != "" {
		if err := requireFolder(f.dataDir); err != nil {
			return "", nil, err
		}
		if err := (&fileKeys{loc: secretLocation{dataDir: f.dataDir}}).checkOutside(path); err != nil {
			return "", nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", nil, kernel.Wrap(kernel.CodeInternal, err, "cannot make the folder of the master key file")
	}
	key := secrets.GenerateMasterKey(productBytes())
	if made, err := putFileOnce(path, ".new-key-*", key.Encode()); !made {
		if err == nil {
			err = keyFileThere(path)
		}
		return "", nil, err
	}
	return path, key, nil
}

func keyFileThere(path string) error {
	return kernel.Errorf(kernel.CodeConflict, "%s is there already; keygen never replaces a key file (the secrets sealed by it would be lost): name another file, or use the key that is there", path)
}
