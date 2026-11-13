import { describe, expect, test } from "vitest";
import { goLines, goRepo } from "../test-support/go-repo";
import { loadGoConfig } from "./go-config";
import { dependencyRows, parseGoMod, run } from "./go-deps";

const table = (...rows: string[]): string =>
  goLines(
    "# Architecture",
    "",
    "| 모듈 | 라이선스 | 이유 | 쓰는 단위 |",
    "|---|---|---|---|",
    ...rows,
  );

const arch = (thirdParty: string): string =>
  goLines(
    "units: {}",
    "go:",
    "  root: backend",
    "  module: example.test/app",
    "  units:",
    "    core.kernel: {path: backend/internal/kernel, may_import: []}",
    "  composition_root: core.kernel",
    `  third_party: ${thirdParty}`,
  );

const mod = (...requires: string[]): string =>
  goLines(
    "module example.test/app",
    "",
    "go 1.24",
    "",
    "require (",
    ...requires.map((r) => `\t${r}`),
    ")",
  );

function codes(files: Record<string, string>): string[] {
  const root = goRepo(files);
  const config = loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>;
  return run(root, config)
    .map((v) => v.code)
    .sort();
}

describe("Go modules: go.mod is read [REQ-239]", () => {
  test("module, go version, requires in blocks and on one line, and rewrites", () => {
    const text = goLines(
      "module example.test/app // the module",
      "",
      "go 1.24",
      "",
      "require example.test/one v1.0.0",
      "require (",
      "\texample.test/two v2.0.0",
      "\texample.test/three v0.1.0 // indirect",
      ")",
      "replace example.test/one => ../one",
      "exclude (",
      "\texample.test/two v1.0.0",
      ")",
      "tool example.test/tool",
    );
    const parsed = parseGoMod(text);
    expect([parsed.module, parsed.go]).toEqual(["example.test/app", "1.24"]);
    expect(parsed.requires).toEqual([
      { path: "example.test/one", indirect: false, line: 5 },
      { path: "example.test/two", indirect: false, line: 7 },
      { path: "example.test/three", indirect: true, line: 8 },
    ]);
    expect(parsed.rewrites).toEqual([10, 12]);
    expect(parseGoMod("").module).toBeUndefined();
  });

  test("table rows are the ones under the module, licence, reason header", () => {
    const text = goLines(
      "| 구분 | 무엇 | 없을 때 |",
      "|---|---|---|",
      "| 필수 | `not-a-module` | x |",
      "| 모듈 | 라이선스 | 이유 | 쓰는 단위 |",
      "|---|---|---|---|",
      "| `example.test/one` | MIT | parses a format | `core.kernel` |",
      "| (아직 없다) | | | |",
      "",
      "| `example.test/outside` | MIT | not in the table | |",
    );
    expect(dependencyRows(text)).toEqual([
      { module: "example.test/one", license: "MIT", reason: "parses a format", line: 6 },
    ]);
  });
});

describe("Go modules: go.mod against the table and the contract [REQ-239]", () => {
  const ROW =
    "| `example.test/one` | MIT | parses a format by hand would be long | `core.kernel` |";
  const healthy = {
    "backend/go.mod": mod("example.test/one v1.0.0"),
    "ARCHITECTURE.md": table(ROW),
    "harness/config/architecture.yaml": arch("{example.test/one: [core.kernel]}"),
  };

  test("no module, no table row: nothing to report", () => {
    expect(codes({})).toEqual([]);
  });

  test("a module in go.mod, the table and the contract passes", () => {
    expect(codes(healthy)).toEqual([]);
  });

  test("a required module missing from the table or the contract is reported", () => {
    expect(codes({ ...healthy, "ARCHITECTURE.md": table() })).toEqual(["GODEP010"]);
    expect(codes({ ...healthy, "harness/config/architecture.yaml": arch("{}") })).toEqual([
      "GODEP013",
    ]);
    expect(codes({ "backend/go.mod": mod("example.test/one v1.0.0 // indirect") })).toEqual([
      "GODEP010",
    ]);
  });

  test("a row or a contract entry that go.mod does not require is stale", () => {
    expect(codes({ ...healthy, "backend/go.mod": mod() })).toEqual(["GODEP011", "GODEP014"]);
  });

  test("a row needs a reason and an allowed licence", () => {
    const row = (license: string, reason: string) =>
      `| \`example.test/one\` | ${license} | ${reason} | x |`;
    expect(codes({ ...healthy, "ARCHITECTURE.md": table(row("MIT", "")) })).toEqual(["GODEP012"]);
    expect(codes({ ...healthy, "ARCHITECTURE.md": table(row("GPL-3.0-only", "because")) })).toEqual(
      ["GODEP012"],
    );
    expect(codes({ ...healthy, "ARCHITECTURE.md": table(row("see file", "because")) })).toEqual([
      "GODEP012",
    ]);
    expect(
      codes({ ...healthy, "ARCHITECTURE.md": table(row("(MIT OR GPL-3.0-only)", "because")) }),
    ).toEqual([]);
    expect(codes({ ...healthy, "ARCHITECTURE.md": table(row("MPL-2.0", "because")) })).toEqual([]);
  });

  test("replace and exclude directives are banned", () => {
    const rewritten = goLines(
      "module example.test/app",
      "go 1.24",
      "replace example.test/x => ../x",
    );
    expect(codes({ "backend/go.mod": rewritten })).toEqual(["GODEP020"]);
  });

  test("go.mod must exist, carry the contract's module path and name a go version", () => {
    expect(codes({ "backend/go.mod": goLines("module example.test/other", "go 1.24") })).toEqual([
      "GODEP001",
    ]);
    expect(codes({ "backend/go.mod": goLines("module example.test/app") })).toEqual(["GODEP001"]);
  });

  test("an empty table document, and a repository without go.mod, are handled", () => {
    const root = goRepo({ "ARCHITECTURE.md": "" });
    const config = loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>;
    expect(run(root, config)).toEqual([]);
    const lonely = goRepo({
      "backend/go.mod": mod("example.test/one v1.0.0"),
      "ARCHITECTURE.md": "",
    });
    expect(
      run(lonely, config)
        .map((v) => v.code)
        .sort(),
    ).toEqual(["GODEP010", "GODEP013"]);
    const gone = loadGoConfig(goRepo({})) as NonNullable<ReturnType<typeof loadGoConfig>>;
    expect(run("/nonexistent-praxium-root", gone).map((v) => v.code)).toEqual(["GODEP001"]);
  });
});
