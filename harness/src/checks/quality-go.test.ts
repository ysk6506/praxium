import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { findRoot } from "../common";
import { GO_ARCHITECTURE, goLines, goRepo } from "../test-support/go-repo";
import { gradeOf, recordCoverage, render, run } from "./quality";
import { gatherFacts, RECORD_FILE, tableUnits } from "./quality-facts";

const real = (file: string): string => readFileSync(path.join(findRoot(), file), "utf8");
const TS_PART = [
  "units: {}",
  "node_builtins: []",
  "third_party: {}",
  "test_only: []",
  "nondeterminism_allowed: []",
  "source_roots: [packages]",
  "alias_prefix: '@x/'",
  "alias_target: packages",
].join("\n");
const MODEL = "backend/internal/model";

function repo(files: Record<string, string> = {}): string {
  return goRepo({
    "harness/config/architecture.yaml": GO_ARCHITECTURE.replace("units: {}", TS_PART),
    "harness/config/ignores.yaml": real("harness/config/ignores.yaml"),
    "harness/config/ignores-baseline.json": '{"biome-ignore": 1}\n',
    "harness/config/docs.yaml": real("harness/config/docs.yaml"),
    "harness/config/tests.yaml": real("harness/config/tests.yaml"),
    "harness/config/coverage-floor.json": '{"go:core.model": 90}\n',
    ...files,
  });
}

const factsOf = (root: string, unit: string) => {
  const found = gatherFacts(root, []).find((f) => f.unit === unit);
  if (!found) throw new Error(`no row ${unit}`);
  return found;
};

const bigFunction = (n: number): string =>
  goLines(
    "package model",
    "",
    "func Big() int {",
    "\tx := 0",
    ...Array(n).fill("\tx++"),
    "\treturn x",
    "}",
  );

describe("Go units in the quality table [REQ-024]", () => {
  test("every Go unit is a row named go:<unit>, after the TypeScript units", () => {
    const names = tableUnits(repo());
    expect(names).toEqual(["go:core.kernel", "go:core.model", "go:core.other", "go:cmd.app"]);
    const table = render(repo());
    for (const name of names) expect(table).toContain(`| ${name} |`);
  });

  test("a Go unit with no recorded coverage is unmeasured, not zero", () => {
    const row = factsOf(repo(), "go:core.model");
    expect(row.percent).toBeUndefined();
    expect(row.floor).toBe(90);
    expect(gradeOf(row).grade).toBe("B");
    expect(render(repo())).toContain("모름");
  });

  test("a recorded Go coverage below the floor is D, at or above it is A", () => {
    const record = (percent: number) => ({
      [RECORD_FILE]: JSON.stringify({
        measuredOn: "2026-11-13",
        units: { "go:core.model": percent },
      }),
    });
    expect(gradeOf(factsOf(repo(record(89)), "go:core.model")).grade).toBe("D");
    expect(gradeOf(factsOf(repo(record(95)), "go:core.model")).grade).toBe("A");
  });

  test("a Go function over a limit makes its unit D and counts as over", () => {
    const row = factsOf(repo({ [`${MODEL}/big.go`]: bigFunction(20) }), "go:core.model");
    expect(row.over).toBe(1);
    expect(gradeOf(row).grade).toBe("D");
    expect(factsOf(repo(), "go:core.model").over).toBe(0);
  });

  test("a Go test file is not measured for size", () => {
    const big = bigFunction(20).replace("package model", 'package model\n\nimport "testing"\n');
    expect(factsOf(repo({ [`${MODEL}/big_test.go`]: big }), "go:core.model").over).toBe(0);
  });

  test("a skipped Go test is counted as a switched-off check of its unit", () => {
    const skipped = goLines(
      "package model",
      "",
      'import "testing"',
      "",
      "func TestSkipped(t *testing.T) {",
      '\tt.Skip("later")',
      "}",
    );
    expect(factsOf(repo({ [`${MODEL}/skip_test.go`]: skipped }), "go:core.model").ignores).toBe(1);
  });

  test("a debt item may name a Go unit, and only one that exists", () => {
    const item = (units: string[]) =>
      YAML.stringify({
        items: [
          {
            id: "TD-0001",
            title: "t",
            severity: "low",
            cost: "c",
            created: "2026-01-01",
            due: "2099-01-01",
            status: "open",
            units,
          },
        ],
      });
    const register = "docs/exec-plans/tech-debt.yaml";
    expect(run(repo({ [register]: item(["go:core.model"]) }))).toEqual([]);
    const bad = run(repo({ [register]: item(["go:core.nope"]) }));
    expect(bad.map((v) => v.code)).toEqual(["QUAL003"]);
    expect(factsOf(repo({ [register]: item(["go:core.model"]) }), "go:core.model").debtOpen).toBe(
      1,
    );
  });
});

describe("recording Go coverage [REQ-024]", () => {
  test("the record carries the statement coverage of each Go unit under its go: label", () => {
    const root = repo({
      "coverage/coverage-summary.json": "{}",
      [`${MODEL}/model_test.go`]: goLines(
        "package model",
        "",
        'import "testing"',
        "",
        "func TestDouble(t *testing.T) {",
        "\tif Double() != 84 {",
        '\t\tt.Fatal("unexpected")',
        "\t}",
        "}",
      ),
    });
    const record = JSON.parse(recordCoverage(root));
    expect(record.units["go:core.model"]).toBe(100);
    expect(existsSync(path.join(root, RECORD_FILE))).toBe(true);
    expect(factsOf(root, "go:core.model").percent).toBe(100);
  }, 20_000);

  test("a failing Go run is an error, never a guessed number", () => {
    const failing = goLines(
      "package model",
      "",
      'import "testing"',
      "",
      "func TestDouble(t *testing.T) {",
      '\tt.Fatal("red")',
      "}",
    );
    const root = repo({
      "coverage/coverage-summary.json": "{}",
      [`${MODEL}/model_test.go`]: failing,
    });
    expect(() => recordCoverage(root)).toThrow("Go coverage could not be measured");
  }, 20_000);
});
