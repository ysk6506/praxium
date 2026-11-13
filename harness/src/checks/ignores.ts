/**
 * No silent ignores (REQ-022, ADR-0148). A guardrail may be switched off only in the open: every suppression
 * comment, skipped test and CI step allowed to fail says why, and says which open debt item (TD-NNNN) or sealed
 * decision (ADR-NNNN) allows it. Their number may only go down.
 *
 * IGN001 no reason written          IGN002 no TD-NNNN / ADR-NNNN to point at
 * IGN003 the debt item is closed or unknown ("the debt is paid, the ignore stayed")
 * IGN004 the ADR does not exist or is not a sealed, active decision
 * IGN005 more ignores of a kind than the baseline
 * IGN007 (with --base) the baseline was raised or gained a kind
 * IGN006 is a note, not a failure: fewer ignores than the baseline. A change that removes an ignore must not go red;
 * the cleanup job lowers the baseline with `pnpm ratchet` (ADR-0148).
 */
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  git,
  isMain,
  loadYaml,
  parseArgs,
  splitFrontMatter,
  type Violation,
  violation,
} from "../common";
import { isSourceFile, isTestFile, listFiles } from "../files";
import { loadDebtConfig, loadRegister } from "./debt";
import {
  KINDS,
  type Kind,
  type Suppression,
  scanCss,
  scanSkips,
  scanTypeScript,
  scanWorkflow,
} from "./ignores-scan";

const CONFIG_PATH = "harness/config/ignores.yaml";
const REF_RE = /\b(TD|ADR)-(\d{4})\b/g;
const HAS_REF = /\b(TD|ADR)-\d{4}\b/;

export interface IgnoresConfig {
  scan_dirs: string[];
  workflow_dirs: string[];
  adr_dir: string;
  baseline: string;
  /** Biome rule ids (or id prefixes ending in `/`) whose suppression needs a reason only, no TD or ADR. */
  reason_only_rules: string[];
  /** Biome rule id (or prefix ending in `/`) -> the debt item that allows every ignore of it while it is open. */
  basis_by_rule: Record<string, string>;
  /** `@ts-expect-error` in a test file needs a reason only (a type test asserts the error on purpose). */
  reason_only_ts_expect_error_in_tests: boolean;
  /** A reason is at least this many characters once spaces and the numbers are taken out. */
  min_reason_chars: number;
}

export const loadIgnoresConfig = (root: string): IgnoresConfig =>
  loadYaml<IgnoresConfig>(path.join(root, CONFIG_PATH));

const isTestPath = (file: string): boolean =>
  isTestFile(file) || file.startsWith("tests/") || /\.e2e\.tsx?$/.test(file);

/** Every suppression in the repository, in file and line order. */
export function collect(root: string, config: IgnoresConfig): Suppression[] {
  const out: Suppression[] = [];
  const read = (file: string): string => readFileSync(path.join(root, file), "utf8");
  for (const file of listFiles(
    root,
    config.scan_dirs,
    (n) => isSourceFile(n) || n.endsWith(".css"),
  )) {
    const text = read(file);
    if (file.endsWith(".css")) {
      out.push(...scanCss(file, text));
      continue;
    }
    out.push(...scanTypeScript(file, text));
    if (isTestPath(file)) out.push(...scanSkips(file, text));
  }
  for (const file of listFiles(root, config.workflow_dirs, (n) => /\.ya?ml$/.test(n)))
    out.push(...scanWorkflow(file, read(file)));
  return out;
}

export function counts(found: readonly Suppression[]): Record<Kind, number> {
  const out = Object.fromEntries(KINDS.map((kind) => [kind, 0])) as Record<Kind, number>;
  for (const s of found) out[s.kind]++;
  return out;
}

const ruleMatches = (entry: string, rule: string): boolean =>
  entry.endsWith("/") ? rule.startsWith(entry) : rule === entry;

/** Debt items that stand in for a number on the line, one per rule; undefined when some rule has none. */
function ruleBases(config: IgnoresConfig, s: Suppression): string[] | undefined {
  if (s.kind !== "biome-ignore" || s.rules.length === 0) return undefined;
  const bases: string[] = [];
  for (const rule of s.rules) {
    if (config.reason_only_rules.some((entry) => ruleMatches(entry, rule))) continue;
    const entry = Object.keys(config.basis_by_rule).find((key) => ruleMatches(key, rule));
    if (entry === undefined) return undefined;
    bases.push(config.basis_by_rule[entry] as string);
  }
  return bases;
}

const textOf = (s: Suppression): string => `${s.own}\n${s.above}`;

function reasonLength(s: Suppression): number {
  return textOf(s)
    .replace(REF_RE, "")
    .replace(/[\s\p{P}\p{S}]/gu, "").length;
}

function reasonOnly(config: IgnoresConfig, s: Suppression): boolean {
  if (s.kind === "ts-expect-error")
    return config.reason_only_ts_expect_error_in_tests && isTestPath(s.file);
  if (s.kind !== "biome-ignore" || s.rules.length === 0) return false;
  return s.rules.every((rule) =>
    config.reason_only_rules.some((entry) => ruleMatches(entry, rule)),
  );
}

/** ADR numbers that exist as an active, sealed decision. */
export function sealedAdrs(root: string, config: IgnoresConfig): Set<string> {
  const dir = path.join(root, config.adr_dir);
  const out = new Set<string>();
  if (!existsSync(dir)) return out;
  for (const name of readdirSync(dir).filter((n) => /^\d{4}-.*\.md$/.test(n))) {
    const { data } = splitFrontMatter(readFileSync(path.join(dir, name), "utf8"));
    if (data?.status === "active" && typeof data.decision_hash === "string")
      out.add(name.slice(0, 4));
  }
  return out;
}

const KIND_LABEL: Record<Kind, string> = {
  "biome-ignore": "biome-ignore",
  "eslint-disable": "eslint-disable",
  "ts-ignore": "@ts-ignore",
  "ts-expect-error": "@ts-expect-error",
  "ts-nocheck": "@ts-nocheck",
  "test-skip": "skipped test",
  "continue-on-error": "continue-on-error",
};

function refProblems(
  s: Suppression,
  status: ReadonlyMap<string, string>,
  adrs: ReadonlySet<string>,
): Violation[] {
  const out: Violation[] = [];
  for (const match of textOf(s).matchAll(REF_RE)) {
    const id = `${match[1]}-${match[2]}`;
    if (match[1] === "TD" && status.get(id) !== "open") {
      out.push(
        violation(
          "IGN003",
          s.file,
          s.line,
          `${KIND_LABEL[s.kind]} cites ${id}, which is ${status.get(id) ?? "not in the register"}`,
          "The debt is closed but the ignore stayed: remove the ignore, or cite an open item or an ADR.",
        ),
      );
    } else if (match[1] === "ADR" && !adrs.has(match[2] as string)) {
      out.push(
        violation(
          "IGN004",
          s.file,
          s.line,
          `${KIND_LABEL[s.kind]} cites ${id}, which is not a sealed active ADR`,
          "Cite the ADR that allows this ignore (it must exist and be sealed), or an open TD-NNNN.",
        ),
      );
    }
  }
  return out;
}

/** What is missing when the line itself names no TD or ADR: nothing for a reason-only rule or one with an open rule-level debt. */
function missingBasis(
  config: IgnoresConfig,
  s: Suppression,
  status: ReadonlyMap<string, string>,
): Violation[] {
  if (HAS_REF.test(textOf(s)) || reasonOnly(config, s)) return [];
  const bases = ruleBases(config, s);
  if (bases === undefined) {
    return [
      violation(
        "IGN002",
        s.file,
        s.line,
        `${KIND_LABEL[s.kind]} does not say which debt item or ADR allows it`,
        "Write TD-NNNN (an open item in docs/exec-plans/tech-debt.yaml) or ADR-NNNN with the reason, or fix the cause.",
      ),
    ];
  }
  return bases
    .filter((id) => status.get(id) !== "open")
    .map((id) =>
      violation(
        "IGN003",
        s.file,
        s.line,
        `${KIND_LABEL[s.kind]} rests on ${id} (basis_by_rule), which is ${status.get(id) ?? "not in the register"}`,
        "The debt is closed but the ignore stayed: remove the ignores of that rule, or reopen the item.",
      ),
    );
}

export function checkSuppressions(
  root: string,
  config: IgnoresConfig,
  found: readonly Suppression[],
): Violation[] {
  const debt = loadDebtConfig(root);
  const status = new Map(loadRegister(root, debt).map((i) => [String(i.id), String(i.status)]));
  const adrs = sealedAdrs(root, config);
  const out: Violation[] = [];
  for (const s of found) {
    if (reasonLength(s) < config.min_reason_chars) {
      out.push(
        violation(
          "IGN001",
          s.file,
          s.line,
          `${KIND_LABEL[s.kind]} without a reason`,
          "Say why on the same line or the line above (at least a short sentence), or remove the ignore and fix the cause.",
        ),
      );
    }
    out.push(...missingBasis(config, s, status));
    out.push(...refProblems(s, status, adrs));
  }
  return out;
}

export function loadBaseline(root: string, config: IgnoresConfig): Record<string, number> {
  const file = path.join(root, config.baseline);
  return existsSync(file) ? (JSON.parse(readFileSync(file, "utf8")) as Record<string, number>) : {};
}

export function checkCounts(
  root: string,
  config: IgnoresConfig,
  found: readonly Suppression[],
): Violation[] {
  const baseline = loadBaseline(root, config);
  const now = counts(found);
  const out: Violation[] = [];
  for (const kind of KINDS) {
    const allowed = baseline[kind] ?? 0;
    if (now[kind] > allowed) {
      out.push(
        violation(
          "IGN005",
          config.baseline,
          0,
          `${now[kind]} ${KIND_LABEL[kind]} ignores, baseline ${allowed}`,
          "The number of ignores may only go down. Fix the cause instead of adding another.",
        ),
      );
    }
  }
  return out;
}

/** One line per kind whose count fell below the baseline. Never a failure. */
export function lowerable(
  root: string,
  config: IgnoresConfig,
  found: readonly Suppression[],
): string[] {
  const baseline = loadBaseline(root, config);
  const now = counts(found);
  return KINDS.filter((kind) => now[kind] < (baseline[kind] ?? 0)).map(
    (kind) =>
      `NOTE ignores [IGN006] ${now[kind]} ${KIND_LABEL[kind]} ignores, baseline still ${baseline[kind]}: run pnpm ratchet to lower it`,
  );
}

/** Against `base`, no kind may be raised. */
export function checkBaselineAgainst(
  root: string,
  config: IgnoresConfig,
  base: string,
): Violation[] {
  const previous = git(root, "show", `${base}:${config.baseline}`);
  if (previous === null) return [];
  const old = JSON.parse(previous) as Record<string, number>;
  return Object.entries(loadBaseline(root, config))
    .filter(([kind, value]) => value > (old[kind] ?? 0))
    .map(([kind, value]) =>
      violation(
        "IGN007",
        config.baseline,
        0,
        `baseline for '${kind}' was raised (was ${old[kind] ?? "absent"}, now ${value})`,
        "Never raise the baseline. Fix the code instead of adding an ignore.",
      ),
    );
}

export function report(root: string, base?: string): { violations: Violation[]; notes: string[] } {
  const config = loadIgnoresConfig(root);
  const found = collect(root, config);
  return {
    violations: [
      ...checkSuppressions(root, config, found),
      ...checkCounts(root, config, found),
      ...(base ? checkBaselineAgainst(root, config, base) : []),
    ],
    notes: lowerable(root, config, found),
  };
}

export const run = (root: string, base?: string): Violation[] => report(root, base).violations;

/** Lower each kind's baseline to today's count. Never raises one. Returns the kinds that went down. */
export function ratchet(root: string): string[] {
  const config = loadIgnoresConfig(root);
  const baseline = loadBaseline(root, config);
  const now = counts(collect(root, config));
  const updated: Record<string, number> = {};
  const lowered: string[] = [];
  for (const kind of KINDS) {
    const old = baseline[kind] ?? 0;
    updated[kind] = Math.min(old, now[kind]);
    if (updated[kind] < old) lowered.push(kind);
  }
  writeFileSync(path.join(root, config.baseline), `${JSON.stringify(updated, null, 2)}\n`);
  return lowered;
}

export function main(argv: readonly string[], out: (line: string) => void = console.log): number {
  const { positional, flags } = parseArgs(argv);
  const root = findRoot(flag(flags, "root"));
  if (positional[0] === "ratchet") {
    const lowered = ratchet(root);
    out(
      `ratchet: lowered ${lowered.length} ignore baseline(s)${lowered.length ? `: ${lowered.join(", ")}` : ""}`,
    );
    return 0;
  }
  const { violations, notes } = report(root, flag(flags, "base"));
  const code = finish("ignores", violations, out);
  for (const line of notes) out(line);
  return code;
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
