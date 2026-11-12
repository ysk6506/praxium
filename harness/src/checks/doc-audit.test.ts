import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { docsRepo, front } from "../test-support/docs-repo";
import { audit, auditQueue, formatQueue, reverify } from "./doc-audit";
import { auditPath } from "./doc-records";
import { testRowNotes } from "./doc-tests";

const docs = ["docs/guide.md", "docs/index.md"];

describe("audit queue [REQ-020]", () => {
  test("a re-verify that leaves the body alone puts the doc in the queue; an edit does not", () => {
    const root = docsRepo({});
    expect(auditQueue(root, docs)).toEqual([]);
    reverify(root, "docs/guide.md", "again", "2026-11-12");
    expect(auditQueue(root, docs).map((e) => [e.file, e.unchanged])).toEqual([
      ["docs/guide.md", 1],
    ]);
    expect(formatQueue(auditQueue(root, docs))).toContain("last_audited=never");
    writeFileSync(
      path.join(root, "docs/index.md"),
      `${front()}# Index\n[guide](guide.md)\nedited\n`,
    );
    reverify(root, "docs/index.md", "after an edit", "2026-11-12");
    expect(auditQueue(root, docs).map((e) => e.file)).toEqual(["docs/guide.md"]);
  });

  test("audit records the date in the document's audit record and needs a note; the longest unaudited comes first", () => {
    const root = docsRepo({});
    for (const file of docs) {
      reverify(root, file, "a", "2026-11-10");
      reverify(root, file, "b", "2026-11-11");
    }
    audit(root, "docs/index.md", "read against the code, fine", "2026-11-12");
    expect(auditQueue(root, docs).map((e) => e.file)).toEqual(["docs/guide.md"]);
    reverify(root, "docs/index.md", "c", "2026-11-13");
    reverify(root, "docs/index.md", "d", "2026-11-14");
    expect(auditQueue(root, docs).map((e) => e.file)).toEqual(["docs/guide.md", "docs/index.md"]);
    expect(readFileSync(path.join(root, auditPath("docs/index.md")), "utf8")).toContain(
      "2026-11-12",
    );
    expect(() => audit(root, "docs/index.md", " ")).toThrow(/--note/);
  });
});

describe("test rows [REQ-020]", () => {
  const table = (rows: string[], head = "| 시험 | 내용 |\n|---|---|") =>
    `${front()}# D\n\n## 시험\n\n${head}\n${rows.join("\n")}\n`;

  test("rows of a test section without an existing test path are counted; planned rows are not", () => {
    const root = docsRepo({
      "apps/x/a.test.ts": "export {};\n",
      "docs/guide.md": table(["| a | `apps/x/a.test.ts` |", "| b | 말만 있다 |", "| c | 예정 |"]),
    });
    const notes = testRowNotes(root, ["docs/guide.md"]);
    expect(notes).toHaveLength(1);
    expect(notes[0]).toContain("DOC096");
    expect(notes[0]).toContain("docs/guide.md (1/2)");
  });

  test("a table with a 시험 파일 column is judged by that column, with brace paths expanded", () => {
    const root = docsRepo({
      "apps/x/a-1.test.ts": "export {};\n",
      "apps/x/a-2.test.ts": "export {};\n",
      "docs/guide.md": `${front()}# D\n\n| 종류 | 시험 파일 |\n|---|---|\n| a | \`apps/x/a-{1,2}.test.ts\` |\n| b | \`apps/x/a-{1,3}.test.ts\` |\n`,
    });
    expect(testRowNotes(root, ["docs/guide.md"])[0]).toContain("(1/2)");
  });
});
