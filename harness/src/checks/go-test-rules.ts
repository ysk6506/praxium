/**
 * Static rules on Go test code, the Go side of TEST003 to TEST005 (ADR-0028). They read the test functions of every
 * `_test.go` file the way go-test-units.ts does, and catch tests that cannot fail or that hide others.
 *
 * GOTEST004 an assertion decided by constants: `if 1+1 != 2 { t.Fatal() }`, `if x != x { ... }`   (TEST003)
 * GOTEST005 a failure swallowed: an error check that only returns, a `recover()` that cannot fail   (TEST004)
 * GOTEST006 tests hidden: a `TestMain` that never runs them, a program that sets `test.run`        (TEST005)
 *
 * Applied to what a test function holds, closures included. Helpers outside test functions are not judged: a helper
 * may return on an error, a test may not. A condition built from names (constants of the package) is not resolved.
 */
import { type Violation, violation } from "../common";
import { type GoConfig, type GoFile, goFiles } from "./go-config";
import { declsOf, type GoDecl } from "./go-decls";
import { closeOf, type GoTok } from "./go-lex";

const TEST_NAME = /^(Test|Fuzz|Example)($|[^a-z])/;
const FAILING = new Set(["Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow"]);
const CONSTANT_WORDS = new Set(["true", "false", "nil"]);
const CONSTANT_PUNCT = new Set("( ) + - * / % ! == != < > <= >= && || & | ^ << >>".split(" "));
const COMPARES = new Set(["==", "!=", "<", ">", "<=", ">="]);
const LEAVES = new Set(["return", "continue", "break"]);
const FLAGS = /test\.(run|skip|list|count|short)\b/;

const isPunct = (tok: GoTok | undefined, text: string): boolean =>
  tok?.kind === "punct" && tok.text === text;

const isTestFunc = (decl: GoDecl): boolean =>
  decl.kind === "func" &&
  !decl.recv &&
  TEST_NAME.test(decl.names[0] ?? "") &&
  decl.names[0] !== "TestMain";

/** The condition of the `if` at `at` (the part after its init statement) and the index of its `{`. */
function conditionOf(
  toks: readonly GoTok[],
  at: number,
): { cond: GoTok[]; open: number } | undefined {
  let depth = 0;
  let from = at + 1;
  for (let k = at + 1; k < toks.length; k++) {
    const tok = toks[k] as GoTok;
    if (tok.kind !== "punct") continue;
    if (depth === 0 && tok.text === "{") return { cond: toks.slice(from, k), open: k };
    if (depth === 0 && tok.text === ";") from = k + 1;
    if (tok.text === "(" || tok.text === "[") depth++;
    if (tok.text === ")" || tok.text === "]") depth--;
  }
  return undefined;
}

const isConstantCondition = (cond: readonly GoTok[]): boolean =>
  cond.length > 0 &&
  cond.every(
    (t) =>
      t.kind === "number" ||
      t.kind === "string" ||
      t.kind === "raw" ||
      t.kind === "rune" ||
      (t.kind === "ident" && CONSTANT_WORDS.has(t.text)) ||
      (t.kind === "punct" && CONSTANT_PUNCT.has(t.text)),
  );

/** `a != a`: one comparison at the top, the same tokens on both sides, nothing called. */
function comparesItself(cond: readonly GoTok[]): boolean {
  if (cond.some((t) => isPunct(t, "(") || isPunct(t, "&&") || isPunct(t, "||"))) return false;
  const at = cond
    .map((t, k) => (t.kind === "punct" && COMPARES.has(t.text) ? k : -1))
    .filter((k) => k >= 0);
  const [split] = at;
  if (at.length !== 1 || split === undefined) return false;
  const left = cond.slice(0, split).map((t) => t.text);
  const right = cond.slice(split + 1).map((t) => t.text);
  return left.length > 0 && left.join(" ") === right.join(" ");
}

/** The tokens inside the braces opened at `open`. */
const bodyOf = (toks: readonly GoTok[], open: number): GoTok[] =>
  toks.slice(open + 1, closeOf(toks, open)).filter((t) => !isPunct(t, ";"));

function ifProblems(file: GoFile, name: string, toks: readonly GoTok[]): Violation[] {
  const out: Violation[] = [];
  toks.forEach((tok, k) => {
    if (tok.kind !== "ident" || tok.text !== "if") return;
    const found = conditionOf(toks, k);
    if (!found) return;
    const { cond, open } = found;
    if (isConstantCondition(cond) || comparesItself(cond)) {
      out.push(
        violation(
          "GOTEST004",
          file.path,
          tok.line,
          `condition of an assertion in ${name} is decided by constants, it cannot fail`,
          "Compare the value the code under test produced with the value it should produce.",
        ),
      );
    }
    const body = bodyOf(toks, open);
    const checksError =
      cond.some((t) => t.kind === "ident" && t.text === "err") &&
      cond.some((t) => isPunct(t, "!=")) &&
      cond.some((t) => t.text === "nil");
    if (checksError && body.length === 1 && LEAVES.has(body[0]?.text ?? "")) {
      out.push(
        violation(
          "GOTEST005",
          file.path,
          tok.line,
          `${name} checks an error and leaves without failing the test`,
          "Call t.Fatal or t.Error with the error: a test that returns on an error passes whatever went wrong.",
        ),
      );
    }
  });
  return out;
}

/** The body range of the function literal that holds token `k`, or undefined when none does. */
function enclosingLiteral(toks: readonly GoTok[], k: number): [number, number] | undefined {
  let depth = 0;
  for (let j = k - 1; j >= 0; j--) {
    const tok = toks[j] as GoTok;
    if (isPunct(tok, "}")) depth++;
    else if (isPunct(tok, "{") && depth > 0) depth--;
    else if (isPunct(tok, "{") && opensLiteral(toks, j)) return [j, closeOf(toks, j)];
  }
  return undefined;
}

/** A `{` that follows `func`, its parameters and its results with no brace or semicolon between. */
function opensLiteral(toks: readonly GoTok[], open: number): boolean {
  for (let j = open - 1; j >= 0; j--) {
    const tok = toks[j] as GoTok;
    if (tok.kind === "ident" && tok.text === "func") return true;
    if (isPunct(tok, "{") || isPunct(tok, "}") || isPunct(tok, ";")) return false;
  }
  return false;
}

/** Whether the tokens hold a call that can fail a test or end it with a panic. */
const canFail = (toks: readonly GoTok[]): boolean =>
  toks.some(
    (tok, k) =>
      (tok.kind === "ident" &&
        FAILING.has(tok.text) &&
        isPunct(toks[k - 1], ".") &&
        isPunct(toks[k + 1], "(")) ||
      (tok.text === "panic" && isPunct(toks[k + 1], "(")),
  );

function recoverProblems(file: GoFile, name: string, toks: readonly GoTok[]): Violation[] {
  const out: Violation[] = [];
  toks.forEach((tok, k) => {
    if (tok.kind !== "ident" || tok.text !== "recover" || !isPunct(toks[k + 1], "(")) return;
    const range = enclosingLiteral(toks, k);
    if (range && canFail(toks.slice(range[0], range[1] + 1))) return;
    out.push(
      violation(
        "GOTEST005",
        file.path,
        tok.line,
        `${name} recovers from a panic and has no way to fail the test`,
        "Check what recover() returned and call t.Fatal when it is not what the test expects.",
      ),
    );
  });
  return out;
}

/** `TestMain` has to run the tests, and nothing may narrow them with the `test.*` flags. */
function hiddenProblems(file: GoFile, decls: readonly GoDecl[]): Violation[] {
  const out: Violation[] = [];
  for (const decl of decls) {
    if (decl.kind === "func" && !decl.recv && decl.names[0] === "TestMain") {
      const runs = decl.toks.some(
        (t, k) =>
          t.text === "Run" && isPunct(decl.toks[k - 1], ".") && isPunct(decl.toks[k + 1], "("),
      );
      if (!runs)
        out.push(
          violation(
            "GOTEST006",
            file.path,
            decl.line,
            "TestMain never calls m.Run: no test of the package runs",
            "Run the tests with os.Exit(m.Run()) or remove TestMain.",
          ),
        );
    }
  }
  for (const tok of file.code) {
    if ((tok.kind === "string" || tok.kind === "raw") && FLAGS.test(tok.text))
      out.push(
        violation(
          "GOTEST006",
          file.path,
          tok.line,
          `a test file sets the flag ${tok.text}, which narrows which tests run`,
          "Choose tests on the command line; a program must not decide it for everyone.",
        ),
      );
  }
  return out;
}

/** GOTEST004 to GOTEST006 over every Go test file of the module. */
export function ruleViolations(root: string, config: GoConfig): Violation[] {
  const out: Violation[] = [];
  for (const file of goFiles(root, config).filter((f) => f.isTest)) {
    const decls = declsOf(file.toks, file.code);
    out.push(...hiddenProblems(file, decls));
    for (const decl of decls.filter(isTestFunc)) {
      const name = decl.names[0] as string;
      out.push(...ifProblems(file, name, decl.toks), ...recoverProblems(file, name, decl.toks));
    }
  }
  return out;
}
