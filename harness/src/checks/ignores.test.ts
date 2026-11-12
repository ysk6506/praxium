import { readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { findRoot } from "../common";
import { commitAll, miniRepo } from "../test-support/mini-repo";
import {
  checkBaselineAgainst,
  collect,
  counts,
  loadIgnoresConfig,
  main,
  ratchet,
  report,
  run,
} from "./ignores";
import { directiveOf, scanCss, scanSkips, scanTypeScript, scanWorkflow } from "./ignores-scan";

const real = (file: string): string => readFileSync(path.join(findRoot(), file), "utf8");
const BASELINE = "harness/config/ignores-baseline.json";

const debtItem = (id: string, status = "open") => ({
  id,
  title: "t",
  severity: "low",
  cost: "c",
  created: "2026-11-01",
  due: "2026-12-08",
  status,
  ...(status === "open" ? {} : { resolution: "done" }),
});

const adr = (name: string, front: string) => ({
  [`docs/design-docs/adr/${name}.md`]: `---\n${front}\n---\n# body\n`,
});

/** A small repository whose baseline already equals what `files` hold, so only the per-ignore rules speak. */
function repo(files: Record<string, string>): string {
  const root = miniRepo(
    {
      "harness/config/ignores.yaml": real("harness/config/ignores.yaml"),
      "harness/config/debt.yaml": real("harness/config/debt.yaml"),
      "docs/exec-plans/tech-debt.yaml": YAML.stringify({
        items: [
          debtItem("TD-0001"),
          debtItem("TD-0002", "done"),
          debtItem("TD-0013"),
          debtItem("TD-0014", "done"),
        ],
      }),
      ...adr("0100-sealed", "status: active\ndecision_hash: abc123"),
      ...adr("0101-unsealed", "status: active"),
      [BASELINE]: "{}\n",
      ...files,
    },
    { "package.json": "{}\n" },
  );
  setBaseline(root, counts(collect(root, loadIgnoresConfig(root))));
  return root;
}

const setBaseline = (root: string, value: object): void =>
  writeFileSync(path.join(root, BASELINE), `${JSON.stringify(value, null, 2)}\n`);

const codes = (root: string): string[] => [...new Set(run(root).map((v) => v.code))].sort();
const ts = (text: string) => ({ "packages/core/src/a.ts": text });
const spec = (text: string) => ({ "packages/core/src/a.test.ts": text });
const NOT_REASON_ONLY = "lint/suspicious/noExplicitAny";

describe("no silent ignores: suppression comments [REQ-022]", () => {
  test("a repository without ignores passes", () => {
    expect(run(repo(ts("export const x = 1;\n")))).toEqual([]);
  });

  test.each([
    ["biome-ignore", `// biome-ignore ${NOT_REASON_ONLY}\nexport const x: any = 1;\n`],
    ["biome-ignore", `// biome-ignore ${NOT_REASON_ONLY}: ok\nexport const x: any = 1;\n`],
    ["eslint-disable", "// eslint-disable-next-line no-console\nexport const x = 1;\n"],
    ["ts-ignore", "// @ts-ignore\nexport const x: number = 'a';\n"],
    ["ts-nocheck", "// @ts-nocheck\nexport const x = 1;\n"],
    ["ts-expect-error", "// @ts-expect-error\nexport const x: number = 'a';\n"],
  ])("%s without a reason fails (IGN001)", (_kind, body) => {
    expect(codes(repo(ts(body)))).toContain("IGN001");
  });

  test.each([
    [
      "biome-ignore",
      `// biome-ignore ${NOT_REASON_ONLY}: a throwaway driver reads loose JSON\nexport const x: any = 1;\n`,
    ],
    [
      "eslint-disable",
      "// eslint-disable-next-line no-console -- the entry point reports a failure\nexport const x = 1;\n",
    ],
    [
      "ts-ignore",
      "// @ts-ignore: generated code with a wrong declaration\nexport const x: number = 'a';\n",
    ],
    [
      "ts-expect-error",
      "// @ts-expect-error: the library types are wrong here\nexport const x: number = 'a';\n",
    ],
  ])("%s with a reason but no TD or ADR fails (IGN002)", (_kind, body) => {
    expect(codes(repo(ts(body)))).toEqual(["IGN002"]);
  });

  test("a reason with an open TD item on the same line or the line above passes", () => {
    const same = `// biome-ignore ${NOT_REASON_ONLY}: TD-0001 the payload type is not known yet\nexport const x: any = 1;\n`;
    expect(run(repo(ts(same)))).toEqual([]);
    const above = `// TD-0001: the payload type is not known yet\n// biome-ignore ${NOT_REASON_ONLY}: payload\nexport const x: any = 1;\n`;
    expect(run(repo(ts(above)))).toEqual([]);
    const far = `// TD-0001: the payload type is not known yet\n\n// biome-ignore ${NOT_REASON_ONLY}: payload type\nexport const x: any = 1;\n`;
    expect(codes(repo(ts(far)))).toEqual(["IGN002"]);
  });

  test("a closed or unknown debt item is not a basis: the debt is paid, the ignore stayed (IGN003)", () => {
    for (const id of ["TD-0002", "TD-0099"]) {
      const body = `// biome-ignore ${NOT_REASON_ONLY}: ${id} the payload type is not known\nexport const x: any = 1;\n`;
      expect(codes(repo(ts(body)))).toEqual(["IGN003"]);
    }
  });

  test("without an ADR folder or a baseline file, nothing is sealed and the baseline is empty", () => {
    const root = repo(ts("export const x = 1;\n"));
    rmSync(path.join(root, "docs/design-docs"), { recursive: true });
    rmSync(path.join(root, BASELINE));
    const cite = `// biome-ignore ${NOT_REASON_ONLY}: ADR-0100 loose JSON in a one-off driver\nexport const x: any = 1;\n`;
    writeFileSync(path.join(root, "packages/core/src/a.ts"), cite);
    expect(codes(root)).toEqual(["IGN004", "IGN005"]);
  });

  test("an ADR is a basis only when it exists, is active and sealed (IGN004)", () => {
    const cite = (id: string) =>
      `// biome-ignore ${NOT_REASON_ONLY}: ${id} loose JSON in a one-off driver\nexport const x: any = 1;\n`;
    expect(run(repo(ts(cite("ADR-0100"))))).toEqual([]);
    expect(codes(repo(ts(cite("ADR-0101"))))).toEqual(["IGN004"]);
    expect(codes(repo(ts(cite("ADR-0999"))))).toEqual(["IGN004"]);
  });

  test("rules listed as reason-only need a reason and nothing else; one rule outside the list does not", () => {
    const only =
      "// biome-ignore lint/suspicious/noArrayIndexKey: rows have no ids until saved\nexport const x = 1;\n";
    expect(run(repo(ts(only)))).toEqual([]);
    const css =
      "// biome-ignore lint/complexity/noImportantStyles: reduced motion must win\nexport const x = 1;\n";
    expect(run(repo(ts(css)))).toEqual([]);
    const mixed = `// biome-ignore lint/suspicious/noArrayIndexKey ${NOT_REASON_ONLY}: a dialog focuses its first field\nexport const x = 1;\n`;
    expect(codes(repo(ts(mixed)))).toEqual(["IGN002"]);
    const bare = "// biome-ignore lint/suspicious/noArrayIndexKey: x\nexport const x = 1;\n";
    expect(codes(repo(ts(bare)))).toEqual(["IGN001"]);
  });

  test("a rule with a basis of its own passes while that debt item is open and fails once it is closed (IGN003)", () => {
    const a11y =
      "// biome-ignore lint/a11y/noAutofocus: a dialog focuses its first field\nexport const x = 1;\n";
    expect(run(repo(ts(a11y)))).toEqual([]);
    const deps =
      "// biome-ignore lint/correctness/useExhaustiveDependencies: follow the value only\nexport const x = 1;\n";
    const closed = run(repo(ts(deps)));
    expect(closed.map((v) => v.code)).toEqual(["IGN003"]);
    expect(closed[0]?.message).toContain("TD-0014");
  });

  test("a rule with a basis of its own still needs a reason, and a rule without one needs a number", () => {
    const bare = "// biome-ignore lint/a11y/noAutofocus: x\nexport const x = 1;\n";
    expect(codes(repo(ts(bare)))).toEqual(["IGN001"]);
    const mixed = `// biome-ignore lint/a11y/noAutofocus ${NOT_REASON_ONLY}: a dialog focuses its first field\nexport const x = 1;\n`;
    expect(codes(repo(ts(mixed)))).toEqual(["IGN002"]);
  });

  test("a number on the line itself takes precedence over the rule's basis", () => {
    const own =
      "// biome-ignore lint/correctness/useExhaustiveDependencies: TD-0001 follow the value only\nexport const x = 1;\n";
    expect(run(repo(ts(own)))).toEqual([]);
  });

  test("@ts-expect-error in a test file needs a reason only; in other code it needs a basis too", () => {
    const body =
      "// @ts-expect-error: the type test asserts this compile error\nexport const x: number = 'a';\n";
    expect(run(repo(spec(body)))).toEqual([]);
    expect(codes(repo(ts(body)))).toEqual(["IGN002"]);
  });

  test("examples inside strings, templates and prose are not counted", () => {
    const body = [
      'export const a = "// @ts-ignore";',
      "export const b = `",
      "// biome-ignore lint/suspicious/noExplicitAny",
      "// eslint-disable-next-line",
      "`;",
      "// This note only mentions biome-ignore and @ts-ignore in passing.",
      "/**",
      " * @ts-nocheck is not valid here, it only appears inside a doc comment line.",
      " */",
      "export const c = 1;",
      "",
    ].join("\n");
    const root = repo(ts(body));
    expect(counts(collect(root, loadIgnoresConfig(root)))["biome-ignore"]).toBe(0);
    expect(run(root)).toEqual([]);
  });

  test("a block comment and a JSX comment are read like a line comment", () => {
    const body = `export const a = 1; /* biome-ignore ${NOT_REASON_ONLY} */\nexport const b = <p>{/* biome-ignore lint/a11y/noAutofocus: focus on purpose */}</p>;\n`;
    const found = scanTypeScript("a.tsx", body);
    expect(found.map((s) => [s.kind, s.rules[0]])).toEqual([
      ["biome-ignore", NOT_REASON_ONLY],
      ["biome-ignore", "lint/a11y/noAutofocus"],
    ]);
  });

  test("directives are told apart from other comments", () => {
    expect(directiveOf("// biome-ignore-end lint/x")).toBeUndefined();
    expect(directiveOf("// biome-ignore-start lint/x/y: a range")?.own).toBe("a range");
    expect(directiveOf("// biome-ignore format")?.rules).toEqual(["format"]);
    expect(directiveOf("// eslint-enable")).toBeUndefined();
    expect(directiveOf("// @ts-check")).toBeUndefined();
    expect(directiveOf("// plain note")).toBeUndefined();
  });
});

describe("no silent ignores: skipped tests [REQ-022]", () => {
  test.each([
    'test.skip("later", () => { expect(1).toBe(1); });',
    'describe.skip("suite", () => {});',
    'test.todo("someday");',
    'test.skipIf(true)("x", () => { expect(1).toBe(1); });',
    'test("x", (ctx) => { ctx.skip(); expect(1).toBe(1); });',
  ])("a skip without a reason fails (IGN001, IGN002): %s", (body) => {
    expect(codes(repo(spec(`${body}\n`)))).toEqual(["IGN001", "IGN002"]);
  });

  test("a skip whose title or comment cites an open debt item passes; a closed one fails", () => {
    const title = 'test.skip("TD-0001: needs a FalkorDB server", () => { expect(1).toBe(1); });\n';
    expect(run(repo(spec(title)))).toEqual([]);
    const above =
      '// TD-0001: needs a FalkorDB server on the runner\ntest.skip("x", () => { expect(1).toBe(1); });\n';
    expect(run(repo(spec(above)))).toEqual([]);
    const closed = 'test.skip("TD-0002: needs a FalkorDB server", () => { expect(1).toBe(1); });\n';
    expect(codes(repo(spec(closed)))).toEqual(["IGN003"]);
  });

  test("skips are read from the call, not from words; non-test files are left alone", () => {
    expect(
      scanSkips(
        "a.test.ts",
        'const s = "test.skip(1)";\ntest("a", () => { expect(1).toBe(1); });\n',
      ),
    ).toEqual([]);
    const root = repo(ts('export const o = { skip: (x: string) => x };\no.skip("a");\n'));
    expect(counts(collect(root, loadIgnoresConfig(root)))["test-skip"]).toBe(0);
  });
});

describe("no silent ignores: CSS and CI [REQ-022]", () => {
  test("a CSS comment is read, a string that looks like one is not", () => {
    const css = [
      '.a::before { content: "/* biome-ignore lint/style/x: not a comment */"; }',
      "/* biome-ignore lint/complexity/noImportantStyles: reduced motion must win */",
      ".b { animation: none !important; }",
      "/* plain */",
      "/* biome-ignore lint/style/x */",
      ".c { color: red; }",
      "",
    ].join("\n");
    const found = scanCss("a.css", css);
    expect(found.map((s) => [s.line, s.own])).toEqual([
      [2, "reduced motion must win"],
      [5, ""],
    ]);
    expect(codes(repo({ "apps/web/src/a.css": css }))).toEqual(["IGN001", "IGN002"]);
  });

  test("a CSS string that ends at the line break does not swallow the next lines", () => {
    const found = scanCss(
      "a.css",
      ".a { content: 'open\n}\n/* biome-ignore lint/style/x: a long enough reason */\n",
    );
    expect(found.map((s) => s.line)).toEqual([3]);
  });

  test("an escaped quote does not end a CSS string, and an unclosed comment runs to the end", () => {
    const css =
      '.a { content: "say \\"/* biome-ignore lint/style/x: no */\\""; }\n/* biome-ignore lint/style/y: never closed';
    expect(scanCss("a.css", css).map((s) => [s.line, s.rules[0]])).toEqual([[2, "lint/style/y"]]);
  });

  test("a CSS comment above counts as the reason", () => {
    const css =
      "/* ADR-0100: the vendor stylesheet is imported whole */\n/* biome-ignore lint/style/useImportType */\n.a {}\n";
    expect(run(repo({ "apps/web/src/a.css": css }))).toEqual([]);
  });

  const workflow = (line: string, before = "") => ({
    ".github/workflows/ci.yml": `jobs:\n  a:\n    steps:\n      - run: x\n${before}${line}\n`,
  });

  test.each([
    ["without a reason", "        continue-on-error: true"],
    [
      "with a reason but no number",
      "        continue-on-error: true # the step may fail without anyone minding",
    ],
    ["with a short comment", "        continue-on-error: true # TD-0001"],
  ])("continue-on-error %s fails", (_name, line) => {
    expect(codes(repo(workflow(line))).length).toBeGreaterThan(0);
  });

  test("continue-on-error passes with a reason and an open TD or sealed ADR; false is not an ignore", () => {
    expect(
      run(repo(workflow("        continue-on-error: true # TD-0001: floors are not gating yet"))),
    ).toEqual([]);
    expect(
      run(
        repo(
          workflow("        continue-on-error: true # ADR-0100: the shards decide pass or fail"),
        ),
      ),
    ).toEqual([]);
    const above = workflow(
      "        continue-on-error: true",
      "      # TD-0001: floors are not gating yet\n",
    );
    expect(run(repo(above))).toEqual([]);
    const off = repo(workflow("        continue-on-error: false"));
    expect(counts(collect(off, loadIgnoresConfig(off)))["continue-on-error"]).toBe(0);
    expect(scanWorkflow("a.yml", "    continue-on-error: true # TD-0001 why\n")[0]?.own).toBe(
      "TD-0001 why",
    );
  });

  test("a job-level continue-on-error citing a closed debt item fails (IGN003)", () => {
    const line = "    continue-on-error: true # TD-0002: floors are not gating yet";
    expect(codes(repo(workflow(line)))).toEqual(["IGN003"]);
  });
});

describe("no silent ignores: the count only falls [REQ-022]", () => {
  const withIgnore = ts(
    "// @ts-expect-error: TD-0001 the generated types are wrong\nexport const x: number = 'a';\n",
  );

  test("more ignores than the baseline fails (IGN005)", () => {
    const root = repo(withIgnore);
    setBaseline(root, { "ts-expect-error": 0 });
    expect(codes(root)).toEqual(["IGN005"]);
  });

  test("a kind missing from the baseline counts as zero", () => {
    const root = repo(withIgnore);
    setBaseline(root, {});
    expect(codes(root)).toEqual(["IGN005"]);
  });

  test("fewer ignores than the baseline passes with a note; ratchet lowers the baseline (IGN006)", () => {
    const root = repo(ts("export const x = 1;\n"));
    setBaseline(root, { "biome-ignore": 2, "continue-on-error": 1 });
    expect(codes(root)).toEqual([]);
    const { violations, notes } = report(root);
    expect(violations).toEqual([]);
    expect(notes).toHaveLength(2);
    expect(notes[0]).toContain("IGN006");
    expect(notes[0]).toContain("baseline still 2");
    expect(ratchet(root)).toEqual(["biome-ignore", "continue-on-error"]);
    expect(report(root)).toEqual({ violations: [], notes: [] });
  });

  test("ratchet never raises a count and never adds a kind", () => {
    const root = repo(withIgnore);
    setBaseline(root, { "ts-expect-error": 0 });
    expect(ratchet(root)).toEqual([]);
    expect(JSON.parse(readFileSync(path.join(root, BASELINE), "utf8"))["ts-expect-error"]).toBe(0);
  });

  test.each([
    [{ "biome-ignore": 3 }, { "biome-ignore": 4 }, ["IGN007"]],
    [{ "biome-ignore": 3 }, { "biome-ignore": 3 }, []],
    [{ "biome-ignore": 3 }, { "biome-ignore": 2 }, []],
    [{}, { "test-skip": 1 }, ["IGN007"]],
  ])("against the base ref the baseline may only shrink (%o -> %o)", (before, after, expected) => {
    const root = repo(ts("export const x = 1;\n"));
    setBaseline(root, before);
    commitAll(root);
    setBaseline(root, after);
    const config = loadIgnoresConfig(root);
    expect(checkBaselineAgainst(root, config, "HEAD").map((v) => v.code)).toEqual(expected);
    expect(run(root, "HEAD").filter((v) => v.code === "IGN007")).toHaveLength(expected.length);
  });

  test("without a baseline at the base ref nothing is compared", () => {
    const root = repo(ts("export const x = 1;\n"));
    commitAll(root);
    expect(checkBaselineAgainst(root, loadIgnoresConfig(root), "no-such-ref")).toEqual([]);
  });

  test("the CLI ratchets and checks", () => {
    const root = repo(ts("export const x = 1;\n"));
    const lines: string[] = [];
    expect(main(["ratchet", "--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines[0]).toBe("ratchet: lowered 0 ignore baseline(s)");
    setBaseline(root, { "biome-ignore": 1 });
    expect(main(["ratchet", "--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines[1]).toBe("ratchet: lowered 1 ignore baseline(s): biome-ignore");
    setBaseline(root, { "biome-ignore": 1 });
    expect(main(["--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines.at(-1)).toContain("NOTE ignores [IGN006]");
    expect(main(["ratchet", "--root", root], (l) => lines.push(l))).toBe(0);
    expect(main(["--root", root], (l) => lines.push(l))).toBe(0);
    expect(lines.at(-1)).toBe("OK   ignores");
  });
});
