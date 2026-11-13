/**
 * A small reader of Go source for the Go checks (ADR-0019): tokens, the package clause, imports, functions with
 * their size and complexity, and selector uses such as `time.Now`. The harness stays a TypeScript tool that reads
 * Go; it does not parse the whole language. Comments, strings, raw strings and runes are tokens of their own, so
 * an example inside a string or a comment is never mistaken for code.
 */

export type GoTokKind = "ident" | "string" | "raw" | "rune" | "number" | "punct" | "comment";

export interface GoTok {
  readonly kind: GoTokKind;
  readonly text: string;
  /** 1-based line where the token starts. */
  readonly line: number;
  /** 1-based line where the token ends (differs from `line` for block comments and raw strings). */
  readonly endLine: number;
}

const TOKEN =
  /(?<ws>[ \t\r\f\v]+|\n)|(?<lc>\/\/[^\n]*)|(?<bc>\/\*[\s\S]*?(?:\*\/|$))|(?<str>"(?:[^"\\\n]|\\.)*"?)|(?<raw>`[^`]*`?)|(?<rune>'(?:[^'\\\n]|\\.)*'?)|(?<num>\.?[0-9][0-9A-Za-z_.]*)|(?<id>[\p{L}_][\p{L}\p{N}_]*)|(?<op>\.\.\.|<<=|>>=|&\^=|&&|\|\||<-|:=|==|!=|<=|>=|<<|>>|\+\+|--|[-+*/%&|^]=|&\^|[\s\S])/uy;

const KIND_OF_GROUP: ReadonlyArray<readonly [string, GoTokKind]> = [
  ["lc", "comment"],
  ["bc", "comment"],
  ["str", "string"],
  ["raw", "raw"],
  ["rune", "rune"],
  ["num", "number"],
  ["id", "ident"],
  ["op", "punct"],
];

const newlines = (text: string): number => text.match(/\n/g)?.length ?? 0;

/** Every token of a Go file, comments included, with the lines it covers. */
export function lex(text: string): GoTok[] {
  const out: GoTok[] = [];
  let line = 1;
  TOKEN.lastIndex = 0;
  for (;;) {
    const match = TOKEN.exec(text);
    if (!match) return out;
    const groups = match.groups as Record<string, string | undefined>;
    const lines = newlines(match[0]);
    const kind = KIND_OF_GROUP.find(([group]) => groups[group] !== undefined)?.[1];
    if (kind) out.push({ kind, text: match[0], line, endLine: line + lines });
    line += lines;
  }
}

/** The tokens that are code: everything but comments. */
export const codeOf = (toks: readonly GoTok[]): GoTok[] => toks.filter((t) => t.kind !== "comment");

const isPunct = (tok: GoTok | undefined, text: string): boolean =>
  tok !== undefined && tok.kind === "punct" && tok.text === text;

const isIdent = (tok: GoTok | undefined, text: string): boolean =>
  tok !== undefined && tok.kind === "ident" && tok.text === text;

/**
 * The text between the quotes of an interpreted or raw string token, escapes left as written. Import paths and
 * directive arguments are read this way, so a path written with an escape keeps its backslash and GOARCH054 refuses it.
 */
export function stringValue(tok: GoTok): string {
  const body = tok.text.slice(1);
  return body.endsWith(tok.text.charAt(0)) ? body.slice(0, -1) : body;
}

export interface GoPackage {
  readonly name: string;
  readonly line: number;
  /** The comment group right above the package clause, without comment markers; "" when there is none. */
  readonly doc: string;
}

const COMMENT_MARKER = /^\/\/ ?|^\/\*+ ?| ?\*+\/$/g;

/** The package clause and its doc comment; undefined when the file has no package clause. */
export function packageOf(toks: readonly GoTok[]): GoPackage | undefined {
  const index = toks.findIndex((t, k) => isIdent(t, "package") && toks[k + 1]?.kind === "ident");
  if (index < 0) return undefined;
  const clause = toks[index] as GoTok;
  const doc: string[] = [];
  let expected = clause.line - 1;
  for (let k = index - 1; k >= 0; k--) {
    const comment = toks[k] as GoTok;
    if (comment.kind !== "comment" || comment.endLine !== expected) break;
    doc.unshift(comment.text.replace(COMMENT_MARKER, ""));
    expected = comment.line - 1;
  }
  return { name: (toks[index + 1] as GoTok).text, line: clause.line, doc: doc.join("\n").trim() };
}

export interface GoImport {
  readonly path: string;
  /** `.`, `_` or a name; undefined when the import has no alias. */
  readonly alias: string | undefined;
  readonly line: number;
}

function importEntry(code: readonly GoTok[], at: number): [GoImport | undefined, number] {
  const first = code[at];
  const second = code[at + 1];
  const aliased = (first?.kind === "ident" || isPunct(first, ".")) && second?.kind === "string";
  const spec = aliased ? second : first;
  if (spec?.kind !== "string" && spec?.kind !== "raw") return [undefined, at + 1];
  return [
    { path: stringValue(spec), alias: aliased ? first?.text : undefined, line: spec.line },
    at + (aliased ? 2 : 1),
  ];
}

/** The imports of a file, from its import declarations (single or grouped). */
export function importsOf(code: readonly GoTok[]): GoImport[] {
  const out: GoImport[] = [];
  let k = code.findIndex((t) => isIdent(t, "package")) + 2;
  while (k > 1 && isIdent(code[k], "import")) {
    k++;
    const grouped = isPunct(code[k], "(");
    if (grouped) k++;
    while (k < code.length && !isPunct(code[k], ")")) {
      const [entry, next] = importEntry(code, k);
      if (entry) out.push(entry);
      k = next;
      if (!grouped) break;
    }
    if (grouped) k++;
    while (isPunct(code[k], ";")) k++;
  }
  return out;
}

/** The name an import is known by in the file: its alias, or the last path element (`v2` suffix skipped). */
export function localName(imp: GoImport): string | undefined {
  if (imp.alias === "_" || imp.alias === ".") return undefined;
  if (imp.alias) return imp.alias;
  const parts = imp.path.split("/");
  const last = parts.at(-1) ?? "";
  return /^v\d+$/.test(last) && parts.length > 1 ? parts.at(-2) : last;
}

export interface GoSelector {
  /** `package.Member`, with the import path of the package: `time.Now` for "time", `rand.Read` for "math/rand". */
  readonly qualified: string;
  readonly line: number;
}

/** Uses of `name.Member` where `name` is an imported package of this file. Key: `<import path>.<Member>`. */
export function selectorsOf(code: readonly GoTok[], imports: readonly GoImport[]): GoSelector[] {
  const byName = new Map<string, string>();
  for (const imp of imports) {
    const name = localName(imp);
    if (name) byName.set(name, imp.path);
  }
  const out: GoSelector[] = [];
  code.forEach((tok, k) => {
    const path = tok.kind === "ident" ? byName.get(tok.text) : undefined;
    const member = code[k + 2];
    if (path === undefined || !isPunct(code[k + 1], ".") || member?.kind !== "ident") return;
    if (isPunct(code[k - 1], ".")) return;
    out.push({ qualified: `${path}.${member.text}`, line: tok.line });
  });
  return out;
}

export interface GoFunc {
  /** `Name`, or `Receiver.Name` for a method. */
  readonly qualname: string;
  readonly name: string;
  /** Line of the `func` keyword. */
  readonly line: number;
  readonly lines: number;
  readonly params: number;
  /** Cyclomatic complexity: 1 plus every `if`, `for`, `case`, `&&` and `||`. */
  readonly complexity: number;
}

const OPEN = new Set(["(", "[", "{"]);
const CLOSE = new Set([")", "]", "}"]);

/** Index of the token that closes the group opened at `open`, or -1. */
export function closeOf(code: readonly GoTok[], open: number): number {
  let depth = 0;
  for (let k = open; k < code.length; k++) {
    const tok = code[k] as GoTok;
    if (tok.kind !== "punct") continue;
    if (OPEN.has(tok.text)) depth++;
    else if (CLOSE.has(tok.text) && --depth === 0) return k;
  }
  return -1;
}

/** Number of top-level, non-empty, comma-separated entries between a pair of parentheses. */
function countEntries(code: readonly GoTok[], open: number, close: number): number {
  let depth = 0;
  let count = 0;
  let filled = false;
  for (let k = open + 1; k < close; k++) {
    const tok = code[k] as GoTok;
    if (tok.kind === "punct" && OPEN.has(tok.text)) depth++;
    if (tok.kind === "punct" && CLOSE.has(tok.text)) depth--;
    if (depth === 0 && isPunct(tok, ",")) {
      if (filled) count++;
      filled = false;
    } else {
      filled = true;
    }
  }
  return count + (filled ? 1 : 0);
}

/** The type name of a receiver group such as `(s *Store)`, `(Clock)` or `(id ID[K])`. */
export function receiverType(code: readonly GoTok[], open: number, close: number): string {
  let k = open + 1;
  const named =
    code[k]?.kind === "ident" && (code[k + 1]?.kind === "ident" || isPunct(code[k + 1], "*"));
  if (named) k++;
  if (isPunct(code[k], "*")) k++;
  return k < close ? (code[k]?.text ?? "") : "";
}

const DECL_WORDS = new Set(["func", "type", "var", "const", "import", "package"]);

const startsDecl = (code: readonly GoTok[], k: number): boolean => {
  const tok = code[k] as GoTok;
  return tok.kind === "ident" && DECL_WORDS.has(tok.text) && tok.line > (code[k - 1]?.endLine ?? 0);
};

/** `interface{` and `struct{` in a result type open a type, not the body. */
const opensType = (code: readonly GoTok[], k: number): boolean =>
  isIdent(code[k - 1], "interface") || isIdent(code[k - 1], "struct");

const groupDelta = (tok: GoTok): number => {
  if (tok.kind !== "punct") return 0;
  if (tok.text === "(" || tok.text === "[") return 1;
  return tok.text === ")" || tok.text === "]" ? -1 : 0;
};

/** The `{` that opens a function body, starting after the parameters; -1 for a declaration without a body. */
function bodyStart(code: readonly GoTok[], from: number): number {
  let depth = 0;
  for (let k = from; k < code.length; k++) {
    if (depth === 0 && startsDecl(code, k)) return -1;
    if (!isPunct(code[k], "{") || depth !== 0) {
      depth += groupDelta(code[k] as GoTok);
    } else if (!opensType(code, k)) {
      return k;
    } else {
      k = closeOf(code, k);
      if (k < 0) return -1;
    }
  }
  return -1;
}

const BRANCH_WORDS = new Set(["if", "for", "case"]);

function complexityOf(code: readonly GoTok[], from: number, to: number): number {
  let total = 1;
  for (let k = from; k <= to; k++) {
    const tok = code[k] as GoTok;
    if (tok.kind === "ident" && BRANCH_WORDS.has(tok.text)) total++;
    else if (isPunct(tok, "&&") || isPunct(tok, "||")) total++;
  }
  return total;
}

function parseFunc(code: readonly GoTok[], at: number): [GoFunc | undefined, number] {
  const keyword = code[at] as GoTok;
  let k = at + 1;
  let receiver = "";
  if (isPunct(code[k], "(")) {
    const end = closeOf(code, k);
    receiver = receiverType(code, k, end);
    k = end + 1;
  }
  const name = code[k];
  if (name?.kind !== "ident") return [undefined, at + 1];
  k++;
  if (isPunct(code[k], "[")) k = closeOf(code, k) + 1;
  if (!isPunct(code[k], "(")) return [undefined, at + 1];
  const paramsEnd = closeOf(code, k);
  const open = bodyStart(code, paramsEnd + 1);
  if (open < 0) return [undefined, at + 1];
  const close = closeOf(code, open);
  if (close < 0) return [undefined, at + 1];
  const func: GoFunc = {
    qualname: receiver ? `${receiver}.${name.text}` : name.text,
    name: name.text,
    line: keyword.line,
    lines: (code[close] as GoTok).endLine - keyword.line + 1,
    params: countEntries(code, k, paramsEnd),
    complexity: complexityOf(code, open, close),
  };
  return [func, close + 1];
}

/** The function and method declarations of a file (function literals count in the function that holds them). */
export function funcsOf(code: readonly GoTok[]): GoFunc[] {
  const out: GoFunc[] = [];
  let depth = 0;
  for (let k = 0; k < code.length; ) {
    const tok = code[k] as GoTok;
    const topLevel = depth === 0 && isIdent(tok, "func") && tok.line > (code[k - 1]?.endLine ?? 0);
    if (topLevel) {
      const [func, next] = parseFunc(code, k);
      if (func) out.push(func);
      k = next;
      continue;
    }
    if (tok.kind === "punct" && OPEN.has(tok.text)) depth++;
    if (tok.kind === "punct" && CLOSE.has(tok.text)) depth--;
    k++;
  }
  return out;
}

/** Lines in a file the way the limits count them. */
export const lineCount = (text: string): number =>
  text.split("\n").length - (text.endsWith("\n") ? 1 : 0);
