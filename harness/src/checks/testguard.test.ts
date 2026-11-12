import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { findRoot } from "../common";
import { commitAll, miniRepo } from "../test-support/mini-repo";
import { sealAdr } from "./docs";
import { lock, main, run } from "./testguard";

const REAL = findRoot();
const read = (file: string) => readFileSync(path.join(REAL, file), "utf8");
const TEST_FILE = "packages/demo/src/add.test.ts";
const GOOD = `import { expect, test } from "vitest";
import { add } from "./add";

const helper = () => 2;

test("adds", () => {
  expect(add(1, 1)).toBe(helper());
});
`;

function guardRepo(files: Record<string, string> = {}, locked = true): string {
  const root = miniRepo(
    {
      "harness/config/tests.yaml": read("harness/config/tests.yaml"),
      "vitest.config.ts": read("vitest.config.ts"),
      "package.json": JSON.stringify({ scripts: { test: "vitest run --coverage" } }),
      "packages/demo/src/add.ts": "export const add = (a: number, b: number) => a + b;\n",
      [TEST_FILE]: GOOD,
      ...files,
    },
    {},
  );
  if (locked) expect(lock(root, undefined, "2026-10-28")).toEqual([]);
  return root;
}

const codes = (root: string, base?: string): string[] =>
  [...new Set(run(root, base).map((v) => v.code))].sort();

function edit(root: string, file: string, from: string, to: string): void {
  const full = path.join(root, file);
  const text = readFileSync(full, "utf8");
  expect(text).toContain(from);
  writeFileSync(full, text.replace(from, to));
}

const withTest = (body: string) => ({
  "packages/demo/src/x.test.ts": `import { describe, expect, test } from "vitest";\n\n${body}\n`,
});

describe("test integrity: static rules [REQ-023]", () => {
  test("a clean locked repo passes", () => {
    expect(run(guardRepo())).toEqual([]);
  });

  test("the real repository's tests are intact", () => {
    expect(run(REAL)).toEqual([]);
  });

  test("a test without assertions fails", () => {
    expect(codes(guardRepo(withTest('test("x", () => {\n  const y = 1;\n});')))).toEqual([
      "TEST001",
    ]);
  });

  test.each([
    ["expect.soft", 'test("x", () => {\n  expect.soft(1 + 1).toBe(2);\n});'],
    ["toThrow", 'test("x", () => {\n  expect(() => JSON.parse("{")).toThrow();\n});'],
    [
      "helper that asserts",
      'const ok = (v: number) => expect(v).toBe(1);\ntest("x", () => {\n  ok(2 - 1);\n});',
    ],
    [
      "node assert",
      'import assert from "node:assert";\ntest("x", () => {\n  assert.equal(1 + 1, 2);\n});',
    ],
  ])("assertion form counts: %s", (_name, body) => {
    expect(codes(guardRepo(withTest(body)))).not.toContain("TEST001");
  });

  test.each([
    ["literal subject", 'test("x", () => {\n  expect(true).toBe(true);\n});'],
    ["same expression", 'test("x", () => {\n  const v = 3;\n  expect(v).toBe(v);\n});'],
    ["constant truthy", 'test("x", () => {\n  expect(1).toBeTruthy();\n});'],
    ["negated constant", 'test("x", () => {\n  expect("a").not.toBe("b");\n});'],
  ])("trivially decided assertions fail: %s", (_name, body) => {
    expect(codes(guardRepo(withTest(body)))).toContain("TEST003");
  });

  test("comparing two evaluations of the same call is not trivial", () => {
    const body =
      'test("x", () => {\n  const run = () => [1, 2].map((v) => v * 2);\n  expect(run()).toEqual(run());\n});';
    expect(codes(guardRepo(withTest(body)))).not.toContain("TEST003");
  });

  test.each([
    ["test.skip", 'test.skip("later", () => {\n  expect(1 + 1).toBe(3);\n});'],
    [
      "describe.skip",
      'describe.skip("suite", () => {\n  test("x", () => {\n    expect(1 + 1).toBe(2);\n  });\n});',
    ],
    ["test.todo", 'test.todo("someday");'],
    ["test.fails", 'test.fails("broken", () => {\n  expect(1 + 1).toBe(3);\n});'],
    ["test.skipIf", 'test.skipIf(true)("x", () => {\n  expect(1 + 1).toBe(2);\n});'],
    ["context.skip", 'test("x", (ctx) => {\n  ctx.skip();\n  expect(1 + 1).toBe(2);\n});'],
  ])("skips without a debt reference fail: %s", (_name, body) => {
    expect(codes(guardRepo(withTest(body)))).toContain("TEST002");
  });

  test("a skip that cites a debt item is allowed", () => {
    const body =
      'test.skip("TD-0001: needs a FalkorDB server", () => {\n  expect(1 + 1).toBe(2);\n});';
    expect(codes(guardRepo(withTest(body)))).toEqual([]);
  });

  test(".only fails anywhere", () => {
    const body =
      'test.only("x", () => {\n  expect(1 + 1).toBe(2);\n});\ndescribe.only("s", () => {});';
    expect(run(guardRepo(withTest(body))).filter((v) => v.code === "TEST005")).toHaveLength(2);
  });

  test("a catch that swallows failures fails; rethrowing or asserting is fine", () => {
    const swallow =
      'test("x", () => {\n  try {\n    expect(1 + 1).toBe(3);\n  } catch {\n    // ignore\n  }\n});';
    expect(codes(guardRepo(withTest(swallow)))).toContain("TEST004");
    const rethrow =
      'test("x", () => {\n  try {\n    expect(1 + 1).toBe(2);\n  } catch (e) {\n    throw e;\n  }\n});';
    expect(codes(guardRepo(withTest(rethrow)))).not.toContain("TEST004");
  });
});

describe("test integrity: configuration [REQ-023]", () => {
  test.each([
    "exclude: []",
    "passWithNoTests: true",
    "retry: 3",
    "bail: 1",
    "testNamePattern: 'x'",
    "setupFiles: ['s.ts']",
    "allowOnly: true",
  ])("forbidden vitest option fails: %s", (option) => {
    const root = guardRepo();
    edit(root, "vitest.config.ts", 'environment: "node",', `environment: "node",\n    ${option},`);
    expect(codes(root)).toContain("TCFG001");
  });

  test("changing the test include list fails", () => {
    const root = guardRepo();
    edit(root, "vitest.config.ts", '"tests/spec/**/*.test.ts",\n    ],', "],");
    expect(codes(root)).toContain("TCFG002");
  });

  test("hiding code from coverage fails", () => {
    const root = guardRepo();
    edit(
      root,
      "vitest.config.ts",
      '"**/test-support/**"]',
      '"**/test-support/**", "apps/server/src/hard.ts"]',
    );
    expect(codes(root)).toContain("TCFG003");
    const narrowed = guardRepo();
    edit(narrowed, "vitest.config.ts", '"apps/web/src/logic/**",', "");
    expect(codes(narrowed)).toContain("TCFG003");
  });

  test("changing the test script fails", () => {
    const root = guardRepo({
      "package.json": JSON.stringify({ scripts: { test: "vitest run --coverage -t adds" } }),
    });
    expect(codes(root)).toEqual(["TCFG001"]);
  });

  test("a second test configuration fails; a vite config without tests is fine", () => {
    expect(codes(guardRepo({ "apps/web/vitest.config.ts": "export default {};\n" }))).toEqual([
      "TCFG004",
    ]);
    expect(
      codes(
        guardRepo({ "apps/web/vite.config.ts": "export default { test: { include: [] } };\n" }),
      ),
    ).toEqual(["TCFG004"]);
    expect(
      codes(guardRepo({ "apps/web/vite.config.ts": "export default { plugins: [] };\n" })),
    ).toEqual([]);
  });
});

describe("test integrity: the lock [REQ-023]", () => {
  test("new tests must be locked, then pass", () => {
    const root = guardRepo();
    writeFileSync(
      path.join(root, "packages/demo/src/new.test.ts"),
      'import { expect, test } from "vitest";\ntest("n", () => {\n  expect(1 + 1).toBe(2);\n});\n',
    );
    expect(codes(root)).toEqual(["TLOCK003"]);
    expect(lock(root)).toEqual([]);
    expect(codes(root)).toEqual([]);
  });

  test("changing an expected value is caught and named", () => {
    const root = guardRepo();
    edit(root, TEST_FILE, "toBe(helper())", "toBe(3)");
    const found = run(root);
    expect(found.map((v) => v.code)).toEqual(["TLOCK001"]);
    expect(found[0]?.message).toContain("expected values changed");
  });

  test("changing a helper the tests rely on is caught", () => {
    const root = guardRepo();
    edit(root, TEST_FILE, "() => 2", "() => 3");
    expect(codes(root)).toEqual(["TLOCK001"]);
  });

  test("deleting or renaming a test is caught", () => {
    const deleted = guardRepo();
    edit(
      deleted,
      TEST_FILE,
      'test("adds", () => {\n  expect(add(1, 1)).toBe(helper());\n});\n',
      "",
    );
    expect(codes(deleted)).toEqual(["TLOCK002"]);
    const renamed = guardRepo();
    edit(renamed, TEST_FILE, 'test("adds"', 'test("adds numbers"');
    expect(codes(renamed)).toEqual(["TLOCK002", "TLOCK003"]);
  });

  test("removing an assertion is reported as weakening", () => {
    const root = guardRepo({
      [TEST_FILE]: GOOD.replace("toBe(helper());", "toBe(helper());\n  expect(add(0, 0)).toBe(0);"),
    });
    edit(root, TEST_FILE, "\n  expect(add(0, 0)).toBe(0);", "");
    expect(run(root)[0]?.message).toContain("assertions 2 -> 1");
  });

  test("loosening toBeCloseTo precision is reported", () => {
    const root = guardRepo(
      withTest('test("x", () => {\n  expect(0.1 + 0.2).toBeCloseTo(0.3, 9);\n});'),
    );
    edit(root, "packages/demo/src/x.test.ts", "0.3, 9", "0.3, 1");
    expect(run(root)[0]?.message).toContain("precision loosened");
  });

  test("adding a skip is reported", () => {
    const root = guardRepo();
    edit(root, TEST_FILE, 'test("adds"', 'test.skip("adds"');
    expect(run(root).some((v) => v.code === "TLOCK002" || v.message.includes("skip"))).toBe(true);
  });

  test("swapping an import for a fake is caught", () => {
    const root = guardRepo();
    edit(root, TEST_FILE, 'from "./add"', 'from "./fake-add"');
    expect(codes(root)).toEqual(["TLOCK002", "TLOCK003"]);
  });

  test("changing a test.each table is caught", () => {
    const root = guardRepo(
      withTest('test.each([[1, 2]])("doubles %i", (a, b) => {\n  expect(a * 2).toBe(b);\n});'),
    );
    edit(root, "packages/demo/src/x.test.ts", "[[1, 2]]", "[[1, 2], [0, 0]]");
    expect(codes(root)).toEqual(["TLOCK001"]);
  });

  test("changing a hook or a describe-level helper is caught", () => {
    const body =
      'describe("s", () => {\n  let n = 0;\n  beforeEach(() => {\n    n = 1;\n  });\n  test("x", () => {\n    expect(n).toBe(1);\n  });\n});';
    const root = guardRepo(withTest(body.replace("import", "")));
    edit(root, "packages/demo/src/x.test.ts", "n = 1;", "n = 2;");
    expect(codes(root)).toEqual(["TLOCK001"]);
  });

  test("editing expected-data files is caught", () => {
    const root = guardRepo({ "packages/demo/src/__fixtures__/expected.json": '{"temp": 24.5}' });
    writeFileSync(path.join(root, "packages/demo/src/__fixtures__/expected.json"), '{"temp": 30}');
    expect(codes(root)).toEqual(["TLOCK001"]);
  });

  test("comments and formatting do not trip the lock", () => {
    const root = guardRepo();
    edit(root, TEST_FILE, 'test("adds", () => {', "// explains the test\ntest('adds', () =>   {");
    expect(codes(root)).toEqual([]);
  });

  test("repeated identical statements are each locked", () => {
    const root = guardRepo(
      withTest(
        'const x = 1;\nconsole.log(x);\nconsole.log(x);\ntest("x", () => {\n  expect(x + 1).toBe(2);\n});',
      ),
    );
    const keys = Object.keys(
      JSON.parse(readFileSync(path.join(root, "harness/config/test-lock.json"), "utf8")).entries,
    );
    expect(keys.filter((k) => k.includes("<stmt:"))).toHaveLength(2);
  });

  test("the lock command refuses unamended changes and writes nothing", () => {
    const root = guardRepo();
    const before = readFileSync(path.join(root, "harness/config/test-lock.json"), "utf8");
    edit(root, TEST_FILE, "toBe(helper())", "toBe(3)");
    expect(lock(root)).toEqual([`${TEST_FILE}::adds`]);
    expect(readFileSync(path.join(root, "harness/config/test-lock.json"), "utf8")).toBe(before);
  });
});

const REASON =
  "REQ-001 changed: addition now saturates at 2, see the requirements change in this commit.";

function writeAdr(root: string, name: string, amends: string[], reason: string, seal = true): void {
  const data = {
    title: "amend",
    kind: "decision",
    status: "active",
    last_verified: "2026-10-28",
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

describe("test integrity: amendments [REQ-023]", () => {
  const setup = () => {
    const root = guardRepo({ "docs/design-docs/adr/.keep": "" });
    edit(root, TEST_FILE, "toBe(helper())", "toBe(3)");
    return root;
  };

  test("a sealed amendment ADR allows exactly the listed change", () => {
    const root = setup();
    writeAdr(root, "0009-amend.md", [`${TEST_FILE}::adds`], REASON);
    expect(lock(root, "0009-amend.md")).toEqual([]);
    expect(codes(root)).toEqual([]);
    edit(root, TEST_FILE, "() => 2", "() => 5");
    expect(lock(root, "0009-amend.md")).toEqual([`${TEST_FILE}::=helper`]);
  });

  test("the amendment ADR can be named by file name or by its repository path", () => {
    const root = setup();
    writeAdr(root, "0009-amend.md", [`${TEST_FILE}::adds`], REASON);
    expect(lock(root, "docs/design-docs/adr/0009-amend.md")).toEqual([]);
    expect(codes(root)).toEqual([]);
  });

  test("an amended removal is recorded and accepted", () => {
    const root = guardRepo({ "docs/design-docs/adr/.keep": "" });
    edit(root, TEST_FILE, 'test("adds", () => {\n  expect(add(1, 1)).toBe(helper());\n});\n', "");
    writeAdr(root, "0009-amend.md", [`${TEST_FILE}::adds`], REASON);
    expect(lock(root, "0009-amend.md")).toEqual([]);
    expect(codes(root)).toEqual([]);
  });

  test.each([
    [false, REASON, "not sealed"],
    [true, "because", "test_change_reason"],
  ])("a weak or unsealed amendment is rejected (sealed=%s)", (seal, reason, fragment) => {
    const root = setup();
    writeAdr(root, "0009-amend.md", [`${TEST_FILE}::adds`], REASON);
    expect(lock(root, "0009-amend.md")).toEqual([]);
    writeAdr(root, "0009-amend.md", [`${TEST_FILE}::adds`], reason, seal);
    const found = run(root);
    expect(found.map((v) => v.code)).toEqual(["TLOCK004"]);
    expect(found[0]?.message).toContain(fragment);
  });

  test("forging the lock by hand with a fake ADR name is caught", () => {
    const root = setup();
    const lockFile = path.join(root, "harness/config/test-lock.json");
    const data = JSON.parse(readFileSync(lockFile, "utf8"));
    const entry = data.entries[`${TEST_FILE}::adds`];
    const fresh = guardRepo({ [TEST_FILE]: GOOD.replace("toBe(helper())", "toBe(3)") });
    entry.hash = JSON.parse(
      readFileSync(path.join(fresh, "harness/config/test-lock.json"), "utf8"),
    ).entries[`${TEST_FILE}::adds`].hash;
    entry.amended_by = "0042-fake.md";
    writeFileSync(lockFile, JSON.stringify(data));
    expect(codes(root)).toEqual(["TLOCK004"]);
  });

  test("forging the lock without amended_by is caught against the base ref", () => {
    const root = guardRepo();
    commitAll(root);
    edit(root, TEST_FILE, "toBe(helper())", "toBe(3)");
    const lockFile = path.join(root, "harness/config/test-lock.json");
    const data = JSON.parse(readFileSync(lockFile, "utf8"));
    const fresh = guardRepo({ [TEST_FILE]: GOOD.replace("toBe(helper())", "toBe(3)") });
    data.entries[`${TEST_FILE}::adds`].hash = JSON.parse(
      readFileSync(path.join(fresh, "harness/config/test-lock.json"), "utf8"),
    ).entries[`${TEST_FILE}::adds`].hash;
    writeFileSync(lockFile, JSON.stringify(data));
    expect(codes(root)).toEqual([]);
    expect(codes(root, "HEAD")).toEqual(["TLOCK005"]);
  });

  test("the CLI reports refusals with the way forward", () => {
    const root = setup();
    const lines: string[] = [];
    expect(main(["lock", "--root", root], (l) => lines.push(l))).toBe(1);
    expect(lines.join("\n")).toContain("refused");
    expect(lines.join("\n")).toContain("requirement changed");
    expect(main(["--root", root], () => undefined)).toBe(1);
  });
});
