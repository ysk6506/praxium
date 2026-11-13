/**
 * No silent ignores in Go code (REQ-022, ADR-0019). A guardrail may be switched off only in the open: a `//nolint`
 * or `//lint:ignore` comment, a `//go:build ignore` file, a `//nosec` mark, a skipped test (`t.Skip`, `t.Skipf`,
 * `t.SkipNow`, `testing.Short`) says why and says which open debt item (TD-NNNN) or sealed decision (ADR-NNNN)
 * allows it, on its own line or the line above. Go code starts with none and has no baseline: every one needs its
 * reason, and a reviewer sees each one.
 *
 * GOIGN001 no reason written          GOIGN002 no TD-NNNN / ADR-NNNN to point at
 * GOIGN003 the debt item is closed or unknown                GOIGN004 the ADR is not a sealed, active decision
 */
import { type Violation, violation } from "../common";
import type { GoConfig, GoFile } from "./go-config";
import { goFiles } from "./go-config";
import type { GoTok } from "./go-lex";
import { stringValue } from "./go-lex";

const REF_RE = /\b(TD|ADR)-(\d{4})\b/g;
const HAS_REF = /\b(TD|ADR)-\d{4}\b/;
const DIRECTIVE =
  /^\/\/\s*(?:(nolint)(?::[\w,-]+)?|(lint:(?:file-)?ignore)\b\s*\S*|(go:build)\s+.*\bignore\b|(#?nosec)\b)/;
const SKIP_CALLS = new Set(["Skip", "Skipf", "SkipNow"]);

export interface GoSuppression {
  readonly what: string;
  readonly file: string;
  readonly line: number;
  /** The reason written on the directive's own line. */
  readonly own: string;
  /** The comment that ends on the line above. */
  readonly above: string;
}

function commentOn(file: GoFile, line: number, skip?: GoTok): string {
  return file.toks
    .filter((t) => t.kind === "comment" && t !== skip && t.endLine === line)
    .map((t) => t.text)
    .join("\n");
}

/** What a directive comment switches off; undefined for any other comment. */
function directiveName(tok: GoTok): { what: string; rest: string } | undefined {
  const match = DIRECTIVE.exec(tok.text);
  if (!match) return undefined;
  const what = (match[1] ?? match[2] ?? match[3] ?? match[4]) as string;
  const rest = tok.text.slice(match[0].length).replace(/^\s*(\/\/|--|:)?\s*/, "");
  return { what: what === "go:build" ? "//go:build ignore" : `//${what}`, rest };
}

function directives(file: GoFile): GoSuppression[] {
  const out: GoSuppression[] = [];
  for (const tok of file.toks) {
    const found = tok.kind === "comment" ? directiveName(tok) : undefined;
    if (!found) continue;
    out.push({
      what: found.what,
      file: file.path,
      line: tok.line,
      own: found.rest,
      above: commentOn(file, tok.line - 1),
    });
  }
  return out;
}

/** `x.Skip(...)`, `x.Skipf(...)`, `x.SkipNow()` and `testing.Short()` in a test file. */
function skips(file: GoFile): GoSuppression[] {
  const out: GoSuppression[] = [];
  file.code.forEach((tok, k) => {
    const prev = file.code[k - 1];
    const isSkip =
      tok.kind === "ident" &&
      SKIP_CALLS.has(tok.text) &&
      prev?.text === "." &&
      file.code[k + 1]?.text === "(";
    const isShort =
      tok.text === "Short" &&
      prev?.text === "." &&
      file.code[k - 2]?.text === "testing" &&
      file.code[k + 1]?.text === "(";
    if (!isSkip && !isShort) return;
    const literals = file.code
      .filter((t) => t.line === tok.line && (t.kind === "string" || t.kind === "raw"))
      .map(stringValue);
    const trailing = commentOn(file, tok.line);
    out.push({
      what: isShort ? "testing.Short() gate" : `t.${tok.text}`,
      file: file.path,
      line: tok.line,
      own: [...literals, trailing].join(" "),
      above: commentOn(file, tok.line - 1),
    });
  });
  return out;
}

export function collect(root: string, config: GoConfig): GoSuppression[] {
  return goFiles(root, config).flatMap((file) => [
    ...directives(file),
    ...(file.isTest ? skips(file) : []),
  ]);
}

const textOf = (s: GoSuppression): string => `${s.own}\n${s.above}`;

function reasonLength(s: GoSuppression): number {
  return textOf(s)
    .replace(REF_RE, "")
    .replace(/\/\/|\/\*|\*\//g, "")
    .replace(/[\s\p{P}\p{S}]/gu, "").length;
}

export function check(
  found: readonly GoSuppression[],
  status: ReadonlyMap<string, string>,
  sealedAdrs: ReadonlySet<string>,
  minReasonChars: number,
): Violation[] {
  const out: Violation[] = [];
  for (const s of found) {
    if (reasonLength(s) < minReasonChars) {
      out.push(
        violation(
          "GOIGN001",
          s.file,
          s.line,
          `${s.what} without a reason`,
          "Say why on the same line or the line above (at least a short sentence), or remove it and fix the cause.",
        ),
      );
    }
    if (!HAS_REF.test(textOf(s))) {
      out.push(
        violation(
          "GOIGN002",
          s.file,
          s.line,
          `${s.what} does not say which debt item or ADR allows it`,
          "Write TD-NNNN (an open item in docs/exec-plans/tech-debt.yaml) or ADR-NNNN with the reason, or fix the cause.",
        ),
      );
    }
    for (const match of textOf(s).matchAll(REF_RE)) {
      const id = `${match[1]}-${match[2]}`;
      if (match[1] === "TD" && status.get(id) !== "open") {
        out.push(
          violation(
            "GOIGN003",
            s.file,
            s.line,
            `${s.what} cites ${id}, which is ${status.get(id) ?? "not in the register"}`,
            "The debt is closed but the ignore stayed: remove the ignore, or cite an open item or an ADR.",
          ),
        );
      } else if (match[1] === "ADR" && !sealedAdrs.has(match[2] as string)) {
        out.push(
          violation(
            "GOIGN004",
            s.file,
            s.line,
            `${s.what} cites ${id}, which is not a sealed active ADR`,
            "Cite the ADR that allows this ignore (it must exist and be sealed), or an open TD-NNNN.",
          ),
        );
      }
    }
  }
  return out;
}
