/** Gaps the audit of the Go checks found, and the fixtures that keep them shut (ADR-0028). */
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import type { Violation } from "../common";
import { findRoot } from "../common";
import { goLines, goRepo } from "../test-support/go-repo";
import { commitAll, writeFiles } from "../test-support/mini-repo";
import { run } from "./go-arch";
import { loadGoConfig } from "./go-config";
import { goTestInfos } from "./go-test-units";

const KERNEL = "backend/internal/kernel/";
const MODEL = "backend/internal/model/";
const OTHER = "backend/internal/other/";
const APP = "backend/cmd/app/";

/** The architecture of the shared Go fixture, with the lines a test wants to vary. */
function architecture(options: {
  model?: string;
  other?: string;
  os?: string;
  lockedSupport?: string;
}): string {
  return goLines(
    "units: {}",
    "go:",
    "  root: backend",
    "  module: example.test/app",
    "  units:",
    "    core.kernel: {path: backend/internal/kernel, may_import: []}",
    `    core.model: {path: backend/internal/model, may_import: ${options.model ?? "[core.kernel]"}}`,
    `    core.other: {path: backend/internal/other, may_import: ${options.other ?? "[core.kernel]"}}`,
    '    cmd.app: {path: backend/cmd/app, may_import: ["core.*"]}',
    "  composition_root: cmd.app",
    "  third_party: {}",
    `  restricted_stdlib: {os: ${options.os ?? "[cmd.app]"}, net/http: [cmd.app]}`,
    "  nondeterminism_allowed: [backend/internal/kernel/entropy.go]",
    `  locked_support: ${options.lockedSupport ?? "[]"}`,
  );
}

const check = (files: Record<string, string>): Violation[] => {
  const root = goRepo(files);
  return run(root, loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>);
};
const codes = (files: Record<string, string>): string[] =>
  [...new Set(check(files).map((v) => v.code))].sort();
const imp = (...paths: string[]): string =>
  goLines("package x", "", "import (", ...paths.map((p) => `\t${p}`), ")");
const arch = (options: Parameters<typeof architecture>[0]): Record<string, string> => ({
  "harness/config/architecture.yaml": architecture(options),
});

describe("Go import paths are read as written [REQ-021]", () => {
  test("a path written with an escape is refused, whichever rule it would have dodged", () => {
    // "o\x73" is "os" and "ti\x6de" is "time": go vet is quiet, and the rules below saw neither.
    const evade = goLines(
      "package model",
      "",
      "import (",
      '\to "o\\x73"',
      '\tt "ti\\x6de"',
      '\t_ "un\\x73afe"',
      ")",
      "",
      'func f() { _, _ = o.LookupEnv("HOME"); _ = t.Now() }',
    );
    const found = check({ [`${MODEL}evade.go`]: evade });
    expect(found.filter((v) => v.code === "GOARCH054").map((v) => v.line)).toEqual([4, 5, 6]);
    expect(codes({ [`${MODEL}evade_test.go`]: evade })).toContain("GOARCH054");
  });

  test("a path with a space or any other odd character is refused, plain paths are not", () => {
    expect(codes({ [`${MODEL}a.go`]: imp('"fmt "') })).toContain("GOARCH054");
    expect(codes({ [`${MODEL}a.go`]: imp("`fmt\u0000`") })).toContain("GOARCH054");
    expect(codes({ [`${MODEL}a.go`]: imp('"fmt"', '"encoding/json"', '"math/rand/v2x"') })).toEqual(
      [],
    );
    expect(codes({ [`${MODEL}a.go`]: imp("`fmt`") })).toEqual([]);
    expect(codes({ [`${MODEL}a.go`]: imp('"example.test/app/internal/kernel"') })).toEqual([]);
  });
});

describe("a test file runs everywhere [REQ-021]", () => {
  const test_ = (lines: string[]): string => goLines(...lines, "package model");

  test("a build constraint line before the package clause is refused", () => {
    for (const lines of [
      ["//go:build linux", ""],
      ["//go:build ignore", ""],
      ["// +build linux,amd64", ""],
      ["// Comment first.", "", "//go:build manual", ""],
    ]) {
      expect(codes({ [`${MODEL}a_test.go`]: test_(lines) }), lines.join("|")).toEqual([
        "GOARCH055",
      ]);
    }
  });

  test("an operating-system or architecture suffix in the file name is refused", () => {
    const body = goLines("package model");
    for (const name of [
      "a_linux_test.go",
      "a_windows_test.go",
      "a_arm64_test.go",
      "a_linux_amd64_test.go",
      "a_wasm_test.go",
    ]) {
      expect(codes({ [`${MODEL}${name}`]: body }), name).toEqual(["GOARCH055"]);
    }
  });

  test("production code, plain names and a constraint-looking comment elsewhere are fine", () => {
    expect(
      codes({ [`${MODEL}a_linux.go`]: goLines("//go:build linux", "", "package model") }),
    ).toEqual([]);
    expect(codes({ [`${MODEL}linux_test.go`]: goLines("package model") })).toEqual([]);
    expect(codes({ [`${MODEL}a_linuxish_test.go`]: goLines("package model") })).toEqual([]);
    const late = goLines("package model", "", "//go:build linux", "var x = 1");
    expect(codes({ [`${MODEL}a_test.go`]: late })).toEqual([]);
  });
});

describe("imports only tests may have are declared apart [REQ-021]", () => {
  const importsOther = goLines("package model", 'import "example.test/app/internal/other"');

  test("production code may not import what only tests may import", () => {
    const configured = {
      "harness/config/architecture.yaml": architecture({ model: "[core.kernel]" }).replace(
        "may_import: [core.kernel]}\n    core.other",
        "may_import: [core.kernel], test_may_import: [core.other]}\n    core.other",
      ),
    };
    const found = check({ ...configured, [`${MODEL}a.go`]: importsOther });
    expect(found.map((v) => v.code)).toContain("GOARCH010");
    expect(found.find((v) => v.code === "GOARCH010")?.message).toContain("test files only");
  });

  test("a test file may import them, and a unit with no such list is as strict as before", () => {
    const configured = {
      "harness/config/architecture.yaml": architecture({}).replace(
        "core.model: {path: backend/internal/model, may_import: [core.kernel]}",
        "core.model: {path: backend/internal/model, may_import: [core.kernel], test_may_import: [core.other]}",
      ),
    };
    expect(codes({ ...configured, [`${MODEL}a_test.go`]: importsOther })).toEqual([]);
    expect(codes({ [`${MODEL}a_test.go`]: importsOther })).toEqual(["GOARCH010"]);
  });

  test("a name that may_import already allows, or that is no unit, is a mistake in the contract", () => {
    const base = architecture({});
    const redundant = base.replace(
      "core.model: {path: backend/internal/model, may_import: [core.kernel]}",
      "core.model: {path: backend/internal/model, may_import: [core.kernel], test_may_import: [core.kernel]}",
    );
    expect(codes({ "harness/config/architecture.yaml": redundant })).toEqual(["GOARCH005"]);
    const unknown = base.replace(
      "core.model: {path: backend/internal/model, may_import: [core.kernel]}",
      "core.model: {path: backend/internal/model, may_import: [core.kernel], test_may_import: [core.ghost]}",
    );
    expect(codes({ "harness/config/architecture.yaml": unknown })).toEqual(["GOARCH004"]);
  });
});

describe("a restricted package can be limited to one file [REQ-021]", () => {
  const useOs = goLines("package kernel", 'import "os"', "var _ = os.Args");
  const oneFile = arch({ os: "[cmd.app, backend/internal/kernel/entropy.go]" });

  test("only the named file of the unit may import it", () => {
    expect(codes({ ...oneFile, [`${KERNEL}entropy.go`]: useOs })).toEqual([]);
    const entropy = { ...oneFile, [`${KERNEL}entropy.go`]: goLines("package kernel") };
    expect(codes({ ...entropy, [`${KERNEL}other.go`]: useOs })).toEqual(["GOARCH021"]);
    expect(codes({ ...entropy, [`${KERNEL}entropy_test.go`]: useOs })).toEqual(["GOARCH021"]);
    expect(codes({ ...entropy, [`${APP}a.go`]: goLines("package main", 'import "os"') })).toEqual(
      [],
    );
  });

  test("naming a file or a unit that does not exist is a mistake in the contract", () => {
    expect(codes(arch({ os: "[cmd.app, backend/internal/kernel/ghost.go]" }))).toEqual([
      "GOARCH006",
    ]);
    expect(codes(arch({ os: "[cmd.app, core.ghost]" }))).toEqual(["GOARCH006"]);
    expect(codes(arch({ lockedSupport: "[backend/internal/ghost]" }))).toEqual(["GOARCH006"]);
  });
});

describe("test support that locked tests lean on is fingerprinted [REQ-023]", () => {
  const SUITE = `${OTHER}suite.go`;
  const suite = (body: string): string =>
    goLines(
      "package other",
      "",
      "// TB is what a case may do.",
      "type TB interface{ Fatalf(string, ...any) }",
      "",
      "// Case is one case.",
      "type Case struct {",
      "\tName string",
      "\tRun  func(TB)",
      "}",
      "",
      "// Cases lists the suite.",
      "func Cases() []Case {",
      "\treturn []Case{",
      body,
      "\t}",
      "}",
    );
  const CASES = [
    '\t\t{Name: "a", Run: func(tb TB) { if one() != 1 { tb.Fatalf("a") } }},',
    '\t\t{Name: "b", Run: func(tb TB) { if one() != 1 { tb.Fatalf("b") } }},',
  ];
  const caller = (pkg: string, via = "other"): string =>
    goLines(
      `package ${pkg}`,
      "",
      "import (",
      '\t"testing"',
      "",
      '\t"example.test/app/internal/other"',
      ")",
      "",
      `func TestSuite(t *testing.T) {`,
      `\tfor _, c := range ${via}.Cases() {`,
      "\t\tt.Run(c.Name, func(t *testing.T) { c.Run(t) })",
      "\t}",
      "}",
    );
  const helper = goLines("package other", "", "func one() int { return 1 }");
  const files = (cases: string[], support = "[backend/internal/other]") => ({
    ...arch({ lockedSupport: support }),
    [SUITE]: suite(cases.join("\n")),
    [`${OTHER}one.go`]: helper,
    [`${MODEL}suite_test.go`]: caller("model"),
  });
  const digest = (root: string, key: string): string | undefined =>
    goTestInfos(root).find((i) => i.key === key)?.digest;
  const KEY = "go:backend/internal/model::TestSuite";

  test("removing a case or weakening one changes the digest of the test that runs the suite", () => {
    const before = digest(goRepo(files(CASES)), KEY);
    expect(before).toBeDefined();
    expect(digest(goRepo(files(CASES)), KEY)).toBe(before);
    expect(digest(goRepo(files(CASES.slice(0, 1))), KEY)).not.toBe(before);
    expect(
      digest(
        goRepo(files([CASES[0] as string, (CASES[1] as string).replace("!= 1", "!= 2")])),
        KEY,
      ),
    ).not.toBe(before);
    expect(
      digest(
        goRepo(files([CASES[0] as string, (CASES[1] as string).replace('tb.Fatalf("b")', "")])),
        KEY,
      ),
    ).not.toBe(before);
  });

  test("a helper of the support package counts too, and an alias does not hide the import", () => {
    const before = digest(goRepo(files(CASES)), KEY);
    const changed = {
      ...files(CASES),
      [`${OTHER}one.go`]: goLines("package other", "func one() int { return 2 }"),
    };
    expect(digest(goRepo(changed), KEY)).not.toBe(before);
    const aliased = {
      ...files(CASES),
      [`${MODEL}suite_test.go`]: caller("model", "o").replace(
        '\t"example.test/app/internal/other"',
        '\to "example.test/app/internal/other"',
      ),
    };
    const base = digest(goRepo(aliased), KEY);
    expect(
      digest(goRepo({ ...aliased, [SUITE]: suite(CASES.slice(0, 1).join("\n")) }), KEY),
    ).not.toBe(base);
  });

  test("without the declaration nothing changes, and tests that do not import the package are untouched", () => {
    const without = (cases: string[]) => files(cases, "[]");
    expect(digest(goRepo(without(CASES)), KEY)).toBe(
      digest(goRepo(without(CASES.slice(0, 1))), KEY),
    );
    const unrelated = (cases: string[]) => ({
      ...files(cases),
      [`${APP}unrelated_test.go`]: goLines(
        "package main",
        'import "testing"',
        "func TestUnrelated(t *testing.T) {",
        "\tCases := 1",
        "\tif Cases != 1 { t.Fatal(1) }",
        "}",
      ),
    });
    const key = "go:backend/cmd/app::TestUnrelated";
    expect(digest(goRepo(unrelated(CASES)), key)).toBe(
      digest(goRepo(unrelated(CASES.slice(0, 1))), key),
    );
  });
});

describe("a compiled binary is not committed [REQ-021]", () => {
  const ELF = "\x7fELF\u0002\u0001\u0001";
  const BIN = "backend/cmd/app/app";

  test("a binary among the files, or a test binary, is refused", () => {
    expect(codes({ [BIN]: ELF })).toEqual(["GOARCH056"]);
    expect(codes({ "backend/internal/model/model.test": "plain text" })).toEqual(["GOARCH056"]);
    expect(codes({ "backend/cmd/app/app.exe": "MZ\u0090\u0000" })).toEqual(["GOARCH056"]);
    expect(codes({ "backend/internal/model/notes.txt": "ELF is a word here" })).toEqual([]);
  });

  test("with git, a binary that .gitignore hides is fine and one that it does not hide is refused", () => {
    const root = goRepo({ ".gitignore": "backend/cmd/app/app\n" });
    commitAll(root);
    writeFiles(root, { [BIN]: ELF });
    const config = loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>;
    expect(run(root, config)).toEqual([]);
    writeFiles(root, { ".gitignore": "\n" });
    expect(run(root, config).map((v) => [v.code, v.path])).toEqual([["GOARCH056", BIN]]);
  });

  test("this repository's .gitignore hides the program and the test binaries", () => {
    const root = goRepo({
      ".gitignore": readFileSync(path.join(findRoot(), ".gitignore"), "utf8"),
    });
    commitAll(root);
    writeFiles(root, {
      "backend/cmd/praxium/praxium": ELF,
      "backend/internal/model/model.test": ELF,
    });
    const config = loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>;
    expect(run(root, config).filter((v) => v.code === "GOARCH056")).toEqual([]);
  });
});
