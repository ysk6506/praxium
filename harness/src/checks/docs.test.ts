import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { findRoot } from "../common";
import { verifyDefaults } from "../test-support/docs-repo";
import { miniRepo } from "../test-support/mini-repo";
import { recordPath } from "./doc-records";
import { reverify, run, sealAdr } from "./docs";

const TODAY = "2026-10-28";
const MODULE =
  "/** Doc. */\nexport function add(a: number, b: number): number {\n  return a + b;\n}\n";
const realConfig = readFileSync(path.join(findRoot(), "harness/config/docs.yaml"), "utf8");

// Every fixture doc follows package.json so that DOC092 (a doc must cover or follow something) stays quiet (ADR-0180).
function front(fields: Record<string, unknown> = {}): string {
  const data = {
    title: "T",
    kind: "guide",
    status: "active",
    follows: ["package.json"],
    ...fields,
  };
  return `---\n${YAML.stringify(data)}---\n`;
}

function docsRepo(files: Record<string, string> = {}): string {
  const root = miniRepo(
    {
      "harness/config/docs.yaml": realConfig,
      "harness/src/rules.ts": 'export const RULE = "ARCH010";\n',
      "src/mod.ts": MODULE,
      "docs/index.md": `${front()}# Index\n[guide](guide.md)\n`,
      "docs/guide.md": `${front()}# Guide\n`,
      ...files,
    },
    { "package.json": "{}\n" },
  );
  verifyDefaults(root, TODAY);
  return root;
}

const codes = (root: string, now = TODAY): string[] =>
  [...new Set(run(root, now).map((v) => v.code))].sort();

function edit(root: string, file: string, from: string, to: string): void {
  const full = path.join(root, file);
  const text = readFileSync(full, "utf8");
  expect(text).toContain(from);
  writeFileSync(full, text.replace(from, to));
}

function verifiedGuide(root: string): void {
  writeFileSync(path.join(root, "docs/guide.md"), `${front({ covers: ["src/*.ts"] })}# Guide\n`);
  reverify(root, "docs/guide.md", "initial", TODAY);
}

describe("docs check [REQ-020]", () => {
  test("a clean repo passes", () => {
    expect(run(docsRepo(), TODAY)).toEqual([]);
  });

  test("the real repository's docs are clean", () => {
    expect(run(findRoot())).toEqual([]);
  });

  test("missing front matter or keys fail", () => {
    expect(codes(docsRepo({ "docs/guide.md": "# no front matter\n" }))).toContain("DOC001");
    expect(codes(docsRepo({ "docs/guide.md": "---\ntitle: x\n---\nbody\n" }))).toContain("DOC001");
    expect(codes(docsRepo({ "docs/guide.md": "---\n- a list\n---\nbody\n" }))).toContain("DOC001");
  });

  test("invalid kind and status fail", () => {
    expect(
      codes(docsRepo({ "docs/guide.md": `${front({ kind: "essay", status: "wip" })}x\n` })),
    ).toEqual(["DOC002"]);
  });

  test("a doc goes stale when its covered code changes", () => {
    const root = docsRepo();
    verifiedGuide(root);
    expect(run(root, TODAY)).toEqual([]);
    edit(root, "src/mod.ts", "a + b", "a - b");
    expect(codes(root)).toEqual(["DOC010"]);
  });

  test("comment and formatting changes do not make docs stale", () => {
    const root = docsRepo();
    verifiedGuide(root);
    writeFileSync(
      path.join(root, "src/mod.ts"),
      "// note\nexport function add(a: number, b: number): number { /* sum */ return a + b }\n",
    );
    expect(run(root, TODAY)).toEqual([]);
  });

  test("a new covered file makes the doc stale", () => {
    const root = docsRepo();
    verifiedGuide(root);
    writeFileSync(path.join(root, "src/extra.ts"), "export const x = 1;\n");
    expect(codes(root)).toEqual(["DOC010"]);
  });

  test("reverify clears staleness and keeps the note of the verification in the record", () => {
    const root = docsRepo();
    verifiedGuide(root);
    edit(root, "src/mod.ts", "a + b", "a * b");
    expect(codes(root)).toEqual(["DOC010"]);
    for (let index = 0; index < 3; index++)
      reverify(root, "docs/guide.md", `checked ${index}`, TODAY);
    expect(run(root, TODAY)).toEqual([]);
    const record = readFileSync(path.join(root, recordPath("docs/guide.md", "src/mod.ts")), "utf8");
    expect(record).toContain("checked 2");
    expect(readFileSync(path.join(root, "docs/guide.md"), "utf8")).not.toContain("checked");
  });

  test("reverify needs a note and front matter", () => {
    const root = docsRepo({ "docs/plain.md": "no front matter\n" });
    expect(() => reverify(root, "docs/guide.md", "  ", TODAY)).toThrow(/note/);
    expect(() => reverify(root, "docs/plain.md", "x", TODAY)).toThrow(/front matter/);
  });

  test("covers without a hash, or matching nothing, fail", () => {
    expect(codes(docsRepo({ "docs/guide.md": `${front({ covers: ["src/*.ts"] })}x\n` }))).toEqual([
      "DOC011",
    ]);
    expect(
      codes(
        docsRepo({
          "docs/guide.md": `${front({ covers: ["nothing/*.ts"], follows: undefined })}x\n`,
        }),
      ),
    ).toEqual(["DOC012"]);
  });

  test("docs expire by kind, decisions never do", () => {
    const aged = (date: string): string => {
      const root = docsRepo();
      reverify(root, "docs/guide.md", "old", date);
      return root;
    };
    expect(codes(aged("2026-09-27"))).toEqual(["DOC020"]);
    expect(codes(aged("2026-09-28"))).toEqual([]);
    expect(codes(adrRepo())).not.toContain("DOC020");
  });

  test("a record dated in the future, or one that cannot be read, fails", () => {
    const future = docsRepo();
    reverify(future, "docs/guide.md", "from tomorrow", "2026-11-01");
    expect(codes(future)).toEqual(["DOC021"]);
    const broken = docsRepo();
    const file = path.join(broken, recordPath("docs/guide.md", "package.json"));
    writeFileSync(file, 'date: "2026-02-30"\n');
    expect(codes(broken)).toEqual(["DOC010", "DOC101"]);
    writeFileSync(file, "<<<<<<< ours\ndoc: x\n=======\ndoc: y\n>>>>>>> theirs\n");
    expect(codes(broken)).toEqual(["DOC010", "DOC101"]);
  });

  test("broken links and dead path references fail; fenced code and external links are ignored", () => {
    const body =
      "# G\n[gone](missing.md)\nSee `packages/nope.ts` and `docs/index.md`.\n```\n[x](missing2.md) `packages/nope2.ts`\n```\n[ext](https://example.com) [anchor](#top)\n";
    const found = run(docsRepo({ "docs/guide.md": front() + body }), TODAY);
    expect(found.map((v) => [v.code, v.line])).toEqual([
      ["DOC030", 9],
      ["DOC031", 10],
    ]);
  });

  test("orphans fail, transitively linked docs do not", () => {
    expect(codes(docsRepo({ "docs/orphan.md": `${front()}x\n` }))).toEqual(["DOC040"]);
    expect(
      run(
        docsRepo({
          "docs/guide.md": `${front()}[deep](sub/deep.md)\n`,
          "docs/sub/deep.md": `${front()}x\n`,
        }),
        TODAY,
      ),
    ).toEqual([]);
  });

  test("generated and archived docs are not checked", () => {
    expect(
      run(
        docsRepo({ "docs/generated/x.md": "no front matter\n", "docs/archive/y.md": "nope\n" }),
        TODAY,
      ),
    ).toEqual([]);
  });

  test("long docs fail, AGENTS.md has a tighter limit", () => {
    expect(codes(docsRepo({ "docs/guide.md": front() + "line\n".repeat(401) }))).toEqual([
      "DOC050",
    ]);
    expect(codes(docsRepo({ "AGENTS.md": front() + "line\n".repeat(125) }))).toEqual(["DOC050"]);
  });

  test("plans must match their folder", () => {
    const completed = {
      "docs/exec-plans/completed/p.md": front({ kind: "plan" }),
      "docs/guide.md": `${front()}[p](exec-plans/completed/p.md)\n`,
    };
    expect(codes(docsRepo(completed))).toEqual(["DOC060"]);
    const active = {
      "docs/exec-plans/active/p.md": front({ kind: "plan", status: "archived" }),
      "docs/guide.md": `${front()}[p](exec-plans/active/p.md)\n`,
    };
    expect(codes(docsRepo(active))).toEqual(["DOC060"]);
  });
});

const ADR = "docs/design-docs/adr/0001-x.md";

function adrRepo(fields: Record<string, unknown> = {}, seal = true): string {
  const root = docsRepo({
    [ADR]: `${front({ kind: "decision", enforced_by: ["ARCH010"], ...fields })}# Decision\nWe do X because Y.\n`,
    "docs/guide.md": `${front()}[adr](design-docs/adr/0001-x.md)\n`,
  });
  if (seal) sealAdr(root, ADR);
  return root;
}

describe("ADR rules [REQ-020] [REQ-021]", () => {
  test("an unsealed accepted ADR fails until sealed", () => {
    expect(codes(adrRepo({}, false))).toEqual(["DOC071"]);
    expect(run(adrRepo(), TODAY)).toEqual([]);
  });

  test("editing a sealed ADR body fails", () => {
    const root = adrRepo();
    edit(root, ADR, "because Y", "because Z");
    expect(codes(root)).toEqual(["DOC070"]);
  });

  test("changing front matter does not break the seal", () => {
    const root = adrRepo();
    writeFileSync(
      path.join(root, "docs/design-docs/adr/0002-y.md"),
      `${front({ kind: "decision", enforced_by: ["none"], no_enforcement_reason: "r" })}n\n`,
    );
    sealAdr(root, "docs/design-docs/adr/0002-y.md");
    edit(root, ADR, "status: active", "status: superseded\nsuperseded_by: 0002-y.md");
    edit(
      root,
      "docs/guide.md",
      "(design-docs/adr/0001-x.md)",
      "(design-docs/adr/0001-x.md) [y](design-docs/adr/0002-y.md)",
    );
    expect(run(root, TODAY)).toEqual([]);
  });

  test("a superseded ADR needs an existing successor", () => {
    expect(codes(adrRepo({ status: "superseded", superseded_by: "9999-none.md" }))).toEqual([
      "DOC060",
    ]);
  });

  test("an ADR must say how it is enforced, with real rule codes or tests", () => {
    expect(codes(adrRepo({ enforced_by: [] }))).toEqual(["DOC080"]);
    expect(codes(adrRepo({ enforced_by: ["ARCH999"] }))).toEqual(["DOC080"]);
    expect(codes(adrRepo({ enforced_by: ["tests/spec/missing.test.ts"] }))).toEqual(["DOC080"]);
    expect(codes(adrRepo({ enforced_by: ["none"] }))).toEqual(["DOC080"]);
    expect(
      run(adrRepo({ enforced_by: ["none"], no_enforcement_reason: "taste, not checkable" }), TODAY),
    ).toEqual([]);
    expect(run(adrRepo({ enforced_by: ["src/mod.ts"] }), TODAY)).toEqual([]);
  });

  test("decisions outside the ADR folder, or non-decisions inside it, fail", () => {
    expect(
      codes(
        docsRepo({
          "docs/guide.md": `${front({ kind: "decision", enforced_by: ["ARCH010"] })}x\n`,
        }),
      ),
    ).toContain("DOC060");
    const wrongKind = {
      "docs/design-docs/adr/0003-z.md": front(),
      "docs/guide.md": `${front()}[z](design-docs/adr/0003-z.md)\n`,
    };
    expect(codes(docsRepo(wrongKind))).toEqual(["DOC060"]);
  });

  test("seal-adr refuses non-decisions", () => {
    const root = docsRepo();
    expect(() => sealAdr(root, "docs/guide.md")).toThrow(/decision/);
  });
});
