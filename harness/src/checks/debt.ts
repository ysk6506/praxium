/**
 * Technical debt control (REQ-022, ADR-0003). Debt may exist only when it is written down.
 *
 * DEBT001 marker without (TD-NNNN)        DEBT002 reference to an unknown item
 * DEBT010 malformed register entry         DEBT011 open item past due        DEBT012 budget exceeded
 * DEBT013 closed item still referenced     DEBT020-023 size/complexity over limit
 * DEBT030 stale baseline entry             DEBT031 baselined violation grew
 * DEBT032 waiver no longer needed          DEBT033 (with --base) baseline grew
 */
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  git,
  isIsoDate,
  isMain,
  loadYaml,
  parseArgs,
  today,
  type Violation,
  violation,
} from "../common";
import { isSourceFile, isTestFile, listFiles } from "../files";
import { comments } from "../ts-ast";
import { type Limits, type Measure, measureFile } from "./debt-measure";

const CONFIG_PATH = "harness/config/debt.yaml";
const ID_RE = /^TD-\d{4}$/;
const WAIVER_RE = /debt-ok:\s*(TD-\d{4})/;
const REQUIRED = ["id", "title", "severity", "cost", "created", "due", "status"] as const;
const SEVERITIES = new Set(["low", "medium", "high"]);
const STATUSES = new Set(["open", "done", "wontfix"]);

export interface DebtConfig {
  register: string;
  baseline: string;
  marker_dirs: string[];
  quality_dirs: string[];
  markers: string[];
  max_open: number;
  max_open_high: number;
  limits: Limits;
}

export type DebtItem = Record<string, unknown>;

export const loadDebtConfig = (root: string): DebtConfig =>
  loadYaml<DebtConfig>(path.join(root, CONFIG_PATH));

export function loadRegister(root: string, config: DebtConfig): DebtItem[] {
  const file = path.join(root, config.register);
  if (!existsSync(file)) return [];
  const raw = loadYaml<{ items?: DebtItem[] } | null>(file);
  return raw?.items ?? [];
}

const bad = (config: DebtConfig, id: string, message: string, fix: string) =>
  violation("DEBT010", config.register, 0, `${id}: ${message}`, fix);

function itemProblems(
  config: DebtConfig,
  item: DebtItem,
  seen: ReadonlySet<string>,
  now: string,
): Violation[] {
  const id = String(item.id ?? "?");
  const missing = REQUIRED.filter(
    (field) => item[field] === undefined || item[field] === null || item[field] === "",
  );
  if (missing.length > 0)
    return [
      bad(config, id, `missing ${missing.join(", ")}`, `Fill every field: ${REQUIRED.join(", ")}.`),
    ];
  const out: Violation[] = [];
  if (!ID_RE.test(id) || seen.has(id))
    out.push(bad(config, id, "id must be a unique TD-NNNN", "Use the next free TD-NNNN."));
  if (!SEVERITIES.has(String(item.severity)) || !STATUSES.has(String(item.status))) {
    out.push(
      bad(
        config,
        id,
        "bad severity or status",
        "severity: low|medium|high, status: open|done|wontfix.",
      ),
    );
  }
  return [...out, ...lifecycleProblems(config, id, item, now)];
}

function lifecycleProblems(
  config: DebtConfig,
  id: string,
  item: DebtItem,
  now: string,
): Violation[] {
  if (!isIsoDate(item.due) || !isIsoDate(item.created))
    return [bad(config, id, "created/due must be YYYY-MM-DD", "Use YYYY-MM-DD.")];
  if (item.status !== "open") {
    return item.resolution
      ? []
      : [
          bad(
            config,
            id,
            "closed without a resolution",
            "Say how it was resolved, or why it will not be.",
          ),
        ];
  }
  if (String(item.due) >= now) return [];
  return [
    violation(
      "DEBT011",
      config.register,
      0,
      `${id} '${String(item.title)}' was due ${String(item.due)}`,
      "Pay it down now. Extend `due` only with a note in `cost` explaining what changed.",
    ),
  ];
}

export function checkRegister(
  config: DebtConfig,
  items: readonly DebtItem[],
  now: string,
): Violation[] {
  const out: Violation[] = [];
  const seen = new Set<string>();
  const open: DebtItem[] = [];
  for (const item of items) {
    const problems = itemProblems(config, item, seen, now);
    out.push(...problems);
    seen.add(String(item.id ?? "?"));
    if (item.status === "open" && !problems.some((p) => p.code === "DEBT010")) open.push(item);
  }
  const high = open.filter((i) => i.severity === "high").length;
  if (open.length > config.max_open || high > config.max_open_high) {
    out.push(
      violation(
        "DEBT012",
        config.register,
        0,
        `${open.length} open items (${high} high); budget is ${config.max_open} (${config.max_open_high} high)`,
        "Resolve existing debt before adding more. Debt is paid continuously, not in bursts.",
      ),
    );
  }
  return out;
}

function referenceProblem(
  file: string,
  line: number,
  id: string,
  status: ReadonlyMap<string, string>,
): Violation[] {
  const state = status.get(id);
  if (state === undefined)
    return [
      violation(
        "DEBT002",
        file,
        line,
        `${id} is not in the register`,
        "Add it to the register or fix the id.",
      ),
    ];
  if (state !== "open")
    return [
      violation(
        "DEBT013",
        file,
        line,
        `${id} is ${state} but code still references it`,
        "Delete the marker or waiver, or reopen the item.",
      ),
    ];
  return [];
}

export function checkMarkers(
  root: string,
  config: DebtConfig,
  items: readonly DebtItem[],
): Violation[] {
  const status = new Map(items.map((i) => [String(i.id), String(i.status)]));
  const markerRe = new RegExp(`\\b(${config.markers.join("|")})\\b(\\((TD-\\d{4})\\))?`, "g");
  const out: Violation[] = [];
  for (const file of listFiles(root, config.marker_dirs, isSourceFile)) {
    for (const comment of comments(file, readFileSync(path.join(root, file), "utf8"))) {
      for (const match of comment.text.matchAll(markerRe)) {
        const id = match[3];
        if (id === undefined) {
          out.push(
            violation(
              "DEBT001",
              file,
              comment.line,
              `${match[1] as string} comment is not tracked`,
              `Add an entry to ${config.register} and write ${match[1] as string}(TD-NNNN), or fix it now.`,
            ),
          );
        } else {
          out.push(...referenceProblem(file, comment.line, id, status));
        }
      }
      const waiver = WAIVER_RE.exec(comment.text);
      if (waiver) out.push(...referenceProblem(file, comment.line, waiver[1] as string, status));
    }
  }
  return out;
}

export function loadBaseline(root: string, config: DebtConfig): Record<string, number> {
  const file = path.join(root, config.baseline);
  return existsSync(file) ? (JSON.parse(readFileSync(file, "utf8")) as Record<string, number>) : {};
}

function qualityFiles(root: string, config: DebtConfig): string[] {
  return listFiles(root, config.quality_dirs, isSourceFile).filter((f) => !isTestFile(f));
}

export function currentMeasures(root: string, config: DebtConfig): Measure[] {
  return qualityFiles(root, config).flatMap((file) =>
    measureFile(file, readFileSync(path.join(root, file), "utf8"), config.limits),
  );
}

/** A waiver on the reported line, or on line 1 for the whole file. */
function waiverFor(lines: readonly string[], line: number): [number, string] | undefined {
  for (const candidate of [line, 1]) {
    const match = WAIVER_RE.exec(lines[candidate - 1] ?? "");
    if (match) return [candidate, match[1] as string];
  }
  return undefined;
}

export function checkQuality(
  root: string,
  config: DebtConfig,
  items: readonly DebtItem[],
): Violation[] {
  const status = new Map(items.map((i) => [String(i.id), String(i.status)]));
  const baseline = loadBaseline(root, config);
  const out: Violation[] = [];
  const used = new Set<string>();
  const current = new Map<string, number>();
  for (const measure of currentMeasures(root, config)) {
    current.set(measure.key, measure.value);
    const allowed = baseline[measure.key];
    if (allowed !== undefined) {
      if (measure.value > allowed)
        out.push(
          violation(
            "DEBT031",
            measure.path,
            measure.line,
            `${measure.what} grew ${allowed} -> ${measure.value}`,
            "Baselined debt may only shrink. Refactor back below the baseline.",
          ),
        );
      continue;
    }
    const lines = readFileSync(path.join(root, measure.path), "utf8").split("\n");
    const waiver = waiverFor(lines, measure.line);
    if (waiver && status.get(waiver[1]) === "open") {
      used.add(`${measure.path}:${waiver[0]}`);
      continue;
    }
    out.push(
      violation(
        measure.code,
        measure.path,
        measure.line,
        `${measure.what} is ${measure.value} (limit ${measure.limit})`,
        "Split it up. If it cannot be fixed now, register debt and add `// debt-ok: TD-NNNN` on that line.",
      ),
    );
  }
  for (const key of Object.keys(baseline)) {
    if (!current.has(key))
      out.push(
        violation(
          "DEBT030",
          config.baseline,
          0,
          `baseline entry '${key}' no longer violates`,
          "Run: pnpm ratchet (removes fixed entries).",
        ),
      );
  }
  return [...out, ...unusedWaivers(root, config, status, used)];
}

function unusedWaivers(
  root: string,
  config: DebtConfig,
  status: ReadonlyMap<string, string>,
  used: ReadonlySet<string>,
): Violation[] {
  const out: Violation[] = [];
  for (const file of qualityFiles(root, config)) {
    for (const comment of comments(file, readFileSync(path.join(root, file), "utf8"))) {
      const waiver = WAIVER_RE.exec(comment.text);
      // Waivers citing closed or unknown items are already reported by checkMarkers.
      if (
        waiver &&
        status.get(waiver[1] as string) === "open" &&
        !used.has(`${file}:${comment.line}`)
      ) {
        out.push(
          violation(
            "DEBT032",
            file,
            comment.line,
            "debt-ok waiver but nothing needs it",
            "Delete the waiver (and close the debt item if it is resolved).",
          ),
        );
      }
    }
  }
  return out;
}

/** Against `base`, the baseline may only lose entries or lower values. */
export function checkBaselineAgainst(root: string, config: DebtConfig, base: string): Violation[] {
  const previous = git(root, "show", `${base}:${config.baseline}`);
  if (previous === null) return [];
  const old = JSON.parse(previous) as Record<string, number>;
  return Object.entries(loadBaseline(root, config))
    .filter(([key, value]) => old[key] === undefined || value > (old[key] as number))
    .map(([key, value]) =>
      violation(
        "DEBT033",
        config.baseline,
        0,
        `baseline entry '${key}' was added or raised (was ${old[key] ?? "absent"}, now ${value})`,
        "Never grow the baseline. Fix the code, or register debt and use a waiver.",
      ),
    );
}

export function run(root: string, now: string = today(), base?: string): Violation[] {
  const config = loadDebtConfig(root);
  const items = loadRegister(root, config);
  return [
    ...checkRegister(config, items, now),
    ...checkMarkers(root, config, items),
    ...checkQuality(root, config, items),
    ...(base ? checkBaselineAgainst(root, config, base) : []),
  ];
}

/** Drop fixed entries and lower values. Never adds or raises an entry. Returns removed keys. */
export function ratchet(root: string): string[] {
  const config = loadDebtConfig(root);
  const baseline = loadBaseline(root, config);
  const current = new Map(currentMeasures(root, config).map((m) => [m.key, m.value]));
  const updated: Record<string, number> = {};
  for (const [key, value] of Object.entries(baseline).sort()) {
    const now = current.get(key);
    if (now !== undefined) updated[key] = Math.min(value, now);
  }
  writeFileSync(path.join(root, config.baseline), `${JSON.stringify(updated, null, 2)}\n`);
  return Object.keys(baseline)
    .filter((key) => !(key in updated))
    .sort();
}

export function main(argv: readonly string[], out: (line: string) => void = console.log): number {
  const { positional, flags } = parseArgs(argv);
  const root = findRoot(flag(flags, "root"));
  if (positional[0] === "ratchet") {
    const removed = ratchet(root);
    out(
      `ratchet: removed ${removed.length} fixed baseline ${removed.length === 1 ? "entry" : "entries"}`,
    );
    return 0;
  }
  return finish("debt", run(root, today(), flag(flags, "base")), out);
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
