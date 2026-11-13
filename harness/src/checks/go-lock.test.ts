import { readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { findRoot } from "../common";
import { goRepo } from "../test-support/go-repo";
import { commitAll } from "../test-support/mini-repo";
import { sealAdr } from "./docs";
import { lock, main, run } from "./testguard";

const REAL = findRoot();
const read = (file: string) => readFileSync(path.join(REAL, file), "utf8");
const PKG = "backend/internal/model";
const FILE = `${PKG}/counts_test.go`;
const GOLDEN = `${PKG}/testdata/golden.json`;
const KEY = `go:${PKG}::TestCounts`;
const LOCK = "harness/config/test-lock.json";

const COUNTS = `package model

import "testing"

// [REQ-023]
func TestCounts(t *testing.T) {
	if count() != goldenCount {
		t.Fatalf("count = %d", count())
	}
	if n := count() * 2; n != 6 {
		t.Error("double")
	}
}

const goldenCount = 3

func count() int { return 3 }

func TestOther(t *testing.T) {
	if got := load("testdata/golden.json"); got != "{}" {
		t.Fatal(got)
	}
}

func load(string) string { return "{}" }
`;

function lockedRepo(files: Record<string, string> = {}): string {
  const root = goRepo({
    "harness/config/tests.yaml": read("harness/config/tests.yaml"),
    "vitest.config.ts": read("vitest.config.ts"),
    "package.json": JSON.stringify({ scripts: { test: "vitest run --coverage" } }),
    [FILE]: COUNTS,
    [GOLDEN]: "{}",
    "docs/design-docs/adr/.keep": "",
    ...files,
  });
  expect(lock(root, undefined, "2026-11-13")).toEqual([]);
  return root;
}

const codes = (root: string, base?: string): string[] => [
  ...new Set(run(root, base).map((v) => v.code)),
];
const messages = (root: string): string[] => run(root).map((v) => v.message);

function edit(root: string, from: string, to: string, file = FILE): void {
  const full = path.join(root, file);
  const text = readFileSync(full, "utf8");
  expect(text).toContain(from);
  writeFileSync(full, text.replace(from, to));
}

const entries = (root: string): Record<string, Record<string, unknown>> =>
  JSON.parse(readFileSync(path.join(root, LOCK), "utf8")).entries;

describe("Go test lock: what is locked [REQ-023]", () => {
  test("every Go test and golden file gets an entry, keyed so it cannot meet a TypeScript key", () => {
    const root = lockedRepo();
    const keys = Object.keys(entries(root));
    expect(keys.filter((k) => k.startsWith("go:")).sort()).toEqual([
      "go:backend/cmd/app::TestRuns",
      "go:backend/internal/kernel::TestKernel",
      `go:${PKG}::TestCounts`,
      `go:${PKG}::TestDouble`,
      `go:${PKG}::TestOther`,
    ]);
    expect(keys).toContain(`${GOLDEN}::<data>`);
    const counts = entries(root)[KEY];
    expect(counts).toMatchObject({ kind: "test", asserts: 2, skips: 0, reqs: ["REQ-023"] });
    expect(counts?.literals).toHaveLength(1);
    expect(counts).not.toHaveProperty("precision.0");
    expect(run(root)).toEqual([]);
  });

  test("a new test is not locked until the lock command adds it, and then only it is added", () => {
    const root = lockedRepo();
    const before = entries(root);
    writeFileSync(
      path.join(root, `${PKG}/more_test.go`),
      'package model\n\nimport "testing"\n\nfunc TestMore(t *testing.T) { t.Fatal(1) }\n',
    );
    expect(codes(root)).toEqual(["TLOCK003"]);
    expect(messages(root)).toEqual([`go:${PKG}::TestMore is not in the test lock`]);
    expect(lock(root, undefined, "2026-11-14")).toEqual([]);
    expect(codes(root)).toEqual([]);
    const after = entries(root);
    for (const [key, entry] of Object.entries(before)) expect(after[key]).toEqual(entry);
    expect(Object.keys(after)).toHaveLength(Object.keys(before).length + 1);
  });
});

describe("Go test lock: what trips it [REQ-023]", () => {
  test("a golden constant that changes", () => {
    const root = lockedRepo();
    edit(root, "goldenCount = 3", "goldenCount = 4");
    const found = run(root);
    expect(found.map((v) => v.code)).toEqual(["TLOCK001"]);
    expect(found[0]?.message).toBe(`locked item ${KEY} changed (expected values changed)`);
    expect(found[0]?.path).toBe(FILE);
  });

  test("a helper or an expected value in the body that changes", () => {
    const helper = lockedRepo();
    edit(helper, "return 3", "return 4");
    expect(codes(helper)).toEqual(["TLOCK001"]);
    const literal = lockedRepo();
    edit(literal, "n != 6", "n != 7");
    expect(messages(literal)).toEqual([`locked item ${KEY} changed (expected values changed)`]);
  });

  test("a test that is deleted or renamed", () => {
    const deleted = lockedRepo();
    edit(deleted, "func TestOther(t *testing.T) {", "func notATest(t *testing.T) {");
    expect(messages(deleted).sort()).toEqual([
      `locked item go:${PKG}::TestOther was removed or renamed`,
    ]);
    const renamed = lockedRepo();
    edit(renamed, "func TestCounts(", "func TestCountsAgain(");
    expect(codes(renamed)).toEqual(["TLOCK003", "TLOCK002"]);
    const gone = lockedRepo();
    rmSync(path.join(gone, FILE));
    expect(run(gone).map((v) => v.code)).toEqual(["TLOCK002", "TLOCK002"]);
  });

  test("a weakened body: an assertion removed, a skip added, an early return", () => {
    const fewer = lockedRepo();
    edit(fewer, '\t\tt.Error("double")\n', "");
    expect(messages(fewer)).toEqual([
      `locked item ${KEY} changed (assertions 2 -> 1; expected values changed)`,
    ]);
    const skipped = lockedRepo();
    edit(
      skipped,
      "func TestCounts(t *testing.T) {\n",
      'func TestCounts(t *testing.T) {\n\tt.Skip("later, TD-0001")\n',
    );
    expect(messages(skipped)[0]).toContain("skip/todo/fails added");
    const early = lockedRepo();
    edit(early, "func TestCounts(t *testing.T) {\n", "func TestCounts(t *testing.T) {\n\treturn\n");
    expect(codes(early)).toEqual(["TLOCK001"]);
  });

  test("a golden file under testdata that changes or goes away", () => {
    const changed = lockedRepo();
    writeFileSync(path.join(changed, GOLDEN), '{"n":1}');
    expect(run(changed).map((v) => `${v.code} ${v.message}`)).toEqual([
      `TLOCK001 locked item ${GOLDEN}::<data> changed (body changed)`,
      `TLOCK001 locked item go:${PKG}::TestOther changed (body changed)`,
    ]);
    const gone = lockedRepo();
    rmSync(path.join(gone, GOLDEN));
    expect(codes(gone)).toEqual(["TLOCK001", "TLOCK002"]);
  });

  test("changing the requirement tags of a test", () => {
    const root = lockedRepo();
    edit(root, "// [REQ-023]\n", "// [REQ-023] [REQ-024]\n");
    expect(messages(root)).toEqual([`locked item ${KEY} changed (requirement tags changed)`]);
  });
});

describe("Go test lock: what does not trip it [REQ-023]", () => {
  test("formatting and comments", () => {
    const root = lockedRepo();
    edit(
      root,
      "func TestCounts(t *testing.T) {\n",
      "// A new explanation.\nfunc  TestCounts( t *testing.T )  {\n\t// and one inside\n",
    );
    edit(
      root,
      'if n := count() * 2; n != 6 {\n\t\tt.Error("double")\n\t}',
      'if n := count() * 2;   n != 6 { // trailing\n\t\tt.Error( "double" , )\n\t}',
    );
    expect(run(lockedRepo())).toEqual([]);
    expect(run(root)).toEqual([]);
  });

  test("moving a test and its helpers to another file of the package", () => {
    const root = lockedRepo();
    const text = readFileSync(path.join(root, FILE), "utf8");
    rmSync(path.join(root, FILE));
    writeFileSync(path.join(root, `${PKG}/zz_moved_test.go`), text);
    expect(run(root)).toEqual([]);
  });

  test("a change to code the tests do not reach, and production code", () => {
    const root = lockedRepo();
    edit(
      root,
      'func load(string) string { return "{}" }',
      'func load(string) string { return "{}" }\n\nconst unrelated = 1\n',
    );
    writeFileSync(
      path.join(root, `${PKG}/model.go`),
      "package model\n\nfunc Double() int { return 100 }\n",
    );
    expect(run(root)).toEqual([]);
  });
});

const REASON = "REQ-001 changed: the count is now 4, see the requirements change in this commit.";

function writeAdr(
  root: string,
  name: string,
  amends: string[],
  reason = REASON,
  seal = true,
): void {
  const data = {
    title: "amend",
    kind: "decision",
    status: "active",
    enforced_by: ["none"],
    no_enforcement_reason: "amendment",
    amends_tests: amends,
    test_change_reason: reason,
  };
  const file = `docs/design-docs/adr/${name}`;
  writeFileSync(
    path.join(root, file),
    `---\n${YAML.stringify(data)}---\n# Amend\nThe requirement changed.\n`,
  );
  if (seal) sealAdr(root, file);
}

describe("Go test lock: amending [REQ-023]", () => {
  const changed = () => {
    const root = lockedRepo();
    edit(root, "goldenCount = 3", "goldenCount = 4");
    return root;
  };

  test("a sealed ADR that lists the Go key allows exactly that change", () => {
    const root = changed();
    writeAdr(root, "0009-amend.md", [KEY]);
    expect(lock(root, "0009-amend.md")).toEqual([]);
    expect(codes(root)).toEqual([]);
    expect(entries(root)[KEY]?.amended_by).toBe("0009-amend.md");
    edit(root, 'got != "{}"', 'got != "[]"');
    expect(lock(root, "0009-amend.md")).toEqual([`go:${PKG}::TestOther`]);
  });

  test("the old file-based form of a key does not amend a Go test", () => {
    const root = changed();
    writeAdr(root, "0009-amend.md", [`${FILE}::TestCounts`, `${PKG}::TestCounts`]);
    expect(lock(root, "0009-amend.md")).toEqual([KEY]);
    expect(codes(root)).toEqual(["TLOCK001"]);
  });

  test("an amended removal is recorded and accepted", () => {
    const root = lockedRepo();
    edit(root, "func TestOther(t *testing.T) {", "func notATest(t *testing.T) {");
    writeAdr(root, "0009-amend.md", [`go:${PKG}::TestOther`]);
    expect(lock(root, "0009-amend.md")).toEqual([]);
    expect(codes(root)).toEqual([]);
  });

  test("an unsealed ADR, a weak reason or a forged name is TLOCK004", () => {
    const unsealed = changed();
    writeAdr(unsealed, "0009-amend.md", [KEY]);
    expect(lock(unsealed, "0009-amend.md")).toEqual([]);
    writeAdr(unsealed, "0009-amend.md", [KEY], REASON, false);
    expect(codes(unsealed)).toEqual(["TLOCK004"]);
    const weak = changed();
    writeAdr(weak, "0009-amend.md", [KEY]);
    expect(lock(weak, "0009-amend.md")).toEqual([]);
    writeAdr(weak, "0009-amend.md", [KEY], "because");
    expect(messages(weak)[0]).toContain("test_change_reason");
    const forged = changed();
    const file = path.join(forged, LOCK);
    const data = JSON.parse(readFileSync(file, "utf8"));
    data.entries[KEY].amended_by = "0042-fake.md";
    writeFileSync(file, JSON.stringify(data));
    expect(codes(forged)).toEqual(["TLOCK001", "TLOCK004"]);
  });

  test("a hand-edited lock is caught against the base ref (TLOCK005)", () => {
    const root = lockedRepo();
    commitAll(root);
    edit(root, "goldenCount = 3", "goldenCount = 4");
    const other = lockedRepo();
    edit(other, "goldenCount = 3", "goldenCount = 4");
    writeAdr(other, "0009-amend.md", [KEY]);
    expect(lock(other, "0009-amend.md")).toEqual([]);
    const forged = JSON.parse(readFileSync(path.join(root, LOCK), "utf8"));
    forged.entries[KEY].hash = entries(other)[KEY]?.hash;
    writeFileSync(path.join(root, LOCK), JSON.stringify(forged));
    expect(codes(root)).toEqual([]);
    expect(codes(root, "HEAD")).toEqual(["TLOCK005"]);
  });

  test("the CLI names the Go key and the way forward", () => {
    const root = changed();
    const lines: string[] = [];
    expect(main(["lock", "--root", root], (l) => lines.push(l))).toBe(1);
    expect(lines.join("\n")).toContain(`refused: ${KEY}`);
    expect(lines.join("\n")).toContain("requirement changed");
    expect(main(["--root", root], () => undefined)).toBe(1);
  });
});
