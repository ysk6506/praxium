import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { ADR_DIR, adr, docsRepo, front } from "../test-support/docs-repo";
import { commitAll, writeFiles } from "../test-support/mini-repo";
import { report } from "./doc-diff";
import { reverify } from "./docs";

const NEW_ADR = `${ADR_DIR}/0002-new.md`;
const base = (): string => {
  const root = docsRepo({
    [`${ADR_DIR}/0001-old.md`]: adr(),
    "docs/index.md": `${front()}[g](guide.md) [d](design-docs/adr/)\n`,
  });
  commitAll(root);
  return root;
};
const problems = (root: string): string[] =>
  report(root, "HEAD").violations.map((v) => `${v.code} ${v.message}`);

describe("a new ADR names the docs it updates [REQ-020]", () => {
  test("no updates_docs fails DOC090; old ADRs are not asked", () => {
    const root = base();
    expect(problems(root)).toEqual([]);
    writeFiles(root, { [NEW_ADR]: adr() });
    expect(problems(root)).toEqual(["DOC090 new ADR does not say which docs it updates"]);
  });

  test("an empty list needs a reason", () => {
    const root = base();
    writeFiles(root, { [NEW_ADR]: adr({ updates_docs: [] }) });
    expect(problems(root)).toEqual(["DOC090 updates_docs is empty without updates_docs_reason"]);
    writeFiles(root, {
      [NEW_ADR]: adr({ updates_docs: [], updates_docs_reason: "no doc changes" }),
    });
    expect(problems(root)).toEqual([]);
  });

  test("a named doc must really change in the same change, and must exist", () => {
    const root = base();
    writeFiles(root, { [NEW_ADR]: adr({ updates_docs: ["docs/guide.md", "docs/none.md"] }) });
    expect(problems(root)).toEqual([
      "DOC090 updates_docs names docs/guide.md, but this change does not edit it",
      "DOC090 updates_docs names docs/none.md, which does not exist",
    ]);
    writeFiles(root, { "docs/guide.md": `${front()}# Guide\nedited\n` });
    writeFiles(root, { "docs/none.md": `${front()}# N\n` });
    expect(problems(root)).toEqual([]);
  });
});

describe("notices [REQ-020]", () => {
  test("a doc whose body is unchanged and only re-verified is named (DOC097); an edited one is not", () => {
    const root = base();
    reverify(root, "docs/guide.md", "looked", "2026-11-12");
    const notes = report(root, "HEAD").notes.join("\n");
    expect(notes).toContain("DOC097");
    expect(notes).toContain("docs/guide.md");
    const text = readFileSync(path.join(root, "docs/guide.md"), "utf8");
    writeFileSync(path.join(root, "docs/guide.md"), `${text}more words\n`);
    expect(report(root, "HEAD").notes.join("\n")).not.toContain("DOC097");
  });

  test("a stale decisions file is a notice, never a violation (DOC098)", () => {
    const root = base();
    const result = report(root, undefined);
    expect(result.violations).toEqual([]);
    expect(result.notes.join("\n")).toContain("DOC098");
  });
});
