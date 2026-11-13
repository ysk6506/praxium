import { rmSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot, type Violation } from "../common";
import { goLines, goRepo } from "../test-support/go-repo";
import { architecture } from "./go";
import { run } from "./go-arch";
import { loadGoConfig } from "./go-config";

const check = (files: Record<string, string>): Violation[] => {
  const root = goRepo(files);
  return run(root, loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>);
};
const codes = (files: Record<string, string>): string[] =>
  [...new Set(check(files).map((v) => v.code))].sort();

const MODEL = "backend/internal/model/";
const KERNEL = "backend/internal/kernel/";
const OTHER = "backend/internal/other/";
const APP = "backend/cmd/app/";
const imp = (...paths: string[]): string =>
  goLines("package x", "", "import (", ...paths.map((p) => `\t${p}`), ")");

describe("Go architecture: units and imports [REQ-021]", () => {
  test("a healthy Go module passes, and so does a repository without Go", () => {
    expect(check({})).toEqual([]);
    expect(architecture(findRoot())).toEqual([]);
    expect(architecture(goRepo({ "harness/config/architecture.yaml": "units: {}\n" }))).toEqual([]);
  });

  test("a unit may import a unit below it, through its package", () => {
    expect(codes({ [`${OTHER}a.go`]: imp('"example.test/app/internal/kernel"') })).toEqual([]);
    expect(
      codes({
        [`${APP}a.go`]: imp(
          '"example.test/app/internal/model"',
          '"example.test/app/internal/other"',
        ),
      }),
    ).toEqual([]);
  });

  test("a unit that imports one it may not, or a sibling, is rejected", () => {
    const upward = check({ [`${KERNEL}a.go`]: imp('"example.test/app/internal/model"') }).filter(
      (v) => v.code === "GOARCH010",
    );
    expect(upward.map((v) => [v.code, v.path, v.line])).toEqual([
      ["GOARCH010", `${KERNEL}a.go`, 4],
    ]);
    expect(codes({ [`${MODEL}a.go`]: imp('"example.test/app/internal/other"') })).toEqual([
      "GOARCH010",
    ]);
    expect(codes({ [`${MODEL}a_test.go`]: imp('"example.test/app/internal/other"') })).toEqual([
      "GOARCH010",
    ]);
  });

  test("reaching inside another unit's package is rejected, inside one's own is free", () => {
    const files = { [`${KERNEL}sub/x.go`]: goLines("package sub") };
    expect(
      codes({ ...files, [`${MODEL}a.go`]: imp('"example.test/app/internal/kernel/sub"') }),
    ).toEqual(["GOARCH011"]);
    expect(
      codes({ ...files, [`${KERNEL}a.go`]: imp('"example.test/app/internal/kernel/sub"') }),
    ).toEqual([]);
  });

  test("a package of the module that does not exist is reported", () => {
    expect(codes({ [`${MODEL}a.go`]: imp('"example.test/app/internal/ghost"') })).toEqual([
      "GOARCH014",
    ]);
  });

  test("import cycles between units are reported", () => {
    const loose = {
      "harness/config/architecture.yaml": goRepoArchitecture({ kernelMayImport: "[core.model]" }),
      [`${KERNEL}a.go`]: imp('"example.test/app/internal/model"'),
      [`${MODEL}a.go`]: imp('"example.test/app/internal/kernel"'),
    };
    expect(codes(loose)).toEqual(["GOARCH030"]);
  });

  test("files outside every unit, missing folders and unknown unit names are reported", () => {
    expect(codes({ "backend/stray/a.go": goLines("package stray") })).toEqual(["GOARCH001"]);
    expect(
      codes({
        "harness/config/architecture.yaml": goRepoArchitecture({
          extraUnit: "ghost: {path: backend/ghost, may_import: [core.nobody]}",
        }),
      }),
    ).toEqual(["GOARCH003", "GOARCH004"]);
  });

  test("a unit needs Go code of its own and a package doc comment", () => {
    expect(codes({ [`${OTHER}doc.go`]: goLines("package other") })).toEqual(["GOARCH041"]);
    expect(codes({ [`${OTHER}doc.go`]: goLines("// Some words.", "package other") })).toEqual([
      "GOARCH041",
    ]);
    expect(
      codes({ [`${OTHER}doc.go`]: goLines("// Package other has docs.", "package other") }),
    ).toEqual([]);
    const root = goRepo({ [`${OTHER}sub/x.go`]: goLines("package sub") });
    rmSync(path.join(root, OTHER, "doc.go"));
    const config = loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>;
    expect(run(root, config).map((v) => v.code)).toEqual(["GOARCH040"]);
  });
});

describe("Go architecture: modules and the outside world [REQ-021]", () => {
  test("a module that is not declared is rejected, a declared one only where allowed", () => {
    const text = imp('"golang.org/x/text/unicode"');
    expect(codes({ [`${MODEL}a.go`]: text })).toEqual(["GOARCH020"]);
    const declared = {
      "harness/config/architecture.yaml": goRepoArchitecture({
        thirdParty: "{golang.org/x/text: [core.model]}",
      }),
    };
    expect(codes({ ...declared, [`${MODEL}a.go`]: text })).toEqual([]);
    expect(codes({ ...declared, [`${OTHER}a.go`]: text })).toEqual(["GOARCH020"]);
  });

  test("the operating system and the network are only for the composition root", () => {
    expect(codes({ [`${MODEL}a.go`]: imp('"os"', '"net/http"') })).toEqual(["GOARCH021"]);
    expect(codes({ [`${APP}a.go`]: imp('"os"', '"net/http"', '"net/url"') })).toEqual([]);
  });

  test("production code must not import testing", () => {
    expect(codes({ [`${MODEL}a.go`]: imp('"testing"') })).toEqual(["GOARCH013"]);
    expect(codes({ [`${MODEL}a_test.go`]: imp('"testing"', '"testing/iotest"') })).toEqual([]);
  });

  test("unsafe, cgo and plugin code is banned", () => {
    expect(codes({ [`${MODEL}a.go`]: imp('"unsafe"', '"plugin"', '"C"') })).toEqual(["GOARCH050"]);
  });

  test("the process is ended only from the composition root", () => {
    const exit = (name: string) =>
      goLines(
        "package x",
        `import "${name === "log" ? "log" : "os"}"`,
        "func f() {",
        name === "log" ? '\tlog.Fatalf("x")' : "\tos.Exit(1)",
        "}",
      );
    expect(codes({ [`${KERNEL}a.go`]: exit("log") })).toEqual(["GOARCH050"]);
    expect(
      codes({
        [`${KERNEL}b.go`]: goLines("package x", 'import "log"', "func f() { log.Print(1) }"),
      }),
    ).toEqual([]);
    expect(codes({ [`${APP}a.go`]: exit("log") })).toEqual([]);
    expect(codes({ [`${APP}b.go`]: exit("os") })).toEqual([]);
  });

  test("dot imports are banned, blank imports only in the composition root, embed excepted", () => {
    expect(codes({ [`${MODEL}a.go`]: imp('. "fmt"') })).toEqual(["GOARCH051"]);
    expect(codes({ [`${MODEL}a.go`]: imp('_ "image/png"') })).toEqual(["GOARCH051"]);
    expect(codes({ [`${MODEL}a.go`]: imp('_ "embed"') })).toEqual([]);
    expect(codes({ [`${APP}a.go`]: imp('_ "image/png"') })).toEqual([]);
  });

  test("func init is banned, a method or a function that merely contains init is not", () => {
    expect(codes({ [`${MODEL}a.go`]: goLines("package model", "func init() {}") })).toEqual([
      "GOARCH053",
    ]);
    expect(
      codes({
        [`${MODEL}a.go`]: goLines(
          "package model",
          "func (s S) init() {}",
          "func initial() {}",
          "var x = func() { init() }",
        ),
      }),
    ).toEqual([]);
  });
});

describe("Go architecture: no direct clock, randomness or environment [REQ-001]", () => {
  const read = (body: string, imports = '"time"') =>
    goLines("package x", `import ${imports}`, "func f() {", `\t${body}`, "}");

  test("reading the clock or the environment outside the designated file is rejected", () => {
    for (const [body, imports] of [
      ["_ = time.Now()", '"time"'],
      ["_ = time.Since(t)", '"time"'],
      ["_ = time.Until(t)", '"time"'],
      ['_ = os.Getenv("A")', '"os"'],
      ['_, _ = os.LookupEnv("A")', '"os"'],
      ["_ = os.Environ()", '"os"'],
      ["now := time.Now", '"time"'],
      ["_ = clock.Now()", 'clock "time"'],
    ] as const) {
      expect(codes({ [`${APP}a.go`]: read(body, imports) }), body).toEqual(["GOARCH052"]);
    }
  });

  test("randomness is banned by import, in tests too", () => {
    for (const path of ['"math/rand"', '"math/rand/v2"', '"crypto/rand"']) {
      expect(codes({ [`${MODEL}a.go`]: imp(path) }), path).toEqual(["GOARCH052"]);
    }
    expect(codes({ [`${MODEL}a_test.go`]: read("_ = time.Now()") })).toEqual(["GOARCH052"]);
  });

  test("the designated file may read them, and a value called Now on something else is fine", () => {
    const entropy = goLines(
      "package kernel",
      'import (\n\t"crypto/rand"\n\t"time"\n)',
      "func f() { _ = time.Now(); rand.Read(nil) }",
    );
    expect(codes({ [`${KERNEL}entropy.go`]: entropy })).toEqual([]);
    expect(codes({ [`${KERNEL}other.go`]: entropy })).toEqual(["GOARCH052"]);
    expect(
      codes({
        [`${MODEL}a.go`]: goLines(
          "package model",
          'import "time"',
          "func f(c Clock) time.Time { return c.Now() }",
        ),
      }),
    ).toEqual([]);
  });
});

function goRepoArchitecture(options: {
  kernelMayImport?: string;
  extraUnit?: string;
  thirdParty?: string;
}): string {
  return goLines(
    "units: {}",
    "go:",
    "  root: backend",
    "  module: example.test/app",
    "  units:",
    `    core.kernel: {path: backend/internal/kernel, may_import: ${options.kernelMayImport ?? "[]"}}`,
    "    core.model: {path: backend/internal/model, may_import: [core.kernel]}",
    "    core.other: {path: backend/internal/other, may_import: [core.kernel]}",
    '    cmd.app: {path: backend/cmd/app, may_import: ["core.*"]}',
    ...(options.extraUnit ? [`    ${options.extraUnit}`] : []),
    "  composition_root: cmd.app",
    `  third_party: ${options.thirdParty ?? "{}"}`,
    "  restricted_stdlib: {os: [cmd.app], net/http: [cmd.app]}",
    "  nondeterminism_allowed: [backend/internal/kernel/entropy.go]",
  );
}
