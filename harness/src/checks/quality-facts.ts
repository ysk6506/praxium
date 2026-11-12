/** What the quality table is built from: one measured row per architecture unit (ADR-0148). No dates of today. */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { splitFrontMatter } from "../common";
import { isSourceFile, isTestFile, listFiles } from "../files";
import { comments } from "../ts-ast";
import { loadArchConfig, unitOf } from "./arch-model";
import { loadFloors } from "./coverage";
import { type DebtItem, loadDebtConfig, loadRegister } from "./debt";
import { type FunctionStat, functionStats, type Limits, lineCount } from "./debt-measure";
import { loadDocsConfig } from "./doc-config";
import { latestDate, readDocRecords } from "./doc-records";
import { DocFiles } from "./doc-targets";
import type { Suppression } from "./ignores-scan";
import { loadTestsConfig } from "./testguard";

export const NO_UNIT = "(단위 밖)";
export const RECORD_FILE = "docs/generated/coverage-record.json";
/** A file or function counts as close to a limit above this share of it. */
export const NEAR_SHARE = 0.9;

export interface UnitFacts {
  readonly unit: string;
  /** The unit's coverage floor; undefined when its coverage is not measured. */
  readonly floor: number | undefined;
  /** Last recorded coverage (combined line + branch percent), undefined when nothing was recorded. */
  readonly percent: number | undefined;
  readonly ignores: number;
  /** Files and functions above 90% of a limit, the ones over a limit included. */
  readonly near: number;
  readonly over: number;
  readonly debtOpen: number;
  readonly debtHigh: number;
  readonly earliestDue: string | undefined;
  readonly oldestDoc: { readonly date: string; readonly file: string } | undefined;
}

export interface CoverageRecord {
  readonly measuredOn: string;
  readonly units: Readonly<Record<string, number>>;
}

/** The recorded coverage, undefined when none is committed. Throws on a malformed file. */
export function readRecord(root: string): CoverageRecord | undefined {
  const file = path.join(root, RECORD_FILE);
  if (!existsSync(file)) return undefined;
  const raw = JSON.parse(readFileSync(file, "utf8")) as Partial<CoverageRecord>;
  const units = raw.units;
  const ok =
    typeof raw.measuredOn === "string" &&
    typeof units === "object" &&
    units !== null &&
    Object.values(units).every((v) => typeof v === "number" && Number.isFinite(v));
  if (!ok)
    throw new Error(
      `${RECORD_FILE} must look like {"measuredOn": "YYYY-MM-DD", "units": {"core.world": 97.4}}`,
    );
  return raw as CoverageRecord;
}

interface Counts {
  near: number;
  over: number;
}

function isOver(fn: FunctionStat, limits: Limits): boolean {
  return (
    fn.lines > limits.max_function_lines ||
    fn.complexity > limits.max_complexity ||
    fn.params > limits.max_params
  );
}

function isNear(fn: FunctionStat, limits: Limits): boolean {
  return (
    fn.lines > limits.max_function_lines * NEAR_SHARE ||
    fn.complexity > limits.max_complexity * NEAR_SHARE
  );
}

/** How many files and functions of one file are close to or over a limit. */
function sizeOf(file: string, text: string, limits: Limits): Counts {
  const lines = lineCount(text);
  const found: Counts = {
    near: lines > limits.max_file_lines * NEAR_SHARE ? 1 : 0,
    over: lines > limits.max_file_lines ? 1 : 0,
  };
  for (const fn of functionStats(file, text)) {
    const over = isOver(fn, limits);
    if (over || isNear(fn, limits)) found.near++;
    if (over) found.over++;
  }
  return found;
}

function sizeFacts(root: string): Map<string, Counts> {
  const debt = loadDebtConfig(root);
  const arch = loadArchConfig(root);
  const out = new Map<string, Counts>();
  const files = listFiles(root, debt.quality_dirs, isSourceFile).filter((f) => !isTestFile(f));
  for (const file of files) {
    const unit = unitOf(arch, file)?.name ?? NO_UNIT;
    const found = sizeOf(file, readFileSync(path.join(root, file), "utf8"), debt.limits);
    const bucket = out.get(unit) ?? { near: 0, over: 0 };
    out.set(unit, { near: bucket.near + found.near, over: bucket.over + found.over });
  }
  return out;
}

/** Which units each open debt item touches: the units it names, and the files whose comments cite it. */
export function debtUnits(root: string, items: readonly DebtItem[]): Map<string, Set<string>> {
  const arch = loadArchConfig(root);
  const debt = loadDebtConfig(root);
  const byItem = new Map<string, Set<string>>();
  const add = (id: string, unit: string): void => {
    byItem.set(id, (byItem.get(id) ?? new Set<string>()).add(unit));
  };
  for (const item of items) {
    for (const unit of Array.isArray(item.units) ? item.units : [])
      add(String(item.id), String(unit));
  }
  const dirs = [...new Set([...debt.marker_dirs, "scripts"])];
  for (const file of listFiles(root, dirs, isSourceFile)) {
    const unit = unitOf(arch, file)?.name ?? NO_UNIT;
    for (const comment of comments(file, readFileSync(path.join(root, file), "utf8")))
      for (const id of comment.text.match(/TD-\d{4}/g) ?? []) add(id, unit);
  }
  return byItem;
}

function debtFacts(
  root: string,
): Map<string, { open: number; high: number; due: string | undefined }> {
  const items = loadRegister(root, loadDebtConfig(root)).filter((i) => i.status === "open");
  const placed = debtUnits(root, items);
  const out = new Map<string, { open: number; high: number; due: string | undefined }>();
  for (const item of items) {
    const units = placed.get(String(item.id)) ?? new Set([NO_UNIT]);
    for (const unit of units) {
      const row = out.get(unit) ?? { open: 0, high: 0, due: undefined };
      row.open++;
      if (item.severity === "high") row.high++;
      const due = String(item.due);
      if (row.due === undefined || due < row.due) row.due = due;
      out.set(unit, row);
    }
  }
  return out;
}

/** Oldest verification date (newest record of a doc, ADR-0194) among the active guides, references, specs and plans that cover code of a unit. */
function docFacts(root: string): Map<string, { date: string; file: string }> {
  const config = loadDocsConfig(root);
  const arch = loadArchConfig(root);
  const files = new DocFiles(root);
  const docs = [
    ...config.root_docs,
    ...listFiles(root, [config.docs_root], (n) => n.endsWith(".md")).filter(
      (f) => !f.startsWith(`${config.generated_dir}/`) && !f.startsWith(`${config.archive_dir}/`),
    ),
  ].filter((f) => existsSync(path.join(root, f)));
  const oldest = new Map<string, { date: string; file: string }>();
  for (const file of docs) {
    const { data } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
    if (!data || data.kind === "decision" || data.status !== "active") continue;
    const covers = Array.isArray(data.covers) ? data.covers.map(String) : [];
    const date = latestDate(readDocRecords(root, file).records.values());
    if (covers.length === 0 || date === null) continue;
    const covered = covers.flatMap((glob) => files.expand(glob));
    for (const unit of new Set(covered.map((f) => unitOf(arch, f)?.name ?? NO_UNIT))) {
      const known = oldest.get(unit);
      if (!known || date < known.date) oldest.set(unit, { date, file });
    }
  }
  return oldest;
}

/** One row per architecture unit, then the files that belong to no unit. */
export function gatherFacts(root: string, found: readonly Suppression[]): UnitFacts[] {
  const arch = loadArchConfig(root);
  const floors = loadFloors(root, loadTestsConfig(root));
  const record = readRecord(root);
  const sizes = sizeFacts(root);
  const debt = debtFacts(root);
  const docs = docFacts(root);
  const ignores = new Map<string, number>();
  for (const s of found) {
    const unit = unitOf(arch, s.file)?.name ?? NO_UNIT;
    ignores.set(unit, (ignores.get(unit) ?? 0) + 1);
  }
  return [...arch.units.map((u) => u.name), NO_UNIT].map((unit) => ({
    unit,
    floor: floors[unit],
    percent: record?.units[unit],
    ignores: ignores.get(unit) ?? 0,
    near: sizes.get(unit)?.near ?? 0,
    over: sizes.get(unit)?.over ?? 0,
    debtOpen: debt.get(unit)?.open ?? 0,
    debtHigh: debt.get(unit)?.high ?? 0,
    earliestDue: debt.get(unit)?.due,
    oldestDoc: docs.get(unit),
  }));
}
