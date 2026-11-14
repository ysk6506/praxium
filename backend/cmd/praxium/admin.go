package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/identity"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The two commands that run at the server machine and need no running server and no token (ADR-0044): they open the
// store of the users and tokens themselves. All other `praxium admin` commands ask a running server (internal/cli).

const bootstrapUsage = "usage: praxium admin bootstrap -name NAME -store file|postgres (-store-data-dir DIR | -store-dsn-file FILE) [-display-name X] [-expires-in-days N]"

const bootstrapBody = `Makes the first administrator of a server and prints its access token. There is no default account and no
default password: this command, run by the person who runs the machine, is the way in.

The token is printed once, alone on its line on standard output, and is not stored anywhere in plain text: the store
keeps a hash. Keep it where only you can read it. The command refuses when an enabled administrator already exists, so
it cannot be used to take over a running server; to get back in with a lost token use "praxium admin issue-token".

With -store file the command takes the lock of the data folder, so the server must not be running; give it the same
folder as "praxium api serve". With -store postgres it uses the same database as the server.

example:
  praxium admin bootstrap -name root -store file -store-data-dir /var/lib/praxium
`

const issueTokenUsage = "usage: praxium admin issue-token -owner NAME -store file|postgres (-store-data-dir DIR | -store-dsn-file FILE) [-role R]... [-description X] [-expires-in-days N]"

const issueTokenBody = `Prints a new access token for an existing user or service account, at the server machine. Use it to get back in when
the token of every administrator is lost or expired, or to give a service account its first token before a server runs.

The token is printed once, alone on its line on standard output. It expires in 90 days unless -expires-in-days (1 to
365) says otherwise, and carries the roles given with -role (they must be roles of its owner), or all of them. The
issue is written to the audit log as made by the local operator.

With -store file the server must not be running (the command takes the lock of the data folder).

example:
  praxium admin issue-token -owner root -store file -store-data-dir /var/lib/praxium
`

// adminOptions are the flags of the two commands.
type adminOptions struct {
	serveOptions
	name, owner, display, description string
	days                              int
	roles                             multiString
}

type multiString []string

func (m *multiString) String() string     { return strings.Join(*m, ",") }
func (m *multiString) Set(v string) error { *m = append(*m, v); return nil }

func newAdminFlags(command string, o *adminOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("admin "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.store, "store", "", "file or postgres: where the users and tokens are kept")
	fs.StringVar(&o.storeDataDir, "store-data-dir", "", "-store file: the data folder (the one praxium api serve uses)")
	fs.StringVar(&o.storeDSNFile, "store-dsn-file", "", "-store postgres: the file that holds the connection string")
	fs.IntVar(&o.days, "expires-in-days", 0, "the lifetime of the token in days, 1 to 365 (default 90)")
	if command == "bootstrap" {
		fs.StringVar(&o.name, "name", "", "the name of the administrator: lower-case letters, digits and . _ @ - (required)")
		fs.StringVar(&o.display, "display-name", "", "the name to show")
		return fs
	}
	fs.StringVar(&o.owner, "owner", "", "the user or service account the token is for (required)")
	fs.Var(&o.roles, "role", "a role of the token; may repeat (default: all the roles of the owner)")
	fs.StringVar(&o.description, "description", "", "what the token is for")
	return fs
}

func adminHelpEntries(out map[string]helpEntry) {
	out["admin bootstrap"] = helpEntry{usage: bootstrapUsage, body: bootstrapBody, flags: func() *flag.FlagSet { return newAdminFlags("bootstrap", &adminOptions{}) }}
	out["admin issue-token"] = helpEntry{usage: issueTokenUsage, body: issueTokenBody, flags: func() *flag.FlagSet { return newAdminFlags("issue-token", &adminOptions{}) }}
}

// isOfflineAdmin says whether a command line is one of the two commands of this file.
func isOfflineAdmin(args []string) bool {
	return len(args) > 1 && args[0] == "admin" && (args[1] == "bootstrap" || args[1] == "issue-token")
}

// runAdminOffline runs `praxium admin bootstrap` and `praxium admin issue-token`.
func runAdminOffline(args []string, stdout, stderr io.Writer, w wiring) int {
	command := args[1]
	path := "admin " + command
	for _, a := range args[2:] {
		if helpFlagWord(a) {
			text, _ := helpTextFor([]string{"admin", command})
			fmt.Fprint(stdout, text)
			return exitOK
		}
	}
	var o adminOptions
	fs := newAdminFlags(command, &o)
	if err := fs.Parse(args[2:]); err != nil {
		return usageFail(stderr, kernel.Wrap(kernel.CodeInvalidValue, flagProblem(err), "bad flags for "+path), path)
	}
	if fs.NArg() != 0 {
		return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "%s takes no arguments", path), path)
	}
	if o.days < 0 || o.days > 365 {
		return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "-expires-in-days is from 1 to 365"), path)
	}
	required, missing := o.name, "-name"
	if command == "issue-token" {
		required, missing = o.owner, "-owner"
	}
	if required == "" {
		return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "%s is required", missing), path)
	}
	err := withIdentity(context.Background(), o.serveOptions, w, func(st identity.Store) error {
		svc, err := identity.New(identity.Config{Store: st, Clock: w.clock, Random: randomOf(w), Log: logger(stderr)})
		if err != nil {
			return err
		}
		if command == "bootstrap" {
			return doBootstrap(context.Background(), svc, o, stdout, stderr)
		}
		return doIssueToken(context.Background(), svc, o, stdout, stderr)
	})
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		return exitFailure
	}
	return exitOK
}

func doBootstrap(ctx context.Context, svc *identity.Service, o adminOptions, stdout, stderr io.Writer) error {
	p, issued, err := svc.Bootstrap(ctx, o.name, o.display, time.Duration(o.days)*24*time.Hour)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Made the administrator %q. Its first access token follows on standard output; it is shown only now, "+
		"and the store keeps nothing but a hash of it. It expires on %s.\n", p.ID, issued.Info.ExpiresAt.UTC().Format("2006-01-02"))
	fmt.Fprintln(stdout, issued.Secret)
	return nil
}

func doIssueToken(ctx context.Context, svc *identity.Service, o adminOptions, stdout, stderr io.Writer) error {
	var roles []access.Role
	for _, name := range o.roles {
		role, ok := access.ParseRole(strings.TrimSpace(name))
		if !ok {
			return kernel.Errorf(kernel.CodeInvalidValue, "unknown role %q", name)
		}
		roles = append(roles, role)
	}
	issued, err := svc.IssueToken(ctx, identity.OfflineActor, access.TokenRequest{
		Owner: o.owner, Roles: roles, Description: o.description, Lifetime: time.Duration(o.days) * 24 * time.Hour,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Made a token for %q (id %s). It follows on standard output and is shown only now. It expires on %s.\n",
		o.owner, issued.Info.ID, issued.Info.ExpiresAt.UTC().Format("2006-01-02"))
	fmt.Fprintln(stdout, issued.Secret)
	return nil
}
