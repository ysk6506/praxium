import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { findRoot } from "../common";
import { verifyDefaults } from "../test-support/docs-repo";
import { miniRepo } from "../test-support/mini-repo";
import { recordPath } from "./doc-records";
import { main, run } from "./docs";

const TODAY = new Date().toISOString().slice(0, 10);
const realConfig = readFileSync(path.join(findRoot(), "harness/config/docs.yaml"), "utf8");
// Every fixture doc follows package.json so that DOC092 (a doc must cover or follow something) stays quiet (ADR-0180).
const front = (fields: Record<string, unknown> = {}): string =>
  `---\n${YAML.stringify({ title: "T", kind: "guide", status: "active", follows: ["package.json"], ...fields })}---\n`;
const ADR = "docs/design-docs/adr/0001-x.md";

function docsRepo(files: Record<string, string> = {}): string {
  const root = miniRepo(
    {
      "harness/config/docs.yaml": realConfig,
      "harness/src/rules.ts": 'export const RULE = "ARCH010";\n',
      "src/mod.ts":
        "/** Doc. */\nexport function add(a: number, b: number): number {\n  return a + b;\n}\n",
      "docs/index.md": `${front()}# Index\n[guide](guide.md)\n`,
      "docs/guide.md": `${front()}# Guide\n`,
      ...files,
    },
    { "package.json": "{}\n" },
  );
  verifyDefaults(root, TODAY);
  return root;
}

const adrRepo = (): string =>
  docsRepo({
    [ADR]: `${front({ kind: "decision", enforced_by: ["ARCH010"] })}# Decision\nWe do X because Y.\n`,
    "docs/guide.md": `${front()}[adr](design-docs/adr/0001-x.md)\n`,
  });
const codes = (root: string): string[] => [...new Set(run(root, TODAY).map((v) => v.code))];

describe("docs command line [REQ-020] [REQ-021]", () => {
  const quiet = <T>(fn: () => T): T => {
    const write = process.stdout.write.bind(process.stdout);
    process.stdout.write = (() => true) as typeof process.stdout.write;
    try {
      return fn();
    } finally {
      process.stdout.write = write;
    }
  };

  test("reverify writes the note into the record and exits 0, with the root taken from --root", () => {
    const root = docsRepo({ "docs/guide.md": `${front({ covers: ["src/*.ts"] })}# Guide\n` });
    const code = main([
      "reverify",
      path.join(root, "docs/guide.md"),
      "--root",
      root,
      "--note",
      "checked",
    ]);
    expect(code).toBe(0);
    expect(
      readFileSync(path.join(root, recordPath("docs/guide.md", "src/mod.ts")), "utf8"),
    ).toContain("checked");
  });

  test("seal-adr seals a decision doc so that the docs check no longer asks for a hash", () => {
    const root = adrRepo();
    expect(codes(root)).toContain("DOC071");
    expect(main(["seal-adr", path.join(root, ADR), "--root", root])).toBe(0);
    expect(codes(root)).not.toContain("DOC071");
  });

  test("with no command it runs the check and exits 1 when a rule fails, 0 when the docs are clean", () => {
    const clean = docsRepo();
    expect(quiet(() => main(["--root", clean]))).toBe(0);
    const broken = docsRepo({ "docs/bad.md": "no front matter\n" });
    expect(quiet(() => main(["--root", broken]))).toBe(1);
  });
});
