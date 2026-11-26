/**
 * One declaration of a Go test pool (ADR-0023) with what the test guard needs of it, worked out once (ADR-0124): the
 * names it mentions, its testing handles, its literals, and its calls that fail or skip a test. A test reaches many
 * declarations and many tests reach the same one, so these facts belong to the declaration, not to each test.
 */
import type { GoFile } from "./go-config";
import { type GoDecl, normalizedText } from "./go-decls";
import { closeOf, type GoTok } from "./go-lex";

const FAILING = new Set(["Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow"]);
const SKIPPING = new Set(["Skip", "Skipf", "SkipNow"]);
const TESTING_TYPES = new Set(["T", "B", "F", "TB"]);
const LITERAL_KINDS = new Set(["string", "raw", "rune", "number"]);

/** A declaration of the package's test files with what it mentions. */
export interface Entry {
  readonly decl: GoDecl;
  readonly file: GoFile;
  readonly text: string;
  /** Identifiers that are not a member selector (`x.name`). */
  readonly refs: ReadonlySet<string>;
  /** Identifiers right after a dot: a method may hide behind them. */
  readonly members: ReadonlySet<string>;
  /** Every identifier token, its own name included: what a helper that fails tests can be called by. */
  readonly idents: ReadonlySet<string>;
  /** Names of the testing handles it declares: `t` in `t *testing.T`. */
  readonly handles: ReadonlySet<string>;
  /** Calls `t.Error(`, `t.Fatal(`... on its own handles. */
  readonly fails: number;
  /** Calls `t.Skip(`... on its own handles, plus `testing.Short(` gates. */
  readonly skips: number;
  /** Its string, number and rune literals, in token order. */
  readonly literals: readonly string[];
  /** The folder of the locked-support package this entry belongs to; undefined for a declaration of a test file. */
  readonly supportDir?: string;
}

export const isPunct = (tok: GoTok | undefined, text: string): boolean =>
  tok?.kind === "punct" && tok.text === text;

function handlesOf(toks: readonly GoTok[]): Set<string> {
  const names = new Set<string>();
  toks.forEach((tok, k) => {
    if (tok.kind !== "ident") return;
    const at = isPunct(toks[k + 1], "*") ? k + 2 : k + 1;
    if (
      toks[at]?.text === "testing" &&
      isPunct(toks[at + 1], ".") &&
      TESTING_TYPES.has(toks[at + 2]?.text ?? "")
    )
      names.add(tok.text);
  });
  return names;
}

/** Index of the name token of a function declaration: after `func`, or after the receiver of a method. */
function nameAt(toks: readonly GoTok[]): number {
  return isPunct(toks[1], "(") ? closeOf(toks, 1) + 1 : 1;
}

/** Calls `handle.Method(` for a handle name and a method set. */
function countCalls(
  toks: readonly GoTok[],
  handles: ReadonlySet<string>,
  methods: ReadonlySet<string>,
): number {
  let count = 0;
  toks.forEach((tok, k) => {
    if (
      tok.kind === "ident" &&
      handles.has(tok.text) &&
      isPunct(toks[k + 1], ".") &&
      methods.has(toks[k + 2]?.text ?? "") &&
      isPunct(toks[k + 3], "(")
    )
      count++;
  });
  return count;
}

/** `testing.Short(` gates a test the same way a skip does. */
function shortGates(toks: readonly GoTok[]): number {
  return toks.filter(
    (tok, k) =>
      tok.text === "Short" &&
      isPunct(toks[k - 1], ".") &&
      toks[k - 2]?.text === "testing" &&
      isPunct(toks[k + 1], "("),
  ).length;
}

export function entryOf(decl: GoDecl, file: GoFile, supportDir?: string): Entry {
  const refs = new Set<string>();
  const members = new Set<string>();
  const idents = new Set<string>();
  const own = decl.kind === "func" ? nameAt(decl.toks) : -1;
  decl.toks.forEach((tok, k) => {
    if (tok.kind !== "ident") return;
    idents.add(tok.text);
    if (k === own) return;
    (isPunct(decl.toks[k - 1], ".") ? members : refs).add(tok.text);
  });
  const handles = handlesOf(decl.toks);
  const kind = decl.recv ? `${decl.kind} (${decl.recv})` : decl.kind;
  return {
    decl,
    file,
    text: `${kind} ${normalizedText(decl.toks)}`,
    refs,
    members,
    idents,
    handles,
    fails: countCalls(decl.toks, handles, FAILING),
    skips: countCalls(decl.toks, handles, SKIPPING) + shortGates(decl.toks),
    literals: decl.toks.filter((t) => LITERAL_KINDS.has(t.kind)).map((t) => t.text),
    ...(supportDir === undefined ? {} : { supportDir }),
  };
}

const mentionsAny = (idents: ReadonlySet<string>, names: ReadonlySet<string>): boolean => {
  for (const name of idents) if (names.has(name)) return true;
  return false;
};

/** Names of the functions and methods among `entries` that can fail a test, directly or through another one. */
export function failingHelpers(entries: readonly Entry[]): Set<string> {
  const helpers = new Set<string>();
  const funcs = entries.filter((e) => e.decl.kind === "func");
  for (let changed = true; changed; ) {
    changed = false;
    for (const e of funcs) {
      const name = e.decl.names[0] as string;
      if (helpers.has(name)) continue;
      const fails = e.fails > 0 || mentionsAny(e.idents, helpers);
      if (fails) helpers.add(name);
      changed ||= fails;
    }
  }
  return helpers;
}
