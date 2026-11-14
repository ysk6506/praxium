package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// docsFixture writes the documents of a small repository into the plan repository's work tree: an index, a spec, and
// a guide that follows the spec with recheck: release. Each document is verified once, so the check starts clean.
func docsFixture(t *testing.T, r planRepo) (node string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("the docs check needs node: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(repoRoot(t), "harness", "config", "docs.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	front := "---\ntitle: T\nkind: guide\nstatus: active\nfollows:\n  - package.json\n---\n"
	guideFront := "---\ntitle: T\nkind: guide\nstatus: active\nfollows:\n  - doc: docs/spec.md\n    recheck: release\n---\n"
	files := map[string]string{
		"package.json":             "{}\n",
		"harness/config/docs.yaml": string(config),
		"docs/index.md":            front + "# Index\n[guide](guide.md) [spec](spec.md)\n",
		"docs/spec.md":             front + "# Spec\nfirst\n",
		"docs/guide.md":            guideFront + "# Guide\n",
	}
	for name, text := range files {
		path := filepath.Join(r.dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, doc := range []string{"index", "spec", "guide"} {
		reverifyDoc(t, r, node, "docs/"+doc+".md", "read "+doc+" for the fixture")
	}
	return node
}

// reverifyDoc runs the docs check's reverify for one document of the plan repository.
func reverifyDoc(t *testing.T, r planRepo, node, doc, note string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(repoRoot(t), "node_modules", ".bin", "tsx"),
		filepath.Join(repoRoot(t), "harness", "src", "checks", "docs.ts"),
		"reverify", filepath.Join(r.dir, doc), "--root", r.dir, "--note", note)
	cmd.Dir = r.dir
	cmd.Env = releaseEnv(r.dir, "PATH="+filepath.Dir(node)+":/usr/bin:/bin")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("reverify %s: %v\n%s", doc, err, out)
	}
}

// docsVars are the variables that let release-plan.sh find node and the harness of this work tree.
func docsVars(t *testing.T, node string) []string {
	t.Helper()
	return []string{"PATH=" + filepath.Dir(node) + ":/usr/bin:/bin", "DOCS_CHECK_DIR=" + repoRoot(t)}
}

// [REQ-396]
func TestReleasePlanRefusesAReleaseWhenAReleaseRelationIsStale(t *testing.T) {
	r := newPlanRepo(t)
	node := docsFixture(t, r)
	spec := filepath.Join(r.dir, "docs", "spec.md")
	if err := os.WriteFile(spec, []byte("---\ntitle: T\nkind: guide\nstatus: active\nfollows:\n  - package.json\n---\n# Spec\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	releases := map[string][]string{
		"a manual release": {"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.tip, "RUN_REF=refs/heads/main", "INPUT_VERSION=0.2.0"},
		"a tag push":       {"EVENT_NAME=push", "REF_NAME=v0.2.0", "RUN_SHA=" + r.tip},
	}
	for name, vars := range releases {
		got, stderr, err := runReleasePlan(t, r, append(vars, docsVars(t, node)...)...)
		if err == nil || len(got) != 0 {
			t.Errorf("%s: err = %v, answers %v, want a refusal with no answers", name, err, got)
		}
		for _, want := range []string{"DOC013", "docs/guide.md", "docs/spec.md", "reverify docs/guide.md --target docs/spec.md", "not ready for a release"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("%s: stderr lacks %q:\n%s", name, want, stderr)
			}
		}
	}
	got, stderr, err := runReleasePlan(t, r, append([]string{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.sideOnly}, docsVars(t, node)...)...)
	if err != nil || got["mode"] != "snapshot" || got["publish"] != "false" {
		t.Errorf("a snapshot is not asked: err = %v, answers %v\n%s", err, got, stderr)
	}
	reverifyDoc(t, r, node, "docs/guide.md", "compared the guide with the second spec")
	for name, vars := range releases {
		got, stderr, err := runReleasePlan(t, r, append(vars, docsVars(t, node)...)...)
		if err != nil || got["publish"] != "true" {
			t.Errorf("%s after the guide was re-read: err = %v, answers %v\n%s", name, err, got, stderr)
		}
	}
}

// [REQ-396]
func TestReleasePlanSaysSoWhenTheCheckoutHasNoDocumentContract(t *testing.T) {
	r := newPlanRepo(t)
	vars := [][]string{
		{"EVENT_NAME=workflow_dispatch", "RUN_SHA=" + r.tip, "RUN_REF=refs/heads/main", "INPUT_VERSION=0.2.0"},
		{"EVENT_NAME=push", "REF_NAME=v0.2.0", "RUN_SHA=" + r.tip},
	}
	for _, v := range vars {
		got, stderr, err := runReleasePlan(t, r, v...)
		if err != nil || got["publish"] != "true" {
			t.Errorf("a repository with no document contract still releases: err = %v, answers %v\n%s", err, got, stderr)
		}
		if !strings.Contains(stderr, "skipped the docs check") || !strings.Contains(stderr, "harness/config/docs.yaml") {
			t.Errorf("the skip is not said: %q", stderr)
		}
	}
	got, stderr, err := runReleasePlan(t, r, "EVENT_NAME=workflow_dispatch", "RUN_SHA="+r.sideOnly)
	if err != nil || got["mode"] != "snapshot" || strings.Contains(stderr, "skipped the docs check") {
		t.Errorf("a snapshot is not asked and says nothing: err = %v, answers %v, %q", err, got, stderr)
	}
}
