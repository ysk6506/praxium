import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot, splitFrontMatter } from "../common";
import { docsRepo, front, TODAY } from "../test-support/docs-repo";
import { followEntries } from "./doc-targets";
import { report, reverify, run } from "./docs";

/** A guide that follows docs/spec.md as `follows` says, verified once against it. */
function repoFollowing(follows: unknown[]): string {
  const root = docsRepo({
    "docs/guide.md": `${front({ follows })}# Guide\n[spec](spec.md)\n`,
    "docs/spec.md": `${front()}# Spec\nfirst\n`,
  });
  reverify(root, "docs/guide.md", "read against the first spec", TODAY);
  return root;
}

const changeSpec = (root: string): void =>
  writeFileSync(path.join(root, "docs/spec.md"), `${front()}# Spec\nsecond\n`);
const codes = (root: string, release: boolean): string[] =>
  [...new Set(run(root, TODAY, release).map((v) => v.code))].sort();
const RELEASE = [{ doc: "docs/spec.md", recheck: "release" }];

describe("a follows entry says when it must be re-read [REQ-394]", () => {
  test("a path entry is a change relation, and so is { doc } and { doc, recheck: change }", () => {
    const entries = followEntries({
      follows: [
        "a.md",
        { doc: "b.md" },
        { doc: "c.md", recheck: "change" },
        { doc: "d.md", recheck: "release" },
      ],
    });
    expect(entries.map((e) => `${e.glob}:${e.recheck}`)).toEqual([
      "a.md:change",
      "b.md:change",
      "c.md:change",
      "d.md:release",
    ]);
  });

  test.each([
    ["an unknown time", [{ doc: "docs/spec.md", recheck: "weekly" }]],
    ["no doc", [{ recheck: "release" }]],
    ["an unknown key", [{ doc: "docs/spec.md", recheck: "release", why: "x" }]],
    ["a number", [3]],
  ])("%s is a front matter error", (_name, follows) => {
    const root = docsRepo({ "docs/guide.md": `${front({ follows })}# Guide\n` });
    expect(codes(root, false)).toContain("DOC001");
  });

  test("a change relation fails at once, in both modes", () => {
    for (const follows of [["docs/spec.md"], [{ doc: "docs/spec.md", recheck: "change" }]]) {
      const root = repoFollowing(follows);
      expect(codes(root, false)).toEqual([]);
      changeSpec(root);
      expect(codes(root, false)).toEqual(["DOC010"]);
      expect(codes(root, true)).toEqual(["DOC010"]);
    }
  });

  test("a doc whose only relation is an object still counts as following something", () => {
    expect(codes(repoFollowing(RELEASE), false)).toEqual([]);
  });

  test("a target that a change entry also names stays a change relation", () => {
    const root = repoFollowing([...RELEASE, "docs/spec.md"]);
    changeSpec(root);
    expect(codes(root, false)).toEqual(["DOC010"]);
  });
});

describe("a stale release relation is a notice, and a failure in the release check [REQ-395]", () => {
  test("up to date is quiet in both modes", () => {
    const root = repoFollowing(RELEASE);
    for (const release of [false, true]) {
      const checked = report(root, TODAY, release);
      expect(checked.violations).toEqual([]);
      expect(checked.notes).toEqual([]);
    }
  });

  test("stale is a notice that names the document, the target and the command", () => {
    const root = repoFollowing(RELEASE);
    changeSpec(root);
    const checked = report(root, TODAY);
    expect(checked.violations).toEqual([]);
    expect(checked.notes).toHaveLength(1);
    const [note] = checked.notes;
    expect(note).toMatch(/^NOTE docs \[DOC103\]/);
    expect(note).toContain("다음 릴리스 전에 다시 확인");
    expect(note).toContain("docs/guide.md");
    expect(note).toContain("docs/spec.md");
    expect(note).toContain("reverify docs/guide.md --target docs/spec.md");
  });

  test("stale fails in the release check, and the message says what to run", () => {
    const root = repoFollowing(RELEASE);
    changeSpec(root);
    const checked = report(root, TODAY, true);
    expect(checked.notes).toEqual([]);
    expect(checked.violations.map((v) => v.code)).toEqual(["DOC013"]);
    const [v] = checked.violations;
    expect(v?.path).toBe("docs/guide.md");
    expect(v?.message).toContain("docs/spec.md");
    expect(v?.fix).toContain("reverify docs/guide.md --target docs/spec.md");
    expect(v?.fix).toContain("--note");
  });

  test("a release relation with no record yet is stale for the release", () => {
    const root = repoFollowing(["package.json"]);
    writeFileSync(
      path.join(root, "docs/guide.md"),
      `${front({ follows: ["package.json", ...RELEASE] })}# Guide\n[spec](spec.md)\n`,
    );
    expect(codes(root, false)).toEqual([]);
    expect(codes(root, true)).toEqual(["DOC013"]);
  });

  test("reverify records it, and the release check is quiet again", () => {
    const root = repoFollowing(RELEASE);
    changeSpec(root);
    expect(reverify(root, "docs/guide.md", "compared with the second spec", TODAY)).toEqual([
      "docs/spec.md",
    ]);
    expect(report(root, TODAY, true)).toEqual({ violations: [], notes: [] });
  });

  test("a stale change relation and a stale release relation are told apart", () => {
    const root = docsRepo({
      "docs/guide.md": `${front({ follows: ["package.json", ...RELEASE] })}# Guide\n[spec](spec.md)\n`,
      "docs/spec.md": `${front()}# Spec\nfirst\n`,
    });
    reverify(root, "docs/guide.md", "first reading", TODAY);
    changeSpec(root);
    writeFileSync(path.join(root, "package.json"), '{"changed":true}\n');
    expect(codes(root, false)).toEqual(["DOC010"]);
    expect(codes(root, true)).toEqual(["DOC010", "DOC013"]);
  });
});

describe("the product design overview is re-read at the release [REQ-395]", () => {
  test("it follows the requirements at release and the decision at once", () => {
    const root = findRoot();
    const { data } = splitFrontMatter(
      readFileSync(path.join(root, "docs/design-docs/product-design.md"), "utf8"),
    );
    const entries = followEntries(data ?? {}).map((e) => `${path.basename(e.glob)}:${e.recheck}`);
    expect(entries).toEqual([
      "requirements.md:release",
      "0005-separate-product-own-model-verification-outside.md:change",
    ]);
  });

  test("the real repository passes the release check", () => {
    expect(run(findRoot(), undefined, true)).toEqual([]);
  });
});
