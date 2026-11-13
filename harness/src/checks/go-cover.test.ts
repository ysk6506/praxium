import { describe, expect, test } from "vitest";
import type { Violation } from "../common";
import { goLines, goRepo } from "../test-support/go-repo";
import { checkGoCoverage } from "./coverage";
import { testIntegrity } from "./go";
import { loadGoConfig } from "./go-config";
import { checkPresence, parseProfile, runTests, unitCoverage } from "./go-cover";
import { loadTestsConfig } from "./testguard";

const MODEL = "backend/internal/model/";

const configOf = (root: string) =>
  loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>;
const codes = (found: Violation[]): string[] => found.map((v) => v.code);

describe("Go tests: the coverage profile [REQ-023]", () => {
  const profile = [
    "mode: set",
    "example.test/app/internal/kernel/kernel.go:5.2,6.3 2 1",
    "example.test/app/internal/kernel/kernel.go:7.2,8.3 3 0",
    "example.test/app/internal/model/model.go:5.2,6.3 4 1",
    "example.test/app/internal/model/model.go:5.2,6.3 4 5",
    "example.test/app/stray/x.go:1.1,2.2 9 0",
    "other.example/lib/y.go:1.1,2.2 9 0",
    "garbage line",
  ].join("\n");

  test("statements are counted per file, and a block counts as hit when it ran", () => {
    expect([...parseProfile(profile)]).toEqual([
      ["example.test/app/internal/kernel/kernel.go", [5, 2]],
      ["example.test/app/internal/model/model.go", [8, 8]],
      ["example.test/app/stray/x.go", [9, 0]],
      ["other.example/lib/y.go", [9, 0]],
    ]);
  });

  test("coverage is per unit, and files outside every unit are ignored", () => {
    const units = unitCoverage(configOf(goRepo()), profile);
    expect([...units]).toEqual([
      ["core.kernel", { percent: 40, measured: 5 }],
      ["core.model", { percent: 100, measured: 8 }],
    ]);
  });
});

describe("Go tests: running them [REQ-023]", () => {
  test("passing tests give coverage per unit", () => {
    const root = goRepo();
    const { violations, units } = runTests(root, configOf(root));
    expect(violations).toEqual([]);
    expect([...units.keys()].sort()).toEqual(["core.kernel", "core.model"]);
    expect(units.get("core.kernel")?.percent).toBe(100);
  });

  test("a failing test is GOTEST001 and the message carries the output", () => {
    const failing = goLines(
      "package model",
      'import "testing"',
      "func TestFails(t *testing.T) {",
      '\tt.Fatal("the expectation broke")',
      "}",
    );
    const root = goRepo({ [`${MODEL}model_test.go`]: failing });
    const { violations, units } = runTests(root, configOf(root));
    expect(codes(violations)).toEqual(["GOTEST001"]);
    expect(violations[0]?.message).toContain("the expectation broke");
    expect(units.size).toBe(0);
  });

  test("code that does not compile is GOTEST001 too", () => {
    const root = goRepo({ [`${MODEL}broken.go`]: "package model\nfunc (\n" });
    expect(codes(runTests(root, configOf(root)).violations)).toEqual(["GOTEST001"]);
  });
});

describe("Go tests: every unit with code has a test [REQ-023]", () => {
  test("the healthy module has", () => {
    expect(testIntegrity(goRepo())).toEqual([]);
    expect(testIntegrity(goRepo({ "harness/config/architecture.yaml": "units: {}\n" }))).toEqual(
      [],
    );
  });

  test("a unit with functions and no Test function is reported, a unit without functions is not", () => {
    const code = goLines(
      "// Package other has code now.",
      "package other",
      "func Work() int { return 1 }",
    );
    const root = goRepo({ "backend/internal/other/work.go": code });
    expect(checkPresence(root, configOf(root)).map((v) => [v.code, v.path])).toEqual([
      ["GOTEST002", "backend/internal/other"],
    ]);
    const helperOnly = goLines("package other", 'import "testing"', "func helper(t *testing.T) {}");
    const root2 = goRepo({
      "backend/internal/other/work.go": code,
      "backend/internal/other/work_test.go": helperOnly,
    });
    expect(codes(checkPresence(root2, configOf(root2)))).toEqual(["GOTEST002"]);
    const real = goLines(
      "package other",
      'import "testing"',
      "func TestWork(t *testing.T) { _ = Work() }",
    );
    const root3 = goRepo({
      "backend/internal/other/work.go": code,
      "backend/internal/other/work_test.go": real,
    });
    expect(checkPresence(root3, configOf(root3))).toEqual([]);
  });
});

describe("Go tests: floors [REQ-023]", () => {
  const floors = (value: Record<string, number>) => ({
    "harness/config/coverage-floor.json": JSON.stringify(value),
  });
  const check = (files: Record<string, string>) => {
    const root = goRepo(files);
    return checkGoCoverage(root, loadTestsConfig(root));
  };

  const uncovered = {
    [`${MODEL}model.go`]: goLines(
      "// Package model sits on the kernel.",
      "package model",
      "",
      'import "example.test/app/internal/kernel"',
      "",
      "// Double doubles the answer.",
      "func Double() int { return 2 * kernel.Answer() }",
      "",
      "// Unused is never called.",
      "func Unused() int { return 1 }",
    ),
  };

  test("a unit at or above its floor passes", () => {
    expect(check(floors({ "go:core.kernel": 100, "go:core.model": 90 }))).toEqual([]);
  });

  test("a unit with no floor, or a floor under the minimum, is GOCOV002", () => {
    expect(codes(check(floors({ "go:core.kernel": 100 })))).toEqual(["GOCOV002"]);
    expect(codes(check(floors({ "go:core.kernel": 80, "go:core.model": 90 })))).toEqual([
      "GOCOV002",
    ]);
    expect(codes(check({}))).toEqual(["GOCOV002", "GOCOV002"]);
  });

  test("a unit below its floor is GOCOV001", () => {
    const found = check({
      ...floors({ "go:core.kernel": 100, "go:core.model": 90 }),
      ...uncovered,
    });
    expect(codes(found)).toEqual(["GOCOV001"]);
    expect(found[0]?.message).toContain("core.model");
  });

  test("a failing test is reported instead of coverage, and a repository without Go is not run", () => {
    const failing = goLines(
      "package model",
      'import "testing"',
      'func TestFails(t *testing.T) { t.Fatal("x") }',
    );
    expect(codes(check({ [`${MODEL}model_test.go`]: failing }))).toEqual(["GOTEST001"]);
    expect(check({ "harness/config/architecture.yaml": "units: {}\n" })).toEqual([]);
  });
});
