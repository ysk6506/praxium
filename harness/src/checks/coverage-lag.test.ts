/** A floor that trails the measured coverage is a failure of its own (ADR-0028). */
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot } from "../common";
import { goLines, goRepo } from "../test-support/go-repo";
import { miniRepo } from "../test-support/mini-repo";
import {
  checkGoCoverage,
  checkSummary,
  MAX_FLOOR_LAG,
  MIN_MEASURED_FOR_RATCHET,
  ratchet,
} from "./coverage";
import { loadTestsConfig } from "./testguard";

const REAL = findRoot();
const tests = readFileSync(path.join(REAL, "harness/config/tests.yaml"), "utf8");

function tsRepo(hit: number, floor: number): string {
  const file = "packages/core/src/kernel/a.ts";
  return miniRepo({
    "harness/config/tests.yaml": tests,
    "coverage/coverage-summary.json": JSON.stringify({
      total: {},
      [file]: { lines: { total: 100, covered: hit }, branches: { total: 0, covered: 0 } },
    }),
    "harness/config/coverage-floor.json": JSON.stringify({ "core.kernel": floor }),
  });
}
const tsCodes = (hit: number, floor: number): string[] => {
  const root = tsRepo(hit, floor);
  return checkSummary(root, loadTestsConfig(root)).map((v) => v.code);
};

describe("coverage floors may not lag [REQ-023]", () => {
  test("a floor more than the margin under the coverage is COV006, anything closer is fine", () => {
    expect(MAX_FLOOR_LAG).toBe(1);
    expect(tsCodes(94, 94)).toEqual([]);
    expect(tsCodes(94, 93)).toEqual([]);
    expect(tsCodes(94, 92.5)).toEqual(["COV006"]);
    expect(tsCodes(94, 90)).toEqual(["COV006"]);
  });

  test("a floor under the cap of 98 is measured against the cap, and a unit below its floor stays COV001", () => {
    expect(tsCodes(100, 98)).toEqual([]);
    expect(tsCodes(100, 97)).toEqual([]);
    expect(tsCodes(100, 96)).toEqual(["COV006"]);
    expect(tsCodes(90, 95)).toEqual(["COV001"]);
  });

  test("a small unit is ratcheted too once it has the amount of code the ratchet needs as evidence", () => {
    const small = (all: number): string =>
      miniRepo({
        "harness/config/tests.yaml": tests,
        "coverage/coverage-summary.json": JSON.stringify({
          total: {},
          "packages/core/src/kernel/a.ts": {
            lines: { total: all, covered: all },
            branches: { total: 0, covered: 0 },
          },
        }),
      });
    expect(MIN_MEASURED_FOR_RATCHET).toBe(15);
    expect(ratchet(small(MIN_MEASURED_FOR_RATCHET))).toEqual({ "core.kernel": 98 });
    expect(ratchet(small(MIN_MEASURED_FOR_RATCHET - 1))).toEqual({ "core.kernel": 90 });
  });

  test("the message names the unit, both numbers and the command that fixes it", () => {
    const root = tsRepo(94, 90);
    const [found] = checkSummary(root, loadTestsConfig(root));
    expect(found?.message).toContain("core.kernel");
    expect(found?.message).toContain("94.0%");
    expect(found?.message).toContain("90.0%");
    expect(found?.fix).toContain("pnpm ratchet");
  });
});

describe("Go coverage floors may not lag [REQ-023]", () => {
  // Sixty statements, all run by the test: the model unit has enough evidence to be ratcheted.
  const big = goLines(
    "package model",
    "",
    "// Big counts to sixty.",
    "func Big() int {",
    "\tx := 0",
    ...Array.from({ length: 60 }, () => "\tx++"),
    "\treturn x",
    "}",
  );
  const bigTest = goLines(
    "package model",
    'import "testing"',
    "func TestBig(t *testing.T) {",
    "\tif Big() != 60 {",
    '\t\tt.Fatal("sixty")',
    "\t}",
    "}",
  );
  const check = (model: number): string[] => {
    const root = goRepo({
      "backend/internal/model/big.go": big,
      "backend/internal/model/big_test.go": bigTest,
      "harness/config/coverage-floor.json": JSON.stringify({
        "go:core.kernel": 90,
        "go:core.model": model,
        "go:core.other": 90,
        "go:cmd.app": 90,
      }),
    });
    return checkGoCoverage(root, loadTestsConfig(root)).map((v) => v.code);
  };

  test("a Go floor far under the measured coverage is GOCOV003, a ratcheted one passes", () => {
    expect(check(98)).toEqual([]);
    expect(check(97)).toEqual([]);
    expect(check(95)).toEqual(["GOCOV003"]);
    expect(check(90)).toEqual(["GOCOV003"]);
  });
});
