/**
 * Test integrity (REQ-023, ADR-0004): stop anyone from making tests pass by changing the tests.
 *
 * Defense in depth: static rules on test code (test-rules.ts), rules on the test configuration
 * (test-config.ts) and the test lock (test-lock.ts). The last step — someone editing harness/ itself —
 * is closed by protected paths plus code-owner review (ADR-0005).
 */
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  isMain,
  loadYaml,
  parseArgs,
  today,
  type Violation,
} from "../common";
import { configViolations, type TestConfigRules } from "./test-config";
import { AMEND_HINT, applyLock, baseViolations, lockViolations } from "./test-lock";
import { staticViolations } from "./test-rules";
import { currentUnits, testFiles, type UnitGlobs } from "./test-units";

const CONFIG_PATH = "harness/config/tests.yaml";

export interface TestsConfig extends UnitGlobs, TestConfigRules {
  readonly lock: string;
  readonly coverage_summary: string;
  readonly coverage_floor: string;
  readonly min_floor: number;
  readonly max_ignore_comments: number;
}

export const loadTestsConfig = (root: string): TestsConfig =>
  loadYaml<TestsConfig>(path.join(root, CONFIG_PATH));

export function run(root: string, base?: string): Violation[] {
  const config = loadTestsConfig(root);
  return [
    ...testFiles(root, config).flatMap((file) => staticViolations(root, file)),
    ...configViolations(root, config),
    ...lockViolations(root, config.lock, currentUnits(root, config)),
    ...(base ? baseViolations(root, config.lock, base) : []),
  ];
}

export function lock(root: string, amend?: string, now: string = today()): string[] {
  const config = loadTestsConfig(root);
  // Accept the ADR's file name or its repository path; the lock records the file name (ADR-0020).
  const adr = amend === undefined ? undefined : path.basename(amend);
  return applyLock(root, config.lock, currentUnits(root, config), adr, now);
}

export function main(argv: readonly string[], out: (line: string) => void = console.log): number {
  const { positional, flags } = parseArgs(argv);
  const root = findRoot(flag(flags, "root"));
  if (positional[0] === "lock") {
    const refused = lock(root, flag(flags, "amend"));
    for (const key of refused)
      out(`refused: ${key} changed or vanished and no amendment ADR lists it`);
    if (refused.length > 0) out(AMEND_HINT);
    return refused.length > 0 ? 1 : 0;
  }
  return finish("test-integrity", run(root, flag(flags, "base")), out);
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
