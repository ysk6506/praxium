import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot } from "../common";
import { goRepo } from "../test-support/go-repo";
import { report, run } from "./traceability";

const real = (file: string) => readFileSync(path.join(findRoot(), file), "utf8");
const REQS = (second: string) =>
  `| ID | x |\n|---|---|\n| REQ-001 | a |\n| REQ-002 | ${second} |\n`;
const TS_SPEC = `import { describe, expect, test } from "vitest";\ndescribe("a [REQ-001]", () => {\n  test("t", () => {\n    expect(1 + 1).toBe(2);\n  });\n});\n`;
const MODEL = "backend/internal/model/model_test.go";

const goTest = (doc: string, extra = ""): string =>
  `package model\n\nimport "testing"\n\n${doc}\nfunc TestDouble(t *testing.T) {\n\tif Double() != 84 {\n\t\tt.Fatal("unexpected")\n\t}\n${extra}}\n`;

function repo(second: string, model: string, files: Record<string, string> = {}): string {
  return goRepo({
    "harness/config/tests.yaml": real("harness/config/tests.yaml"),
    "docs/product-specs/requirements.md": REQS(second),
    "tests/spec/a.test.ts": TS_SPEC,
    "docs/design-docs/adr/0007-later.md": "# later\n",
    [MODEL]: model,
    ...files,
  });
}

const codes = (root: string) => run(root).map((v) => v.code);

describe("Go tags in the traceability check [REQ-025]", () => {
  test("a requirement a Go test cites in its doc comment is covered", () => {
    const root = repo("b", goTest("// [REQ-002]"));
    expect(codes(root)).toEqual([]);
  });

  test("one comment line may carry several tags", () => {
    const root = repo("b", goTest("// [REQ-001] [REQ-002]"));
    expect(codes(root)).toEqual([]);
  });

  test("a requirement no test cites still fails, Go or not", () => {
    expect(codes(repo("b", goTest("")))).toEqual(["TRACE001"]);
  });

  test("a tag for an id that is no requirement is TRACE002 at the Go line", () => {
    const root = repo("b", goTest("// [REQ-002] [REQ-777]"));
    const found = run(root).filter((v) => v.code === "TRACE002");
    expect(found).toHaveLength(1);
    expect(found[0]?.path).toBe(MODEL);
    expect(found[0]?.message).toContain("REQ-777");
  });

  test("a subtest name may carry a tag", () => {
    const body = '\tt.Run("row [REQ-002]", func(t *testing.T) {\n\t\tt.Log("x")\n\t})\n';
    expect(codes(repo("b", goTest("", body)))).toEqual([]);
  });

  test("a planned requirement that only a Go test cites must drop the marker", () => {
    const root = repo("**예정(ADR-0007)** later", goTest("// [REQ-002]"));
    expect(codes(root)).toEqual(["TRACE006"]);
    expect(report(root).planned).toEqual([]);
  });

  test("a planned requirement no Go test cites stays listed", () => {
    const root = repo("**예정(ADR-0007)** later", goTest(""));
    expect(codes(root)).toEqual([]);
    expect(report(root).planned.join("\n")).toContain("REQ-002");
  });
});

describe("Go tags that link nothing are TRACE007 [REQ-025]", () => {
  test.each([
    ["a tag in a comment inside a body", goTest("", "\t// [REQ-002] checks it\n")],
    ["a tag on a helper's comment", `${goTest("// [REQ-002]")}\n// [REQ-001]\nfunc helper() {}\n`],
    ["several ids in one bracket", goTest("// [REQ-001, REQ-002]")],
  ])("%s", (_name, model) => {
    const root = repo("b", model);
    expect(codes(root)).toContain("TRACE007");
    expect(run(root).find((v) => v.code === "TRACE007")?.path).toBe(MODEL);
  });

  test("a tag in a comment of a non-test Go file is not read", () => {
    const root = repo("b", goTest("// [REQ-002]"), {
      "backend/internal/model/model.go":
        "package model\n\n// [REQ-001] is mentioned here\nfunc Double() int { return 84 }\n",
    });
    expect(codes(root)).toEqual([]);
  });
});
