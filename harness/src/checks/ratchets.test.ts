import { readFileSync, unlinkSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { findRoot } from "../common";
import { commitAll, miniRepo } from "../test-support/mini-repo";
import {
  checkFloorsAgainst,
  run as coverage,
  main as coverageMain,
  perUnit,
  ratchet,
} from "./coverage";
import { sealAdr } from "./docs";
import { run as protectedRun, RATCHET_FILES } from "./protected";
import { loadTestsConfig } from "./testguard";
import { run as traceability } from "./traceability";

const REAL = findRoot();
const read = (file: string) => readFileSync(path.join(REAL, file), "utf8");

// ------------------------------------------------------------------------------------ coverage

function summary(files: Record<string, [number, number]>): string {
  const entries = Object.fromEntries(
    Object.entries(files).map(([file, [hit, all]]) => [
      file,
      { lines: { total: all, covered: hit }, branches: { total: 0, covered: 0 } },
    ]),
  );
  return JSON.stringify({ total: {}, ...entries });
}

function coverageRepo(
  files: Record<string, [number, number]>,
  floors?: Record<string, number>,
  extra: Record<string, string> = {},
): string {
  return miniRepo(
    {
      "harness/config/tests.yaml": read("harness/config/tests.yaml"),
      "coverage/coverage-summary.json": summary(files),
      ...(floors ? { "harness/config/coverage-floor.json": JSON.stringify(floors) } : {}),
      ...extra,
    },
    undefined,
  );
}

const covCodes = (root: string, base?: string) =>
  [...new Set(coverage(root, base).map((v) => v.code))].sort();

describe("coverage ratchet [REQ-023]", () => {
  test("coverage is grouped by architecture unit and absolute paths are understood", () => {
    const root = coverageRepo({});
    const units = perUnit(root, {
      [path.join(root, "packages/core/src/kernel/a.ts")]: {
        lines: { total: 10, covered: 9 },
        branches: { total: 10, covered: 10 },
      },
      "packages/core/src/world/b.ts": {
        lines: { total: 0, covered: 0 },
        branches: { total: 0, covered: 0 },
      },
      "elsewhere/c.ts": { lines: { total: 5, covered: 0 }, branches: { total: 0, covered: 0 } },
    });
    expect(Object.fromEntries(units)).toEqual({ "core.kernel": 95, "core.world": 100 });
  });

  test("at or above the floor passes, below fails", () => {
    expect(
      coverage(coverageRepo({ "packages/core/src/kernel/a.ts": [95, 100] }, { "core.kernel": 95 })),
    ).toEqual([]);
    expect(
      covCodes(coverageRepo({ "packages/core/src/kernel/a.ts": [94, 100] }, { "core.kernel": 95 })),
    ).toEqual(["COV001"]);
  });

  test("a unit without a floor, or a floor below the minimum, fails", () => {
    expect(covCodes(coverageRepo({ "packages/core/src/world/a.ts": [99, 100] }, {}))).toEqual([
      "COV002",
    ]);
    expect(
      covCodes(coverageRepo({ "packages/core/src/world/a.ts": [99, 100] }, { "core.world": 50 })),
    ).toEqual(["COV002"]);
  });

  test("a missing summary fails", () => {
    const root = coverageRepo({});
    unlinkSync(path.join(root, "coverage/coverage-summary.json"));
    expect(covCodes(root)).toEqual(["COV004"]);
  });

  test("ratchet raises floors, never lowers them and never goes under the minimum", () => {
    const files = {
      "packages/core/src/kernel/a.ts": [97, 100],
      "packages/core/src/world/b.ts": [80, 100],
      "apps/server/src/http/c.ts": [93, 100],
    } as const;
    const root = coverageRepo(files as unknown as Record<string, [number, number]>, {
      "core.kernel": 95,
      "server.http": 99,
    });
    expect(ratchet(root)).toEqual({ "core.kernel": 97, "core.world": 90, "server.http": 99 });
  });

  test("ratchet keeps the minimum floor for units with little code and caps floors at 98 (ADR-0014)", () => {
    const root = coverageRepo({
      "packages/core/src/kernel/a.ts": [10, 10],
      "packages/core/src/world/b.ts": [200, 200],
    });
    expect(ratchet(root)).toEqual({ "core.kernel": 90, "core.world": 98 });
  });

  test.each([
    [{ "core.kernel": 96 }, []],
    [{ "core.kernel": 94 }, ["COV003"]],
    [{}, ["COV003"]],
  ])("floors may not drop against the base ref (%o)", (after, expected) => {
    const root = coverageRepo(
      { "packages/core/src/kernel/a.ts": [99, 100] },
      { "core.kernel": 95 },
    );
    commitAll(root);
    writeFileSync(path.join(root, "harness/config/coverage-floor.json"), JSON.stringify(after));
    expect(checkFloorsAgainst(root, loadTestsConfig(root), "HEAD").map((v) => v.code)).toEqual(
      expected,
    );
  });

  test("coverage-ignore comments need a reason and have a budget", () => {
    const one = coverageRepo(
      {},
      {},
      { "packages/core/src/kernel/a.ts": "/* v8 ignore next */\nexport const a = 1;\n" },
    );
    expect(covCodes(one)).toEqual(["COV005"]);
    const many =
      "/* v8 ignore next -- only reachable on Windows hosts */\nexport const a = 1;\n".repeat(4);
    expect(covCodes(coverageRepo({}, {}, { "packages/core/src/kernel/a.ts": many }))).toEqual([
      "COV005",
    ]);
    const ok = "/* v8 ignore next -- only reachable on Windows hosts */\nexport const a = 1;\n";
    expect(coverage(coverageRepo({}, {}, { "packages/core/src/kernel/a.ts": ok }))).toEqual([]);
  });

  test("the CLI prints ratcheted floors", () => {
    const root = coverageRepo({ "packages/core/src/kernel/a.ts": [99, 100] });
    const lines: string[] = [];
    expect(coverageMain(["ratchet", "--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines).toEqual(["core.kernel: 98.0%"]);
    expect(coverageMain(["--root", root], () => undefined)).toBe(0);
  });
});

// ------------------------------------------------------------------------------------ traceability

const REQS = "| ID | x |\n|---|---|\n| REQ-001 | a |\n| REQ-002 | b |\n";
const SPEC = (title: string) =>
  `import { describe, expect, test } from "vitest";\ndescribe("${title}", () => {\n  test("t", () => {\n    expect(1 + 1).toBe(2);\n  });\n});\n`;

function traceRepo(files: Record<string, string>): string {
  return miniRepo(
    {
      "harness/config/tests.yaml": read("harness/config/tests.yaml"),
      "docs/product-specs/requirements.md": REQS,
      ...files,
    },
    {},
  );
}

describe("traceability [REQ-023] [REQ-025]", () => {
  test("every requirement needs a test", () => {
    const found = traceability(traceRepo({ "tests/spec/a.test.ts": SPEC("a [REQ-001]") }));
    expect(found.map((v) => [v.code, v.message])).toEqual([["TRACE001", "REQ-002 has no test"]]);
  });

  test("a test citing an unknown requirement fails", () => {
    const found = traceability(
      traceRepo({ "tests/spec/a.test.ts": SPEC("a [REQ-001] [REQ-002] [REQ-999]") }),
    );
    expect(found.map((v) => v.code)).toEqual(["TRACE002"]);
  });

  test("spec tests must cite a requirement, unit tests need not", () => {
    const found = traceability(
      traceRepo({
        "tests/spec/a.test.ts": SPEC("a [REQ-001] [REQ-002]"),
        "tests/spec/b.test.ts": SPEC("unlinked"),
        "packages/core/src/x.test.ts": SPEC("unit"),
      }),
    );
    expect(found.map((v) => [v.code, v.path])).toEqual([["TRACE003", "tests/spec/b.test.ts"]]);
  });

  test("duplicate requirement ids fail", () => {
    const found = traceability(
      traceRepo({
        "docs/product-specs/requirements.md": `${REQS}| REQ-001 | dup |\n`,
        "tests/spec/a.test.ts": SPEC("[REQ-001] [REQ-002]"),
      }),
    );
    expect(found.map((v) => v.code)).toEqual(["TRACE004"]);
  });
});

// ------------------------------------------------------------------------------------ protected paths

function protectedRepo(): string {
  const root = miniRepo(
    {
      "harness/config/protected-paths.txt": "harness/**\npackage.json\n",
      "harness/config/x.yaml": "limit: 10\n",
      "harness/config/debt-baseline.json": "{}\n",
      ".github/CODEOWNERS": "/harness/** @o\n/package.json @o\n",
      "src/a.ts": "export const a = 1;\n",
      "docs/design-docs/adr/.keep": "",
    },
    { "package.json": "{}\n" },
  );
  commitAll(root);
  return root;
}

function addAdr(root: string, touches: string[], seal = true): void {
  const data = {
    title: "t",
    kind: "decision",
    status: "active",
    last_verified: "2026-10-28",
    enforced_by: ["none"],
    no_enforcement_reason: "x",
    touches_protected: touches,
  };
  const file = "docs/design-docs/adr/0050-loosen.md";
  writeFileSync(path.join(root, file), `---\n${YAML.stringify(data)}---\n# Why\nBecause.\n`);
  if (seal) sealAdr(root, file);
}

const protCodes = (root: string) =>
  [...new Set(protectedRun(root, "HEAD").map((v) => v.code))].sort();

describe("protected paths [REQ-021]", () => {
  test("unprotected changes pass", () => {
    const root = protectedRepo();
    writeFileSync(path.join(root, "src/a.ts"), "export const a = 2;\n");
    expect(protCodes(root)).toEqual([]);
  });

  test("changing, deleting or adding under a protected path without an ADR fails", () => {
    const changed = protectedRepo();
    writeFileSync(path.join(changed, "harness/config/x.yaml"), "limit: 999\n");
    expect(protCodes(changed)).toEqual(["PROT001"]);
    const deleted = protectedRepo();
    unlinkSync(path.join(deleted, "harness/config/x.yaml"));
    expect(protCodes(deleted)).toEqual(["PROT001"]);
    const added = protectedRepo();
    writeFileSync(path.join(added, "harness/config/new.yaml"), "a: 1\n");
    expect(protCodes(added)).toEqual(["PROT001"]);
  });

  test("a sealed ADR added in the same change authorizes the path or its pattern", () => {
    const exact = protectedRepo();
    writeFileSync(path.join(exact, "harness/config/x.yaml"), "limit: 999\n");
    addAdr(exact, ["harness/config/x.yaml"]);
    expect(protCodes(exact)).toEqual([]);
    const pattern = protectedRepo();
    writeFileSync(path.join(pattern, "harness/config/x.yaml"), "limit: 999\n");
    addAdr(pattern, ["harness/**"]);
    expect(protCodes(pattern)).toEqual([]);
  });

  test("an unsealed ADR does not authorize", () => {
    const root = protectedRepo();
    writeFileSync(path.join(root, "harness/config/x.yaml"), "limit: 999\n");
    addAdr(root, ["harness/config/x.yaml"], false);
    expect(protCodes(root)).toEqual(["PROT001", "PROT002"]);
  });

  test("an ADR that already existed cannot authorize new changes", () => {
    const root = protectedRepo();
    addAdr(root, ["harness/config/x.yaml"]);
    commitAll(root, "old adr");
    writeFileSync(path.join(root, "harness/config/x.yaml"), "limit: 999\n");
    expect(protCodes(root)).toEqual(["PROT001"]);
  });

  test("ratchet files are exempt and each names an existing guard", () => {
    const root = protectedRepo();
    writeFileSync(path.join(root, "harness/config/debt-baseline.json"), '{"a": 1}\n');
    expect(protCodes(root)).toEqual([]);
    for (const guard of Object.values(RATCHET_FILES))
      expect(guard).toMatch(/^[a-z-]+ [A-Z]+\d{3}$/);
  });

  test("CODEOWNERS must cover every protected path", () => {
    const root = protectedRepo();
    writeFileSync(path.join(root, ".github/CODEOWNERS"), "/harness/** @o\n");
    expect(protectedRun(root).map((v) => v.code)).toEqual(["PROT003"]);
    expect(protectedRun(REAL)).toEqual([]);
  });
});
