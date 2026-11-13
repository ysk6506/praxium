import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { splitFrontMatter } from "../common";
import { ADR_DIR, adr, docsRepo, front, TODAY } from "../test-support/docs-repo";
import { commitAll, writeFiles } from "../test-support/mini-repo";
import { amendEntries, backEntry } from "./doc-amends";
import { decisionsTable, linkAmends } from "./doc-decisions";
import { report } from "./doc-diff";
import { loadDocsConfig, run } from "./docs";

const OLD = `${ADR_DIR}/0001-old.md`;
const NEW = `${ADR_DIR}/0002-new.md`;
const WHAT = "사용자 타입은 실험에 안 들어감";
const index = `${front()}[g](guide.md) [d](design-docs/adr/)\n`;
const pair = (older: Record<string, unknown>, newer: Record<string, unknown>): string =>
  docsRepo({ [OLD]: adr(older), [NEW]: adr(newer), "docs/index.md": index });
const amendProblems = (root: string): string[] =>
  run(root, TODAY)
    .filter((v) => v.code === "DOC091")
    .map((v) => v.path);

describe("amends entries come in two forms [REQ-020]", () => {
  test("a file name and { adr, what } both read as the ADR they name", () => {
    const names = ["0001-old.md", "0002-new.md"];
    expect(amendEntries(["0001-old.md"], names)).toEqual([
      { name: "0001-old.md", what: null, rich: false },
    ]);
    expect(amendEntries([{ adr: "0002", what: " 한 구절 " }], names)).toEqual([
      { name: "0002-new.md", what: "한 구절", rich: true },
    ]);
    expect(amendEntries([{ adr: "0009" }], names)[0]?.name).toBe("0009");
    expect(amendEntries("nope", names)).toEqual([]);
  });

  test("the back entry carries the phrase when there is one", () => {
    expect(backEntry("0002-new.md", WHAT)).toEqual({ adr: "0002", what: WHAT });
    expect(backEntry("0002-new.md", null)).toBe("0002-new.md");
  });
});

describe("DOC091 reads the rich form on both sides [REQ-020]", () => {
  test("rich on both sides passes, and a number that is no ADR fails", () => {
    const linked = pair(
      { amended_by: [{ adr: "0002", what: WHAT }] },
      { amends: [{ adr: "0001", what: WHAT }] },
    );
    expect(amendProblems(linked)).toEqual([]);
    expect(amendProblems(pair({}, { amends: [{ adr: "0009", what: WHAT }] }))).toEqual([NEW]);
  });

  test("a rich amends without the other side still fails on the amending ADR", () => {
    expect(amendProblems(pair({}, { amends: [{ adr: "0001", what: WHAT }] }))).toEqual([NEW]);
  });
});

describe("link-amends copies the phrase to the amended ADR [REQ-020]", () => {
  test("the phrase lands in amended_by, once, and a bare entry is upgraded", () => {
    const root = pair({ amended_by: ["0002-new.md"] }, { amends: [{ adr: "0001", what: WHAT }] });
    const config = loadDocsConfig(root);
    expect(linkAmends(root, config, NEW)).toEqual([OLD]);
    expect(linkAmends(root, config, NEW)).toEqual([]);
    const after = splitFrontMatter(readFileSync(path.join(root, OLD), "utf8"));
    expect(after.data?.amended_by).toEqual([{ adr: "0002", what: WHAT }]);
    expect(amendProblems(root)).toEqual([]);
  });

  test("the generated list shows the phrase beside the amending ADR", () => {
    const root = pair({}, { amends: [{ adr: "0001", what: WHAT }] });
    linkAmends(root, loadDocsConfig(root), NEW);
    const line = decisionsTable(root, loadDocsConfig(root))
      .split("\n")
      .find((l) => l.includes("0001-old.md"));
    expect(line).toContain(`일부 수정: [0002](`);
    expect(line).toContain(`(${WHAT})`);
  });
});

describe("a new ADR says what it changed [REQ-020]", () => {
  const base = (): string => {
    const root = docsRepo({ [OLD]: adr(), "docs/index.md": index });
    commitAll(root);
    return root;
  };
  const withAmends = (amends: unknown[]): string[] => {
    const root = base();
    writeFiles(root, { [NEW]: adr({ updates_docs: [], updates_docs_reason: "none", amends }) });
    return report(root, "HEAD")
      .violations.filter((v) => v.code === "DOC091")
      .map((v) => v.message);
  };

  test("the file-name form is refused for a new ADR; the rich form passes", () => {
    expect(withAmends(["0001-old.md"])).toEqual([
      "amends 0001-old.md without saying what it changed",
    ]);
    expect(withAmends([{ adr: "0001" }])).toHaveLength(1);
    expect(withAmends([{ adr: "0001", what: WHAT }])).toEqual([]);
  });
});
