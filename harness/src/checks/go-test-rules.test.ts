/** The Go side of TEST003 to TEST005: tests that cannot fail or that hide others (ADR-0028). */
import { describe, expect, test } from "vitest";
import { goLines, goRepo } from "../test-support/go-repo";
import { testIntegrity } from "./go";
import { loadGoConfig } from "./go-config";
import { ruleViolations } from "./go-test-rules";

const FILE = "backend/internal/model/rules_test.go";

/** A test file whose test function holds `body`. */
const withBody = (...body: string[]): string =>
  goLines(
    "package model",
    'import "testing"',
    "func TestRule(t *testing.T) {",
    ...body.map((line) => `\t${line}`),
    "}",
  );

const codesOf = (text: string): string[] => {
  const root = goRepo({ [FILE]: text });
  const config = loadGoConfig(root) as NonNullable<ReturnType<typeof loadGoConfig>>;
  return ruleViolations(root, config).map((v) => v.code);
};

describe("a Go assertion decided by constants cannot fail [REQ-023]", () => {
  test.each([
    ['if 1+1 != 2 { t.Fatal("arithmetic") }'],
    ['if true == false { t.Fatal("x") }'],
    ['if false { t.Error("never") }'],
    ['if "a" != "a" { t.Error("same") }'],
    ['if nil != nil { t.Fatal("nil") }'],
    ["if x := 3; 3 != 3 { t.Fatal(x) }"],
    ['got := 3; if got != got { t.Fatal("same value") }'],
    ['var p [2]int; if p[0] != p[0] { t.Fatal("same element") }'],
  ])("%s is GOTEST004", (body) => {
    expect(codesOf(withBody(body))).toEqual(["GOTEST004"]);
  });

  test("a condition that reads a value, calls something or compares two things is fine", () => {
    for (const body of [
      'if len("ab") != 2 { t.Fatal("length") }',
      'got, want := 3, 4; if got != want { t.Fatal("different") }',
      'if f() != f() { t.Fatal("f is not stable") }',
      "if got := f(); got != 3 { t.Fatal(got) }",
      'if a, b := 1, 2; a < b && b < 3 { t.Log("ok") }',
    ])
      expect(codesOf(`${withBody(body)}\nfunc f() int { return 3 }\n`), body).toEqual([]);
  });

  test("a constant condition in a helper that is not a test is left alone", () => {
    const text = goLines(
      "package model",
      "func helper() bool { if 1 == 1 { return true }; return false }",
    );
    expect(codesOf(text)).toEqual([]);
  });
});

describe("a Go test must not swallow a failure [REQ-023]", () => {
  test("an error check that only leaves is GOTEST005, one that fails or returns something is not", () => {
    for (const leave of ["return", "continue", "break"])
      expect(
        codesOf(withBody(`err := g(); for range 1 { if err != nil { ${leave} } }`)),
        leave,
      ).toEqual(["GOTEST005"]);
    expect(codesOf(withBody("err := g(); if err != nil { t.Fatal(err) }"))).toEqual([]);
    expect(codesOf(withBody('err := g(); if err != nil { t.Errorf("%v", err); return }'))).toEqual(
      [],
    );
    expect(
      codesOf(
        withBody(
          "run := func() error { err := g(); if err != nil { return err }; return nil }; _ = run",
        ),
      ),
    ).toEqual([]);
  });

  test("a helper may return on an error, a test function may not", () => {
    const helper = goLines(
      "package model",
      "func helper() { err := g(); if err != nil { return } }",
    );
    expect(codesOf(helper)).toEqual([]);
  });

  test("recover() needs a way to fail in the function that holds it", () => {
    expect(codesOf(withBody("defer func() { recover() }()", "boom()"))).toEqual(["GOTEST005"]);
    expect(codesOf(withBody("defer func() { _ = recover() }()", "boom()"))).toEqual(["GOTEST005"]);
    expect(
      codesOf(withBody("defer func() { if r := recover(); r != nil { _ = r } }()", "boom()")),
    ).toEqual(["GOTEST005"]);
    expect(
      codesOf(withBody('defer func() { if recover() == nil { t.Fatal("no panic") } }()', "boom()")),
    ).toEqual([]);
    expect(
      codesOf(withBody("defer func() { if r := recover(); r != nil { panic(r) } }()", "boom()")),
    ).toEqual([]);
  });
});

describe("a Go test must not hide the others [REQ-023]", () => {
  const main = (...body: string[]): string =>
    goLines(
      "package model",
      'import ("os"; "testing")',
      "func TestMain(m *testing.M) {",
      ...body.map((line) => `\t${line}`),
      "}",
    );

  test("a TestMain that never runs the tests is GOTEST006, one that does is fine", () => {
    expect(codesOf(main("os.Exit(0)"))).toEqual(["GOTEST006"]);
    expect(codesOf(main("os.Exit(m.Run())"))).toEqual([]);
    expect(codesOf(main("code := m.Run()", "os.Exit(code)"))).toEqual([]);
  });

  test("setting a test.* flag in a test file is GOTEST006", () => {
    const text = main('flag.Set("test.run", "TestOne")', "os.Exit(m.Run())");
    expect(codesOf(text)).toEqual(["GOTEST006"]);
    expect(codesOf(main('_ = "test.skip=TestTwo"', "os.Exit(m.Run())"))).toEqual(["GOTEST006"]);
  });
});

describe("the rules run with the Go test integrity check [REQ-023]", () => {
  test("a repository with a constant test fails test-integrity and a healthy one passes", () => {
    expect(testIntegrity(goRepo())).toEqual([]);
    const found = testIntegrity(goRepo({ [FILE]: withBody('if 1 == 2 { t.Fatal("never") }') }));
    expect(found.map((v) => v.code)).toEqual(["GOTEST004"]);
    expect(found[0]?.path).toBe(FILE);
  });
});
