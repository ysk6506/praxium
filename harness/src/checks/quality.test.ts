import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { afterEach, describe, expect, test } from "vitest";
import YAML from "yaml";
import { findRoot } from "../common";
import { miniRepo } from "../test-support/mini-repo";
import { writeRecord } from "./doc-records";
import {
  gradeOf,
  main,
  recordCoverage,
  render,
  renderTable,
  report,
  run,
  sorted,
  write,
} from "./quality";
import { gatherFacts, NO_UNIT, RECORD_FILE, type UnitFacts } from "./quality-facts";

const real = (file: string): string => readFileSync(path.join(findRoot(), file), "utf8");
const TABLE = "docs/generated/quality.md";

const CLEAN: UnitFacts = {
  unit: "core.world",
  floor: 98,
  percent: 98.5,
  ignores: 0,
  near: 0,
  over: 0,
  debtOpen: 0,
  debtHigh: 0,
  earliestDue: undefined,
  oldestDoc: undefined,
};
const facts = (change: Partial<UnitFacts>): UnitFacts => ({ ...CLEAN, ...change });

describe("quality grades follow from measured values [REQ-024]", () => {
  test.each([
    ["recorded coverage below its floor", { percent: 97.9 }, "D"],
    ["a file or function over a limit", { over: 1, near: 1 }, "D"],
    ["an open high-severity debt item", { debtOpen: 1, debtHigh: 1 }, "D"],
    ["two open debt items", { debtOpen: 2 }, "C"],
    ["five files or functions above 90% of a limit", { near: 5 }, "C"],
    ["ten ignores", { ignores: 10 }, "C"],
    ["one open debt item", { debtOpen: 1 }, "B"],
    ["four files or functions above 90% of a limit", { near: 4 }, "B"],
    ["one ignore", { ignores: 1 }, "B"],
    ["nine ignores", { ignores: 9 }, "B"],
    ["coverage not recorded", { percent: undefined }, "B"],
    ["coverage exactly at the floor, nothing else", { percent: 98 }, "A"],
    ["nothing at all", {}, "A"],
    [
      "a unit whose coverage is not measured, nothing else",
      { floor: undefined, percent: undefined },
      "A",
    ],
  ] as const)("%s", (_name, change, grade) => {
    expect(gradeOf(facts(change)).grade).toBe(grade);
  });

  test("the reasons name what to fix, worst class first; an A has none", () => {
    expect(gradeOf(facts({ percent: 90, over: 2, debtOpen: 1, debtHigh: 1 })).why).toEqual([
      "커버리지가 하한 아래",
      "한도 초과 2",
      "열린 고위험 부채 1",
    ]);
    expect(gradeOf(facts({ debtOpen: 3, near: 6, ignores: 12 })).why).toEqual([
      "열린 부채 3",
      "한도 90% 넘음 6",
      "끄기 12",
    ]);
    expect(gradeOf(facts({ percent: undefined, ignores: 1 })).why).toEqual([
      "끄기 1",
      "커버리지 모름",
    ]);
    expect(gradeOf(CLEAN).why).toEqual([]);
  });

  test("the lowest grade comes first and equal grades keep their order", () => {
    const rows = [
      facts({ unit: "a" }),
      facts({ unit: "b", ignores: 1 }),
      facts({ unit: "c", over: 1 }),
      facts({ unit: "d", debtOpen: 2 }),
      facts({ unit: "e", ignores: 2 }),
    ];
    expect(sorted(rows).map((f) => f.unit)).toEqual(["c", "d", "b", "e", "a"]);
  });

  test("the table prints the rule above the rows, and `모름` for a unit without a record", () => {
    const text = renderTable(
      [facts({ unit: "core.world", percent: undefined })],
      { "biome-ignore": 3 },
      undefined,
    );
    expect(text.indexOf("## 등급 규칙")).toBeLessThan(text.indexOf("| core.world |"));
    expect(text).toContain("| core.world | B | 모름 / 98.0 |");
    expect(text).toContain('커버리지 기록: 없음 (모든 단위가 "모름")');
    expect(text).toContain("biome-ignore 3, eslint-disable 0");
    const known = renderTable(
      [
        facts({
          percent: 98.26,
          debtOpen: 1,
          earliestDue: "2026-12-08",
          oldestDoc: { date: "2026-11-01", file: "docs/a/guide.md" },
        }),
      ],
      {},
      "2026-11-11",
    );
    expect(known).toContain("| core.world | B | 98.3 / 98.0 |");
    expect(known).toContain("| 1 (2026-12-08) | 2026-11-01 (guide.md) |");
    expect(known).toContain("커버리지 기록: 2026-11-11");
    expect(renderTable([facts({ floor: undefined, percent: undefined })], {}, undefined)).toContain(
      "| — |",
    );
  });
});

const MEMBER = "packages/core/src/world/index.ts";

function repo(
  files: Record<string, string> = {},
  items: Array<Record<string, unknown>> = [],
): string {
  return miniRepo({
    "harness/config/ignores.yaml": real("harness/config/ignores.yaml"),
    "harness/config/ignores-baseline.json": '{"biome-ignore": 1}\n',
    "harness/config/debt.yaml": real("harness/config/debt.yaml"),
    "harness/config/docs.yaml": real("harness/config/docs.yaml"),
    "harness/config/tests.yaml": real("harness/config/tests.yaml"),
    "harness/config/coverage-floor.json": '{"core.world": 98, "server.http": 97}\n',
    "docs/exec-plans/tech-debt.yaml": YAML.stringify({ items }),
    ...files,
  });
}

const item = (id: string, fields: Record<string, unknown> = {}) => ({
  id,
  title: "t",
  severity: "low",
  cost: "c",
  created: "2026-11-01",
  due: "2026-12-08",
  status: "open",
  ...fields,
});

const doc = (_verified: string, covers: string[], extra = "kind: guide\nstatus: active") =>
  `---\ntitle: T\n${extra}\ncovers:\n${covers.map((c) => `  - ${c}`).join("\n")}\n---\n# T\n`;

/** Verification now lives in records (ADR-0194): one record per document is enough for the date. */
const stamp = (root: string, dates: Record<string, string>): void => {
  for (const [file, date] of Object.entries(dates))
    writeRecord(root, {
      doc: file,
      target: "package.json",
      hash: "h",
      date,
      note: "n",
      text_changed: true,
      doc_hash: "d",
    });
};

const factsOf = (root: string, unit: string) => {
  const found = gatherFacts(root, []).find((f) => f.unit === unit);
  if (!found) throw new Error(`no row for ${unit}`);
  return found;
};

describe("the quality table is generated and kept fresh [REQ-024]", () => {
  afterEach(() => {
    delete process.env.PRAXIUM_TODAY;
  });

  const STALE = "NOTE quality [QUAL001] 낡음: pnpm quality write";

  test("a fresh table is silent; a missing one is a note, not a failure (QUAL001)", () => {
    const root = repo();
    expect(report(root)).toEqual({ violations: [], notes: [STALE] });
    write(root);
    expect(report(root)).toEqual({ violations: [], notes: [] });
    expect(run(root)).toEqual([]);
    expect(readFileSync(path.join(root, TABLE), "utf8")).toBe(render(root));
  });

  test.each([
    [
      "an ignore appears",
      "packages/core/src/world/x.ts",
      "// biome-ignore lint/a11y/noAutofocus: focus on purpose\nexport const x = 1;\n",
    ],
    [
      "a file grows near its limit",
      "packages/core/src/world/big.ts",
      `${"export const a = 1;\n".repeat(460)}`,
    ],
    [
      "a function becomes complex",
      "packages/core/src/world/fn.ts",
      `export function f(x: number): number {\n${"  if (x > 1) return 1;\n".repeat(12)}  return 0;\n}\n`,
    ],
    [
      "a debt comment cites an item",
      "packages/core/src/world/d.ts",
      "// TODO(TD-0001): later\nexport const d = 1;\n",
    ],
  ])("when %s, the committed table is stale: a note, never a failure", (_name, file, text) => {
    const root = repo({}, [item("TD-0001")]);
    write(root);
    writeFileSync(path.join(root, file), text);
    expect(report(root)).toEqual({ violations: [], notes: [STALE] });
    write(root);
    expect(report(root).notes).toEqual([]);
  });

  test("every unit gets a grade, and a table that cannot be generated fails the check", () => {
    const root = repo();
    const grades = render(root)
      .split("\n")
      .filter((line) => /^\| [\w.()가-힣 ]+ \| [ABCD] \|/.test(line))
      .map((line) => line.split("|")[1]?.trim());
    expect(grades).toHaveLength(8);
    expect(grades).toContain(NO_UNIT);
    writeFileSync(path.join(root, "docs/exec-plans/tech-debt.yaml"), "items: [: bad");
    expect(() => report(root)).toThrow();
  });

  test("the table does not depend on today's date", () => {
    const root = repo({}, [item("TD-0001")]);
    process.env.PRAXIUM_TODAY = "2026-11-12";
    const first = render(root);
    process.env.PRAXIUM_TODAY = "2031-03-04";
    expect(render(root)).toBe(first);
    expect(first).not.toContain("2031-03-04");
  });

  test("ignores count in the unit that holds the file; CI ignores fall outside every unit", () => {
    const root = repo({
      "packages/core/src/world/x.ts":
        "// biome-ignore lint/a11y/noAutofocus: focus on purpose\nexport const x = 1;\n",
      ".github/workflows/ci.yml": "jobs:\n  a:\n    continue-on-error: true # TD-0001 why\n",
    });
    const found = gatherFacts(root, [
      {
        kind: "biome-ignore",
        file: "packages/core/src/world/x.ts",
        line: 1,
        rules: [],
        own: "",
        above: "",
      },
      {
        kind: "continue-on-error",
        file: ".github/workflows/ci.yml",
        line: 3,
        rules: [],
        own: "",
        above: "",
      },
    ]);
    expect(found.find((f) => f.unit === "core.world")?.ignores).toBe(1);
    expect(found.find((f) => f.unit === NO_UNIT)?.ignores).toBe(1);
  });

  test("file and function size: above 90% is near, above the limit is over", () => {
    const root = repo({
      "packages/core/src/world/near.ts": "export const a = 1;\n".repeat(451),
      "packages/core/src/world/over.ts": "export const a = 1;\n".repeat(501),
      "packages/core/src/world/small.ts": "export const a = 1;\n".repeat(450),
      "packages/core/src/world/long-fn.ts": `export function f(): number {\n${"  let x = 1;\n".repeat(75)}  return 1;\n}\n`,
      "packages/core/src/world/params.ts":
        "export function g(a: 1, b: 2, c: 3, d: 4, e: 5, f: 6, h: 7): number {\n  return a + b + c + d + e + f + h;\n}\n",
      "packages/core/src/world/a.test.ts": "export const t = 1;\n".repeat(600),
      "packages/core/src/world/cx.ts": `export function c(x: number): number {\n${"  if (x > 1) return 1;\n".repeat(10)}  return 0;\n}\n`,
      "harness/src/big.ts": "export const a = 1;\n".repeat(451),
    });
    const row = factsOf(root, "core.world");
    expect(row.near).toBe(5);
    expect(row.over).toBe(2);
    expect(factsOf(root, NO_UNIT)).toMatchObject({ near: 1, over: 0 });
  });

  test("debt is placed by `units` and by the comments that cite the item; the rest is outside every unit", () => {
    const root = repo(
      {
        "apps/server/src/http/h.ts": "// TODO(TD-0002): later\nexport const h = 1;\n",
        "harness/src/x.ts": "// TODO(TD-0006): later\nexport const x = 1;\n",
      },
      [
        item("TD-0001", { units: ["core.world"], severity: "high", due: "2026-12-01" }),
        item("TD-0002", { due: "2026-12-15" }),
        item("TD-0003", { due: "2026-11-30" }),
        item("TD-0004", { status: "done", resolution: "done" }),
        item("TD-0005", { units: ["core.world"], due: "2026-12-09" }),
        item("TD-0006", { due: "2026-11-20" }),
      ],
    );
    expect(factsOf(root, "core.world")).toMatchObject({
      debtOpen: 2,
      debtHigh: 1,
      earliestDue: "2026-12-01",
    });
    expect(factsOf(root, "server.http")).toMatchObject({ debtOpen: 1, earliestDue: "2026-12-15" });
    expect(factsOf(root, NO_UNIT)).toMatchObject({ debtOpen: 2, earliestDue: "2026-11-20" });
    expect(factsOf(root, "core.kernel").debtOpen).toBe(0);
  });

  test("a debt item may only name units that exist (QUAL003)", () => {
    const root = repo({}, [
      item("TD-0001", { units: ["core.nowhere"] }),
      item("TD-0002", { units: [NO_UNIT, "core.world"] }),
    ]);
    write(root);
    expect(run(root).map((v) => v.code)).toEqual(["QUAL003"]);
  });

  test("the oldest active doc that covers a unit's code is shown; ADRs, old status and uncovered docs are not", () => {
    const root = repo({
      "docs/design-docs/new.md": doc("2026-11-09", [MEMBER]),
      "docs/design-docs/old.md": doc("2026-10-01", ["packages/core/src/world/*.ts"]),
      "docs/design-docs/dir.md": doc("2026-10-15", ["packages/core/src/world/"]),
      "docs/design-docs/adr/0001-x.md": doc(
        "2026-01-01",
        [MEMBER],
        "kind: decision\nstatus: active",
      ),
      "docs/design-docs/archived.md": doc("2026-01-02", [MEMBER], "kind: guide\nstatus: archived"),
      "docs/design-docs/free.md": "---\ntitle: T\nkind: guide\nstatus: active\n---\n# T\n",
      "docs/generated/ignored.md": doc("2026-01-04", [MEMBER]),
      "docs/design-docs/other.md": doc("2026-09-01", ["apps/server/src/http/index.ts"]),
      "docs/design-docs/outside.md": doc("2026-08-01", ["package.json"]),
      "docs/design-docs/undated.md":
        "---\ntitle: T\nkind: guide\nstatus: active\ncovers:\n  - apps/server/src/main.ts\n---\n# T\n",
    });
    stamp(root, {
      "docs/design-docs/new.md": "2026-11-09",
      "docs/design-docs/old.md": "2026-10-01",
      "docs/design-docs/dir.md": "2026-10-15",
      "docs/design-docs/archived.md": "2026-01-02",
      "docs/generated/ignored.md": "2026-01-04",
      "docs/design-docs/other.md": "2026-09-01",
      "docs/design-docs/outside.md": "2026-08-01",
    });
    expect(factsOf(root, "core.world").oldestDoc).toEqual({
      date: "2026-10-01",
      file: "docs/design-docs/old.md",
    });
    expect(factsOf(root, "server.http").oldestDoc?.date).toBe("2026-09-01");
    expect(factsOf(root, "core.kernel").oldestDoc).toBeUndefined();
    expect(factsOf(root, NO_UNIT).oldestDoc?.date).toBe("2026-08-01");
    expect(factsOf(root, "server.main").oldestDoc).toBeUndefined();
  });
});

describe("recorded coverage feeds the table, nothing is invented [REQ-024]", () => {
  afterEach(() => {
    delete process.env.PRAXIUM_TODAY;
  });

  const record = (units: Record<string, unknown>) => ({
    [RECORD_FILE]: JSON.stringify({ measuredOn: "2026-11-11", units }),
  });

  test("without a record every measured unit is unknown, units without a floor show a dash", () => {
    const text = render(repo());
    expect(text).toContain("| core.world | B | 모름 / 98.0 |");
    expect(text).toContain("| core.kernel | A | — |");
  });

  test("a recorded value is shown against the floor; below it the unit is a D", () => {
    const ok = repo(record({ "core.world": 98.5, "server.http": 96.5 }));
    expect(factsOf(ok, "core.world").percent).toBe(98.5);
    expect(gradeOf(factsOf(ok, "core.world")).grade).toBe("A");
    expect(gradeOf(factsOf(ok, "server.http")).grade).toBe("D");
    expect(render(ok)).toContain("| server.http | D | 96.5 / 97.0 |");
  });

  test.each([
    ["not an object", '"x"'],
    ["no date", '{"units": {}}'],
    ["a value that is not a number", '{"measuredOn": "2026-11-11", "units": {"core.world": "98"}}'],
    ["no units", '{"measuredOn": "2026-11-11"}'],
  ])("a malformed record fails (QUAL002): %s", (_name, text) => {
    expect(run(repo({ [RECORD_FILE]: text })).map((v) => v.code)).toEqual(["QUAL002"]);
  });

  test("recording reads the coverage summary and writes one rounded value per unit", () => {
    process.env.PRAXIUM_TODAY = "2026-11-12";
    const counter = (covered: number, total: number) => ({ covered, total });
    const root = repo({
      "coverage/coverage-summary.json": JSON.stringify({
        total: { lines: counter(1, 1), branches: counter(1, 1) },
        "packages/core/src/world/a.ts": { lines: counter(9, 10), branches: counter(8, 10) },
        "packages/core/src/world/b.ts": { lines: counter(10, 10), branches: counter(9, 10) },
        "apps/server/src/http/h.ts": { lines: counter(7, 8), branches: counter(0, 0) },
      }),
    });
    const text = recordCoverage(root);
    expect(JSON.parse(text)).toEqual({
      measuredOn: "2026-11-12",
      units: { "core.world": 90, "server.http": 87.5 },
    });
    expect(readFileSync(path.join(root, RECORD_FILE), "utf8")).toBe(text);
    expect(factsOf(root, "core.world").percent).toBe(90);
  });

  test("recording without a coverage summary stops instead of guessing", () => {
    expect(() => recordCoverage(repo())).toThrow("coverage summary missing");
  });

  test("the CLI writes, records and checks", () => {
    const root = repo({
      "coverage/coverage-summary.json": JSON.stringify({
        "packages/core/src/world/a.ts": {
          lines: { covered: 1, total: 1 },
          branches: { covered: 1, total: 1 },
        },
      }),
    });
    const lines: string[] = [];
    expect(main(["--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines.at(-1)).toBe("NOTE quality [QUAL001] 낡음: pnpm quality write");
    expect(main(["print", "--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines.at(-1)).toBe(render(root));
    expect(existsSync(path.join(root, TABLE))).toBe(false);
    expect(main(["write", "--root", root], (l) => lines.push(l))).toBe(0);
    expect(existsSync(path.join(root, TABLE))).toBe(true);
    expect(main(["record", "--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines.join("\n")).toContain('"core.world": 100');
    expect(main(["--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines.at(-1)).toContain("QUAL001");
    expect(main(["write", "--root", root], (l) => lines.push(l))).toBe(0);
    expect(main(["--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines.at(-1)).toBe("OK   quality");
  });
});
