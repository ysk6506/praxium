package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const publishSHA = "0123456789abcdef0123456789abcdef01234567"

// A gh that only writes down what it was asked: the arguments one per line in $GH_LOG, the notes file in
// $GH_LOG.notes. `gh api` answers about the tag as $FAKE_TAG says (exists, 404, 503, or anything else for a network
// error; 404 when unset) and gives $FAKE_TAG_COMMIT as the commit of the tag.
const fakeGH = `#!/usr/bin/env bash
if [ "$1" = api ]; then
  echo "$*" >> "$GH_LOG.api"
  case "$*" in
    *git/ref/tags*)
      case "${FAKE_TAG:-404}" in
        exists) echo "HTTP/2.0 200 OK"; exit 0 ;;
        404) echo "HTTP/2.0 404 Not Found"; echo "gh: Not Found (HTTP 404)" >&2; exit 1 ;;
        503) echo "HTTP/2.0 503 Service Unavailable"; echo "gh: Service Unavailable (HTTP 503)" >&2; exit 1 ;;
        *) echo "error connecting to api.github.com" >&2; exit 1 ;;
      esac ;;
    *commits/*) echo "${FAKE_TAG_COMMIT:?}"; exit 0 ;;
  esac
  exit 1
fi
printf '%s\n' "$@" > "$GH_LOG"
while [ $# -gt 0 ]; do
  if [ "$1" = --notes-file ]; then cp "$2" "$GH_LOG.notes"; fi
  shift
done
`

// publishKit is a copy of the release script in a tree of its own, so that docs/releases/ is a folder of the test.
type publishKit struct {
	tree, files, bin, tmp, log string
}

func newPublishKit(t *testing.T) publishKit {
	t.Helper()
	k := publishKit{tree: t.TempDir(), files: t.TempDir(), bin: t.TempDir(), tmp: t.TempDir()}
	k.log = filepath.Join(k.tmp, "gh.log")
	script, err := os.ReadFile(filepath.Join(scriptsDir(t), "release-publish.sh"))
	if err != nil {
		t.Fatal(err)
	}
	writeReleaseFile(t, filepath.Join(k.tree, "scripts", "release-publish.sh"), string(script))
	writeReleaseFile(t, filepath.Join(k.bin, "gh"), fakeGH)
	for _, name := range []string{"praxium_0.2.0_linux_amd64.tar.gz", "praxium_0.2.0_windows_amd64.zip"} {
		body := "contents of " + name
		writeReleaseFile(t, filepath.Join(k.files, name), body)
	}
	k.sums(t, "")
	return k
}

func writeReleaseFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// sums writes SHA256SUMS for the files, with the digest of the first one replaced by wrong when it is not empty.
func (k publishKit) sums(t *testing.T, wrong string) {
	t.Helper()
	var lines []string
	for _, name := range []string{"praxium_0.2.0_linux_amd64.tar.gz", "praxium_0.2.0_windows_amd64.zip"} {
		data, err := os.ReadFile(filepath.Join(k.files, name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		digest := hex.EncodeToString(sum[:])
		if wrong != "" && len(lines) == 0 {
			digest = wrong
		}
		lines = append(lines, digest+"  "+name)
	}
	writeReleaseFile(t, filepath.Join(k.files, "SHA256SUMS"), strings.Join(lines, "\n")+"\n")
}

func (k publishKit) run(t *testing.T, tag, mode string, more ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(k.tree, "scripts", "release-publish.sh"), k.files)
	cmd.Env = releaseEnv(k.tmp, "TMPDIR="+k.tmp, "GH_LOG="+k.log,
		"GH_REPO=example/praxium", "FAKE_TAG_COMMIT="+publishSHA, "TAG="+tag, "SHA="+publishSHA, "MODE="+mode)
	cmd.Env[0] = "PATH=" + k.bin + ":/usr/local/bin:/usr/bin:/bin"
	cmd.Env = append(cmd.Env, more...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ghArgs is what the fake gh was asked, one argument per element; nil when it was not called.
func (k publishKit) ghArgs(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(k.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func (k publishKit) notes(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(k.log + ".notes")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// next is the argument after flag.
func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func leftovers(t *testing.T, k publishKit) []string {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(k.tmp, "praxium-publish.*"))
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// [REQ-283]
func TestReleasePublishForATagPushUsesGeneratedNotesAndChecksTheTag(t *testing.T) {
	k := newPublishKit(t)
	if out, err := k.run(t, "v0.2.0-alpha.1", "tag-push"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	args := k.ghArgs(t)
	if len(args) < 3 || args[0] != "release" || args[1] != "create" || args[2] != "v0.2.0-alpha.1" {
		t.Fatalf("gh was asked %q", args)
	}
	for _, want := range []string{"--verify-tag", "--generate-notes", "--prerelease",
		filepath.Join(k.files, "SHA256SUMS"), filepath.Join(k.files, "praxium_0.2.0_windows_amd64.zip")} {
		if !hasArg(args, want) {
			t.Errorf("gh was not given %q: %q", want, args)
		}
	}
	if hasArg(args, "--target") || argAfter(args, "--title") != "v0.2.0-alpha.1" {
		t.Errorf("a tag push must not give a target, and the title is the tag: %q", args)
	}
	notes := k.notes(t)
	sums, err := os.ReadFile(filepath.Join(k.files, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(notes, "```\n"+string(sums)+"```\n") || !strings.Contains(notes, "SHA256SUMS") {
		t.Errorf("the notes do not end with the checksums in a code block:\n%s", notes)
	}
	if left := leftovers(t, k); len(left) != 0 {
		t.Errorf("temporary files were left: %v", left)
	}
}

// [REQ-283]
func TestReleasePublishForAManualReleaseTargetsTheCommitAndReadsTheNotesFile(t *testing.T) {
	k := newPublishKit(t)
	writeReleaseFile(t, filepath.Join(k.tree, "docs", "releases", "v0.2.0.md"),
		"---\ntitle: v0.2.0\nkind: guide\n---\nFirst line of the notes.\n\n---\n\nAfter a rule.\n")
	if out, err := k.run(t, "v0.2.0", "manual"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	args := k.ghArgs(t)
	if argAfter(args, "--target") != publishSHA || hasArg(args, "--verify-tag") || hasArg(args, "--generate-notes") || hasArg(args, "--prerelease") {
		t.Errorf("a stable manual release gives the target and no other flags: %q", args)
	}
	notes := k.notes(t)
	if !strings.HasPrefix(notes, "First line of the notes.\n\n---\n\nAfter a rule.\n") || strings.Contains(notes, "kind: guide") {
		t.Errorf("the notes are not the body of the file without its front matter:\n%s", notes)
	}
	if !strings.Contains(notes, "### SHA256SUMS\n\n```\n") || !strings.Contains(notes, "praxium_0.2.0_linux_amd64.tar.gz") {
		t.Errorf("the notes lack the checksums:\n%s", notes)
	}
	if api, err := os.ReadFile(k.log + ".api"); err != nil || !strings.Contains(string(api), "repos/example/praxium/git/ref/tags/v0.2.0") {
		t.Errorf("the script did not ask whether the tag exists: %v %q", err, api)
	}
}

// [REQ-283]
func TestReleasePublishTakesANotesFileWithoutFrontMatterAsItIs(t *testing.T) {
	k := newPublishKit(t)
	writeReleaseFile(t, filepath.Join(k.tree, "docs", "releases", "v0.2.0-beta.1.md"), "Only text.\n")
	if out, err := k.run(t, "v0.2.0-beta.1", "manual"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	args := k.ghArgs(t)
	if !hasArg(args, "--prerelease") || hasArg(args, "--generate-notes") || !strings.HasPrefix(k.notes(t), "Only text.\n") {
		t.Errorf("args %q, notes %q", args, k.notes(t))
	}
}

// [REQ-283]
func TestReleasePublishPublishesNothingWhenSomethingIsWrong(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, k publishKit)
		tag   string
		mode  string
		env   []string
		want  string
	}{
		{"a checksum that does not match", func(t *testing.T, k publishKit) { k.sums(t, strings.Repeat("0", 64)) }, "v0.2.0", "manual", nil, "do not match"},
		{"a file that was changed", func(t *testing.T, k publishKit) {
			writeReleaseFile(t, filepath.Join(k.files, "praxium_0.2.0_windows_amd64.zip"), "changed")
		}, "v0.2.0", "tag-push", nil, "do not match"},
		{"a tag that exists by now", func(t *testing.T, k publishKit) {}, "v0.2.0", "manual", []string{"FAKE_TAG=exists"}, "exists now"},
		{"a tag check that failed with a server error", func(t *testing.T, k publishKit) {}, "v0.2.0", "manual", []string{"FAKE_TAG=503"}, "could not find out"},
		{"a tag check that never got an answer", func(t *testing.T, k publishKit) {}, "v0.2.0", "manual", []string{"FAKE_TAG=down"}, "could not find out"},
		{"a notes file with no text", func(t *testing.T, k publishKit) {
			writeReleaseFile(t, filepath.Join(k.tree, "docs", "releases", "v0.2.0.md"), "---\ntitle: x\n---\n")
		}, "v0.2.0", "manual", nil, "no text"},
		{"a tag that is not semantic versioning", func(t *testing.T, k publishKit) {}, "v1", "manual", nil, "TAG"},
		{"a tag with a command in it", func(t *testing.T, k publishKit) {}, "v1.2.3;id", "manual", nil, "TAG"},
		{"an unknown mode", func(t *testing.T, k publishKit) {}, "v0.2.0", "snapshot", nil, "MODE"},
	}
	for _, c := range cases {
		k := newPublishKit(t)
		c.setup(t, k)
		out, err := k.run(t, c.tag, c.mode, c.env...)
		if err == nil || !strings.Contains(out, c.want) {
			t.Errorf("%s: err = %v, output %q, want it to say %q", c.name, err, out, c.want)
		}
		if args := k.ghArgs(t); args != nil {
			t.Errorf("%s: gh release was run: %q", c.name, args)
		}
		if left := leftovers(t, k); len(left) != 0 {
			t.Errorf("%s: temporary files were left: %v", c.name, left)
		}
	}
}

// [REQ-283]
func TestReleasePublishChecksTheCommitOfTheNewTag(t *testing.T) {
	k := newPublishKit(t)
	if out, err := k.run(t, "v0.2.0", "manual"); err != nil {
		t.Fatalf("a tag on the built commit: %v\n%s", err, out)
	}
	api, err := os.ReadFile(k.log + ".api")
	if err != nil || !strings.Contains(string(api), "repos/example/praxium/commits/v0.2.0") {
		t.Errorf("the script did not read the commit of the tag: %v %q", err, api)
	}
	other := strings.Repeat("f", 40)
	k = newPublishKit(t)
	out, err := k.run(t, "v0.2.0", "manual", "FAKE_TAG_COMMIT="+other)
	if err == nil || !strings.Contains(out, "is on "+other) || !strings.Contains(out, "gh release delete v0.2.0 --cleanup-tag") {
		t.Errorf("a tag on another commit: err = %v, output %q", err, out)
	}
	if args := k.ghArgs(t); args == nil {
		t.Errorf("the release should have been made before the check")
	}
	k = newPublishKit(t)
	if out, err := k.run(t, "v0.2.0-alpha.1", "tag-push", "FAKE_TAG_COMMIT="+other); err != nil {
		t.Errorf("a tag push does not check the commit of the tag: %v\n%s", err, out)
	}
}
