import { describe, expect, test } from "vitest";
import {
  codeOf,
  funcsOf,
  importsOf,
  lex,
  lineCount,
  localName,
  packageOf,
  selectorsOf,
  stringValue,
} from "./go-lex";

const code = (text: string) => codeOf(lex(text));

describe("Go reader: tokens [REQ-021]", () => {
  test("comments, strings, raw strings and runes are tokens of their own", () => {
    const toks = lex("x := \"a // b\" + `c\n/* d */` + '\\'' // tail\n/* e\nf */ y");
    expect(toks.map((t) => [t.kind, t.line, t.endLine])).toEqual([
      ["ident", 1, 1],
      ["punct", 1, 1],
      ["string", 1, 1],
      ["punct", 1, 1],
      ["raw", 1, 2],
      ["punct", 2, 2],
      ["rune", 2, 2],
      ["comment", 2, 2],
      ["comment", 3, 4],
      ["ident", 4, 4],
    ]);
  });

  test("operators that matter stay whole and numbers do not swallow selectors", () => {
    const texts = code("a && b || c := d...; 0x1F + 1_000 + 3.5e2 + .5").map((t) => t.text);
    expect(texts).toEqual([
      "a",
      "&&",
      "b",
      "||",
      "c",
      ":=",
      "d",
      "...",
      ";",
      "0x1F",
      "+",
      "1_000",
      "+",
      "3.5e2",
      "+",
      ".5",
    ]);
  });

  test("an unterminated string or comment ends at the line or the file", () => {
    expect(lex('"open\nnext').map((t) => t.text)).toEqual(['"open', "next"]);
    expect(lex("/* open").map((t) => t.kind)).toEqual(["comment"]);
    expect(lex("`open").map((t) => t.kind)).toEqual(["raw"]);
  });

  test("string values lose their quotes", () => {
    const [a, b, c] = code('"x/y" `z` "open');
    expect([a, b, c].map((t) => (t ? stringValue(t) : ""))).toEqual(["x/y", "z", "open"]);
  });

  test("lines are counted the way the limits count them", () => {
    expect([lineCount(""), lineCount("a"), lineCount("a\n"), lineCount("a\nb")]).toEqual([
      1, 1, 1, 2,
    ]);
  });
});

describe("Go reader: package and imports [REQ-021]", () => {
  test("the package clause keeps the comment group right above it", () => {
    const toks = lex("// detached\n\n// Package p does a thing.\n// More.\npackage p\n");
    expect(packageOf(toks)).toEqual({ name: "p", line: 5, doc: "Package p does a thing.\nMore." });
    expect(packageOf(lex("package q")))?.toEqual({ name: "q", line: 1, doc: "" });
    expect(packageOf(lex("/* Package b is\n   blocky. */\npackage b"))?.doc).toBe(
      "Package b is\n   blocky.",
    );
    expect(packageOf(lex("var x = 1"))).toBeUndefined();
  });

  test("single, grouped, aliased, dot and blank imports are all read", () => {
    const text = [
      "package p",
      'import "a"',
      'import b "b/c"',
      "import (",
      '\t"d"',
      '\t_ "e"',
      '\t. "f"',
      '\tg "h/v2"',
      "\t`raw/i`",
      ")",
      "import ()",
      "var x = 1",
      'import "late"',
    ].join("\n");
    expect(importsOf(code(text)).map((i) => [i.alias, i.path, i.line])).toEqual([
      [undefined, "a", 2],
      ["b", "b/c", 3],
      [undefined, "d", 5],
      ["_", "e", 6],
      [".", "f", 7],
      ["g", "h/v2", 8],
      [undefined, "raw/i", 9],
    ]);
    expect(importsOf(code("var x = 1"))).toEqual([]);
  });

  test("an import is known by its alias or the last path element, skipping a version suffix", () => {
    const names = importsOf(
      code('package p\nimport (\n"a/b"\nx "c"\n"d/e/v2"\n_ "f"\n. "g"\n"v2"\n)'),
    ).map(localName);
    expect(names).toEqual(["b", "x", "e", undefined, undefined, "v2"]);
  });

  test("selectors are found for imported names only, and not after another dot", () => {
    const text = [
      "package p",
      'import (\n"time"\nr "math/rand"\n"os"\n)',
      "func f() {",
      "\t_ = time.Now()",
      "\t_ = r.Int()",
      "\t_ = obj.time.Now()",
      "\t_ = other.Now()",
      "\tos.Exit(1)",
      "\ttime.Now",
      "}",
    ].join("\n");
    const toks = code(text);
    expect(selectorsOf(toks, importsOf(toks)).map((s) => [s.qualified, s.line])).toEqual([
      ["time.Now", 8],
      ["math/rand.Int", 9],
      ["os.Exit", 12],
      ["time.Now", 13],
    ]);
  });
});

describe("Go reader: functions [REQ-022]", () => {
  test("functions and methods get their size, parameters and complexity", () => {
    const text = [
      "package p",
      "",
      "func Add(a, b int) int { return a + b }",
      "",
      "func (s *Store[K]) Put(key K, fn func(a, b int) int, rest ...string) (n int, err error) {",
      "\tfor i := 0; i < 3 && s != nil; i++ {",
      "\t\tswitch key {",
      "\t\tcase 1, 2:",
      "\t\tdefault:",
      "\t\t}",
      "\t}",
      "\tif a || b {",
      "\t}",
      "\treturn",
      "}",
      "",
      "func (Clock) Now() int { return 0 }",
      'func (id ID[K]) String() string { return "" }',
      "func Gen[T any, U comparable](x T) {}",
    ].join("\n");
    expect(
      funcsOf(code(text)).map((f) => [f.qualname, f.line, f.lines, f.params, f.complexity]),
    ).toEqual([
      ["Add", 3, 1, 2, 1],
      ["Store.Put", 5, 11, 3, 6],
      ["Clock.Now", 17, 1, 0, 1],
      ["ID.String", 18, 1, 0, 1],
      ["Gen", 19, 1, 1, 1],
    ]);
  });

  test("an inline type in the result is not the body, and a literal inside a body is not a declaration", () => {
    const text = [
      "package p",
      "var handler = func() { if x {} }",
      "func A() interface{ M() } {",
      "\tf := func() {",
      "\t\tif y {",
      "\t\t}",
      "\t}",
      "\tf()",
      "\treturn nil",
      "}",
      "func B() struct{ N int } { return struct{ N int }{} }",
      "func C(a int,",
      "\tb int,",
      ") {",
      "}",
    ].join("\n");
    expect(funcsOf(code(text)).map((f) => [f.qualname, f.lines, f.params, f.complexity])).toEqual([
      ["A", 8, 0, 2],
      ["B", 1, 0, 1],
      ["C", 4, 2, 1],
    ]);
  });

  test("a declaration without a body, and broken code, are skipped without failing", () => {
    const text = [
      "package p",
      "func External(x int) int",
      "func Real() {}",
      "func (",
      "func Broken(x",
    ].join("\n");
    expect(funcsOf(code(text)).map((f) => f.qualname)).toEqual(["Real"]);
    expect(funcsOf(code("package p\nfunc F() {"))).toEqual([]);
    expect(funcsOf(code("package p\nfunc 5() {}\nfunc G {}"))).toEqual([]);
  });
});
