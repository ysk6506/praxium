import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { splitFrontMatter } from "../common";
import { ADR_DIR, adr, docsRepo, front, TODAY } from "../test-support/docs-repo";
import { reverify, run } from "./docs";

const codes = (root: string): string[] => [...new Set(run(root, TODAY).map((v) => v.code))].sort();
const read = (root: string, file: string) => readFileSync(path.join(root, file), "utf8");

describe("docs follow other docs [REQ-020]", () => {
  test("a document with neither covers nor follows fails DOC092, an ADR is exempt", () => {
    const bare = docsRepo({
      "docs/guide.md": `${front({ follows: undefined })}# G\n`,
    });
    expect(codes(bare)).toEqual(["DOC092"]);
    const withAdr = docsRepo({
      [`${ADR_DIR}/0001-x.md`]: adr(),
      "docs/guide.md": `${front()}[a](design-docs/adr/0001-x.md)\n`,
    });
    expect(codes(withAdr)).toEqual(["DOC071"]);
  });

  test("a followed doc whose body changes makes the follower stale, front matter changes do not", () => {
    const root = docsRepo({
      "docs/spec.md": `${front()}# Spec\nv1\n`,
      "docs/plan.md": `${front({ follows: ["docs/spec.md"] })}# Plan\n[s](spec.md)\n`,
      "docs/index.md": `${front()}[g](guide.md) [p](plan.md) [s](spec.md)\n`,
    });
    expect(codes(root)).toEqual(["DOC011"]);
    reverify(root, "docs/plan.md", "read it", TODAY);
    expect(codes(root)).toEqual([]);
    const spec = read(root, "docs/spec.md");
    writeFileSync(
      path.join(root, "docs/spec.md"),
      spec.replace("title: T", "title: Another title"),
    );
    expect(codes(root)).toEqual([]);
    writeFileSync(path.join(root, "docs/spec.md"), spec.replace("v1", "v2"));
    expect(codes(root)).toEqual(["DOC010"]);
    reverify(root, "docs/plan.md", "read v2", TODAY);
    expect(codes(root)).toEqual([]);
  });

  test("follows that match nothing fail DOC012", () => {
    const root = docsRepo({
      "docs/guide.md": `${front({ follows: ["docs/gone.md"] })}# G\n`,
    });
    expect(codes(root)).toEqual(["DOC012"]);
  });

  test("the ADR folder is reached once the index links to it", () => {
    const sealed = adr();
    const root = docsRepo({ [`${ADR_DIR}/0001-x.md`]: sealed });
    expect(codes(root)).toContain("DOC040");
    writeFileSync(
      path.join(root, "docs/index.md"),
      `${front()}[g](guide.md) [decisions](design-docs/adr/)\n`,
    );
    expect(codes(root)).not.toContain("DOC040");
  });
});

describe("amends and amended_by point at each other [REQ-020]", () => {
  const pair = (older: Record<string, unknown>, newer: Record<string, unknown>) =>
    docsRepo({
      [`${ADR_DIR}/0001-old.md`]: adr(older),
      [`${ADR_DIR}/0002-new.md`]: adr(newer),
      "docs/index.md": `${front()}[g](guide.md) [d](design-docs/adr/)\n`,
    });
  const onlyAmend = (root: string): string[] =>
    run(root, TODAY)
      .filter((v) => v.code === "DOC091")
      .map((v) => v.path);

  test("a one-sided amends fails DOC091 on the amending ADR", () => {
    expect(onlyAmend(pair({}, { amends: ["0001-old.md"] }))).toEqual([`${ADR_DIR}/0002-new.md`]);
  });

  test("both sides present passes; amended_by without amends fails on the amended ADR", () => {
    expect(onlyAmend(pair({ amended_by: ["0002-new.md"] }, { amends: ["0001-old.md"] }))).toEqual(
      [],
    );
    expect(onlyAmend(pair({ amended_by: ["0002-new.md"] }, {}))).toEqual([
      `${ADR_DIR}/0001-old.md`,
    ]);
  });

  test("amends naming a missing ADR fails", () => {
    expect(onlyAmend(pair({}, { amends: ["0009-none.md"] }))).toEqual([`${ADR_DIR}/0002-new.md`]);
  });
});

describe("the product goal sentence leads README and AGENTS [REQ-020]", () => {
  const goal = "목표 한 문장.";
  const spec = `${front()}# 요구사항\n\n## 제품 목표 (ADR-1)\n\n**${goal}**\n\n1. x\n`;
  const withDocs = (readme: string, agents: string) =>
    docsRepo({
      "docs/product-specs/requirements.md": spec,
      "README.md": `${front()}# T\n\n${readme}\n\n## 다음\n`,
      "AGENTS.md": `${front()}# A\n\n${agents} 이 파일은 지도다.\n`,
      "docs/index.md": `${front()}[g](guide.md) [r](product-specs/requirements.md)\n`,
    });

  test("the sentence in the first paragraph passes, bold or plain", () => {
    expect(codes(withDocs(`**${goal}**`, goal))).toEqual([]);
  });

  test("an old sentence or one that is only later in the file fails DOC093", () => {
    expect(codes(withDocs("옛 문장.", goal))).toEqual(["DOC093"]);
    const later = withDocs("옛 문장.\n\n" + goal, goal);
    expect(run(later, TODAY).map((v) => v.path)).toEqual(["README.md"]);
  });
});

test("front matter helper stays parseable [REQ-020]", () => {
  expect(splitFrontMatter(front()).data?.follows).toEqual(["package.json"]);
});
