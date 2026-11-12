import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, test } from "vitest";
import { main as architectureMain } from "./checks/architecture";
import { main as docsMain } from "./checks/docs";
import { main as protectedMain } from "./checks/protected";
import { main as traceabilityMain } from "./checks/traceability";
import {
  daysBetween,
  findRoot,
  finish,
  git,
  isIsoDate,
  isMain,
  joinFrontMatter,
  parseArgs,
  render,
  splitFrontMatter,
  today,
  violation,
} from "./common";
import { expandGlobs, globToRegExp, isTestFile } from "./files";
import { checks, main as runAllMain } from "./run-all";
import { miniRepo } from "./test-support/mini-repo";

describe("harness common helpers [REQ-024]", () => {
  afterEach(() => {
    delete process.env.PRAXIUM_TODAY;
  });

  test("violations render with location, problem and fix", () => {
    const text = render(violation("X001", "a/b.ts", 7, "bad", "do this"));
    expect(text).toBe("[X001] a/b.ts:7\n    problem: bad\n    fix: do this");
    expect(render(violation("X001", "a/b.ts", 0, "bad", "fix"))).toMatch(/^\[X001\] a\/b\.ts\n/);
  });

  test("finish prints OK, or sorted failures", () => {
    const lines: string[] = [];
    expect(finish("demo", [], (l) => lines.push(l))).toBe(0);
    expect(
      finish("demo", [violation("B", "z", 1, "m", "f"), violation("A", "a", 1, "m", "f")], (l) =>
        lines.push(l),
      ),
    ).toBe(1);
    expect(lines[0]).toBe("OK   demo");
    expect(lines[1]).toBe("FAIL demo: 2 violation(s)\n");
    expect(lines[2]?.startsWith("[A]")).toBe(true);
  });

  test("findRoot walks up and fails outside a repo", () => {
    expect(findRoot(path.join(findRoot(), "harness", "src"))).toBe(findRoot());
    expect(() => findRoot(mkdtempSync(path.join(tmpdir(), "nowhere-")))).toThrow(
      /repo root not found/,
    );
  });

  test("today can be pinned, and a bad pin is rejected", () => {
    process.env.PRAXIUM_TODAY = "2030-01-02";
    expect(today()).toBe("2030-01-02");
    process.env.PRAXIUM_TODAY = "soon";
    expect(() => today()).toThrow(/YYYY-MM-DD/);
    delete process.env.PRAXIUM_TODAY;
    expect(isIsoDate(today())).toBe(true);
  });

  test.each([
    ["2026-10-28", true],
    ["2026-02-30", false],
    ["26-10-28", false],
    [20261028, false],
  ])("isIsoDate(%s) = %s", (value, expected) => {
    expect(isIsoDate(value)).toBe(expected);
  });

  test("daysBetween counts whole days", () => {
    expect(daysBetween("2026-10-01", "2026-10-28")).toBe(27);
    expect(daysBetween("2026-10-28", "2026-10-01")).toBe(-27);
  });

  test("front matter splits and joins", () => {
    const { data, body, bodyLine } = splitFrontMatter("---\na: 1\n---\nhello\n");
    expect(data).toEqual({ a: 1 });
    expect(body).toBe("hello\n");
    expect(bodyLine).toBe(4);
    expect(splitFrontMatter(joinFrontMatter({ b: "x" }, "\n\nbody")).data).toEqual({ b: "x" });
  });

  test.each([
    "no front matter",
    "---\nnever closed\n",
    "---\n- a\n---\nbody",
    "---\na: [\n---\nbody",
  ])("unusable front matter gives null: %j", (text) => {
    expect(splitFrontMatter(text).data).toBeNull();
  });

  test("git returns null outside a repository", () => {
    expect(git(mkdtempSync(path.join(tmpdir(), "nogit-")), "rev-parse", "HEAD")).toBeNull();
  });

  test("parseArgs separates positional arguments, valued flags and boolean flags", () => {
    const { positional, flags } = parseArgs([
      "lock",
      "--amend",
      "x.md",
      "--with-coverage",
      "--root",
      "/r",
    ]);
    expect(positional).toEqual(["lock"]);
    expect(Object.fromEntries(flags)).toEqual({ amend: "x.md", "with-coverage": true, root: "/r" });
  });

  test("isMain is false for imported modules", () => {
    expect(isMain(import.meta.url)).toBe(false);
  });
});

describe("file helpers [REQ-024]", () => {
  test.each([
    ["**/*.test.ts", "a/b/c.test.ts", true],
    ["**/*.test.ts", "c.test.ts", true],
    ["packages/*/src/**", "packages/core/src/a/b.ts", true],
    ["packages/*/src/**", "packages/core/lib/a.ts", false],
    ["a?.md", "ab.md", true],
    ["a.md", "aXmd", false],
  ])("glob %s matches %s: %s", (glob, file, expected) => {
    expect(globToRegExp(glob).test(file)).toBe(expected);
  });

  test("test files and test support are recognised", () => {
    expect(isTestFile("a/b.test.ts")).toBe(true);
    expect(isTestFile("a/b.test.tsx")).toBe(true);
    expect(isTestFile("a/test-support/x.ts")).toBe(true);
    expect(isTestFile("a/b.ts")).toBe(false);
  });

  test("expandGlobs matches files and whole directories", () => {
    const root = miniRepo({ "docs/a.md": "", "docs/sub/b.md": "", "src/c.ts": "" }, {});
    expect(expandGlobs(root, ["docs"])).toEqual(["docs/a.md", "docs/sub/b.md"]);
    expect(expandGlobs(root, ["**/*.ts"])).toEqual(["src/c.ts"]);
  });
});

describe("check entry points [REQ-024]", () => {
  const root = findRoot();
  const quiet = () => undefined;

  test.each([
    ["architecture", (argv: string[]) => architectureMain(argv)],
    ["docs", (argv: string[]) => docsMain(argv)],
  ])("%s CLI returns 0 or 1", (_name, run) => {
    expect([0, 1]).toContain(run(["--root", root]));
  });

  test("traceability and protected CLIs return 0 or 1", () => {
    expect([0, 1]).toContain(traceabilityMain(["--root", root], quiet));
    expect(protectedMain(["--root", root], quiet)).toBe(0);
  });

  test("run-all runs every check and prints a verdict", () => {
    const names = checks(root, undefined, true).map(([name]) => name);
    expect(names).toEqual([
      "architecture",
      "docs",
      "debt",
      "ignores",
      "test-integrity",
      "traceability",
      "ui-words",
      "quality",
      "protected-paths",
      "coverage",
    ]);
    expect(checks(root, undefined, false).map(([name]) => name)).not.toContain("coverage");
    const lines: string[] = [];
    const code = runAllMain(["--root", root], (l) => lines.push(l));
    const verdict = lines.at(-1) ?? "";
    expect(verdict.includes("ALL CHECKS PASSED")).toBe(code === 0);
    expect(verdict.includes("CHECK(S) FAILED")).toBe(code === 1);
    // Runs every check over the whole repository; a change that touches many documents takes longer than the default limit (ADR-0188).
  }, 120_000);
});
