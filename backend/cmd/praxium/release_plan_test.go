package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scriptsDir is the folder of the release scripts of this work tree.
func scriptsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts")
}

// gitIn runs git in dir with a fixed identity and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(releaseEnv(dir), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// releaseEnv is the whole environment of a script run by a test: the tools of the system, a home that is not the
// person's, and what the test adds. Nothing is read from the environment of the test.
func releaseEnv(home string, more ...string) []string {
	return append([]string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home, "LC_ALL=C"}, more...)
}

// planRepo is a throwaway repository: two commits on main (the second is its tip, and the tag v0.1.0 is on the
// first), and one commit on a side branch that main does not have.
type planRepo struct {
	dir                  string
	first, tip, sideOnly string
}

func newPlanRepo(t *testing.T) planRepo {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	r := planRepo{dir: dir, first: gitIn(t, dir, "rev-parse", "HEAD")}
	gitIn(t, dir, "tag", "v0.1.0")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "second")
	r.tip = gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", r.tip)
	gitIn(t, dir, "switch", "-q", "-c", "side")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "side")
	r.sideOnly = gitIn(t, dir, "rev-parse", "HEAD")
	return r
}

// runReleasePlan runs release-plan.sh in the repository with the given variables and returns the answers it printed.
func runReleasePlan(t *testing.T, r planRepo, vars ...string) (map[string]string, string, error) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(scriptsDir(t), "release-plan.sh"))
	cmd.Dir = r.dir
	cmd.Env = append(releaseEnv(r.dir, "GITHUB_OUTPUT="), vars...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	answers := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			answers[k] = v
		}
	}
	return answers, stderr.String(), err
}

// [REQ-283]
func TestReleasePlanDecidesTheModeTheTagAndTheCommit(t *testing.T) {
	r := newPlanRepo(t)
	cases := []struct {
		name                          string
		vars                          []string
		mode, tag, sha, publish, from string
	}{
		{"a tag push is taken as it is", []string{"EVENT_NAME=push", "REF_NAME=v0.1.0-alpha.1", "RUN_SHA=" + r.sideOnly},
			"tag-push", "v0.1.0-alpha.1", r.sideOnly, "true", ""},
		{"no version builds a snapshot of the tip of the branch", []string{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.sideOnly},
			"snapshot", "", r.sideOnly, "false", ""},
		{"a snapshot may name a commit", []string{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.tip, "INPUT_COMMIT=" + r.sideOnly},
			"snapshot", "", r.sideOnly, "false", ""},
		{"a version without v gets the v", []string{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.tip, "INPUT_VERSION=0.2.0", "INPUT_COMMIT=" + r.first},
			"manual", "v0.2.0", r.first, "true", ""},
		{"a version with v is the same", []string{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.tip, "INPUT_VERSION=v0.2.0-alpha.1", "INPUT_COMMIT=" + r.tip},
			"manual", "v0.2.0-alpha.1", r.tip, "true", ""},
		{"no commit is the tip of the branch that was run", []string{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.tip, "INPUT_VERSION=0.2.0"},
			"manual", "v0.2.0", r.tip, "true", ""},
		{"an upper-case commit is the same commit", []string{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.tip, "INPUT_VERSION=0.2.0", "INPUT_COMMIT=" + strings.ToUpper(r.first)},
			"manual", "v0.2.0", r.first, "true", ""},
	}
	for _, c := range cases {
		got, stderr, err := runReleasePlan(t, r, c.vars...)
		if err != nil {
			t.Errorf("%s: %v\n%s", c.name, err, stderr)
			continue
		}
		if got["mode"] != c.mode || got["tag"] != c.tag || got["sha"] != c.sha || got["publish"] != c.publish {
			t.Errorf("%s: got %v, want mode=%s tag=%s sha=%s publish=%s", c.name, got, c.mode, c.tag, c.sha, c.publish)
		}
	}
}

// [REQ-283]
func TestReleasePlanRefusesWhatIsNotSafeToRelease(t *testing.T) {
	r := newPlanRepo(t)
	manual := func(more ...string) []string {
		return append([]string{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.tip}, more...)
	}
	cases := []struct {
		name string
		vars []string
		want string
	}{
		{"a version that is not semantic versioning", manual("INPUT_VERSION=1.0"), "semantic versioning"},
		{"a word", manual("INPUT_VERSION=latest"), "semantic versioning"},
		{"four numbers", manual("INPUT_VERSION=v1.2.3.4"), "semantic versioning"},
		{"a dash and nothing", manual("INPUT_VERSION=1.2.3-"), "semantic versioning"},
		{"a second line in the version", manual("INPUT_VERSION=1.2.3\nmode=x"), "semantic versioning"},
		{"a command in the version", manual("INPUT_VERSION=1.2.3;id"), "semantic versioning"},
		{"a short commit", manual("INPUT_VERSION=0.2.0", "INPUT_COMMIT="+r.tip[:7]), "40-digit"},
		{"a branch name as the commit", manual("INPUT_VERSION=0.2.0", "INPUT_COMMIT=main"), "40-digit"},
		{"a commit that is not in the repository", manual("INPUT_VERSION=0.2.0", "INPUT_COMMIT="+strings.Repeat("a", 40)), "not in this repository"},
		{"a commit that is not on main", manual("INPUT_VERSION=0.2.0", "INPUT_COMMIT="+r.sideOnly), "not on the main branch"},
		{"the tip of a branch that is not main", []string{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.sideOnly, "INPUT_VERSION=0.2.0"}, "not on the main branch"},
		{"a tag that exists", manual("INPUT_VERSION=v0.1.0", "INPUT_COMMIT="+r.tip), "already exists"},
		{"a tag that exists, given without v", manual("INPUT_VERSION=0.1.0"), "already exists"},
		{"a pushed tag that is not semantic versioning", []string{"EVENT_NAME=push", "REF_NAME=v1", "RUN_SHA=" + r.tip}, "semantic versioning"},
		{"a pushed tag without v", []string{"EVENT_NAME=push", "REF_NAME=release-1", "RUN_SHA=" + r.tip}, "does not start with v"},
		{"another event", []string{"EVENT_NAME=pull_request", "RUN_SHA=" + r.tip}, "not push or workflow_dispatch"},
		{"a run commit that is not a SHA", []string{"EVENT_NAME=push", "REF_NAME=v1.0.0", "RUN_SHA=HEAD"}, "RUN_SHA"},
	}
	for _, c := range cases {
		got, stderr, err := runReleasePlan(t, r, c.vars...)
		if err == nil || !strings.Contains(stderr, c.want) || len(got) != 0 {
			t.Errorf("%s: err = %v, answers %v, stderr %q, want it to say %q", c.name, err, got, stderr, c.want)
		}
	}
}
