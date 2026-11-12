/**
 * Test units: what the test guard fingerprints and locks (ADR-0004).
 *
 * A unit is one test (`test`/`it`, keyed by its describe path and title), one hook, one helper,
 * import or other statement in a test file, or one expected-data file. For tests we also measure
 * what a weakening would change: assertion count, expected literals, toBeCloseTo precision, skips.
 */
import { readFileSync } from "node:fs";
import path from "node:path";
import { expandGlobs } from "../files";
import { dotted, isFunctionLike, lineOf, parse, sha16, structureOf, ts, walk } from "../ts-ast";

export interface Unit {
  readonly key: string;
  readonly kind: "test" | "support" | "data";
  readonly digest: string;
  readonly path: string;
  readonly line: number;
  readonly asserts: number;
  readonly literals: readonly string[];
  readonly precision: readonly number[];
  readonly skips: number;
}

const TEST_NAMES = new Set(["test", "it"]);
const SUITE_NAMES = new Set(["describe", "suite"]);
const HOOKS = new Set(["beforeEach", "afterEach", "beforeAll", "afterAll"]);
const SKIP_MODIFIERS = new Set(["skip", "todo", "skipIf", "runIf", "fails"]);
const ASSERTIONS = new Set([
  "expect",
  "expect.soft",
  "expect.assertions",
  "expect.hasAssertions",
  "expectTypeOf",
  "assert",
]);

/** The base name (`test`, `describe`...) and modifiers (`skip`, `only`, `each`...) of a test API call. */
export function testApi(
  call: ts.CallExpression,
): { base: string; modifiers: string[] } | undefined {
  let callee: ts.Expression = call.expression;
  const modifiers: string[] = [];
  // test.each(table)(name, fn): unwrap the inner call
  if (ts.isCallExpression(callee)) {
    const inner = testApi(callee);
    return inner ? { base: inner.base, modifiers: [...inner.modifiers] } : undefined;
  }
  while (ts.isPropertyAccessExpression(callee)) {
    modifiers.unshift(callee.name.text);
    callee = callee.expression;
  }
  if (!ts.isIdentifier(callee)) return undefined;
  const base = callee.text;
  if (!TEST_NAMES.has(base) && !SUITE_NAMES.has(base) && !HOOKS.has(base)) return undefined;
  return { base, modifiers };
}

function titleOf(call: ts.CallExpression): string {
  const first = call.arguments[0];
  if (first && ts.isStringLiteralLike(first)) return first.text;
  if (first && ts.isTemplateExpression(first)) return first.getText();
  return "<untitled>";
}

function callbackOf(call: ts.CallExpression): ts.Node | undefined {
  return [...call.arguments].reverse().find((arg) => isFunctionLike(arg));
}

/** Names of functions in the file that themselves assert (helpers like `expectOk()`). */
export function assertionHelpers(sf: ts.SourceFile): Set<string> {
  const helpers = new Set<string>();
  for (const statement of sf.statements) {
    const declared =
      ts.isFunctionDeclaration(statement) && statement.name
        ? [[statement.name.text, statement] as const]
        : [];
    const consts = ts.isVariableStatement(statement)
      ? statement.declarationList.declarations
          .filter((d) => ts.isIdentifier(d.name) && d.initializer && isFunctionLike(d.initializer))
          .map((d) => [(d.name as ts.Identifier).text, d.initializer as ts.Node] as const)
      : [];
    for (const [name, node] of [...declared, ...consts]) {
      if (countAssertions(node, new Set()) > 0) helpers.add(name);
    }
  }
  return helpers;
}

export function countAssertions(node: ts.Node, helpers: ReadonlySet<string>): number {
  let count = 0;
  walk(node, (child) => {
    if (!ts.isCallExpression(child)) return;
    const name = dotted(child.expression);
    if (ASSERTIONS.has(name) || name.startsWith("assert.") || helpers.has(name)) count++;
  });
  return count;
}

function expectChains(node: ts.Node): ts.Node[] {
  const out: ts.Node[] = [];
  walk(node, (child) => {
    if (ts.isExpressionStatement(child) && /\bexpect\s*(\.soft)?\s*\(/.test(child.getText()))
      out.push(child);
  });
  return out;
}

function literalsIn(node: ts.Node): string[] {
  const values: string[] = [];
  for (const chain of expectChains(node)) {
    walk(chain, (child) => {
      if (ts.isStringLiteralLike(child) || ts.isNumericLiteral(child))
        values.push(JSON.stringify(child.text));
      else if (
        child.kind === ts.SyntaxKind.TrueKeyword ||
        child.kind === ts.SyntaxKind.FalseKeyword
      )
        values.push(child.getText());
    });
  }
  return values.sort();
}

/** Digits argument of every toBeCloseTo (fewer digits = looser). */
function precisionIn(node: ts.Node): number[] {
  const found: number[] = [];
  walk(node, (child) => {
    if (
      ts.isCallExpression(child) &&
      ts.isPropertyAccessExpression(child.expression) &&
      child.expression.name.text === "toBeCloseTo"
    ) {
      const digits = child.arguments[1];
      found.push(digits && ts.isNumericLiteral(digits) ? Number(digits.text) : 2);
    }
  });
  return found;
}

export function skipCount(node: ts.Node): number {
  let count = 0;
  walk(node, (child) => {
    if (ts.isCallExpression(child)) {
      const api = testApi(child);
      if (api?.modifiers.some((m) => SKIP_MODIFIERS.has(m))) count++;
      const name = dotted(child.expression);
      if (/\.skip$/.test(name) && !api) count++; // context.skip()
    }
  });
  return count;
}

class Collector {
  readonly units: Unit[] = [];
  private readonly seen = new Map<string, number>();

  constructor(
    private readonly file: string,
    private readonly sf: ts.SourceFile,
    private readonly helpers: ReadonlySet<string>,
  ) {}

  add(key: string, kind: "test" | "support", node: ts.Node): void {
    const count = this.seen.get(key) ?? 0;
    this.seen.set(key, count + 1);
    const unique = count === 0 ? key : `${key}#${count + 1}`;
    const isTest = kind === "test";
    this.units.push({
      key: `${this.file}::${unique}`,
      kind,
      digest: sha16(structureOf(node)),
      path: this.file,
      line: lineOf(this.sf, node.getStart(this.sf)),
      asserts: isTest ? countAssertions(node, this.helpers) : 0,
      literals: isTest ? literalsIn(node) : [],
      precision: isTest ? precisionIn(node) : [],
      skips: isTest ? skipCount(node) : 0,
    });
  }

  statements(statements: readonly ts.Statement[], prefix: string): void {
    for (const statement of statements) this.statement(statement, prefix);
  }

  private statement(statement: ts.Statement, prefix: string): void {
    const call =
      ts.isExpressionStatement(statement) && ts.isCallExpression(statement.expression)
        ? statement.expression
        : undefined;
    const api = call ? testApi(call) : undefined;
    if (call && api) {
      this.testApiCall(statement, call, api.base, prefix);
      return;
    }
    const names = declaredNames(statement);
    if (names.length > 0) {
      for (const name of names) this.add(`${prefix}=${name}`, "support", statement);
      return;
    }
    const label = ts.isImportDeclaration(statement) ? "import" : "stmt";
    this.add(
      `${prefix}<${label}:${sha16(structureOf(statement)).slice(0, 8)}>`,
      "support",
      statement,
    );
  }

  private testApiCall(
    statement: ts.Statement,
    call: ts.CallExpression,
    base: string,
    prefix: string,
  ): void {
    if (HOOKS.has(base)) {
      this.add(`${prefix}<hook:${base}>`, "support", statement);
      return;
    }
    const title = titleOf(call);
    if (TEST_NAMES.has(base)) {
      this.add(`${prefix}${title}`, "test", statement);
      return;
    }
    const body = callbackOf(call);
    const block =
      body && (ts.isArrowFunction(body) || ts.isFunctionExpression(body)) ? body.body : undefined;
    if (block && ts.isBlock(block)) {
      this.add(`${prefix}${title} > <suite>`, "support", suiteHeader(call));
      this.statements(block.statements, `${prefix}${title} > `);
    } else {
      this.add(`${prefix}${title}`, "support", statement);
    }
  }
}

/** The describe call without its body, so the suite's own modifiers and title are locked. */
function suiteHeader(call: ts.CallExpression): ts.Node {
  return call.expression;
}

function declaredNames(statement: ts.Statement): string[] {
  if ((ts.isFunctionDeclaration(statement) || ts.isClassDeclaration(statement)) && statement.name)
    return [statement.name.text];
  if (ts.isVariableStatement(statement)) {
    return statement.declarationList.declarations.flatMap((d) =>
      ts.isIdentifier(d.name) ? [d.name.text] : [d.name.getText()],
    );
  }
  if (ts.isTypeAliasDeclaration(statement) || ts.isInterfaceDeclaration(statement))
    return [statement.name.text];
  return [];
}

export function collectUnits(root: string, file: string): Unit[] {
  const sf = parse(file, readFileSync(path.join(root, file), "utf8"));
  const collector = new Collector(file, sf, assertionHelpers(sf));
  collector.statements(sf.statements, "");
  return collector.units;
}

export interface UnitGlobs {
  readonly test_globs: readonly string[];
  readonly support_globs: readonly string[];
  readonly data_globs: readonly string[];
}

export function testFiles(root: string, globs: UnitGlobs): string[] {
  return expandGlobs(root, [...globs.test_globs, ...globs.support_globs]).filter((f) =>
    /\.tsx?$/.test(f),
  );
}

export function currentUnits(root: string, globs: UnitGlobs): Map<string, Unit> {
  const units = new Map<string, Unit>();
  for (const file of testFiles(root, globs)) {
    for (const unit of collectUnits(root, file)) units.set(unit.key, unit);
  }
  for (const file of expandGlobs(root, globs.data_globs)) {
    const key = `${file}::<data>`;
    units.set(key, {
      key,
      kind: "data",
      digest: sha16(readFileSync(path.join(root, file), "utf8")),
      path: file,
      line: 1,
      asserts: 0,
      literals: [],
      precision: [],
      skips: 0,
    });
  }
  return units;
}
