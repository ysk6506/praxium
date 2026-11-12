import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { docsRepo, front, TODAY } from "../test-support/docs-repo";
import { commitAll } from "../test-support/mini-repo";
import { reverify } from "./doc-audit";
import { readDocRecords, recordPath } from "./doc-records";
import { run } from "./docs";

const A = "export const a = () => 1;\n\n\nexport const second = () => 2;\n";
const B = "export const b = () => 1;\n";
const codes = (root: string, now = TODAY): string[] =>
  [...new Set(run(root, now).map((v) => v.code))].sort();
const edit = (root: string, file: string, from: string, to: string): void => {
  const full = path.join(root, file);
  writeFileSync(full, readFileSync(full, "utf8").replace(from, to));
};

/** A repository whose guide covers two source files and has been verified against both. */
function twoTargets(): string {
  const root = docsRepo({
    "src/a.ts": A,
    "src/b.ts": B,
    "docs/guide.md": `${front({ covers: ["src/*.ts"], follows: undefined })}# Guide\n`,
  });
  reverify(root, "docs/guide.md", "first reading", TODAY);
  return root;
}

const git = (root: string, ...args: string[]): string =>
  execFileSync("git", ["-c", "user.email=t@t", "-c", "user.name=t", ...args], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });

describe("freshness is per target [REQ-020]", () => {
  test("a glob is one target per matched file, and only the changed file makes the doc stale", () => {
    const root = twoTargets();
    expect(codes(root)).toEqual([]);
    expect([...readDocRecords(root, "docs/guide.md").records.keys()]).toEqual([
      "src/a.ts",
      "src/b.ts",
    ]);
    edit(root, "src/a.ts", "() => 1", "() => 11");
    const [problem] = run(root, TODAY);
    expect(problem?.code).toBe("DOC010");
    expect(problem?.message).toContain("1 of 2 targets: src/a.ts");
  });

  test("a document with no record is never verified; a new or removed target is stale", () => {
    const root = twoTargets();
    writeFileSync(path.join(root, "src/c.ts"), "export const c = 1;\n");
    expect(codes(root)).toEqual(["DOC010"]);
    reverify(root, "docs/guide.md", "read c", TODAY);
    expect(codes(root)).toEqual([]);
    edit(root, "docs/guide.md", "- src/*.ts", "- src/a.ts\n  - src/b.ts");
    expect(codes(root)).toEqual(["DOC010"]);
    reverify(root, "docs/guide.md", "c is no longer covered", TODAY);
    expect(codes(root)).toEqual([]);
    expect([...readDocRecords(root, "docs/guide.md").records.keys()]).toEqual([
      "src/a.ts",
      "src/b.ts",
    ]);
  });

  test("reverify with no target writes only the stale ones; --target writes the one named", () => {
    const root = twoTargets();
    edit(root, "src/a.ts", "() => 1", "() => 11");
    edit(root, "src/b.ts", "() => 1", "() => 12");
    expect(reverify(root, "docs/guide.md", "only b", TODAY, "src/b.ts")).toEqual(["src/b.ts"]);
    expect(run(root, TODAY)[0]?.message).toContain("1 of 2 targets: src/a.ts");
    expect(reverify(root, "docs/guide.md", "the rest", TODAY)).toEqual(["src/a.ts"]);
    expect(codes(root)).toEqual([]);
    expect(() => reverify(root, "docs/guide.md", "x", TODAY, "src/nope.ts")).toThrow(
      /not a target/,
    );
  });

  test("the date a document was last read is its newest record; a removed target takes its record away", () => {
    const root = twoTargets();
    reverify(root, "docs/guide.md", "old", "2026-09-01", "src/a.ts");
    expect(codes(root)).toEqual([]);
    edit(root, "docs/guide.md", "src/*.ts", "src/a.ts");
    // b's record is gone with the target, and what is left of the document was last read long ago.
    expect(codes(root)).toEqual(["DOC010", "DOC020"]);
    reverify(root, "docs/guide.md", "narrowed", TODAY);
    expect([...readDocRecords(root, "docs/guide.md").records.keys()]).toEqual(["src/a.ts"]);
  });
});

describe("two changes that verify the same document [REQ-020]", () => {
  /** Two branches from one base; each edits `edits` of its own and re-verifies the guide. */
  function branches(plan: { a: [string, string, string]; b: [string, string, string] }): {
    root: string;
    merged: boolean;
  } {
    const root = twoTargets();
    commitAll(root);
    const base = git(root, "rev-parse", "HEAD").trim();
    for (const [name, [file, from, to]] of [
      ["side-a", plan.a],
      ["side-b", plan.b],
    ] as const) {
      git(root, "checkout", "-q", "-b", name, base);
      edit(root, file, from, to);
      reverify(root, "docs/guide.md", `read after ${name}`, TODAY, file);
      git(root, "add", "-A");
      git(root, "commit", "-qm", name);
    }
    git(root, "checkout", "-q", "side-a");
    try {
      git(root, "merge", "--no-edit", "side-b");
      return { root, merged: true };
    } catch {
      return { root, merged: false };
    }
  }

  test("against different targets they merge without a conflict and the result is fresh", () => {
    const { root, merged } = branches({
      a: ["src/a.ts", "() => 1;", "() => 11;"],
      b: ["src/b.ts", "() => 1;", "() => 12;"],
    });
    expect(merged).toBe(true);
    expect(codes(root)).toEqual([]);
  });

  test("against the same target they conflict in that record, and the merge is stale until read again", () => {
    const { root, merged } = branches({
      a: ["src/a.ts", "a = () => 1", "a = () => 11"],
      b: ["src/a.ts", "second = () => 2", "second = () => 22"],
    });
    expect(merged).toBe(false);
    const conflicted = git(root, "diff", "--name-only", "--diff-filter=U").trim();
    expect(conflicted).toBe(recordPath("docs/guide.md", "src/a.ts"));
    git(root, "checkout", "--ours", "--", conflicted);
    git(root, "add", "-A");
    git(root, "commit", "-qm", "take ours");
    expect(codes(root)).toEqual(["DOC010"]);
    reverify(root, "docs/guide.md", "read the merge", TODAY);
    expect(codes(root)).toEqual([]);
  });
});
