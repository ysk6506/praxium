import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { splitFrontMatter } from "../common";
import { ADR_DIR, adr, docsRepo, front } from "../test-support/docs-repo";
import { decisionsStale, decisionsTable, linkAmends } from "./doc-decisions";
import { bodyFingerprint } from "./doc-print";
import { loadDocsConfig, main, run } from "./docs";

const repo = (): string =>
  docsRepo({
    [`${ADR_DIR}/0001-old.md`]: adr({
      title: "ADR-0001 옛 결정",
      status: "superseded",
      superseded_by: "0003-newer.md",
    }),
    [`${ADR_DIR}/0002-mid.md`]: adr({ title: "ADR-0002 중간 | 결정", amends: ["0001-old.md"] }),
    [`${ADR_DIR}/0003-newer.md`]: adr({ title: "새 결정" }),
    "docs/index.md": `${front()}[g](guide.md) [d](design-docs/adr/)\n`,
  });

describe("the decisions table is made from ADR front matter [REQ-020]", () => {
  test("one line per ADR in number order with status and who changed it", () => {
    const root = repo();
    linkAmends(root, loadDocsConfig(root), `${ADR_DIR}/0002-mid.md`);
    const lines = decisionsTable(root, loadDocsConfig(root))
      .split("\n")
      .filter((l) => l.startsWith("| ["));
    expect(lines).toHaveLength(3);
    expect(lines[0]).toContain("대체됨");
    expect(lines[0]).toContain("대체: [0003]");
    expect(lines[0]).toContain("일부 수정: [0002]");
    expect(lines[1]).toContain("중간 \\| 결정");
    expect(lines[2]).toContain("ADR-0003 새 결정");
  });

  test("the file is a notice when behind and quiet when current", () => {
    const root = repo();
    expect(decisionsStale(root, loadDocsConfig(root))).toContain("DOC098");
    expect(main(["decisions", "--root", root])).toBe(0);
    expect(decisionsStale(root, loadDocsConfig(root))).toBeNull();
    expect(readFileSync(path.join(root, "docs/generated/decisions.md"), "utf8")).toContain(
      "# 결정 목록",
    );
  });
});

describe("link-amends writes amended_by on the amended ADR, front matter only [REQ-020]", () => {
  test("it adds the entry once and leaves the sealed body alone", () => {
    const root = repo();
    const target = `${ADR_DIR}/0001-old.md`;
    const before = splitFrontMatter(readFileSync(path.join(root, target), "utf8")).body;
    expect(linkAmends(root, loadDocsConfig(root), `${ADR_DIR}/0002-mid.md`)).toEqual([target]);
    expect(linkAmends(root, loadDocsConfig(root), `${ADR_DIR}/0002-mid.md`)).toEqual([]);
    const after = splitFrontMatter(readFileSync(path.join(root, target), "utf8"));
    expect(after.data?.amended_by).toEqual(["0002-mid.md"]);
    expect(bodyFingerprint(after.body)).toBe(bodyFingerprint(before));
    expect(run(root, "2026-11-11").filter((v) => v.code === "DOC091")).toEqual([]);
  });

  test("an unknown target is an error", () => {
    const root = docsRepo({ [`${ADR_DIR}/0002-mid.md`]: adr({ amends: ["0009-none.md"] }) });
    expect(() => linkAmends(root, loadDocsConfig(root), `${ADR_DIR}/0002-mid.md`)).toThrow(
      /no such ADR/,
    );
  });
});
