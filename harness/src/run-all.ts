/** Run every structural check and print one verdict (`pnpm structure`, part of `pnpm check`). */
import { run as architecture } from "./checks/architecture";
import { run as coverage } from "./checks/coverage";
import { run as debt } from "./checks/debt";
import { report as docsDiff } from "./checks/doc-diff";
import { run as docs } from "./checks/docs";
import * as go from "./checks/go";
import { report as ignores } from "./checks/ignores";
import { report as licenses } from "./checks/licenses";
import { run as protectedPaths } from "./checks/protected";
import { report as quality } from "./checks/quality";
import { run as testguard } from "./checks/testguard";
import { report as traceability } from "./checks/traceability";
import { run as uiWords } from "./checks/ui-words";
import { findRoot, finish, flag, isMain, parseArgs, today, type Violation } from "./common";

export type Check = readonly [name: string, run: () => Violation[]];

export function checks(
  root: string,
  base: string | undefined,
  withCoverage: boolean,
  notes: string[] = [],
): Check[] {
  const plan: Check[] = [
    [
      "architecture",
      () => {
        // License rules ride on this check: common.test.ts is locked and lists the check names (ADR-0185).
        const { violations, notes: lic } = licenses(root);
        notes.push(...lic);
        return [...architecture(root), ...violations, ...go.architecture(root)];
      },
    ],
    [
      "docs",
      () => {
        const { violations, notes: docNotes } = docsDiff(root, base);
        notes.push(...docNotes);
        return [...docs(root), ...violations];
      },
    ],
    ["debt", () => [...debt(root, today(), base), ...go.debtOf(root)]],
    [
      "ignores",
      () => {
        const { violations, notes: lowered } = ignores(root, base);
        notes.push(...lowered);
        return [...violations, ...go.ignoresOf(root)];
      },
    ],
    ["test-integrity", () => [...testguard(root, base), ...go.testIntegrity(root)]],
    [
      "traceability",
      () => {
        const { violations, planned } = traceability(root);
        notes.push(...planned);
        return violations;
      },
    ],
    ["ui-words", () => uiWords(root)],
    [
      "quality",
      () => {
        const { violations, notes: stale } = quality(root);
        notes.push(...stale);
        return violations;
      },
    ],
    ["protected-paths", () => protectedPaths(root, base)],
  ];
  if (withCoverage) plan.push(["coverage", () => coverage(root, base)]);
  return plan;
}

export function main(argv: readonly string[], out: (line: string) => void = console.log): number {
  const { flags } = parseArgs(argv);
  const root = findRoot(flag(flags, "root"));
  let failed = 0;
  const notes: string[] = [];
  const timing: string[] = [];
  for (const [name, run] of checks(root, flag(flags, "base"), flags.has("with-coverage"), notes)) {
    const started = performance.now();
    failed += finish(name, run(), out);
    timing.push(`${name.padEnd(16)} ${((performance.now() - started) / 1000).toFixed(2)}s`);
  }
  for (const line of notes) out(line);
  if (flags.has("timing")) for (const line of timing) out(`TIME ${line}`);
  out(failed === 0 ? "\nALL CHECKS PASSED" : `\n${failed} CHECK(S) FAILED`);
  return failed === 0 ? 0 : 1;
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
