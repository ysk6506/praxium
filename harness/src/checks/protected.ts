/**
 * Protected paths (REQ-021, REQ-023, ADR-0005): guardrails cannot be loosened quietly.
 *
 * PROT001 a protected path changed (vs --base) without an ADR, added in the same change, that lists
 *         it in touches_protected
 * PROT002 the ADR that authorizes the change is not sealed
 * PROT003 .github/CODEOWNERS does not cover every protected path
 */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  git,
  isMain,
  parseArgs,
  splitFrontMatter,
  type Violation,
  violation,
} from "../common";
import { matchesGlob } from "../files";
import { bodyFingerprint } from "./doc-print";

const PATTERNS = "harness/config/protected-paths.txt";
const CODEOWNERS = ".github/CODEOWNERS";
const ADR_DIR = "docs/design-docs/adr/";

/** Ratchet files change routinely in one safe direction; each has its own --base guard. */
export const RATCHET_FILES: Readonly<Record<string, string>> = {
  "harness/config/test-lock.json": "test-lock TLOCK005",
  "harness/config/debt-baseline.json": "debt DEBT033",
  "harness/config/coverage-floor.json": "coverage COV003",
  "harness/config/ux-floor.json": "ux UX003",
};

export function loadPatterns(root: string): string[] {
  return readFileSync(path.join(root, PATTERNS), "utf8")
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("#"));
}

export function changedFiles(root: string, base: string): Array<[string, string]> {
  const diff = (git(root, "diff", "--name-status", "--no-renames", base) ?? "")
    .split("\n")
    .filter((line) => line.includes("\t"))
    .map((line) => line.split("\t") as [string, string]);
  const untracked = (git(root, "ls-files", "--others", "--exclude-standard") ?? "")
    .split("\n")
    .filter(Boolean);
  return [...diff, ...untracked.map((file) => ["A", file] as [string, string])];
}

function authorizations(
  root: string,
  changes: ReadonlyArray<[string, string]>,
): { granted: Map<string, string>; problems: Violation[] } {
  const granted = new Map<string, string>();
  const problems: Violation[] = [];
  for (const [status, file] of changes) {
    if (
      status !== "A" ||
      !file.startsWith(ADR_DIR) ||
      !file.endsWith(".md") ||
      !existsSync(path.join(root, file))
    )
      continue;
    const { data, body } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
    const touches = Array.isArray(data?.touches_protected)
      ? data.touches_protected.map(String)
      : [];
    if (touches.length === 0) continue;
    if (data?.decision_hash !== bodyFingerprint(body)) {
      problems.push(
        violation(
          "PROT002",
          file,
          1,
          "ADR authorizing protected changes is not sealed",
          `Seal it: pnpm tsx harness/src/checks/docs.ts seal-adr ${file}`,
        ),
      );
      continue;
    }
    for (const item of touches) granted.set(item, file);
  }
  return { granted, problems };
}

export function checkDiff(root: string, base: string): Violation[] {
  const patterns = loadPatterns(root);
  const changes = changedFiles(root, base);
  const { granted, problems } = authorizations(root, changes);
  const out = [...problems];
  for (const [status, file] of changes) {
    if ((status === "A" && file.startsWith(ADR_DIR)) || file in RATCHET_FILES) continue;
    const hit = patterns.find((p) => matchesGlob(file, p));
    if (!hit) continue;
    if (![...granted.keys()].some((g) => g === hit || matchesGlob(file, g))) {
      out.push(
        violation(
          "PROT001",
          file,
          0,
          `protected path changed (matches '${hit}') without authorization`,
          `Guardrails change only by decision: add a sealed ADR in this change whose touches_protected lists '${file}' (or '${hit}') and says why.`,
        ),
      );
    }
  }
  return out;
}

export function checkCodeowners(root: string): Violation[] {
  const file = path.join(root, CODEOWNERS);
  const owned = existsSync(file)
    ? readFileSync(file, "utf8")
        .split("\n")
        .filter((line) => line.trim() && !line.startsWith("#"))
        .map((line) => (line.split(/\s+/)[0] as string).replace(/^\//, ""))
    : [];
  const missing = loadPatterns(root).filter((p) => !owned.includes(p));
  return missing.length === 0
    ? []
    : [
        violation(
          "PROT003",
          CODEOWNERS,
          0,
          `CODEOWNERS misses: ${missing.join(", ")}`,
          "Add a line '/<pattern> @owner' for each protected path.",
        ),
      ];
}

export function run(root: string, base?: string): Violation[] {
  return [...checkCodeowners(root), ...(base ? checkDiff(root, base) : [])];
}

export function main(argv: readonly string[], out: (line: string) => void = console.log): number {
  const { flags } = parseArgs(argv);
  return finish("protected-paths", run(findRoot(flag(flags, "root")), flag(flags, "base")), out);
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
