/**
 * Go tests and their coverage (REQ-023, ADR-0019). `go test` runs once with a coverage profile; coverage is
 * measured per unit, as statements, from the tests of the unit's own package.
 *
 * GOTEST001 `go test` failed (a test or the build)
 * GOTEST002 a unit has Go code but no test
 * The floors and their ratchet live in coverage.ts (GOCOV001, GOCOV002, and COV003 for a lowered floor).
 */
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { type Violation, violation } from "../common";
import { type GoConfig, goFiles, unitOfPath } from "./go-config";
import { funcsOf } from "./go-lex";
import { runTool, tail } from "./go-run";

export interface UnitCoverage {
  readonly percent: number;
  /** Statements measured: how much evidence the percentage rests on. */
  readonly measured: number;
}

const BLOCK = /^(.+?):\d+\.\d+,\d+\.\d+ (\d+) (\d+)$/;

/** Statements and covered statements per file of a coverage profile (paths as Go writes them). */
export function parseProfile(text: string): Map<string, [statements: number, covered: number]> {
  const files = new Map<string, [number, number]>();
  for (const line of text.split("\n")) {
    const match = BLOCK.exec(line);
    if (!match) continue;
    const entry = files.get(match[1] as string) ?? [0, 0];
    entry[0] += Number(match[2]);
    if (Number(match[3]) > 0) entry[1] += Number(match[2]);
    files.set(match[1] as string, entry);
  }
  return files;
}

/** Coverage per unit from a profile; files outside every unit are ignored. */
export function unitCoverage(config: GoConfig, profile: string): Map<string, UnitCoverage> {
  const totals = new Map<string, [number, number]>();
  for (const [file, [statements, covered]] of parseProfile(profile)) {
    const prefix = `${config.module}/`;
    const repoPath = file.startsWith(prefix) ? `${config.root}/${file.slice(prefix.length)}` : file;
    const unit = unitOfPath(config, repoPath);
    if (!unit) continue;
    const bucket = totals.get(unit.name) ?? [0, 0];
    bucket[0] += statements;
    bucket[1] += covered;
    totals.set(unit.name, bucket);
  }
  return new Map(
    [...totals]
      .filter(([, [all]]) => all > 0)
      .map(([name, [all, hit]]) => [name, { percent: (100 * hit) / all, measured: all }]),
  );
}

export interface GoTestRun {
  readonly violations: Violation[];
  readonly units: Map<string, UnitCoverage>;
}

/** Run every Go test once and read the coverage profile. A failing test is GOTEST001 and yields no coverage. */
export function runTests(root: string, config: GoConfig): GoTestRun {
  const cwd = path.join(root, config.root);
  const dir = mkdtempSync(path.join(tmpdir(), "praxium-gocover-"));
  try {
    const profile = path.join(dir, "cover.out");
    const result = runTool(cwd, "go", ["test", "-count=1", `-coverprofile=${profile}`, "./..."]);
    if (result.missing) {
      return {
        violations: [
          violation(
            "GOTOOL000",
            config.root,
            0,
            "the Go toolchain is not installed",
            "Install Go (the version in go.mod) and put it on the PATH.",
          ),
        ],
        units: new Map(),
      };
    }
    if (!result.ok) {
      const message = `go test failed:\n${tail(`${result.stdout}\n${result.stderr}`, 15)}`;
      return {
        violations: [
          violation(
            "GOTEST001",
            config.root,
            0,
            message,
            "A failing test means the code is wrong, not the test: fix the code.",
          ),
        ],
        units: new Map(),
      };
    }
    return { violations: [], units: unitCoverage(config, readFileSync(profile, "utf8")) };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

/** A unit whose code has functions needs a test file with at least one `Test` function in its own folder. */
export function checkPresence(root: string, config: GoConfig): Violation[] {
  const files = goFiles(root, config);
  const out: Violation[] = [];
  for (const unit of config.units) {
    const own = files.filter((f) => path.posix.dirname(f.path) === unit.path);
    const hasCode = own.some((f) => !f.isTest && funcsOf(f.code).length > 0);
    const hasTests = own.some(
      (f) =>
        f.isTest &&
        funcsOf(f.code).some((fn) => /^Test[A-Z0-9_]/.test(fn.name) || fn.name === "Test"),
    );
    if (hasCode && !hasTests) {
      out.push(
        violation(
          "GOTEST002",
          unit.path,
          0,
          `unit '${unit.name}' has code but no test`,
          "Add a *_test.go file with a table-driven Test function in the unit's folder.",
        ),
      );
    }
  }
  return out;
}
