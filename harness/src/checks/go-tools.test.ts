import { mkdtempSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, test } from "vitest";
import { goLines, goRepo } from "../test-support/go-repo";
import { loadGoConfig } from "./go-config";
import { runGofmt, runTool, tail } from "./go-run";
import { run } from "./go-tools";

const MODEL = "backend/internal/model/";

function check(files: Record<string, string>) {
  const root = goRepo(files);
  return run(root, loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>);
}

const originalPath = process.env.PATH;
afterEach(() => {
  process.env.PATH = originalPath;
});

describe("Go tools: formatting and vet [REQ-021]", () => {
  test("a formatted module that vet accepts passes", () => {
    expect(check({})).toEqual([]);
  });

  test("a file that gofmt would change is reported", () => {
    const found = check({ [`${MODEL}a.go`]: "package model\nfunc   Ugly( ) {  }\n" });
    expect(found.map((v) => [v.code, v.path])).toEqual([["GOTOOL001", `${MODEL}a.go`]]);
  });

  test("a problem vet finds is reported with its file and line", () => {
    const text = goLines(
      "package model",
      "",
      'import "fmt"',
      "",
      'func Bad() { fmt.Printf("%d", "x") }',
    );
    const found = check({ [`${MODEL}a.go`]: text });
    expect(found.map((v) => [v.code, v.path, v.line])).toEqual([["GOTOOL002", `${MODEL}a.go`, 5]]);
  });

  test("code that does not compile is reported by vet", () => {
    const found = check({ [`${MODEL}a.go`]: goLines("package model", "", 'import "os"') });
    expect(found.map((v) => v.code)).toEqual(["GOTOOL002"]);
    expect(found[0]?.path).toBe(`${MODEL}a.go`);
  });

  test("a syntax error is reported once, by gofmt, and vet adds its own word", () => {
    const found = check({ [`${MODEL}a.go`]: "package model\nfunc (\n" });
    expect([...new Set(found.map((v) => v.code))]).toEqual(["GOTOOL001", "GOTOOL002"]);
  });

  test("files under testdata are not formatted by the rules of the module", () => {
    expect(check({ [`${MODEL}testdata/x.go`]: "package   x\n" })).toEqual([]);
  });

  test("a module without go.mod is left to GODEP001", () => {
    const root = goRepo();
    const config = loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>;
    expect(run(root, { ...config, root: "nowhere" })).toEqual([]);
  });

  test("without Go on the PATH the check says so", () => {
    const root = goRepo();
    const config = loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>;
    process.env.PATH = path.join(tmpdir(), "praxium-no-such-bin");
    expect(run(root, config).map((v) => v.code)).toEqual(["GOTOOL000"]);
  });

  test("gofmt is found through GOROOT when only go is on the PATH", () => {
    const root = goRepo();
    const goBinary = runTool(root, "sh", ["-c", "command -v go"]).stdout.trim();
    const bin = mkdtempSync(path.join(tmpdir(), "praxium-gobin-"));
    symlinkSync(goBinary, path.join(bin, "go"));
    process.env.PATH = bin;
    const result = runGofmt(root, ["-l", "."]);
    expect([result.ok, result.missing]).toEqual([true, false]);
  });

  test("the tail of an output keeps its last lines", () => {
    expect(tail("a\nb\nc\nd", 2)).toBe("c\nd");
  });
});
