import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { ADR_DIR, adr, docsRepo, front, TODAY, verifyDefaults } from "../test-support/docs-repo";
import { commitAll, writeFiles } from "../test-support/mini-repo";
import { reverify } from "./doc-audit";
import { loadDocsConfig } from "./doc-config";
import { decisionsTable, writeDecisions } from "./doc-decisions";
import { report } from "./doc-diff";
import { freshIndex, listBlock, renderList } from "./doc-index";
import { main, run, sealAdr } from "./docs";

const INDEX_TEXT = `${front()}# Index\n\nHand-written introduction. [decisions](design-docs/adr/)\n\n<!-- docs-list:begin -->\n<!-- docs-list:end -->\n\n## After the list\n`;
const read = (root: string, file: string): string => readFileSync(path.join(root, file), "utf8");
const quietly = <T>(fn: () => T): T => {
  const write = process.stdout.write.bind(process.stdout);
  process.stdout.write = (() => true) as typeof process.stdout.write;
  try {
    return fn();
  } finally {
    process.stdout.write = write;
  }
};

/** A committed repository whose generated files are current, with an index that has the generated list. */
function committed(): string {
  const root = docsRepo({
    "docs/index.md": INDEX_TEXT,
    "docs/design-docs/plan.md": `${front({ title: "A plan [draft]" })}# Plan\n`,
    [`${ADR_DIR}/0001-old.md`]: adr(),
  });
  sealAdr(root, `${ADR_DIR}/0001-old.md`);
  quietly(() => main(["generate", "--root", root]));
  verifyDefaults(root);
  commitAll(root);
  return root;
}
const problems = (root: string): string[] =>
  report(root, "HEAD").violations.map((v) => `${v.code} ${v.path}`);

describe("the document list in the index is made, not written [REQ-020]", () => {
  test("one line per document from its title, grouped by folder, around the hand-written text", () => {
    const root = committed();
    const text = read(root, "docs/index.md");
    expect(text).toContain("Hand-written introduction.");
    expect(text).toContain("## After the list");
    expect(text).toContain("- [A plan draft](design-docs/plan.md)");
    expect(text).toContain("- [T](guide.md)");
    expect(text).not.toContain("0001-old");
    expect(freshIndex(root, loadDocsConfig(root), text)).toBe(text);
  });

  test("a new document needs no line in the index: it is listed by construction, and a stale list is a notice", () => {
    const root = committed();
    writeFiles(root, { "docs/new.md": `${front({ title: "New" })}# New\n` });
    verifyDefaults(root);
    expect(run(root, TODAY).map((v) => v.code)).toEqual([]);
    const notes = report(root, "HEAD").notes.join("\n");
    expect(notes).toContain("DOC102");
    expect(renderList(root, loadDocsConfig(root))).toContain("[New](new.md)");
  });

  test("without the markers the index is a hand-kept page again and an unlinked document is an orphan", () => {
    const root = docsRepo({ "docs/new.md": `${front()}# New\n` });
    expect(run(root, TODAY).map((v) => v.code)).toEqual(["DOC040"]);
  });
});

describe("changes do not commit generated files [REQ-020]", () => {
  test("a generated file edited by hand fails, and the same file regenerated passes", () => {
    const root = committed();
    expect(problems(root)).toEqual([]);
    writeFileSync(path.join(root, "docs/generated/decisions.md"), "| hand | edited |\n");
    expect(problems(root)).toEqual(["DOC099 docs/generated/decisions.md"]);
    writeFiles(root, { [`${ADR_DIR}/0002-new.md`]: adr() });
    writeDecisions(root, loadDocsConfig(root));
    expect(problems(root).filter((p) => p.includes("decisions"))).toEqual([]);
    expect(read(root, "docs/generated/decisions.md")).toBe(
      decisionsTable(root, loadDocsConfig(root)),
    );
  });

  test("a hand edit inside the generated list of the index fails; the text around it may be edited", () => {
    const root = committed();
    writeFileSync(
      path.join(root, "docs/index.md"),
      read(root, "docs/index.md").replace("Hand-written introduction.", "A better introduction."),
    );
    expect(problems(root)).toEqual([]);
    writeFileSync(
      path.join(root, "docs/index.md"),
      read(root, "docs/index.md").replace("- [T](guide.md)", "- [Mine](guide.md)"),
    );
    expect(problems(root)).toEqual(["DOC099 docs/index.md"]);
    expect(listBlock(read(root, "docs/index.md"))).toContain("[Mine]");
  });

  test("a generated file left stale by a change is only a notice", () => {
    const root = committed();
    writeFiles(root, { [`${ADR_DIR}/0002-new.md`]: adr() });
    const result = report(root, "HEAD");
    expect(result.violations.filter((v) => v.code === "DOC099")).toEqual([]);
    expect(result.notes.join("\n")).toContain("DOC098");
  });
});

describe("the commands [REQ-020]", () => {
  test("reverify --target verifies one target, migrate on a converted repository does nothing", () => {
    const root = docsRepo({
      "src/a.ts": "export const a = 1;\n",
      "src/b.ts": "export const b = 1;\n",
      "docs/guide.md": `${front({ covers: ["src/*.ts"], follows: undefined })}# Guide\n`,
    });
    reverify(root, "docs/guide.md", "first", TODAY);
    writeFileSync(path.join(root, "src/a.ts"), "export const a = 2;\n");
    writeFileSync(path.join(root, "src/b.ts"), "export const b = 2;\n");
    const code = quietly(() =>
      main([
        "reverify",
        path.join(root, "docs/guide.md"),
        "--target",
        path.join(root, "src/a.ts"),
        "--note",
        "a only",
        "--root",
        root,
      ]),
    );
    expect(code).toBe(0);
    expect(run(root, TODAY)[0]?.message).toContain("1 of 2 targets: src/b.ts");
    expect(quietly(() => main(["migrate", "--root", root]))).toBe(0);
    expect(run(root, TODAY)[0]?.message).toContain("1 of 2 targets: src/b.ts");
  });
});

describe("the docs check is fast [REQ-020]", () => {
  test("two hundred documents with three targets each are checked within a few seconds", () => {
    const files: Record<string, string> = {};
    for (let n = 0; n < 60; n++) files[`src/m${n}.ts`] = `export const v${n} = ${n};\n`;
    const names = Array.from({ length: 200 }, (_, n) => `docs/many/d${n}.md`);
    for (const [n, name] of names.entries())
      files[name] =
        `${front({ covers: [`src/m${n % 58}.ts`, `src/m${(n % 58) + 1}.ts`, `src/m${(n % 58) + 2}.ts`], follows: undefined })}# D${n}\n`;
    files["docs/index.md"] =
      `${front()}# Index\n[guide](guide.md) ${names.map((n) => `[x](${n.slice(5)})`).join(" ")}\n`;
    const root = docsRepo(files);
    for (const name of names) reverify(root, name, "fixture", TODAY);
    const started = performance.now();
    const found = run(root, TODAY);
    const elapsed = performance.now() - started;
    expect(found.map((v) => v.code)).toEqual([]);
    expect(elapsed).toBeLessThan(5000);
  });
});
