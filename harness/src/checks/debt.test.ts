import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { findRoot } from "../common";
import { commitAll, miniRepo } from "../test-support/mini-repo";
import { complexity, functions, parse } from "../ts-ast";
import { checkBaselineAgainst, loadDebtConfig, main, ratchet, run } from "./debt";

const TODAY = "2026-10-28";
const realConfig = readFileSync(path.join(findRoot(), "harness/config/debt.yaml"), "utf8");

function item(id = "TD-0001", fields: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id,
    title: "t",
    severity: "low",
    cost: "slow",
    created: "2026-10-01",
    due: "2026-12-01",
    status: "open",
    ...fields,
  };
}

function debtRepo(
  files: Record<string, string> = {},
  items: Array<Record<string, unknown>> = [],
): string {
  return miniRepo(
    {
      "harness/config/debt.yaml": realConfig,
      "harness/config/debt-baseline.json": "{}\n",
      "docs/exec-plans/tech-debt.yaml": YAML.stringify({ items }),
      "packages/core/src/a.ts": "export const x = 1;\n",
      ...files,
    },
    { "package.json": "{}\n" },
  );
}

const codes = (root: string): string[] => [...new Set(run(root, TODAY).map((v) => v.code))].sort();
const longFunction = (lines: number) =>
  `export function f(): number {\n${"  let x = 1;\n".repeat(lines)}  return 1;\n}\n`;
const branchy = (branches: number) =>
  `export function f(x: number): number {\n${Array.from({ length: branches }, (_, i) => `  if (x === ${i}) return ${i};\n`).join("")}  return 0;\n}\n`;

describe("debt markers [REQ-022]", () => {
  test("a clean repo passes", () => {
    expect(run(debtRepo(), TODAY)).toEqual([]);
  });

  test("the real repository's debt is under control", () => {
    expect(run(findRoot())).toEqual([]);
  });

  test.each(["TODO", "FIXME", "HACK", "XXX"])("untracked %s fails", (marker) => {
    expect(
      codes(debtRepo({ "packages/core/src/a.ts": `export const x = 1; // ${marker} later\n` })),
    ).toEqual(["DEBT001"]);
  });

  test("tracked markers pointing at open items pass; unknown or closed items fail", () => {
    const tracked = { "packages/core/src/a.ts": "export const x = 1; // TODO(TD-0001) split\n" };
    expect(run(debtRepo(tracked, [item()]), TODAY)).toEqual([]);
    expect(
      codes(
        debtRepo({ "packages/core/src/a.ts": "// TODO(TD-0009) x\nexport const x = 1;\n" }, [
          item(),
        ]),
      ),
    ).toEqual(["DEBT002"]);
    expect(
      codes(debtRepo(tracked, [item("TD-0001", { status: "done", resolution: "r" })])),
    ).toEqual(["DEBT013"]);
  });

  test("markers inside strings are not comments; block comments are", () => {
    expect(
      run(debtRepo({ "packages/core/src/a.ts": 'export const x = "TODO later";\n' }), TODAY),
    ).toEqual([]);
    expect(
      codes(debtRepo({ "packages/core/src/a.ts": "/* FIXME */\nexport const x = 1;\n" })),
    ).toEqual(["DEBT001"]);
  });

  test("markers in tests count too", () => {
    expect(
      codes(debtRepo({ "tests/spec/x.test.ts": "// HACK fast path\nexport const t = 1;\n" })),
    ).toEqual(["DEBT001"]);
  });
});

describe("debt register [REQ-022]", () => {
  test.each([
    ["missing fields", [{ id: "TD-0001", title: "t" }]],
    ["duplicate ids", [item(), item()]],
    ["bad id", [item("TD-1")]],
    ["bad severity", [item("TD-0001", { severity: "huge" })]],
    ["bad date", [item("TD-0001", { due: "soon" })]],
    ["closed without resolution", [item("TD-0001", { status: "done" })]],
  ])("%s fails", (_name, items) => {
    expect(codes(debtRepo({}, items))).toEqual(["DEBT010"]);
  });

  test("overdue open items fail, overdue closed items do not", () => {
    expect(codes(debtRepo({}, [item("TD-0001", { due: "2026-10-01" })]))).toEqual(["DEBT011"]);
    expect(
      run(
        debtRepo({}, [item("TD-0001", { due: "2026-10-01", status: "done", resolution: "r" })]),
        TODAY,
      ),
    ).toEqual([]);
  });

  test("the open-debt budget is enforced, for high severity too", () => {
    const many = Array.from({ length: 13 }, (_, i) => item(`TD-${String(i + 1).padStart(4, "0")}`));
    expect(codes(debtRepo({}, many))).toEqual(["DEBT012"]);
    const high = Array.from({ length: 3 }, (_, i) => item(`TD-000${i + 1}`, { severity: "high" }));
    expect(codes(debtRepo({}, high))).toEqual(["DEBT012"]);
  });
});

describe("size and complexity [REQ-022]", () => {
  test("long files, long functions, complex functions and long parameter lists fail", () => {
    expect(
      codes(debtRepo({ "packages/core/src/a.ts": "export const x = 1;\n".repeat(501) })),
    ).toEqual(["DEBT020"]);
    expect(codes(debtRepo({ "packages/core/src/a.ts": longFunction(85) }))).toEqual(["DEBT021"]);
    expect(codes(debtRepo({ "packages/core/src/a.ts": branchy(13) }))).toEqual(["DEBT022"]);
    expect(
      codes(
        debtRepo({
          "packages/core/src/a.ts":
            "export function f(a: 1, b: 1, c: 1, d: 1, e: 1, g: 1, h: 1) {\n  return a;\n}\n",
        }),
      ),
    ).toEqual(["DEBT023"]);
  });

  test("methods and arrow functions are measured; `this` parameters do not count", () => {
    const method = `export class C {\n  m(this: C, a: 1, b: 1, c: 1, d: 1, e: 1, f: 1) {\n    return a;\n  }\n}\n`;
    expect(run(debtRepo({ "packages/core/src/a.ts": method }), TODAY)).toEqual([]);
    const arrow = `export const f = (x: number): number => {\n${Array.from({ length: 13 }, (_, i) => `  if (x === ${i}) return ${i};\n`).join("")}  return 0;\n};\n`;
    expect(codes(debtRepo({ "packages/core/src/a.ts": arrow }))).toEqual(["DEBT022"]);
  });

  test("test files are exempt from size limits", () => {
    expect(run(debtRepo({ "packages/core/src/a.test.ts": longFunction(150) }), TODAY)).toEqual([]);
  });

  test("a waiver pointing at an open item tolerates a violation; a closed item does not", () => {
    const waived = longFunction(85).replace(
      "export function f(): number {",
      "export function f(): number { // debt-ok: TD-0001",
    );
    expect(run(debtRepo({ "packages/core/src/a.ts": waived }, [item()]), TODAY)).toEqual([]);
    const closed = debtRepo({ "packages/core/src/a.ts": waived }, [
      item("TD-0001", { status: "done", resolution: "r" }),
    ]);
    expect(codes(closed)).toEqual(["DEBT013", "DEBT021"]);
  });

  test("a file-level waiver on line 1 covers the file length", () => {
    const waived = `// debt-ok: TD-0001\n${"export const x = 1;\n".repeat(510)}`;
    expect(run(debtRepo({ "packages/core/src/a.ts": waived }, [item()]), TODAY)).toEqual([]);
  });

  test("a waiver that nothing needs fails", () => {
    expect(
      codes(
        debtRepo({ "packages/core/src/a.ts": "export const x = 1; // debt-ok: TD-0001\n" }, [
          item(),
        ]),
      ),
    ).toEqual(["DEBT032"]);
  });
});

describe("ratchet baseline [REQ-022]", () => {
  const writeBaseline = (root: string, data: Record<string, number>) =>
    writeFileSync(path.join(root, "harness/config/debt-baseline.json"), JSON.stringify(data));

  test("baselined violations are tolerated until they grow", () => {
    const root = debtRepo({ "packages/core/src/a.ts": branchy(13) });
    writeBaseline(root, { "packages/core/src/a.ts::f::complexity": 14 });
    expect(run(root, TODAY)).toEqual([]);
    writeFileSync(path.join(root, "packages/core/src/a.ts"), branchy(15));
    expect(codes(root)).toEqual(["DEBT031"]);
  });

  test("fixed entries must be removed, and ratchet removes them and lowers values", () => {
    const root = debtRepo({ "packages/core/src/a.ts": branchy(13) });
    writeBaseline(root, {
      "packages/core/src/a.ts::f::complexity": 20,
      "packages/core/src/a.ts::gone::lines": 99,
    });
    expect(codes(root)).toEqual(["DEBT030"]);
    expect(ratchet(root)).toEqual(["packages/core/src/a.ts::gone::lines"]);
    expect(
      JSON.parse(readFileSync(path.join(root, "harness/config/debt-baseline.json"), "utf8")),
    ).toEqual({
      "packages/core/src/a.ts::f::complexity": 14,
    });
  });

  test.each([
    [{ k: 14 }, { k: 13 }, []],
    [{ k: 14 }, {}, []],
    [{ k: 14 }, { k: 15 }, ["DEBT033"]],
    [{}, { k: 14 }, ["DEBT033"]],
  ])("against the base ref the baseline may only shrink (%o -> %o)", (before, after, expected) => {
    const root = debtRepo();
    writeBaseline(root, before);
    commitAll(root);
    writeBaseline(root, after);
    expect(checkBaselineAgainst(root, loadDebtConfig(root), "HEAD").map((v) => v.code)).toEqual(
      expected,
    );
  });

  test("the CLI ratchets and checks", () => {
    const root = debtRepo();
    const lines: string[] = [];
    expect(main(["ratchet", "--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines).toEqual(["ratchet: removed 0 fixed baseline entries"]);
    expect(main(["--root", root], (l) => lines.push(l))).toBe(0);
  });
});

describe("complexity measurement [REQ-022]", () => {
  test.each([
    ["function f() { return 1; }", 1],
    ["function f(x) { if (x) { return 1; } return 2; }", 2],
    ["function f(x, y) { if (x && y) return 1; return x ?? y; }", 4],
    ["function f(x) { for (const i of x) { while (i) {} } }", 3],
    ["function f(x) { try { } catch { } return x ? 1 : 2; }", 3],
    ["function f(x) { switch (x) { case 1: return 1; case 2: return 2; default: return 0; } }", 3],
    ["function f(x) { const g = () => { if (x) return 1; }; return g; }", 1],
  ])("%s has complexity %i", (source, expected) => {
    const fn = functions(parse("a.ts", source))[0];
    expect(fn && complexity(fn.node)).toBe(expected);
  });
});
