/**
 * Finds every place a guardrail is switched off (ADR-0148): lint/type suppression comments, skipped tests and
 * CI steps allowed to fail. TypeScript is read through the compiler's comment ranges (`comments`) and call
 * expressions, so an example inside a string or a template is never counted. CSS and YAML have no parser here:
 * CSS is read with a small comment scanner that skips strings, YAML line by line.
 */
import { comments, dotted, lineOf, parse, ts, walk } from "../ts-ast";
import { testApi } from "./test-units";

export const KINDS = [
  "biome-ignore",
  "eslint-disable",
  "ts-ignore",
  "ts-expect-error",
  "ts-nocheck",
  "test-skip",
  "continue-on-error",
] as const;
export type Kind = (typeof KINDS)[number];

export interface Suppression {
  readonly kind: Kind;
  readonly file: string;
  readonly line: number;
  /** Lint rules the comment names (biome only); empty otherwise. */
  readonly rules: readonly string[];
  /** The reason written on the directive itself (after the colon, `--` or the title of a skipped test). */
  readonly own: string;
  /** Text of the comment that ends on the line above, if any. */
  readonly above: string;
}

const SKIP_MODIFIERS = new Set(["skip", "todo", "skipIf", "runIf", "fails", "fixme"]);

interface Directive {
  readonly kind: Kind;
  readonly rules: readonly string[];
  readonly own: string;
}

/** First line of a comment without its delimiters. */
function headOf(text: string): string {
  const first = text.split("\n", 1)[0] as string;
  return first
    .replace(/^\s*(\/\/+|\/\*+)\s*/, "")
    .replace(/\*\/\s*$/, "")
    .trim();
}

function biomeDirective(head: string): Directive | undefined {
  // `biome-ignore-end` closes a `biome-ignore-start` range and is not a suppression of its own.
  const match = /^biome-ignore(-all|-start)?(?![-\w])\s*(.*)$/.exec(head);
  if (!match) return undefined;
  const rest = match[2] as string;
  const colon = rest.indexOf(":");
  const names = colon < 0 ? rest : rest.slice(0, colon);
  return {
    kind: "biome-ignore",
    rules: names.split(/\s+/).filter((name) => name !== ""),
    own: colon < 0 ? "" : rest.slice(colon + 1).trim(),
  };
}

const TS_KINDS: Record<string, Kind> = {
  ignore: "ts-ignore",
  "expect-error": "ts-expect-error",
  nocheck: "ts-nocheck",
};

/** What a comment switches off, judged from where the comment starts (a mention further in is prose). */
export function directiveOf(text: string): Directive | undefined {
  const head = headOf(text);
  const biome = biomeDirective(head);
  if (biome) return biome;
  const eslint = /^eslint-disable(?:-next-line|-line)?(?![-\w])(.*)$/.exec(head);
  if (eslint) {
    const tail = eslint[1] as string;
    const dashes = tail.indexOf("--");
    const own = dashes < 0 ? "" : tail.slice(dashes + 2).trim();
    return { kind: "eslint-disable", rules: [], own };
  }
  const tsMatch = /^@ts-(ignore|expect-error|nocheck)(?![-\w])[\s:-]*(.*)$/.exec(head);
  if (tsMatch) {
    return { kind: TS_KINDS[tsMatch[1] as string] as Kind, rules: [], own: tsMatch[2] as string };
  }
  return undefined;
}

const newlines = (text: string): number => text.match(/\n/g)?.length ?? 0;

const endLine = (line: number, text: string): number => line + newlines(text);

/** Suppression comments of one TypeScript file, with the comment that ends right above each. */
export function scanTypeScript(file: string, text: string): Suppression[] {
  const all = comments(file, text);
  const out: Suppression[] = [];
  for (const comment of all) {
    const directive = directiveOf(comment.text);
    if (!directive) continue;
    const above = all.find(
      (other) => other !== comment && endLine(other.line, other.text) === comment.line - 1,
    );
    out.push({ ...directive, file, line: comment.line, above: above?.text ?? "" });
  }
  return out;
}

/** A skipped, todo or conditionally run test: `test.skip(...)`, `describe.skipIf(...)`, `ctx.skip()`. */
export function scanSkips(file: string, text: string): Suppression[] {
  const sf = parse(file, text);
  const all = comments(file, text);
  const out: Suppression[] = [];
  walk(sf, (node) => {
    if (!ts.isCallExpression(node)) return;
    // `test.skipIf(c)(name, fn)` is one skip: only the outer call counts.
    if (ts.isCallExpression(node.parent) && node.parent.expression === node) return;
    const api = testApi(node);
    const skips = api
      ? api.modifiers.some((m) => SKIP_MODIFIERS.has(m))
      : /\.skip$/.test(dotted(node.expression));
    if (!skips) return;
    const line = lineOf(sf, node.getStart(sf));
    const first = node.arguments[0];
    const title = first && ts.isStringLiteralLike(first) ? first.text : "";
    const above = all.find((c) => endLine(c.line, c.text) === line - 1);
    out.push({
      kind: "test-skip",
      file,
      line,
      rules: [],
      own: title,
      above: above?.text ?? "",
    });
  });
  return out;
}

/** Index just past the string that starts at `start` (a string ends at its closing quote or the line end). */
function afterString(text: string, start: number): number {
  const quote = text[start];
  let index = start + 1;
  while (index < text.length && text[index] !== quote && text[index] !== "\n") {
    index += text[index] === "\\" ? 2 : 1;
  }
  return text[index] === quote ? index + 1 : index;
}

/** CSS comments (`/* ... *\/`) outside strings, with their first line. */
function cssComments(text: string): Array<{ line: number; text: string }> {
  const found: Array<{ line: number; text: string }> = [];
  let line = 1;
  let index = 0;
  while (index < text.length) {
    const char = text[index];
    if (char === '"' || char === "'") {
      index = afterString(text, index);
    } else if (char === "/" && text[index + 1] === "*") {
      const close = text.indexOf("*/", index + 2);
      const body = text.slice(index, close < 0 ? text.length : close + 2);
      found.push({ line, text: body });
      line += newlines(body);
      index += body.length;
    } else {
      if (char === "\n") line++;
      index++;
    }
  }
  return found;
}

export function scanCss(file: string, text: string): Suppression[] {
  const all = cssComments(text);
  const out: Suppression[] = [];
  for (const comment of all) {
    const directive = directiveOf(comment.text);
    if (!directive) continue;
    const above = all.find(
      (other) => other !== comment && endLine(other.line, other.text) === comment.line - 1,
    );
    out.push({ ...directive, file, line: comment.line, above: above?.text ?? "" });
  }
  return out;
}

/** `continue-on-error` set to anything but `false`, with the trailing comment or the comment line above. */
export function scanWorkflow(file: string, text: string): Suppression[] {
  const lines = text.split("\n");
  const out: Suppression[] = [];
  lines.forEach((raw, index) => {
    const match = /^\s*(?:-\s+)?continue-on-error\s*:\s*([^#]*?)\s*(?:#\s?(.*))?$/.exec(raw);
    if (!match || (match[1] as string).trim() === "false") return;
    const previous = lines[index - 1] ?? "";
    out.push({
      kind: "continue-on-error",
      file,
      line: index + 1,
      rules: [],
      own: (match[2] ?? "").trim(),
      above: /^\s*#/.test(previous) ? previous.replace(/^\s*#\s?/, "") : "",
    });
  });
  return out;
}
