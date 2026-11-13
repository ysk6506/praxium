/** A verification note says what was read this time: no copies, no one note for a crowd of targets (ADR-0028). */
import { writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { docsRepo, front } from "../test-support/docs-repo";
import { MAX_BATCH_TARGETS, reverify } from "./doc-audit";
import { readDocRecords } from "./doc-records";

const TODAY = "2026-11-12";
const names = (count: number): string[] => Array.from({ length: count }, (_, k) => `src/f${k}.ts`);

/** A document that covers `count` files of src/, none verified yet. */
function crowd(count: number): string {
  return docsRepo({
    "docs/crowd.md": `${front({ covers: ["src/*.ts"], follows: undefined })}# Crowd\n`,
    ...Object.fromEntries(names(count).map((file, k) => [file, `export const v${k} = ${k};\n`])),
  });
}

describe("verification notes [REQ-020]", () => {
  test("the note of a target may not repeat the note already recorded for it", () => {
    const root = docsRepo({});
    const first = readDocRecords(root, "docs/guide.md").records.get("package.json")?.note ?? "";
    expect(first).toContain("fixture");
    expect(() => reverify(root, "docs/guide.md", first, TODAY)).toThrow(/same as the one/);
    expect(() => reverify(root, "docs/guide.md", `  ${first} `, TODAY)).toThrow(/package.json/);
    reverify(root, "docs/guide.md", "read it against package.json again", TODAY);
    expect(readDocRecords(root, "docs/guide.md").records.get("package.json")?.note).toBe(
      "read it against package.json again",
    );
  });

  test("the same words on another target or another document are fine", () => {
    const root = docsRepo({});
    reverify(root, "docs/guide.md", "read it", TODAY);
    reverify(root, "docs/index.md", "read it", TODAY);
    expect(readDocRecords(root, "docs/index.md").records.size).toBe(1);
  });

  test("more than the limit of targets in one call is refused, each one named alone is not", () => {
    const root = crowd(MAX_BATCH_TARGETS + 1);
    expect(() => reverify(root, "docs/crowd.md", "looked at all of them", TODAY)).toThrow(
      /at most 10.*--target/,
    );
    expect(readDocRecords(root, "docs/crowd.md").records.size).toBe(0);
    for (const file of names(MAX_BATCH_TARGETS + 1))
      expect(reverify(root, "docs/crowd.md", `read ${file}`, TODAY, file)).toEqual([file]);
    expect(readDocRecords(root, "docs/crowd.md").records.size).toBe(MAX_BATCH_TARGETS + 1);
  });

  test("a call for exactly the limit is a reading, and the stale ones left after an edit are small", () => {
    const root = crowd(MAX_BATCH_TARGETS);
    expect(reverify(root, "docs/crowd.md", "read the ten", TODAY)).toHaveLength(MAX_BATCH_TARGETS);
    writeFileSync(path.join(root, "src/f3.ts"), "export const v3 = 33;\n");
    expect(reverify(root, "docs/crowd.md", "f3 changed, the text still holds", TODAY)).toEqual([
      "src/f3.ts",
    ]);
  });
});
