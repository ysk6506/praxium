package main

import (
	"bytes"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/webui"
)

// setBuildInfo changes what the program says about its build and puts it back when the test ends.
func setBuildInfo(t *testing.T, v, c, d string) {
	t.Helper()
	oldVersion, oldCommit, oldDate := version, commit, buildDate
	t.Cleanup(func() { version, commit, buildDate = oldVersion, oldCommit, oldDate })
	version, commit, buildDate = v, c, d
}

func versionOutput(t *testing.T) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr, fixedDeps()); code != exitOK || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr %q", code, stderr.String())
	}
	return stdout.String()
}

// [REQ-282]
func TestVersionCommandPrintsWhatTheBuildKnows(t *testing.T) {
	setBuildInfo(t, "0.1.0-alpha.1", "0123456789abcdef0123456789abcdef01234567", "2030-01-02T03:04:05Z")
	ui := "not included (placeholder only)"
	if webui.Built(webui.Files()) {
		ui = "included"
	}
	want := "praxium 0.1.0-alpha.1\n" +
		"commit:  0123456789abcdef0123456789abcdef01234567\n" +
		"built:   2030-01-02T03:04:05Z\n" +
		"go:      " + runtime.Version() + "\n" +
		"web ui:  " + ui + "\n"
	if got := versionOutput(t); got != want {
		t.Fatalf("version output:\n%s\nwant:\n%s", got, want)
	}
}

// [REQ-282]
func TestVersionOfABuildWithoutValuesIsDev(t *testing.T) {
	// The values a plain `go build` leaves are the ones compiled in; the test binary is such a build.
	if version != "dev" || commit != "unknown" || buildDate != "unknown" {
		t.Fatalf("defaults = %q %q %q", version, commit, buildDate)
	}
	got := versionOutput(t)
	for _, want := range []string{"praxium dev\n", "commit:  unknown\n", "built:   unknown\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
}

// [REQ-282]
func TestVersionWithArgumentsIsAUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version", "extra"}, &stdout, &stderr, fixedDeps()); code != exitUsage || stdout.Len() != 0 {
		t.Fatalf("exit code = %d, stdout %q", code, stdout.String())
	}
}

// gitOutput runs git in the repository (the folder three levels above this package) and returns its trimmed output.
func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// releaseCompile runs the release script's compile-only mode for the target of this machine and returns the program.
func releaseCompile(t *testing.T, version string) string {
	t.Helper()
	target := runtime.GOOS + "/" + runtime.GOARCH
	out := t.TempDir()
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "release-build.sh"),
		"--compile-only", "--version", version, "--targets", target, "--out", out)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("release-build.sh: %v\n%s", err, output)
	}
	name := "praxium"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(out, runtime.GOOS+"_"+runtime.GOARCH, name)
}

// [REQ-283]
func TestReleaseBuildSetsTheVersionAndIsReproducible(t *testing.T) {
	first := releaseCompile(t, "v9.9.9-test.1")
	second := releaseCompile(t, "v9.9.9-test.1")
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(a) != sha256.Sum256(b) {
		t.Fatal("two builds of one commit differ")
	}

	out, err := exec.Command(first, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	commitID := gitOutput(t, "rev-parse", "HEAD")
	seconds, err := strconv.ParseInt(gitOutput(t, "log", "-1", "--format=%ct"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05Z")
	for _, want := range []string{"praxium 9.9.9-test.1\n", "commit:  " + commitID + "\n", "built:   " + date + "\n", "go:      " + runtime.Version() + "\n"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// [REQ-283]
func TestReleaseBuildRefusesWhatIsNotSemanticVersioning(t *testing.T) {
	for _, bad := range []string{"1.0", "latest", "v1.2.3.4", "1.2.3-"} {
		cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "release-build.sh"), "--compile-only", "--version", bad)
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "semantic versioning") {
			t.Errorf("version %q: err = %v, output %q", bad, err, output)
		}
	}
}
