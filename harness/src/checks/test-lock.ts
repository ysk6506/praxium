/**
 * The test lock (ADR-0004): every test, helper and expected-data file is fingerprinted.
 * Existing entries cannot change or vanish without a sealed amendment ADR.
 *
 * TLOCK001 locked item changed   TLOCK002 locked item removed   TLOCK003 new item not locked
 * TLOCK004 amendment ADR missing/unsealed/does not list the item
 * TLOCK005 (with --base) the lock differs from the base ref without a new amendment ADR
 *
 * Go tests (ADR-0023) are units too, keyed `go:<package folder>::<function>`; the codes and the amendment path are the same.
 */
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { git, splitFrontMatter, type Violation, violation } from "../common";
import { bodyFingerprint } from "./doc-print";
import type { Unit } from "./test-units";

const ADR_DIR = "docs/design-docs/adr";
export const AMEND_HINT =
  "A failing test means the code is wrong, not the test: fix the code. Only if the requirement changed: " +
  "update docs/product-specs/requirements.md, write an ADR with amends_tests + test_change_reason, seal it, " +
  "then run: pnpm tsx harness/src/checks/testguard.ts lock --amend <adr-file>";

export interface LockEntry {
  kind: string;
  hash: string;
  asserts: number;
  literals: string[];
  precision: number[];
  skips: number;
  /** Requirement ids tagged on a Go test (absent on TypeScript entries). */
  reqs?: string[];
  added: string;
  amended_by: string | null;
}

export interface Lock {
  version: number;
  entries: Record<string, LockEntry>;
  removed: Record<string, string>;
}

export function loadLock(root: string, lockPath: string): Lock {
  const file = path.join(root, lockPath);
  if (!existsSync(file)) return { version: 1, entries: {}, removed: {} };
  const data = JSON.parse(readFileSync(file, "utf8")) as Partial<Lock>;
  return { version: data.version ?? 1, entries: data.entries ?? {}, removed: data.removed ?? {} };
}

export function saveLock(root: string, lockPath: string, lock: Lock): void {
  const sorted: Lock = {
    version: lock.version,
    entries: Object.fromEntries(
      Object.entries(lock.entries).sort(([a], [b]) => a.localeCompare(b)),
    ),
    removed: Object.fromEntries(
      Object.entries(lock.removed).sort(([a], [b]) => a.localeCompare(b)),
    ),
  };
  writeFileSync(path.join(root, lockPath), `${JSON.stringify(sorted, null, 1)}\n`);
}

export function entryFor(unit: Unit, now: string, amendedBy: string | null = null): LockEntry {
  return {
    kind: unit.kind,
    hash: unit.digest,
    asserts: unit.asserts,
    literals: [...unit.literals],
    precision: [...unit.precision],
    skips: unit.skips,
    ...(unit.reqs ? { reqs: [...unit.reqs] } : {}),
    added: now,
    amended_by: amendedBy,
  };
}

/** Human description of how a locked test changed, naming the weakening when there is one. */
export function describeChange(entry: LockEntry, unit: Unit): string {
  const notes: string[] = [];
  if (unit.kind === "test") {
    if (unit.asserts < entry.asserts) notes.push(`assertions ${entry.asserts} -> ${unit.asserts}`);
    if (unit.literals.join("\u0000") !== entry.literals.join("\u0000"))
      notes.push("expected values changed");
    const looser =
      unit.precision.some((digits, i) => digits < (entry.precision[i] ?? digits)) ||
      unit.precision.length < entry.precision.length;
    if (looser) notes.push("toBeCloseTo precision loosened");
    if (unit.skips > entry.skips) notes.push("skip/todo/fails added");
    if (unit.reqs && unit.reqs.join(",") !== (entry.reqs ?? []).join(","))
      notes.push("requirement tags changed");
  }
  return notes.join("; ") || "body changed";
}

function adrProblem(root: string, adrName: string, key: string): string | undefined {
  const file = path.join(root, ADR_DIR, adrName);
  if (!existsSync(file)) return `ADR ${adrName} does not exist`;
  const { data, body } = splitFrontMatter(readFileSync(file, "utf8"));
  if (data === null || data.decision_hash !== bodyFingerprint(body))
    return `ADR ${adrName} is not sealed (or was edited after sealing)`;
  if (!Array.isArray(data.amends_tests) || !data.amends_tests.includes(key))
    return `ADR ${adrName} does not list it in amends_tests`;
  if (String(data.test_change_reason ?? "").trim().length < 40)
    return `ADR ${adrName} has no real test_change_reason (min 40 chars)`;
  return undefined;
}

export function lockViolations(
  root: string,
  lockPath: string,
  units: ReadonlyMap<string, Unit>,
): Violation[] {
  const lock = loadLock(root, lockPath);
  const out: Violation[] = [];
  for (const [key, unit] of [...units].sort(([a], [b]) => a.localeCompare(b))) {
    const entry = lock.entries[key];
    if (!entry)
      out.push(
        violation(
          "TLOCK003",
          unit.path,
          unit.line,
          `${key} is not in the test lock`,
          "Run: pnpm lock-tests (appends new items only).",
        ),
      );
    else if (entry.hash !== unit.digest)
      out.push(
        violation(
          "TLOCK001",
          unit.path,
          unit.line,
          `locked item ${key} changed (${describeChange(entry, unit)})`,
          AMEND_HINT,
        ),
      );
  }
  for (const key of Object.keys(lock.entries)
    .filter((k) => !units.has(k))
    .sort()) {
    out.push(
      violation("TLOCK002", lockPath, 0, `locked item ${key} was removed or renamed`, AMEND_HINT),
    );
  }
  const claims: Array<[string, string]> = [
    ...Object.entries(lock.entries).flatMap(([key, e]) =>
      e.amended_by ? [[key, e.amended_by] as [string, string]] : [],
    ),
    ...Object.entries(lock.removed),
  ];
  for (const [key, adr] of claims.sort()) {
    const problem = adrProblem(root, adr, key);
    if (problem)
      out.push(
        violation(
          "TLOCK004",
          lockPath,
          0,
          `amendment of ${key}: ${problem}`,
          "Write and seal an ADR whose amends_tests lists this item.",
        ),
      );
  }
  return out;
}

/** Append new items; with `amend`, also accept the changes and removals that ADR lists. */
export function applyLock(
  root: string,
  lockPath: string,
  units: ReadonlyMap<string, Unit>,
  amend: string | undefined,
  now: string,
): string[] {
  const lock = loadLock(root, lockPath);
  let allowed = new Set<string>();
  if (amend) {
    const { data } = splitFrontMatter(readFileSync(path.join(root, ADR_DIR, amend), "utf8"));
    allowed = new Set(Array.isArray(data?.amends_tests) ? data.amends_tests.map(String) : []);
  }
  const refused: string[] = [];
  for (const [key, unit] of units) {
    const existing = lock.entries[key];
    if (!existing) lock.entries[key] = entryFor(unit, now);
    else if (existing.hash !== unit.digest) {
      if (allowed.has(key)) lock.entries[key] = entryFor(unit, now, amend ?? null);
      else refused.push(key);
    }
  }
  for (const key of Object.keys(lock.entries).filter((k) => !units.has(k))) {
    if (allowed.has(key) && amend) {
      lock.removed[key] = amend;
      delete lock.entries[key];
    } else {
      refused.push(key);
    }
  }
  if (refused.length === 0) saveLock(root, lockPath, lock);
  return refused.sort();
}

/** Compare with the lock at `base`: changed or removed entries need an amendment ADR new in this diff. */
export function baseViolations(root: string, lockPath: string, base: string): Violation[] {
  const previous = git(root, "show", `${base}:${lockPath}`);
  if (previous === null) return [];
  const old = JSON.parse(previous) as Lock;
  const lock = loadLock(root, lockPath);
  const out: Violation[] = [];
  for (const [key, entry] of Object.entries(old.entries ?? {}).sort()) {
    const now = lock.entries[key];
    if (now && now.hash === entry.hash) continue;
    const adr = now ? now.amended_by : lock.removed[key];
    const adrIsNew = adr
      ? git(root, "cat-file", "-e", `${base}:${ADR_DIR}/${adr}`) === null
      : false;
    if (!adrIsNew)
      out.push(
        violation(
          "TLOCK005",
          lockPath,
          0,
          `${key} differs from ${base} without an amendment ADR added in this change`,
          AMEND_HINT,
        ),
      );
  }
  return out;
}
