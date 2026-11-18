package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// `praxium secret ...` (ADR-0076): the commands that put secrets (the passwords and tokens that sources need) in the
// data folder and look at them, without a server. A value is never an argument of a command: it comes from a file
// (-field <name>=<file>) or from the standard input, so it is in no list of processes and no shell history.

const secretUsage = "usage: praxium secret put|rotate|list|show|retire|delete|set-scope|set-hosts ..."

const secretBody = `Secrets for the sources: the passwords and tokens that Praxium needs to read your databases and APIs. They are kept in the
data folder (secrets.json) encrypted, and the key that opens them is a file of its own outside the data folder.
  put         put a new secret in (the first version)
  rotate      add a new version to a secret that is there (the newest version is the one that is used)
  list        list the secrets (names, kinds, versions, never a value)
  show        one secret: its fields' names, versions, scope, allowed addresses (never a value)
  retire      retire one version of a secret: its encrypted value is deleted
  delete      delete a secret with all its versions
  set-scope   replace the sources that may use a secret
  set-hosts   replace the addresses (host:port) a secret may be used for

A value is written and never read back: no command shows it. It is never an argument either, so it does not end up in
the history of your shell: give it as a file (-field <name>=<file>) or pipe it in, one line per field in the order of the
kind (password: username, password; token: token). A terminal is not read, since the value would show on the screen.
A secret may be used by nobody until you say which sources (-scope) and which addresses (-host) it is for.

The commands that change secrets take the lock of the data folder: stop a server that has it open first. list and show
take no lock. Exit codes: 0 done, 1 failed, 2 the command line is wrong. The guide is docs/guides/operate-the-server.md.

The master key: a file that is made the first time you put a secret in. Without -secrets-key-file it is
<your configuration folder>/praxium/keys/<identifier of the data folder>.key (the identifier is in the file FOLDER_ID of the
data folder, so a data folder that is moved finds its key). It is never inside the data folder. Keep a copy of it
somewhere safe: secrets cannot be opened without it. A secret whose key is lost can be deleted and put in again.
`

const (
	secretPutUsage    = "usage: praxium secret put <id> -kind password|token -data-dir <folder> [-secrets-key-file <file>] [-field <name>=<file> ...] [-scope <source-id>,...] [-host <host:port>,...] [--json]"
	secretRotateUsage = "usage: praxium secret rotate <id> -data-dir <folder> [-secrets-key-file <file>] [-field <name>=<file> ...] [--json]"
	secretListUsage   = "usage: praxium secret list -data-dir <folder> [--json]"
	secretShowUsage   = "usage: praxium secret show <id> -data-dir <folder> [--json]"
	secretRetireUsage = "usage: praxium secret retire <id> -version <N> -data-dir <folder> [-secrets-key-file <file>] [--json]"
	secretDelUsage    = "usage: praxium secret delete <id> -data-dir <folder> [--json]"
	secretScopeUsage  = "usage: praxium secret set-scope <id> -scope <source-id>,... -data-dir <folder> [-secrets-key-file <file>] [--json]"
	secretHostsUsage  = "usage: praxium secret set-hosts <id> -host <host:port>,... -data-dir <folder> [-secrets-key-file <file>] [--json]"
)

// secretSpec is what one command of `praxium secret` is: the flags it has, what it does with the store, its help.
type secretSpec struct {
	usage, body string
	access      secretAccess
	// the flags beyond -data-dir and --json
	kind, scope, host, field, version, key bool
}

const secretPutBody = `Puts a new secret in. The id starts with a lower-case letter and has lower-case letters, digits, _ and -, up to 64 characters.
A secret that is there is not changed by put: use "praxium secret rotate" for a new version.
The values: either one -field <name>=<file> for every field of the kind (a file with the value; one line break at its end is
not part of it), or no -field and the values on the standard input (from a file: < file), one line per field in the order of the kind.
-scope and -host say who may use it: the ids of the sources (comma separated) and the addresses as host:port, where host is
a name or an IP address ([::1]:5432 for IPv6). Without them nobody may use the secret yet; set-scope and set-hosts say it later.
The first time a secret is put in, a master key file is made (see "praxium secret --help"): keep a copy of it.

A value written in the command itself (printf ... | praxium secret put) stays in the history of your shell: keep it in a file.
A value is one line: a control character in it (a NUL, a second line break, a tab) or a byte order mark at its start is refused, and
so is a -field file that is not a plain file (a link, a device, a pipe).

example (db-credentials.txt has the two lines, the user name and the password):
  praxium secret put db_main -kind password -data-dir data -scope src_db -host db.example.test:5432 < ./db-credentials.txt
  praxium secret put api_token -kind token -data-dir data -field token=./token.txt
`

const secretRotateBody = `Adds a new version to a secret that is there; the newest version that is not retired is the one that is used. The older
versions stay until you retire them ("praxium secret retire <id> -version N"), so a source that still needs the old one keeps working.
The values are given as for put (-field <name>=<file> for every field, or the standard input, one line per field).

example:
  praxium secret rotate db_main -data-dir data -field username=./user.txt -field password=./password.txt
`

const secretRetireBody = `Retires a version: its encrypted value is deleted and only its name and times stay. It cannot be undone. When the newest
version is retired the one before it is used again; when every version is retired the secret cannot be opened.

example:
  praxium secret retire db_main -version 1 -data-dir data
`

var secretCommands = map[string]secretSpec{
	"put":       {usage: secretPutUsage, body: secretPutBody, access: secretCreate, kind: true, scope: true, host: true, field: true, key: true},
	"rotate":    {usage: secretRotateUsage, body: secretRotateBody, access: secretChange, field: true, key: true},
	"list":      {usage: secretListUsage, body: "Lists every secret: id, kind, the version that is used, how many versions, scope and addresses. Never a value. Takes no lock.\n\nexample:\n  praxium secret list -data-dir data\n", access: secretRead},
	"show":      {usage: secretShowUsage, body: "Shows one secret: kind, the names of its fields, its versions with their times, scope, allowed addresses and the id of the key that sealed it. Never a value. Takes no lock.\n\nexample:\n  praxium secret show db_main -data-dir data\n", access: secretRead},
	"retire":    {usage: secretRetireUsage, body: secretRetireBody, access: secretChange, version: true, key: true},
	"delete":    {usage: secretDelUsage, body: "Deletes a secret with all its versions. It needs no master key. Praxium does not check whether a source still uses the secret (that check comes with the sources that refer to secrets).\n\nexample:\n  praxium secret delete db_main -data-dir data\n", access: secretDelete},
	"set-scope": {usage: secretScopeUsage, body: "Replaces the sources that may use the secret (the ids of the sources, comma separated). -scope \"\" means no source.\n\nexample:\n  praxium secret set-scope db_main -scope src_db,src_other -data-dir data\n", access: secretChange, scope: true, key: true},
	"set-hosts": {usage: secretHostsUsage, body: "Replaces the addresses the secret may be used for, as host:port (comma separated or -host given again). -host \"\" means no address.\n\nexample:\n  praxium secret set-hosts db_main -host db.example.test:5432 -data-dir data\n", access: secretChange, host: true, key: true},
}

// secretFlags is what the command line of a secret command says. The lists keep what was typed; nothing is checked
// while the flags are read, so that the text of a flag is never repeated in a message of the flag package.
type secretFlags struct {
	id, dataDir, keyFile, kind string
	scope, hosts, fields       []string
	scopeGiven, hostsGiven     bool
	keyFileGiven               bool
	// addrs are the hosts as addresses, read once when the command line is read.
	addrs   []secrets.Address
	version int
	asJSON  bool
}

func newSecretFlags(spec secretSpec, name string, f *secretFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("secret "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&f.dataDir, "data-dir", "", "the data folder that holds the secrets (secrets.json)")
	if spec.key {
		fs.Func("secrets-key-file", "the master key file (default: in your configuration folder, under praxium/keys, named by the identifier of the data folder)", func(v string) error {
			f.keyFile, f.keyFileGiven = v, true
			return nil
		})
	}
	if spec.kind {
		fs.StringVar(&f.kind, "kind", "", "the kind of the secret: password (fields username, password) or token (field token)")
	}
	if spec.scope {
		fs.Func("scope", "the ids of the sources that may use the secret, comma separated", func(v string) error {
			f.scope, f.scopeGiven = append(f.scope, splitList(v)...), true
			return nil
		})
	}
	if spec.host {
		fs.Func("host", "the addresses the secret may be used for, host:port, comma separated; may be given again", func(v string) error {
			f.hosts, f.hostsGiven = append(f.hosts, splitList(v)...), true
			return nil
		})
	}
	if spec.field {
		fs.Func("field", "the file that holds the value of a field, <name>=<file>; for every field of the kind (default: one line per field on the standard input)", func(v string) error {
			f.fields = append(f.fields, v)
			return nil
		})
	}
	if spec.version {
		fs.IntVar(&f.version, "version", 0, "the number of the version")
	}
	fs.BoolVar(&f.asJSON, "json", false, "print JSON instead of text")
	return fs
}

func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// secretHelpEntries are the help entries of this file's commands.
func secretHelpEntries(out map[string]helpEntry) {
	out["secret"] = helpEntry{usage: secretUsage, body: secretBody}
	for name, spec := range secretCommands {
		out["secret "+name] = helpEntry{usage: spec.usage, body: spec.body, flags: func() *flag.FlagSet { return newSecretFlags(spec, name, &secretFlags{}) }}
	}
}

// parseSecret reads the command line of one command: the id first when the command has one, then the flags.
func parseSecret(name string, args []string) (secretFlags, error) {
	spec := secretCommands[name]
	var f secretFlags
	if name != "list" {
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return f, kernel.Errorf(kernel.CodeInvalidValue, "secret %s wants the id of the secret first", name)
		}
		f.id, args = args[0], args[1:]
	}
	fs := newSecretFlags(spec, name, &f)
	if err := fs.Parse(args); err != nil {
		return f, secretFlagProblem(err, name)
	}
	if fs.NArg() > 0 {
		// Not repeated: a value that was typed as an argument must not be echoed. Values are never arguments.
		return f, kernel.Errorf(kernel.CodeInvalidValue, "unexpected argument: a value is never given as an argument; give it as a file (-field <name>=<file>) or on the standard input")
	}
	if err := checkSecretFlags(spec, f); err != nil {
		return f, err
	}
	var err error
	if f.addrs, err = parseAddresses(f.hosts); err != nil {
		return f, err
	}
	if err := checkSourceIDs(f.scope); err != nil {
		return f, err
	}
	return f, checkFieldArgs(f.fields)
}

// emptyKeyFile is a -secrets-key-file that was given with nothing in it: not the same as leaving the flag out.
func emptyKeyFile(spec secretSpec, f secretFlags) bool {
	return spec.key && f.keyFileGiven && f.keyFile == ""
}

func checkSecretFlags(spec secretSpec, f secretFlags) error {
	switch {
	case f.dataDir == "":
		return kernel.Errorf(kernel.CodeInvalidValue, "a secret command needs -data-dir <folder>: the data folder that holds the secrets")
	case emptyKeyFile(spec, f):
		return kernel.Errorf(kernel.CodeInvalidValue, "-secrets-key-file is empty: name the key file, or leave the flag out to use the default place")
	case spec.kind && f.kind == "":
		return kernel.Errorf(kernel.CodeInvalidValue, "secret put needs -kind password or -kind token")
	case spec.version && f.version < 1:
		return kernel.Errorf(kernel.CodeInvalidValue, "secret retire needs -version <N>, a number from 1")
	}
	return checkSetFlags(spec, f)
}

// checkSetFlags is the check of the two commands that only replace a list: the list must be given, even when empty.
func checkSetFlags(spec secretSpec, f secretFlags) error {
	switch {
	case spec.scope && !spec.kind && !f.scopeGiven:
		return kernel.Errorf(kernel.CodeInvalidValue, "secret set-scope needs -scope <source-id>,... (-scope \"\" for no source)")
	case spec.host && !spec.kind && !f.hostsGiven:
		return kernel.Errorf(kernel.CodeInvalidValue, "secret set-hosts needs -host <host:port>,... (-host \"\" for no address)")
	}
	return nil
}

// secretFlagProblem is a flag error of a secret command in fixed words. The flag package repeats what was typed ("flag
// provided but not defined: -<text>", "invalid value <text> for flag -version"), and what was typed may be a value that
// was given by mistake, so none of it is passed on.
func secretFlagProblem(err error, name string) error {
	text := err.Error()
	switch {
	case strings.HasPrefix(text, "flag provided but not defined"):
		return kernel.Errorf(kernel.CodeInvalidValue, "unknown flag: a value is never given as an argument or a flag; give it as a file (-field <name>=<file>) or on the standard input (praxium secret %s --help)", name)
	case strings.HasPrefix(text, "flag needs an argument"):
		return kernel.Errorf(kernel.CodeInvalidValue, "a flag is missing its value (praxium secret %s --help)", name)
	}
	return kernel.Errorf(kernel.CodeInvalidValue, "a flag has a value that cannot be read: -version wants a whole number from 1, and --json takes no value (praxium secret %s --help)", name)
}

// checkSourceIDs makes sure each id of a scope has the grammar of a source id (ADR-0075). Whether the source is there is
// not looked at: that comes with the stores of definitions.
func checkSourceIDs(ids []string) error {
	for _, id := range ids {
		if !ingest.ValidID(id) {
			return kernel.Errorf(kernel.CodeInvalidValue, "a -scope is not the id of a source: use lower-case letters, digits, _ and -, a letter first, up to 64 characters")
		}
	}
	return nil
}

// parseAddresses reads host:port texts. The text of a bad one is not repeated (it may be a URL with a password).
func parseAddresses(list []string) ([]secrets.Address, error) {
	out := make([]secrets.Address, 0, len(list))
	for _, text := range list {
		host, port, err := net.SplitHostPort(text)
		n, perr := strconv.Atoi(port)
		if err != nil || perr != nil || host == "" {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "a -host is not host:port: write the host name or IP address, a colon and the port, as db.example.test:5432 or [::1]:5432")
		}
		out = append(out, secrets.Address{Host: host, Port: n})
	}
	return out, nil
}

// runSecret is `praxium secret ...`.
func runSecret(args []string, stdout, stderr io.Writer, w wiring) int {
	if len(args) == 0 {
		return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "secret needs put, rotate, list, show, retire, delete, set-scope or set-hosts"), "secret")
	}
	name := args[0]
	if _, ok := secretCommands[name]; !ok {
		return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "secret has no command %q", name), "secret")
	}
	f, err := parseSecret(name, args[1:])
	if err != nil {
		return usageFail(stderr, err, "secret "+name)
	}
	if err := runSecretCommand(name, f, stdout, w); err != nil {
		printSecretFailure(stderr, err)
		return exitFailure
	}
	return exitOK
}

// printSecretFailure says what failed, with the code of the error and what to do, as the messages say. A value or a
// key never is in one: the messages are made of names and of what to do.
func printSecretFailure(stderr io.Writer, err error) {
	code := kernel.CodeOf(err)
	if code == "" || code == kernel.CodeInternal {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		return
	}
	fmt.Fprintf(stderr, "praxium: %s (%s)\n", errText(err), code)
}
