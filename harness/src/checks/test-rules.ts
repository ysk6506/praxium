/**
 * Static rules on test code (ADR-0004). These catch tests that cannot fail or that hide others.
 *
 * TEST001 test without assertions        TEST002 skip/todo/fails without a TD-NNNN reason
 * TEST003 assertion decided by constants TEST004 broad catch swallows failures
 * TEST005 `.only` hides every other test
 */
import { readFileSync } from "node:fs";
import path from "node:path";
import { type Violation, violation } from "../common";
import { dotted, lineOf, parse, structureOf, ts, walk } from "../ts-ast";
import { assertionHelpers, countAssertions, testApi } from "./test-units";

const TD_RE = /TD-\d{4}/;
const SKIP_MODIFIERS = new Set(["skip", "todo", "skipIf", "runIf", "fails"]);
const CONSTANT_KINDS = new Set([
  ts.SyntaxKind.TrueKeyword,
  ts.SyntaxKind.FalseKeyword,
  ts.SyntaxKind.NullKeyword,
  ts.SyntaxKind.NumericLiteral,
  ts.SyntaxKind.StringLiteral,
  ts.SyntaxKind.NoSubstitutionTemplateLiteral,
]);

function hasTdReason(call: ts.CallExpression): boolean {
  let found = false;
  walk(call, (node) => {
    if (ts.isStringLiteralLike(node) && TD_RE.test(node.text)) found = true;
  });
  return found;
}

function isConstant(node: ts.Node): boolean {
  if (CONSTANT_KINDS.has(node.kind)) return true;
  return ts.isIdentifier(node) && node.text === "undefined";
}

/** `expect(<subject>)...matcher(<arg>)`: the subject and the first matcher argument. */
function expectParts(
  call: ts.CallExpression,
): { subject: ts.Node; arg: ts.Node | undefined } | undefined {
  if (!ts.isPropertyAccessExpression(call.expression)) return undefined;
  let target: ts.Expression = call.expression.expression;
  while (ts.isPropertyAccessExpression(target)) target = target.expression; // .not, .resolves
  if (
    !ts.isCallExpression(target) ||
    !["expect", "expect.soft"].includes(dotted(target.expression))
  )
    return undefined;
  const subject = target.arguments[0];
  return subject ? { subject, arg: call.arguments[0] } : undefined;
}

function containsCall(node: ts.Node): boolean {
  let found = false;
  walk(node, (child) => {
    if (ts.isCallExpression(child) || ts.isNewExpression(child)) found = true;
  });
  return found;
}

function trivialAssertions(file: string, sf: ts.SourceFile, test: ts.Node): Violation[] {
  const out: Violation[] = [];
  walk(test, (node) => {
    if (!ts.isCallExpression(node)) return;
    const parts = expectParts(node);
    if (!parts) return;
    // `expect(x).toBe(x)` cannot fail, but `expect(run()).toEqual(run())` compares two evaluations.
    const sameAsArg =
      parts.arg !== undefined &&
      !containsCall(parts.subject) &&
      structureOf(parts.subject) === structureOf(parts.arg);
    if (isConstant(parts.subject) || sameAsArg) {
      out.push(
        violation(
          "TEST003",
          file,
          lineOf(sf, node.getStart(sf)),
          "assertion is decided by constants, it cannot fail",
          "Assert on the value produced by the code under test.",
        ),
      );
    }
  });
  return out;
}

function swallowedFailures(file: string, sf: ts.SourceFile, test: ts.Node): Violation[] {
  const out: Violation[] = [];
  walk(test, (node) => {
    if (!ts.isTryStatement(node) || !node.catchClause) return;
    let handles = false;
    walk(node.catchClause.block, (child) => {
      if (ts.isThrowStatement(child)) handles = true;
      if (ts.isCallExpression(child) && countAssertions(child, new Set()) > 0) handles = true;
    });
    if (!handles) {
      out.push(
        violation(
          "TEST004",
          file,
          lineOf(sf, node.getStart(sf)),
          "catch block swallows failures inside a test",
          "Use expect(...).toThrow() / rejects, or rethrow.",
        ),
      );
    }
  });
  return out;
}

function apiCallViolations(
  file: string,
  sf: ts.SourceFile,
  call: ts.CallExpression,
  helpers: ReadonlySet<string>,
): Violation[] {
  const api = testApi(call);
  // In `test.each(table)(title, fn)` only the outer call is the test; skip the inner one.
  if (!api || (ts.isCallExpression(call.parent) && call.parent.expression === call)) return [];
  const line = lineOf(sf, call.getStart(sf));
  const out: Violation[] = [];
  if (api.modifiers.includes("only")) {
    out.push(
      violation(
        "TEST005",
        file,
        line,
        "`.only` makes every other test silently not run",
        "Remove .only; select tests on the command line while working.",
      ),
    );
  }
  if (api.modifiers.some((m) => SKIP_MODIFIERS.has(m)) && !hasTdReason(call)) {
    out.push(
      violation(
        "TEST002",
        file,
        line,
        "skipped/todo/expected-failure test without a debt reference",
        "Register debt in docs/exec-plans/tech-debt.yaml and put 'TD-NNNN: why' in the title, or fix the cause.",
      ),
    );
  }
  const isTest = api.base === "test" || api.base === "it";
  if (isTest && !api.modifiers.includes("todo")) {
    if (countAssertions(call, helpers) === 0) {
      out.push(
        violation(
          "TEST001",
          file,
          line,
          "test asserts nothing",
          "Assert the behaviour under test; a test that cannot fail proves nothing.",
        ),
      );
    }
    out.push(...trivialAssertions(file, sf, call), ...swallowedFailures(file, sf, call));
  }
  return out;
}

export function staticViolations(root: string, file: string): Violation[] {
  const sf = parse(file, readFileSync(path.join(root, file), "utf8"));
  const helpers = assertionHelpers(sf);
  const out: Violation[] = [];
  walk(sf, (node) => {
    if (ts.isCallExpression(node)) out.push(...apiCallViolations(file, sf, node, helpers));
    if (
      ts.isCallExpression(node) &&
      /\.skip$/.test(dotted(node.expression)) &&
      !testApi(node) &&
      !hasTdReason(node)
    ) {
      out.push(
        violation(
          "TEST002",
          file,
          lineOf(sf, node.getStart(sf)),
          "test skips itself at run time without a debt reference",
          "Pass 'TD-NNNN: why' to skip(), or fix the cause.",
        ),
      );
    }
  });
  return out;
}
