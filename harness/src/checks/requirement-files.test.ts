import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot } from "../common";
import { miniRepo } from "../test-support/mini-repo";
import { requirementFiles } from "./requirement-files";
import { plannedRequirements, requirementIds, run } from "./traceability";

const read = (file: string) => readFileSync(path.join(findRoot(), file), "utf8");
const SPEC = (title: string) =>
  `import { describe, expect, test } from "vitest";\ndescribe("${title}", () => {\n  test("t", () => {\n    expect(1 + 1).toBe(2);\n  });\n});\n`;
const table = (...rows: string[]): string => `| ID | x |\n|---|---|\n${rows.join("\n")}\n`;

function repo(files: Record<string, string>): string {
  return miniRepo(
    {
      "harness/config/tests.yaml": read("harness/config/tests.yaml"),
      "tests/spec/a.test.ts": SPEC("a [REQ-001]"),
      ...files,
    },
    {},
  );
}

describe("requirements in the entry document and the area documents [REQ-397]", () => {
  test("the entry document comes first, then the area documents by name", () => {
    const root = repo({
      "docs/product-specs/requirements.md": table("| REQ-001 | a |"),
      "docs/product-specs/requirements-zeta.md": table("| REQ-003 | c |"),
      "docs/product-specs/requirements-alpha.md": table("| REQ-002 | b |"),
      "docs/product-specs/requirements-Bad.md": table("| REQ-009 | not read |"),
      "docs/product-specs/notes.md": table("| REQ-008 | not read |"),
    });
    expect(requirementFiles(root)).toEqual([
      "docs/product-specs/requirements.md",
      "docs/product-specs/requirements-alpha.md",
      "docs/product-specs/requirements-zeta.md",
    ]);
    expect(requirementIds(root).ids).toEqual(["REQ-001", "REQ-002", "REQ-003"]);
  });

  test("a test can cite a number that only an area document has", () => {
    const root = repo({
      "docs/product-specs/requirements.md": table("| REQ-001 | a |"),
      "docs/product-specs/requirements-area.md": table("| REQ-002 | b |"),
      "tests/spec/b.test.ts": SPEC("b [REQ-002]"),
    });
    expect(run(root)).toEqual([]);
  });

  test("an area requirement without a test fails, and a cited unknown number fails", () => {
    const root = repo({
      "docs/product-specs/requirements.md": table("| REQ-001 | a |"),
      "docs/product-specs/requirements-area.md": table("| REQ-002 | b |"),
      "tests/spec/b.test.ts": SPEC("b [REQ-777]"),
    });
    expect(run(root).map((v) => v.code)).toEqual(["TRACE002", "TRACE001"]);
  });

  test("the same number in two documents fails, naming the second file", () => {
    const root = repo({
      "docs/product-specs/requirements.md": table("| REQ-001 | a |"),
      "docs/product-specs/requirements-area.md": table("| REQ-001 | again |"),
    });
    const found = run(root).filter((v) => v.code === "TRACE004");
    expect(found.map((v) => v.path)).toEqual(["docs/product-specs/requirements-area.md"]);
    expect(found[0]?.message).toContain("REQ-001");
  });

  test("a planned marker counts in an area document", () => {
    const root = repo({
      "docs/product-specs/requirements.md": table("| REQ-001 | a |"),
      "docs/product-specs/requirements-area.md": table("| REQ-002 | **예정(ADR-0007)** later |"),
      "docs/design-docs/adr/0007-later.md": "# later\n",
    });
    expect([...plannedRequirements(root)]).toEqual([["REQ-002", "ADR-0007"]]);
    expect(run(root)).toEqual([]);
  });
});
