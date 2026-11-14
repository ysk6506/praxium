import { spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  utimesSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { openScratch, SCRATCH_PARENT, STALE_AFTER_MS, sweepStale } from "./scratch";
import { miniRepo } from "./test-support/mini-repo";

const DAY = STALE_AFTER_MS / 1000;

/** The process id of a process that has ended. */
function deadPid(): number {
  return spawnSync(process.execPath, ["-e", ""]).pid;
}

function runFolder(parent: string, pid: number, ageSeconds: number): string {
  const folder = path.join(parent, `run-${pid}-abc123`);
  mkdirSync(path.join(folder, "inner"), { recursive: true });
  writeFileSync(path.join(folder, "inner", "file.txt"), "x");
  const then = Date.now() / 1000 - ageSeconds;
  utimesSync(folder, then, then);
  return folder;
}

describe("the scratch folder of a run [REQ-024]", () => {
  test("a scratch is made below the parent and disposing removes all of it, twice is fine", () => {
    const base = mkdtempSync(path.join(tmpdir(), "scratch-base-"));
    const scratch = openScratch(base);
    expect(path.dirname(scratch.dir)).toBe(path.join(base, SCRATCH_PARENT));
    writeFileSync(path.join(scratch.dir, "a.txt"), "a");
    scratch.dispose();
    expect(existsSync(scratch.dir)).toBe(false);
    scratch.dispose();
    expect(readdirSync(path.join(base, SCRATCH_PARENT))).toEqual([]);
  });

  test("the sweep removes an old folder of a finished run and nothing else", () => {
    const parent = mkdtempSync(path.join(tmpdir(), "scratch-sweep-"));
    const old = runFolder(parent, deadPid(), 2 * DAY);
    const young = runFolder(parent, deadPid(), DAY / 2);
    const running = runFolder(parent, process.pid, 2 * DAY);
    const foreign = path.join(parent, "praxium-mini-old");
    mkdirSync(foreign);
    expect(sweepStale(parent, Date.now())).toEqual([path.basename(old)]);
    expect([old, young, running, foreign].map((f) => existsSync(f))).toEqual([
      false,
      true,
      true,
      true,
    ]);
  });

  test("a parent that does not exist has nothing to sweep", () => {
    expect(sweepStale(path.join(tmpdir(), "no-such-parent-folder"), Date.now())).toEqual([]);
  });

  test("opening a scratch sweeps what a killed run left", () => {
    const base = mkdtempSync(path.join(tmpdir(), "scratch-open-"));
    const parent = path.join(base, SCRATCH_PARENT);
    mkdirSync(parent);
    const old = runFolder(parent, deadPid(), 3 * DAY);
    const scratch = openScratch(base);
    expect(existsSync(old)).toBe(false);
    expect(existsSync(scratch.dir)).toBe(true);
    scratch.dispose();
  });

  test("a test file that uses the support code makes its folders below the scratch", () => {
    const root = miniRepo();
    expect(tmpdir().split(path.sep)).toContain(SCRATCH_PARENT);
    expect(root.startsWith(tmpdir())).toBe(true);
    expect(existsSync(path.join(root, "package.json"))).toBe(true);
  });
});

const SRC = path.resolve(__dirname);
const HOOK = path.join(SRC, "test-support", "scratch-hook.ts");

function sources(): string[] {
  return (readdirSync(SRC, { recursive: true }) as string[])
    .filter((name) => name.endsWith(".ts"))
    .map((name) => path.join(SRC, name));
}

function importsOf(file: string): string[] {
  const text = readFileSync(file, "utf8");
  const found = [...text.matchAll(/(?:from\s+|import\s+)"(\.[^"]*)"/g)];
  return found
    .map((m) => path.resolve(path.dirname(file), m[1] ?? ""))
    .map((base) => (existsSync(`${base}.ts`) ? `${base}.ts` : path.join(base, "index.ts")));
}

function reachesHook(file: string, seen = new Set<string>()): boolean {
  if (file === HOOK) return true;
  if (seen.has(file) || !existsSync(file)) return false;
  seen.add(file);
  return importsOf(file).some((next) => reachesHook(next, seen));
}

const MAKES_TEMP = /\bmkdtempSync\(|\bmkdtemp\(|\btmpdir\(\)/;

describe("nothing is made outside the scratch [REQ-024]", () => {
  test("a test file or support module that makes temporary folders imports the hook, directly or through another support module", () => {
    const own = new Set(["scratch.ts", "scratch.test.ts"]);
    const files = sources().filter((f) => {
      const isTestCode = f.endsWith(".test.ts") || f.includes(`${path.sep}test-support${path.sep}`);
      return isTestCode && !own.has(path.basename(f)) && MAKES_TEMP.test(readFileSync(f, "utf8"));
    });
    expect(files.length).toBeGreaterThan(3);
    expect(files.filter((f) => !reachesHook(f))).toEqual([]);
  });

  test("code of the checks gets its folders from the scratch, never from mkdtemp", () => {
    const own = new Set(["scratch.ts", "scratch.test.ts"]);
    const files = sources().filter((f) => {
      const isTestCode = f.endsWith(".test.ts") || f.includes(`${path.sep}test-support${path.sep}`);
      return !isTestCode && !own.has(path.basename(f)) && MAKES_TEMP.test(readFileSync(f, "utf8"));
    });
    expect(files).toEqual([]);
  });
});
