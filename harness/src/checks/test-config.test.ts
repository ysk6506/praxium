import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot } from "../common";
import { miniRepo } from "../test-support/mini-repo";
import { run } from "./testguard";

const REAL = findRoot();
const read = (file: string) => readFileSync(path.join(REAL, file), "utf8");

const configCodes = (setupFiles: string): string[] => {
  const config = read("vitest.config.ts").replace(
    /setupFiles: \[[^\]]*\]/,
    `setupFiles: [${setupFiles}]`,
  );
  const root = miniRepo(
    {
      "harness/config/tests.yaml": read("harness/config/tests.yaml"),
      "vitest.config.ts": config,
      "package.json": JSON.stringify({ scripts: { test: "vitest run --coverage" } }),
    },
    {},
  );
  return run(root)
    .map((v) => v.code)
    .filter((code) => code.startsWith("TCFG"));
};

describe("the one allowed vitest setup file [REQ-023]", () => {
  test("the listed setup file is accepted", () => {
    expect(configCodes('"tests/setup/yield-between-tests.ts"')).toEqual([]);
  });

  test("any other setup file is still a violation", () => {
    expect(configCodes('"tests/setup/other.ts"')).toEqual(["TCFG001"]);
    expect(configCodes('"tests/setup/yield-between-tests.ts", "tests/setup/other.ts"')).toEqual([
      "TCFG001",
    ]);
  });
});
