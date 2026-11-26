import { readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { goLines, goRepo } from "../test-support/go-repo";
import { miniRepo } from "../test-support/mini-repo";
import { sealAdr } from "./docs";
import { type GoTestInfo, goTestInfos } from "./go-test-units";
import { entryFor, type Lock, lockViolations } from "./test-lock";
import type { Unit } from "./test-units";

// What a Go test reaches is worked out once per declaration and reused across tests (ADR-0124). These pin the cases
// where reuse could go wrong: a cycle of helpers entered from different tests, methods that share a name, and
// declarations spread over several files in any order.

const PKG = "backend/internal/model";

const HELPER_A = goLines(
  "package model",
  "",
  'import "testing"',
  "",
  "func checkA(t *testing.T, n int) {",
  "\tif n > 0 {",
  "\t\tcheckB(t, n-1)",
  "\t}",
  "}",
);

const HELPER_B = goLines(
  "package model",
  "",
  'import "testing"',
  "",
  "func checkB(t *testing.T, n int) {",
  "\tif n == 0 {",
  '\t\tt.Fatal("bottom")',
  "\t}",
  "\tcheckA(t, n-1)",
  "}",
);

const ENTER_A = goLines(
  "package model",
  "",
  'import "testing"',
  "",
  "// [REQ-023]",
  "func TestEnterA(t *testing.T) { checkA(t, 2) }",
);

const ENTER_B = goLines(
  "package model",
  "",
  'import "testing"',
  "",
  "// [REQ-023]",
  "func TestEnterB(t *testing.T) { checkB(t, 2) }",
);

const CYCLE: Record<string, string> = {
  [`${PKG}/a_test.go`]: HELPER_A,
  [`${PKG}/b_test.go`]: HELPER_B,
  [`${PKG}/enter_a_test.go`]: ENTER_A,
  [`${PKG}/enter_b_test.go`]: ENTER_B,
};

const METHODS = goLines(
  "package model",
  "",
  'import "testing"',
  "",
  "type alpha struct{}",
  "",
  "func (alpha) verify() int { return 1 }",
  "",
  "type beta struct{}",
  "",
  "func (beta) verify() int { return 2 }",
  "",
  "func verify(t *testing.T, n int) {",
  "\tif n != 1 {",
  '\t\tt.Fatal("verify")',
  "\t}",
  "}",
  "",
  "// [REQ-023]",
  "func TestByFunction(t *testing.T) { verify(t, 1) }",
  "",
  "// [REQ-023]",
  "func TestByMethod(t *testing.T) {",
  "\tvar a alpha",
  "\tif a.verify() != 1 {",
  '\t\tt.Fatal("alpha")',
  "\t}",
  "}",
  "",
  "// [REQ-023]",
  "func TestByType(t *testing.T) {",
  "\tif got := len([]alpha{{}}); got != 1 {",
  "\t\tt.Fatal(got)",
  "\t}",
  "}",
);

const byName = (root: string): Map<string, GoTestInfo> =>
  new Map(goTestInfos(root).map((info) => [info.name, info]));

/** The parts of a test's fingerprint that do not depend on where it sits. */
const print = (info: GoTestInfo | undefined) =>
  info && {
    digest: info.digest,
    asserts: info.asserts,
    literals: info.literals,
    skips: info.skips,
    reqs: info.reqs,
  };

function rewrite(root: string, file: string, from: string, to: string): void {
  const full = path.join(root, file);
  const text = readFileSync(full, "utf8");
  expect(text).toContain(from);
  writeFileSync(full, text.replace(from, to));
}

describe("Go test reach: a cycle of helpers [REQ-023]", () => {
  test("both tests that enter the cycle reach all of it and count its failing helpers", () => {
    const root = goRepo(CYCLE);
    const before = byName(root);
    expect(print(before.get("TestEnterA"))).toMatchObject({ asserts: 1, skips: 0 });
    expect(print(before.get("TestEnterB"))).toMatchObject({ asserts: 1, skips: 0 });
    rewrite(root, `${PKG}/b_test.go`, '"bottom"', '"floor"');
    const after = byName(root);
    for (const name of ["TestEnterA", "TestEnterB"]) {
      expect(after.get(name)?.digest).not.toBe(before.get(name)?.digest);
      expect(after.get(name)?.literals).not.toBe(before.get(name)?.literals);
    }
    expect(print(after.get("TestDouble"))).toEqual(print(before.get("TestDouble")));
    rewrite(root, `${PKG}/a_test.go`, "n > 0", "n > 1");
    const again = byName(root);
    for (const name of ["TestEnterA", "TestEnterB"])
      expect(again.get(name)?.digest).not.toBe(after.get(name)?.digest);
  });

  test("a test's fingerprint does not depend on the other tests that share its helpers", () => {
    const both = byName(goRepo(CYCLE));
    const alone = goRepo(CYCLE);
    rmSync(path.join(alone, `${PKG}/enter_b_test.go`));
    const one = byName(alone);
    expect(one.has("TestEnterB")).toBe(false);
    expect(print(one.get("TestEnterA"))).toEqual(print(both.get("TestEnterA")));
  });
});

describe("Go test reach: methods that share a name [REQ-023]", () => {
  test("a call through a member reaches every method of that name, a bare call only the function", () => {
    const root = goRepo({ [`${PKG}/verify_test.go`]: METHODS });
    const before = byName(root);
    expect(print(before.get("TestByFunction"))).toMatchObject({ asserts: 1 });
    expect(print(before.get("TestByMethod"))).toMatchObject({ asserts: 1 });
    rewrite(root, `${PKG}/verify_test.go`, "return 2", "return 3");
    const after = byName(root);
    expect(after.get("TestByMethod")?.digest).not.toBe(before.get("TestByMethod")?.digest);
    expect(print(after.get("TestByFunction"))).toEqual(print(before.get("TestByFunction")));
    expect(print(after.get("TestByType"))).toEqual(print(before.get("TestByType")));
  });

  test("naming a type brings its methods, and changing the function leaves method users alone", () => {
    const root = goRepo({ [`${PKG}/verify_test.go`]: METHODS });
    const before = byName(root);
    rewrite(root, `${PKG}/verify_test.go`, "return 1", "return 4");
    const after = byName(root);
    expect(after.get("TestByType")?.digest).not.toBe(before.get("TestByType")?.digest);
    rewrite(root, `${PKG}/verify_test.go`, "n != 1", "n != 5");
    const again = byName(root);
    expect(again.get("TestByFunction")?.digest).not.toBe(after.get("TestByFunction")?.digest);
    expect(print(again.get("TestByMethod"))).toEqual(print(after.get("TestByMethod")));
    expect(print(again.get("TestByType"))).toEqual(print(after.get("TestByType")));
  });
});

describe("Go test reach: declarations over several files [REQ-023]", () => {
  test("splitting the package into other files or reversing their order changes no fingerprint", () => {
    const split = byName(goRepo(CYCLE));
    const body = (text: string): string => text.split('import "testing"\n')[1] ?? "";
    const merged = goLines(
      "package model",
      "",
      'import "testing"',
      body(ENTER_B),
      body(HELPER_B),
      body(ENTER_A),
      body(HELPER_A),
    );
    const one = byName(goRepo({ [`${PKG}/all_test.go`]: merged }));
    expect([...one.keys()].sort()).toEqual([...split.keys()].sort());
    for (const [name, info] of split) expect(print(one.get(name))).toEqual(print(info));
  });
});

describe("Go test reach: an amendment ADR is read once, answered per claim [REQ-023]", () => {
  const REASON = "The requirement changed, so the expected value changed with it.";

  function adr(root: string, name: string, amends: string[]): void {
    const data = {
      title: "amend",
      kind: "decision",
      status: "active",
      enforced_by: ["none"],
      no_enforcement_reason: "amendment",
      amends_tests: amends,
      test_change_reason: REASON,
    };
    const file = `docs/design-docs/adr/${name}`;
    writeFileSync(path.join(root, file), `---\n${YAML.stringify(data)}---\n# Amend\nChanged.\n`);
    sealAdr(root, file);
  }

  const unit = (key: string): Unit => ({
    key,
    kind: "test",
    digest: "d",
    path: "x.test.ts",
    line: 1,
    asserts: 1,
    literals: [],
    precision: [],
    skips: 0,
  });

  test("each claim gets its own answer from the same ADR, a missing ADR fails every claim", () => {
    const root = miniRepo({ "docs/design-docs/adr/.keep": "" });
    adr(root, "0009-amend.md", ["k1", "k3", "gone1"]);
    const keys = ["k1", "k2", "k3", "k4", "k5"];
    const by = (key: string): string | null =>
      ({ k4: "0042-missing.md", k5: "0042-missing.md" })[key] ?? "0009-amend.md";
    const lock: Lock = {
      version: 1,
      entries: Object.fromEntries(keys.map((k) => [k, entryFor(unit(k), "2026-11-26", by(k))])),
      removed: { gone1: "0009-amend.md", gone2: "0009-amend.md" },
    };
    writeFileSync(path.join(root, "lock.json"), JSON.stringify(lock));
    const found = lockViolations(root, "lock.json", new Map(keys.map((k) => [k, unit(k)])));
    expect(found.map((v) => [v.code, v.message])).toEqual([
      ["TLOCK004", "amendment of gone2: ADR 0009-amend.md does not list it in amends_tests"],
      ["TLOCK004", "amendment of k2: ADR 0009-amend.md does not list it in amends_tests"],
      ["TLOCK004", "amendment of k4: ADR 0042-missing.md does not exist"],
      ["TLOCK004", "amendment of k5: ADR 0042-missing.md does not exist"],
    ]);
  });
});
