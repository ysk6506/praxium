import { describe, expect, test } from "vitest";
import { declsOf, normalizedText } from "./go-decls";
import { codeOf, lex } from "./go-lex";

const read = (source: string) => {
  const toks = lex(source);
  return declsOf(toks, codeOf(toks));
};
const summary = (source: string) =>
  read(source).map((d) => `${d.kind}${d.recv ? `(${d.recv})` : ""}:${d.names.join(",")}`);
const text = (source: string) => read(source).map((d) => normalizedText(d.toks));

describe("Go declarations: what a file declares at its top level [REQ-023]", () => {
  test("functions, methods, types, variables and constants have names and receivers", () => {
    const source = [
      "package p",
      'import "testing"',
      "type Store struct{ n int }",
      "type ID = string",
      "var a, b = 1, 2",
      "var _ = register()",
      "const c = 3",
      "func helper() {}",
      "func (s *Store) Get() int { return s.n }",
      "func (Store) Name() string { return `x` }",
      "func Generic[K comparable](k K) K { return k }",
      "func TestX(t *testing.T) {}",
    ].join("\n");
    expect(summary(source)).toEqual([
      "type:Store",
      "type:ID",
      "var:a,b",
      "var:_",
      "const:c",
      "func:helper",
      "func(Store):Get",
      "func(Store):Name",
      "func:Generic",
      "func:TestX",
    ]);
  });

  test("a group of variables or types is split into its specs, a group of constants stays whole", () => {
    const source = [
      "package p",
      "var (",
      "\tone = 1",
      "\ttwo, three = 2,",
      "\t\t3",
      "\tcases = []struct{ n int }{{1}, {2}}",
      ")",
      "type (",
      "\tA int",
      "\tB struct {",
      "\t\tx int",
      "\t}",
      ")",
      "const (",
      "\tFirst = iota",
      "\tSecond",
      ")",
    ].join("\n");
    expect(summary(source)).toEqual([
      "var:one",
      "var:two,three",
      "var:cases",
      "type:A",
      "type:B",
      "const:First,Second",
    ]);
  });

  test("a declaration ends where the next one starts, and braces inside strings do not count", () => {
    const source = [
      "package p",
      'var open = "{ not a block"',
      "func f() {",
      '\tx := `}` + "}"',
      "\t_ = x",
      "}",
      "var after = 1",
    ].join("\n");
    expect(summary(source)).toEqual(["var:open", "func:f", "var:after"]);
    expect(read(source)[1]?.endLine).toBe(6);
  });

  test("the comment lines directly above a declaration are its doc, a blank line or code breaks the group", () => {
    const source = [
      "package p",
      "// stray",
      "",
      "// first line",
      "// second line",
      "func a() {}",
      "var x = 1 // trailing",
      "func b() {}",
      "/* block */",
      "func c() {}",
    ].join("\n");
    const docs = read(source).map((d) => d.doc.map((c) => c.text));
    expect(docs).toEqual([["// first line", "// second line"], [], [], ["/* block */"]]);
  });

  test("a spec of a group has the comments above it as its doc", () => {
    const source = ["package p", "var (", "\t//go:embed testdata/x.json", "\tx string", ")"].join(
      "\n",
    );
    expect(read(source)[0]?.doc.map((c) => c.text)).toEqual(["//go:embed testdata/x.json"]);
  });

  test("a function without a name is not a declaration", () => {
    expect(summary("package p\nfunc (")).toEqual([]);
  });
});

describe("Go declarations: normalised text ignores layout and comments [REQ-023]", () => {
  test("spacing, line breaks, comments and trailing commas do not change the text", () => {
    const a = "package p\nfunc f(a, b int) []int {\n\treturn []int{a, b}\n}\n";
    const b =
      "package p\n// doc\nfunc  f(\n\ta,\n\tb int,\n) []int { // note\n\treturn []int{\n\t\ta,\n\t\tb,\n\t}\n}\n";
    expect(text(a)).toEqual(text(b));
  });

  test("a changed token, string or number does change it", () => {
    const base = text('package p\nvar want = "a" + f(1)\n');
    expect(text('package p\nvar want = "b" + f(1)\n')).not.toEqual(base);
    expect(text('package p\nvar want = "a" + f(2)\n')).not.toEqual(base);
    expect(text('package p\nvar want = "a"+f(1)+g\n')).not.toEqual(base);
  });

  test("a raw string keeps its inner whitespace", () => {
    expect(text("package p\nvar s = `a  b`\n")).not.toEqual(text("package p\nvar s = `a b`\n"));
  });
});
