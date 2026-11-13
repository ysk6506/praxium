/**
 * The top-level declarations of a Go file, read from the tokens of go-lex.ts (ADR-0023): functions, methods, and the
 * specs of `type`, `var` and `const` declarations, each with its names, its code tokens and the comment lines right
 * above it. The test lock fingerprints Go tests from these pieces; nothing here knows what a test is.
 *
 * A grouped `var ( ... )` or `type ( ... )` is split into one declaration per spec, so referencing one golden value
 * does not pull in its neighbours. A grouped `const ( ... )` stays whole because later specs repeat earlier ones
 * (`iota`).
 */
import { closeOf, type GoTok, receiverType } from "./go-lex";

export type DeclKind = "func" | "type" | "var" | "const";

export interface GoDecl {
  readonly kind: DeclKind;
  /** Declared names (`_` for a blank one). A method has its own name only; see `recv`. */
  readonly names: readonly string[];
  /** The receiver type of a method; "" for everything else. */
  readonly recv: string;
  /** 1-based line of the first token. */
  readonly line: number;
  readonly endLine: number;
  /** Code tokens, comments excluded. For a spec of a group, the tokens after the keyword. */
  readonly toks: readonly GoTok[];
  /** The comment lines directly above the declaration (standalone comments, no blank line between). */
  readonly doc: readonly GoTok[];
}

const KINDS = new Set<string>(["func", "type", "var", "const"]);
const HEADERS = new Set<string>([...KINDS, "import", "package"]);
const OPEN = new Set(["(", "[", "{"]);
const CLOSE = new Set([")", "]", "}"]);
/** Tokens that cannot end a spec: the next line continues it. */
const CONTINUES = new Set([
  ",",
  "=",
  "+",
  "-",
  "*",
  "/",
  "%",
  "&",
  "|",
  "^",
  "<<",
  ">>",
  "&&",
  "||",
]);

const isPunct = (tok: GoTok | undefined, text: string): boolean =>
  tok?.kind === "punct" && tok.text === text;

const depthDelta = (tok: GoTok): number => {
  if (tok.kind !== "punct") return 0;
  if (OPEN.has(tok.text)) return 1;
  return CLOSE.has(tok.text) ? -1 : 0;
};

/** Whether the token at `k` begins a new line, as a statement or declaration does. */
const startsLine = (code: readonly GoTok[], k: number): boolean => {
  const prev = code[k - 1];
  return prev === undefined || prev.endLine < (code[k] as GoTok).line || isPunct(prev, ";");
};

/** Index ranges of the declarations at nesting depth 0: each starts at a header word that begins a line. */
function segments(code: readonly GoTok[]): Array<[number, number]> {
  const starts: number[] = [];
  let depth = 0;
  code.forEach((tok, k) => {
    if (depth === 0 && tok.kind === "ident" && HEADERS.has(tok.text) && startsLine(code, k))
      starts.push(k);
    depth += depthDelta(tok);
  });
  return starts.map((start, i) => [start, starts[i + 1] ?? code.length]);
}

/** Leading identifiers of a spec: `a, b = 1, 2` has a and b. */
function leadingNames(toks: readonly GoTok[]): string[] {
  const names: string[] = [];
  for (let k = 0; toks[k]?.kind === "ident"; k += 2) {
    names.push((toks[k] as GoTok).text);
    if (!isPunct(toks[k + 1], ",")) break;
  }
  return names;
}

/** The specs inside a group's parentheses: a new one starts on a new line unless the line before runs on. */
function specsOf(inner: readonly GoTok[]): GoTok[][] {
  const specs: GoTok[][] = [];
  let depth = 0;
  inner.forEach((tok, k) => {
    const prev = inner[k - 1];
    const fresh =
      depth === 0 &&
      startsLine(inner, k) &&
      !(prev !== undefined && prev.kind === "punct" && CONTINUES.has(prev.text));
    if (fresh || specs.length === 0) specs.push([]);
    (specs.at(-1) as GoTok[]).push(tok);
    depth += depthDelta(tok);
  });
  return specs.filter((spec) => spec.length > 0);
}

/** The name and receiver of a function declaration segment, or undefined when it has no name. */
function funcHead(seg: readonly GoTok[]): { name: string; recv: string } | undefined {
  let k = 1;
  let recv = "";
  if (isPunct(seg[k], "(")) {
    const end = closeOf(seg, k);
    if (end < 0) return undefined;
    recv = receiverType(seg, k, end);
    k = end + 1;
  }
  const name = seg[k];
  return name?.kind === "ident" ? { name: name.text, recv } : undefined;
}

/** The standalone comment tokens ending on the lines right above `line`, in source order. */
function docAbove(byEnd: ReadonlyMap<number, GoTok>, line: number): GoTok[] {
  const out: GoTok[] = [];
  for (let c = byEnd.get(line - 1); c !== undefined; c = byEnd.get(c.line - 1)) out.unshift(c);
  return out;
}

function standaloneComments(toks: readonly GoTok[]): Map<number, GoTok> {
  const codeLines = new Set<number>();
  for (const tok of toks) {
    if (tok.kind === "comment") continue;
    for (let line = tok.line; line <= tok.endLine; line++) codeLines.add(line);
  }
  return new Map(
    toks.filter((t) => t.kind === "comment" && !codeLines.has(t.line)).map((t) => [t.endLine, t]),
  );
}

const lastLine = (toks: readonly GoTok[]): number => (toks.at(-1) as GoTok).endLine;

/** Collects declarations; `above` finds the comment lines over a line. */
class Collector {
  readonly out: GoDecl[] = [];
  constructor(private readonly byEnd: ReadonlyMap<number, GoTok>) {}

  add(kind: DeclKind, names: string[], recv: string, whole: readonly GoTok[], skip: number): void {
    const body = whole.slice(skip);
    const first = whole[0] as GoTok;
    const doc = docAbove(this.byEnd, first.line);
    this.out.push({
      kind,
      names,
      recv,
      line: first.line,
      endLine: lastLine(whole),
      toks: body,
      doc,
    });
  }

  /** `type`, `var` and `const`: one spec, or the specs of a group. */
  specs(kind: DeclKind, seg: readonly GoTok[]): void {
    if (!isPunct(seg[1], "(")) {
      const names = leadingNames(kind === "type" ? seg.slice(1, 2) : seg.slice(1));
      this.add(kind, names, "", seg, 1);
      return;
    }
    const end = closeOf(seg, 1);
    const specs = specsOf(seg.slice(2, end < 0 ? seg.length : end));
    if (kind === "const") {
      this.add(kind, specs.flatMap(leadingNames), "", seg, 1);
      return;
    }
    for (const spec of specs)
      this.add(kind, leadingNames(kind === "type" ? spec.slice(0, 1) : spec), "", spec, 0);
  }
}

/** The top-level functions, methods, types, variables and constants of a file. */
export function declsOf(toks: readonly GoTok[], code: readonly GoTok[]): GoDecl[] {
  const collector = new Collector(standaloneComments(toks));
  for (const [from, to] of segments(code)) {
    const seg = code.slice(from, to);
    const kind = (seg[0] as GoTok).text;
    const head = kind === "func" ? funcHead(seg) : undefined;
    if (head) collector.add("func", [head.name], head.recv, seg, 0);
    else if (kind !== "func" && KINDS.has(kind)) collector.specs(kind as DeclKind, seg);
  }
  return collector.out;
}

/** The tokens as one string that ignores layout and comments: the same code gives the same text. */
export function normalizedText(toks: readonly GoTok[]): string {
  const parts: string[] = [];
  toks.forEach((tok, k) => {
    const next = toks[k + 1];
    const trailingComma =
      isPunct(tok, ",") && next?.kind === "punct" && CLOSE.has(next.text) && next.text !== "";
    if (!trailingComma) parts.push(tok.text);
  });
  return parts.join(" ");
}
