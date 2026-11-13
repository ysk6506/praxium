/**
 * Technical debt in Go code (REQ-022, ADR-0019): the same limits as the TypeScript code (harness/config/debt.yaml)
 * and the same register (docs/exec-plans/tech-debt.yaml). Go code starts clean, so there is no baseline: a limit is
 * kept, or an open debt item is named in a `// debt-ok: TD-NNNN` waiver on the reported line or on line 1.
 *
 * GODEBT001 task-marker comment (the words listed as `markers` in debt.yaml) without (TD-NNNN)
 * GODEBT002 reference to an unknown item
 * GODEBT013 a closed item is still referenced                GODEBT020 file too long
 * GODEBT021 function too long        GODEBT022 complexity too high        GODEBT023 too many parameters
 * GODEBT032 a waiver nothing needs
 */
import { type Violation, violation } from "../common";
import type { DebtConfig, DebtItem } from "./debt";
import type { GoConfig, GoFile } from "./go-config";
import { goFiles } from "./go-config";
import { funcsOf, lineCount } from "./go-lex";

const WAIVER_RE = /debt-ok:\s*(TD-\d{4})/;

interface Measure {
  readonly code: string;
  readonly line: number;
  readonly value: number;
  readonly limit: number;
  readonly what: string;
}

function measures(file: GoFile, config: DebtConfig): Measure[] {
  const out: Measure[] = [];
  const lines = lineCount(file.text);
  const { limits } = config;
  if (lines > limits.max_file_lines) {
    out.push({
      code: "GODEBT020",
      line: 1,
      value: lines,
      limit: limits.max_file_lines,
      what: "file length (lines)",
    });
  }
  for (const fn of funcsOf(file.code)) {
    const checks: Array<[string, number, number, string]> = [
      ["GODEBT021", fn.lines, limits.max_function_lines, "function length (lines)"],
      ["GODEBT022", fn.complexity, limits.max_complexity, "cyclomatic complexity"],
      ["GODEBT023", fn.params, limits.max_params, "parameter count"],
    ];
    for (const [code, value, limit, label] of checks) {
      if (value > limit)
        out.push({ code, line: fn.line, value, limit, what: `${label} of ${fn.qualname}` });
    }
  }
  return out;
}

/** The waiver comment on `line`, if any: its comment's line and the item it names. */
function waiverOn(file: GoFile, line: number): [number, string] | undefined {
  for (const tok of file.toks) {
    if (tok.kind !== "comment" || tok.line !== line) continue;
    const match = WAIVER_RE.exec(tok.text);
    if (match) return [line, match[1] as string];
  }
  return undefined;
}

function referenceProblem(
  file: GoFile,
  line: number,
  id: string,
  status: ReadonlyMap<string, string>,
): Violation[] {
  const state = status.get(id);
  if (state === undefined) {
    return [
      violation(
        "GODEBT002",
        file.path,
        line,
        `${id} is not in the register`,
        "Add it to the register or fix the id.",
      ),
    ];
  }
  return state === "open"
    ? []
    : [
        violation(
          "GODEBT013",
          file.path,
          line,
          `${id} is ${state} but code still references it`,
          "Delete the marker or waiver, or reopen the item.",
        ),
      ];
}

function markerProblems(
  file: GoFile,
  config: DebtConfig,
  status: ReadonlyMap<string, string>,
): Violation[] {
  const markerRe = new RegExp(`\\b(${config.markers.join("|")})\\b(\\((TD-\\d{4})\\))?`, "g");
  const out: Violation[] = [];
  for (const tok of file.toks.filter((t) => t.kind === "comment")) {
    for (const match of tok.text.matchAll(markerRe)) {
      if (match[3] === undefined) {
        out.push(
          violation(
            "GODEBT001",
            file.path,
            tok.line,
            `${match[1]} comment is not tracked`,
            `Add an entry to ${config.register} and write ${match[1]}(TD-NNNN), or fix it now.`,
          ),
        );
      } else {
        out.push(...referenceProblem(file, tok.line, match[3], status));
      }
    }
    const waiver = WAIVER_RE.exec(tok.text);
    if (waiver) out.push(...referenceProblem(file, tok.line, waiver[1] as string, status));
  }
  return out;
}

function sizeProblems(
  file: GoFile,
  config: DebtConfig,
  status: ReadonlyMap<string, string>,
): Violation[] {
  const out: Violation[] = [];
  const used = new Set<number>();
  for (const measure of file.isTest ? [] : measures(file, config)) {
    const waiver = waiverOn(file, measure.line) ?? waiverOn(file, 1);
    if (waiver && status.get(waiver[1]) === "open") {
      used.add(waiver[0]);
      continue;
    }
    out.push(
      violation(
        measure.code,
        file.path,
        measure.line,
        `${measure.what} is ${measure.value} (limit ${measure.limit})`,
        "Split it up. If it cannot be fixed now, register debt and add `// debt-ok: TD-NNNN` on that line.",
      ),
    );
  }
  for (const tok of file.toks) {
    const waiver = tok.kind === "comment" ? WAIVER_RE.exec(tok.text) : null;
    if (waiver && status.get(waiver[1] as string) === "open" && !used.has(tok.line)) {
      out.push(
        violation(
          "GODEBT032",
          file.path,
          tok.line,
          "debt-ok waiver but nothing needs it",
          "Delete the waiver (and close the debt item if it is resolved).",
        ),
      );
    }
  }
  return out;
}

export function run(
  root: string,
  goConfig: GoConfig,
  config: DebtConfig,
  items: readonly DebtItem[],
): Violation[] {
  const status = new Map(items.map((i) => [String(i.id), String(i.status)]));
  return goFiles(root, goConfig).flatMap((file) => [
    ...markerProblems(file, config, status),
    ...sizeProblems(file, config, status),
  ]);
}
