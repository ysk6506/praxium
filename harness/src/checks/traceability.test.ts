import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot } from "../common";
import { miniRepo } from "../test-support/mini-repo";
import { plannedRequirements, report, run } from "./traceability";

const REAL = findRoot();
const read = (file: string) => readFileSync(path.join(REAL, file), "utf8");
const REQS = (second: string) =>
  `| ID | x |\n|---|---|\n| REQ-001 | a |\n| REQ-002 | ${second} |\n`;
const SPEC = (title: string) =>
  `import { describe, expect, test } from "vitest";\ndescribe("${title}", () => {\n  test("t", () => {\n    expect(1 + 1).toBe(2);\n  });\n});\n`;

function repo(second: string, files: Record<string, string> = {}): string {
  return miniRepo(
    {
      "harness/config/tests.yaml": read("harness/config/tests.yaml"),
      "docs/product-specs/requirements.md": REQS(second),
      "tests/spec/a.test.ts": SPEC("a [REQ-001]"),
      ...files,
    },
    {},
  );
}

const PLANNED = "**예정(ADR-0007)** later";
const ADR = { "docs/design-docs/adr/0007-later.md": "# later\n" };

describe("planned requirements in the traceability check [REQ-023] [REQ-025]", () => {
  test("a planned requirement without a test is listed, not failed", () => {
    const root = repo(PLANNED, ADR);
    expect(run(root)).toEqual([]);
    expect([...plannedRequirements(root)]).toEqual([["REQ-002", "ADR-0007"]]);
    expect(report(root).planned.join("\n")).toContain("REQ-002 (ADR-0007)");
  });

  test("the same requirement without the marker still fails", () => {
    expect(run(repo("later", ADR)).map((v) => v.code)).toEqual(["TRACE001"]);
  });

  test("a planned requirement must cite an ADR that exists", () => {
    expect(run(repo(PLANNED)).map((v) => v.code)).toEqual(["TRACE005"]);
  });

  test("a planned requirement that a test already cites must drop the marker", () => {
    const root = repo(PLANNED, { ...ADR, "tests/spec/b.test.ts": SPEC("b [REQ-002]") });
    expect(run(root).map((v) => v.code)).toEqual(["TRACE006"]);
    expect(report(root).planned).toEqual([]);
  });
});
