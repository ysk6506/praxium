/**
 * One scratch folder per run, so nothing the harness or a test makes stays behind (ADR-0036).
 *
 * Every run makes `<tmp>/praxium-scratch/run-<pid>-<random>` and everything the tests create goes
 * below it. The run removes it when it ends. A run that was killed cannot remove its folder, so the
 * next run sweeps the folders that are more than a day old and whose process is gone. A folder of a
 * run that is still going, however old, is never touched.
 */
import { mkdirSync, mkdtempSync, readdirSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

export const SCRATCH_PARENT = "praxium-scratch";
export const STALE_AFTER_MS = 24 * 60 * 60 * 1000;

const RUN_FOLDER = /^run-(\d+)-[A-Za-z0-9]+$/;

export interface Scratch {
  /** The folder of this run. */
  readonly dir: string;
  /** Remove the folder and everything below it. Safe to call twice. */
  readonly dispose: () => void;
}

function isAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    // EPERM: the process exists but belongs to someone else.
    return (error as NodeJS.ErrnoException).code === "EPERM";
  }
}

/** Remove the run folders in `parent` that are old and whose process is gone. Returns their names. */
export function sweepStale(parent: string, nowMs: number, maxAgeMs = STALE_AFTER_MS): string[] {
  const removed: string[] = [];
  let names: string[];
  try {
    names = readdirSync(parent);
  } catch {
    return removed;
  }
  for (const name of names) {
    const match = RUN_FOLDER.exec(name);
    if (!match) continue;
    const folder = path.join(parent, name);
    try {
      if (nowMs - statSync(folder).mtimeMs < maxAgeMs || isAlive(Number(match[1]))) continue;
      rmSync(folder, { recursive: true, force: true });
      removed.push(name);
    } catch {
      // Another run removed it first, or it cannot be read: leave it to the next sweep.
    }
  }
  return removed;
}

/** Make the folder of this run below `base` and sweep what older runs left. */
export function openScratch(base: string = tmpdir(), nowMs: number = Date.now()): Scratch {
  const parent = path.join(base, SCRATCH_PARENT);
  mkdirSync(parent, { recursive: true });
  sweepStale(parent, nowMs);
  const dir = mkdtempSync(path.join(parent, `run-${process.pid}-`));
  return { dir, dispose: () => rmSync(dir, { recursive: true, force: true }) };
}
