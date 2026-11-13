/**
 * Coverage ratchet (REQ-023, ADR-0004). Coverage is measured per architecture unit; each unit has a
 * floor in harness/config/coverage-floor.json and floors only go up.
 *
 * COV001 unit below its floor   COV002 unit without a valid floor   COV003 (with --base) floor lowered
 * COV004 coverage summary missing   COV005 coverage-ignore comment over budget or without a reason
 * COV006 floor more than MAX_FLOOR_LAG points below what the ratchet would set (ADR-0028)
 *
 * Go code (ADR-0019) is measured per Go unit as statements by `go test`, and its floors sit in the same file under
 * the key `go:<unit>`. GOCOV001 Go unit below its floor   GOCOV002 Go unit without a valid floor
 * GOCOV003 Go floor lagging behind the measured coverage (a lowered Go floor is COV003, a failing Go test is GOTEST001)
 */
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  git,
  isMain,
  parseArgs,
  rel,
  type Violation,
  violation,
} from "../common";
import { isSourceFile, isTestFile, listFiles } from "../files";
import { comments } from "../ts-ast";
import { loadArchConfig, unitOf } from "./arch-model";
import { loadGoConfig } from "./go-config";
import { runTests } from "./go-cover";
import { loadTestsConfig, type TestsConfig } from "./testguard";

interface Counter {
  total: number;
  covered: number;
}
type Summary = Record<string, { lines: Counter; branches: Counter }>;

const IGNORE_RE = /\b(v8|c8|istanbul)\s+ignore\b/;

export interface UnitCoverage {
  readonly percent: number;
  /** Lines + branches measured: how much evidence the percentage rests on. */
  readonly measured: number;
}

/** Units with less measured code than this keep the minimum floor (too little evidence to ratchet). */
export const MIN_MEASURED_FOR_RATCHET = 15;
/** Floors never go above this, leaving room for a defensive branch or two (ADR-0014). */
export const MAX_FLOOR = 98;
/**
 * A floor may sit this many points under what `pnpm ratchet` would set for the unit. Past that the floor lags: coverage
 * was raised and the ratchet was not run, so a later drop back down would still pass (ADR-0028).
 */
export const MAX_FLOOR_LAG = 1;

/** Combined line + branch percentage per unit. */
export function perUnit(root: string, summary: Summary): Map<string, number> {
  return new Map([...coverageByUnit(root, summary)].map(([unit, c]) => [unit, c.percent]));
}

/** Combined line + branch coverage per unit, with the amount of evidence behind it. */
export function coverageByUnit(root: string, summary: Summary): Map<string, UnitCoverage> {
  const config = loadArchConfig(root);
  const totals = new Map<string, [number, number]>();
  for (const [file, data] of Object.entries(summary)) {
    if (file === "total") continue;
    const unit = unitOf(config, path.isAbsolute(file) ? rel(root, file) : file);
    if (!unit) continue;
    const bucket = totals.get(unit.name) ?? [0, 0];
    bucket[0] += data.lines.covered + data.branches.covered;
    bucket[1] += data.lines.total + data.branches.total;
    totals.set(unit.name, bucket);
  }
  return new Map(
    [...totals].map(([unit, [hit, all]]) => [
      unit,
      { percent: all === 0 ? 100 : (100 * hit) / all, measured: all },
    ]),
  );
}

export function loadFloors(root: string, config: TestsConfig): Record<string, number> {
  const file = path.join(root, config.coverage_floor);
  return existsSync(file) ? (JSON.parse(readFileSync(file, "utf8")) as Record<string, number>) : {};
}

function loadSummary(root: string, config: TestsConfig): Summary | undefined {
  const file = path.join(root, config.coverage_summary);
  return existsSync(file) ? (JSON.parse(readFileSync(file, "utf8")) as Summary) : undefined;
}

export function checkSummary(root: string, config: TestsConfig): Violation[] {
  const summary = loadSummary(root, config);
  if (!summary)
    return [
      violation(
        "COV004",
        config.coverage_summary,
        0,
        "coverage summary not found",
        "Run `pnpm test` (vitest run --coverage) before this check.",
      ),
    ];
  const floors = loadFloors(root, config);
  const out: Violation[] = [];
  const measured = coverageByUnit(root, summary);
  for (const [unit, percent] of [...perUnit(root, summary)].sort()) {
    const floor = floors[unit];
    if (floor === undefined || floor < config.min_floor) {
      out.push(
        violation(
          "COV002",
          config.coverage_floor,
          0,
          `${unit} has no valid floor (needs >= ${config.min_floor})`,
          "Run `pnpm ratchet` to set floors from current coverage.",
        ),
      );
    } else if (percent + 1e-9 < floor) {
      out.push(
        violation(
          "COV001",
          unit,
          0,
          `${unit} coverage ${percent.toFixed(1)}% is below its floor ${floor.toFixed(1)}%`,
          "Add tests for the uncovered code (see coverage/); never lower the floor.",
        ),
      );
    } else if (isLagging(floor, measured.get(unit), config.min_floor)) {
      out.push(
        violation(
          "COV006",
          config.coverage_floor,
          0,
          `${unit} coverage is ${percent.toFixed(1)}% but its floor is only ${floor.toFixed(1)}%`,
          "Raise the floor with `pnpm ratchet`: a floor that lags lets coverage fall back unnoticed.",
        ),
      );
    }
  }
  return out;
}

/** Whether a floor is more than MAX_FLOOR_LAG under the floor the ratchet would set. */
function isLagging(floor: number, coverage: UnitCoverage | undefined, minFloor: number): boolean {
  return (
    coverage !== undefined && floor + MAX_FLOOR_LAG + 1e-9 < candidateFloor(coverage, minFloor)
  );
}

/** The floor a unit may be raised to: its coverage rounded down to 0.5, kept apart from the minimum when thin. */
function candidateFloor(coverage: UnitCoverage, minFloor: number): number {
  if (coverage.measured < MIN_MEASURED_FOR_RATCHET) return minFloor;
  return Math.min(Math.floor(coverage.percent * 2) / 2, MAX_FLOOR);
}

/** Go units: run the tests once, then compare each unit with its floor `go:<unit>`. */
export function checkGoCoverage(root: string, config: TestsConfig): Violation[] {
  const goConfig = loadGoConfig(root);
  if (!goConfig) return [];
  const { violations, units } = runTests(root, goConfig);
  const floors = loadFloors(root, config);
  const out = [...violations];
  for (const [unit, { percent }] of [...units].sort()) {
    const floor = floors[`go:${unit}`];
    if (floor === undefined || floor < config.min_floor) {
      out.push(
        violation(
          "GOCOV002",
          config.coverage_floor,
          0,
          `Go unit ${unit} has no valid floor go:${unit} (needs >= ${config.min_floor})`,
          "Run `pnpm ratchet` to set floors from current coverage.",
        ),
      );
    } else if (percent + 1e-9 < floor) {
      out.push(
        violation(
          "GOCOV001",
          unit,
          0,
          `Go unit ${unit} coverage ${percent.toFixed(1)}% is below its floor ${floor.toFixed(1)}%`,
          "Add tests for the uncovered code (go test -cover ./... in backend); never lower the floor.",
        ),
      );
    } else if (isLagging(floor, units.get(unit), config.min_floor)) {
      out.push(
        violation(
          "GOCOV003",
          config.coverage_floor,
          0,
          `Go unit ${unit} coverage is ${percent.toFixed(1)}% but its floor go:${unit} is only ${floor.toFixed(1)}%`,
          "Raise the floor with `pnpm ratchet`: a floor that lags lets coverage fall back unnoticed.",
        ),
      );
    }
  }
  return out;
}

export function checkIgnoreComments(root: string, config: TestsConfig): Violation[] {
  const out: Violation[] = [];
  let count = 0;
  for (const file of listFiles(root, ["packages", "apps"], isSourceFile).filter(
    (f) => !isTestFile(f),
  )) {
    for (const comment of comments(file, readFileSync(path.join(root, file), "utf8"))) {
      if (!IGNORE_RE.test(comment.text)) continue;
      count++;
      const reason = comment.text.split("--")[1]?.replace(/\*\/$/, "").trim() ?? "";
      if (reason.replace("@preserve", "").trim().length < 10) {
        out.push(
          violation(
            "COV005",
            file,
            comment.line,
            "coverage-ignore comment without a reason",
            "Write /* v8 ignore next -- <why this cannot be tested> */.",
          ),
        );
      }
    }
  }
  if (count > config.max_ignore_comments) {
    out.push(
      violation(
        "COV005",
        "packages",
        0,
        `${count} coverage-ignore comments (budget ${config.max_ignore_comments})`,
        "Test the code instead of excluding it.",
      ),
    );
  }
  return out;
}

export function checkFloorsAgainst(root: string, config: TestsConfig, base: string): Violation[] {
  const previous = git(root, "show", `${base}:${config.coverage_floor}`);
  if (previous === null) return [];
  const old = JSON.parse(previous) as Record<string, number>;
  const now = loadFloors(root, config);
  return Object.entries(old)
    .filter(([unit, value]) => now[unit] === undefined || (now[unit] as number) + 1e-9 < value)
    .map(([unit, value]) =>
      violation(
        "COV003",
        config.coverage_floor,
        0,
        `floor for ${unit} went ${value} -> ${now[unit] ?? "removed"}`,
        "Floors only go up. Restore it and add tests.",
      ),
    );
}

/** Raise each floor to current coverage (rounded down to 0.5). Never lowers a floor. */
export function ratchet(root: string): Record<string, number> {
  const config = loadTestsConfig(root);
  const summary = loadSummary(root, config);
  if (!summary) throw new Error("coverage summary missing: run `pnpm test` first");
  const floors = loadFloors(root, config);
  for (const [unit, { percent, measured }] of coverageByUnit(root, summary)) {
    const candidate = candidateFloor({ percent, measured }, config.min_floor);
    floors[unit] = Math.max(floors[unit] ?? config.min_floor, candidate, config.min_floor);
  }
  const goConfig = loadGoConfig(root);
  for (const [unit, coverage] of goConfig ? runTests(root, goConfig).units : []) {
    const key = `go:${unit}`;
    const candidate = candidateFloor(coverage, config.min_floor);
    floors[key] = Math.max(floors[key] ?? config.min_floor, candidate, config.min_floor);
  }
  const sorted = Object.fromEntries(Object.entries(floors).sort(([a], [b]) => a.localeCompare(b)));
  writeFileSync(path.join(root, config.coverage_floor), `${JSON.stringify(sorted, null, 2)}\n`);
  return sorted;
}

export function run(root: string, base?: string): Violation[] {
  const config = loadTestsConfig(root);
  return [
    ...checkSummary(root, config),
    ...checkIgnoreComments(root, config),
    ...(base ? checkFloorsAgainst(root, config, base) : []),
    ...checkGoCoverage(root, config),
  ];
}

export function main(argv: readonly string[], out: (line: string) => void = console.log): number {
  const { positional, flags } = parseArgs(argv);
  const root = findRoot(flag(flags, "root"));
  if (positional[0] === "ratchet") {
    for (const [unit, floor] of Object.entries(ratchet(root))) out(`${unit}: ${floor.toFixed(1)}%`);
    return 0;
  }
  return finish("coverage", run(root, flag(flags, "base")), out);
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
