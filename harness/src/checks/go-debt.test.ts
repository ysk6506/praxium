import { describe, expect, test } from "vitest";
import type { Violation } from "../common";
import { goLines, goRepo } from "../test-support/go-repo";
import { debtOf } from "./go";

const MODEL = "backend/internal/model/";
const run = (files: Record<string, string>): Violation[] => debtOf(goRepo(files));
const codes = (files: Record<string, string>): string[] =>
  run(files)
    .map((v) => v.code)
    .sort();

// The fixture limits are small on purpose: 60 lines, 12 lines a function, complexity 4, 3 parameters.
const body = (n: number): string => Array.from({ length: n }, () => "\t_ = 1").join("\n");
const fn = (name: string, header: string, lines: number): string =>
  `${header.replace("NAME", name)} {\n${body(lines)}\n}`;

describe("Go debt: size and complexity [REQ-022]", () => {
  test("code within the limits passes, and a repository without Go is not read", () => {
    expect(run({})).toEqual([]);
    expect(debtOf(goRepo({ "harness/config/architecture.yaml": "units: {}\n" }))).toEqual([]);
  });

  test("a long function, a complex one and one with many parameters are reported", () => {
    const text = goLines(
      "package model",
      fn("Long", "func NAME()", 12),
      "func Branchy(a, b bool) int {",
      "\tif a && b || a {",
      "\t\tfor i := 0; i < 2; i++ {",
      "\t\t}",
      "\t}",
      "\treturn 0",
      "}",
      "func Wide(a, b, c, d int) {}",
    );
    const found = run({ [`${MODEL}a.go`]: text });
    expect(found.map((v) => [v.code, v.line])).toEqual(
      [
        ["GODEBT022", 16],
        ["GODEBT021", 2],
        ["GODEBT023", 23],
      ].sort((x, y) => String(x[0]).localeCompare(String(y[0])) || Number(x[1]) - Number(y[1])),
    );
  });

  test("a switch counts every case, and function literals count in the function that holds them", () => {
    const sw = goLines(
      "package model",
      "func S(x int) {",
      "\tswitch x {",
      "\tcase 1:",
      "\tcase 2:",
      "\tcase 3:",
      "\tcase 4:",
      "\t}",
      "}",
    );
    expect(codes({ [`${MODEL}a.go`]: sw })).toEqual(["GODEBT022"]);
    const ifs = Array.from({ length: 4 }, () => "\t\tif x {}");
    const lit = goLines(
      "package model",
      "func L(x bool) {",
      "\tf := func() {",
      ...ifs,
      "\t}",
      "\tf()",
      "}",
    );
    expect(codes({ [`${MODEL}a.go`]: lit })).toEqual(["GODEBT022"]);
  });

  test("a long file is reported, test files are not measured", () => {
    const long = goLines("package model", ...Array.from({ length: 60 }, () => "var _ = 1"));
    expect(codes({ [`${MODEL}a.go`]: long })).toEqual(["GODEBT020"]);
    expect(codes({ [`${MODEL}a_test.go`]: long })).toEqual([]);
  });

  test("a waiver naming an open item allows it, on the line or on line 1; unused or closed ones fail", () => {
    const wide = (waiver: string, first = "") =>
      goLines(`package model${first}`, `func Wide(a, b, c, d int) {} ${waiver}`);
    expect(codes({ [`${MODEL}a.go`]: wide("// debt-ok: TD-0001") })).toEqual([]);
    expect(
      codes({
        [`${MODEL}a.go`]: goLines(
          "// debt-ok: TD-0001",
          "package model",
          "func Wide(a, b, c, d int) {}",
        ),
      }),
    ).toEqual([]);
    expect(codes({ [`${MODEL}a.go`]: wide("// debt-ok: TD-0002") })).toEqual([
      "GODEBT013",
      "GODEBT023",
    ]);
    expect(codes({ [`${MODEL}a.go`]: wide("// debt-ok: TD-0099") })).toEqual([
      "GODEBT002",
      "GODEBT023",
    ]);
    expect(
      codes({ [`${MODEL}a.go`]: goLines("package model", "func Fine() {} // debt-ok: TD-0001") }),
    ).toEqual(["GODEBT032"]);
  });
});

describe("Go debt: markers [REQ-022]", () => {
  test("a marker needs an open debt item", () => {
    const comment = (text: string) => goLines("package model", `// ${text}`);
    expect(codes({ [`${MODEL}a.go`]: comment("TODO(TD-0001): later") })).toEqual([]);
    expect(codes({ [`${MODEL}a.go`]: comment("TODO: later") })).toEqual(["GODEBT001"]);
    expect(codes({ [`${MODEL}a.go`]: comment("FIXME(TD-0002) done already") })).toEqual([
      "GODEBT013",
    ]);
    expect(codes({ [`${MODEL}a.go`]: comment("HACK(TD-0077) unknown") })).toEqual(["GODEBT002"]);
    expect(codes({ [`${MODEL}a_test.go`]: comment("XXX in a test") })).toEqual(["GODEBT001"]);
  });

  test("the word inside a string is not a marker", () => {
    expect(codes({ [`${MODEL}a.go`]: goLines("package model", 'var _ = "TODO later"') })).toEqual(
      [],
    );
  });
});
