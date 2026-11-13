import { describe, expect, test } from "vitest";
import type { Violation } from "../common";
import { goLines, goRepo } from "../test-support/go-repo";
import { ignoresOf } from "./go";

const MODEL = "backend/internal/model/";
const run = (files: Record<string, string>): Violation[] => ignoresOf(goRepo(files));
const codes = (files: Record<string, string>): string[] =>
  run(files)
    .map((v) => v.code)
    .sort();
const src = (...rows: string[]) => goLines("package model", ...rows);
const test_ = (...rows: string[]) =>
  goLines(
    "package model",
    'import "testing"',
    "func TestX(t *testing.T) {",
    ...rows.map((r) => `\t${r}`),
    "}",
  );

describe("Go ignores: directives [REQ-022]", () => {
  test("the healthy module has none, and a repository without Go is not read", () => {
    expect(run({})).toEqual([]);
    expect(ignoresOf(goRepo({ "harness/config/architecture.yaml": "units: {}\n" }))).toEqual([]);
  });

  test("a directive needs a reason and an open item or a sealed ADR", () => {
    const ok = [
      "var a = 1 //nolint:unused // kept for the migration, TD-0001",
      "// Waits for the parser rewrite (TD-0001).\n//nolint",
      "//lint:ignore SA1019 the old API stays until ADR-0001 says otherwise\nvar b = 1",
      "// nosec G104 reason given in ADR-0001: output is a fixed string\nvar c = 1",
      "//go:build ignore // generator, see ADR-0001",
    ];
    for (const text of ok) expect(codes({ [`${MODEL}a.go`]: src(text) }), text).toEqual([]);
  });

  test("no reason, no basis, a closed item and an unsealed ADR are each reported", () => {
    expect(codes({ [`${MODEL}a.go`]: src("var a = 1 //nolint") })).toEqual([
      "GOIGN001",
      "GOIGN002",
    ]);
    expect(
      codes({ [`${MODEL}a.go`]: src("var a = 1 //nolint:errcheck // it is fine really") }),
    ).toEqual(["GOIGN002"]);
    expect(codes({ [`${MODEL}a.go`]: src("var a = 1 //nolint // TD-0001") })).toEqual(["GOIGN001"]);
    expect(
      codes({ [`${MODEL}a.go`]: src("var a = 1 //nolint // paid off long ago TD-0002") }),
    ).toEqual(["GOIGN003"]);
    expect(
      codes({ [`${MODEL}a.go`]: src("var a = 1 //nolint // never registered TD-0099") }),
    ).toEqual(["GOIGN003"]);
    expect(
      codes({ [`${MODEL}a.go`]: src("var a = 1 //nolint // no such decision ADR-0042") }),
    ).toEqual(["GOIGN004"]);
  });

  test("an ordinary comment that mentions the word is not a directive", () => {
    expect(
      codes({
        [`${MODEL}a.go`]: src("// the nolint tags are banned; nosec too", 'var a = "//nolint"'),
      }),
    ).toEqual([]);
  });
});

describe("Go ignores: skipped tests [REQ-023]", () => {
  test("a skip needs a reason and a basis, from the call or a comment", () => {
    expect(
      codes({ [`${MODEL}a_test.go`]: test_('t.Skip("flaky on slow disks, TD-0001")') }),
    ).toEqual([]);
    expect(
      codes({ [`${MODEL}a_test.go`]: test_("// Needs the real clock, TD-0001", "t.SkipNow()") }),
    ).toEqual([]);
    expect(
      codes({
        [`${MODEL}a_test.go`]: test_(
          't.Skipf("no %s", "x") // waits for the fixture work, ADR-0001',
        ),
      }),
    ).toEqual([]);
  });

  test("a bare skip and a testing.Short gate are reported", () => {
    expect(codes({ [`${MODEL}a_test.go`]: test_("t.Skip()") })).toEqual(["GOIGN001", "GOIGN002"]);
    expect(codes({ [`${MODEL}a_test.go`]: test_("if testing.Short() {", "return", "}") })).toEqual([
      "GOIGN001",
      "GOIGN002",
    ]);
    expect(codes({ [`${MODEL}a_test.go`]: test_('t.Skip("later, TD-0002 closed it")') })).toEqual([
      "GOIGN003",
    ]);
  });

  test("Skip in production code is something else", () => {
    expect(codes({ [`${MODEL}a.go`]: src("func f(l list) { l.Skip(1) }") })).toEqual([]);
  });
});
