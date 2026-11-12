import { describe, expect, test } from "vitest";
import {
  comments,
  functions,
  isFunctionLike,
  moduleRefs,
  normalized,
  parse,
  structureOf,
  ts,
  walk,
} from "./ts-ast";

const SOURCE = [
  'import { a } from "./a"; // first',
  "export class C { m(x: number, y: number) { return x && y ? 1 : 2; } get g() { return `t${1}`; } }",
  "const f = (n: number) => { /* inside */ return n; };",
  "function outer() { function inner() {} return inner; }",
  "",
].join("\n");

describe("ts-ast answers are computed once per parsed tree [REQ-024]", () => {
  test("the same text gives the same tree and equal answers; arrays are copies", () => {
    const sf = parse("x.ts", SOURCE);
    expect(parse("x.ts", SOURCE)).toBe(sf);
    const first = comments("x.ts", SOURCE);
    first.pop();
    expect(comments("x.ts", SOURCE).map((c) => c.text)).toEqual(["// first", "/* inside */"]);
    const refs = moduleRefs(sf);
    refs.length = 0;
    expect(moduleRefs(sf).map((r) => r.specifier)).toEqual(["./a"]);
    const fns = functions(sf);
    fns.length = 0;
    expect(functions(sf).map((f) => f.qualname)).toEqual([
      "C.m",
      "C.g",
      "f",
      "outer",
      "outer.inner",
    ]);
  });

  test("changed text is parsed again and never answered from the old tree", () => {
    const changed = SOURCE.replace("// first", "// second");
    expect(comments("x.ts", changed)[0]?.text).toBe("// second");
    expect(comments("x.ts", SOURCE)[0]?.text).toBe("// first");
    expect(normalized("x.ts", changed)).toBe(structureOf(parse("x.ts", changed)));
    expect(normalized("x.ts", changed)).not.toBe(normalized("x.ts", `${changed}const z = 1;`));
  });

  test("isFunctionLike is true for exactly the seven function kinds", () => {
    const kinds: string[] = [];
    walk(parse("x.ts", SOURCE), (n) => {
      if (isFunctionLike(n)) kinds.push(ts.SyntaxKind[n.kind] ?? "");
    });
    expect(kinds).toEqual([
      "MethodDeclaration",
      "GetAccessor",
      "ArrowFunction",
      "FunctionDeclaration",
      "FunctionDeclaration",
    ]);
  });

  test("literal text is part of the structure, comments and spacing are not", () => {
    const a = structureOf(parse("a.ts", 'const s = "x"; const n = 1n; const t = `a${s}b`;'));
    const b = structureOf(
      parse("b.ts", 'const   s = "x";\n/* c */ const n = 1n; const t = `a${s}b`;'),
    );
    expect(b).toBe(a);
    expect(structureOf(parse("c.ts", 'const s = "y"; const n = 1n; const t = `a${s}b`;'))).not.toBe(
      a,
    );
    expect(a).toContain('"a"');
    expect(a).toContain('"b"');
  });
});
