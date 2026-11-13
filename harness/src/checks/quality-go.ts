/**
 * The Go half of the quality table (ADR-0023). A Go unit is a row named `go:<unit>`, the same key its coverage floor
 * has in coverage-floor.json, so a TypeScript `core.kernel` and a Go `core.kernel` stay two rows. The rubric is the
 * one of quality.ts; this file measures its inputs on Go code from the same sources the Go checks use (go-lex.ts,
 * go-ignores.ts) with the same limits (debt.yaml).
 *
 * A measured value that does not exist stays unknown: a unit the coverage record has no `go:<unit>` entry for is
 * "모름", never a guess. Coverage of a Go unit is statements; of a TypeScript unit it is lines plus branches.
 */
import type { Violation } from "../common";
import type { Limits } from "./debt-measure";
import { type GoConfig, type GoFile, goFiles, unitOfPath } from "./go-config";
import { runTests } from "./go-cover";
import { collect } from "./go-ignores";
import { funcsOf, lineCount } from "./go-lex";

export const goLabel = (unit: string): string => `go:${unit}`;

/** The table label of the Go unit a file belongs to; undefined for a file outside every unit. */
export function labelOfFile(config: GoConfig, file: string): string | undefined {
  const unit = unitOfPath(config, file);
  return unit ? goLabel(unit.name) : undefined;
}

export interface Counts {
  near: number;
  over: number;
}

/** Near and over counts of one file: the file itself and each of its functions. */
function sizeOf(file: GoFile, limits: Limits, share: number): Counts {
  const lines = lineCount(file.text);
  const found = {
    near: lines > limits.max_file_lines * share ? 1 : 0,
    over: lines > limits.max_file_lines ? 1 : 0,
  };
  for (const fn of funcsOf(file.code)) {
    const over =
      fn.lines > limits.max_function_lines ||
      fn.complexity > limits.max_complexity ||
      fn.params > limits.max_params;
    const near =
      fn.lines > limits.max_function_lines * share || fn.complexity > limits.max_complexity * share;
    if (over || near) found.near++;
    if (over) found.over++;
  }
  return found;
}

/** Files and functions of production Go code that are above `share` of a limit, and those over a limit. */
export function sizeCounts(
  root: string,
  config: GoConfig,
  limits: Limits,
  share: number,
): Map<string, Counts> {
  const out = new Map<string, Counts>();
  for (const file of goFiles(root, config).filter((f) => !f.isTest)) {
    const label = labelOfFile(config, file.path);
    if (!label) continue;
    const found = sizeOf(file, limits, share);
    const bucket = out.get(label) ?? { near: 0, over: 0 };
    out.set(label, { near: bucket.near + found.near, over: bucket.over + found.over });
  }
  return out;
}

/** Switched-off checks of Go code per unit: directives and skipped tests (GOIGN*). */
export function ignoreCounts(root: string, config: GoConfig): Map<string, number> {
  const out = new Map<string, number>();
  for (const s of collect(root, config)) {
    const label = labelOfFile(config, s.file) ?? "";
    out.set(label, (out.get(label) ?? 0) + 1);
  }
  return out;
}

/** `[TD id, unit label]` for every debt id a Go comment cites; the label is undefined outside every unit. */
export function markerUnits(root: string, config: GoConfig): Array<[string, string | undefined]> {
  const out: Array<[string, string | undefined]> = [];
  for (const file of goFiles(root, config)) {
    const label = labelOfFile(config, file.path);
    for (const tok of file.toks)
      if (tok.kind === "comment")
        for (const id of tok.text.match(/TD-\d{4}/g) ?? []) out.push([id, label]);
  }
  return out;
}

/** Statement coverage per Go unit from one `go test` run, under the label `go:<unit>`. Throws when the run fails. */
export function coverageRecord(root: string, config: GoConfig): Record<string, number> {
  const { violations, units } = runTests(root, config);
  if (violations.length > 0) throw new Error(describe(violations));
  return Object.fromEntries(
    [...units].map(([unit, c]) => [goLabel(unit), Math.round(c.percent * 100) / 100]),
  );
}

const describe = (violations: readonly Violation[]): string =>
  `Go coverage could not be measured: ${violations.map((v) => v.message).join("; ")}`;
