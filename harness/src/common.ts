/**
 * Shared helpers for harness checks: violations, repo discovery, front matter, git, CLI output.
 * Everything under harness/ is a protected path; product code must never import it.
 */
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import YAML from "yaml";

export interface Violation {
  /** Stable rule code such as ARCH010; ADRs and tests reference it. */
  readonly code: string;
  /** Repo-relative path, posix separators. */
  readonly path: string;
  readonly line: number;
  readonly message: string;
  /** Written for an agent that reads it cold: what to do next. */
  readonly fix: string;
}

export function violation(
  code: string,
  file: string,
  line: number,
  message: string,
  fix: string,
): Violation {
  return { code, path: file, line, message, fix };
}

export function render(v: Violation): string {
  const location = v.line ? `${v.path}:${v.line}` : v.path;
  return `[${v.code}] ${location}\n    problem: ${v.message}\n    fix: ${v.fix}`;
}

export function sortViolations(items: readonly Violation[]): Violation[] {
  return [...items].sort(
    (a, b) =>
      a.code.localeCompare(b.code) ||
      a.path.localeCompare(b.path) ||
      a.line - b.line ||
      a.message.localeCompare(b.message),
  );
}

/** Print a result block and return a process exit code. */
export function finish(
  name: string,
  violations: readonly Violation[],
  out: (line: string) => void = console.log,
): number {
  if (violations.length === 0) {
    out(`OK   ${name}`);
    return 0;
  }
  out(`FAIL ${name}: ${violations.length} violation(s)\n`);
  for (const v of sortViolations(violations)) {
    out(render(v));
    out("");
  }
  return 1;
}

/** Walk up from `start` until the repo root (package.json + harness/config) is found. */
export function findRoot(start: string = process.cwd()): string {
  let current = path.resolve(start);
  for (;;) {
    if (
      existsSync(path.join(current, "package.json")) &&
      existsSync(path.join(current, "harness", "config"))
    ) {
      return current;
    }
    const parent = path.dirname(current);
    if (parent === current)
      throw new Error("praxium repo root not found (needs package.json and harness/config)");
    current = parent;
  }
}

/** Today as YYYY-MM-DD. PRAXIUM_TODAY pins it for deterministic runs. */
export function today(): string {
  const pinned = process.env.PRAXIUM_TODAY;
  if (pinned) {
    if (!isIsoDate(pinned)) throw new Error(`PRAXIUM_TODAY must be YYYY-MM-DD, got ${pinned}`);
    return pinned;
  }
  return new Date().toISOString().slice(0, 10);
}

export function isIsoDate(value: unknown): value is string {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const parsed = new Date(`${value}T00:00:00Z`);
  return !Number.isNaN(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
}

/** Whole days from `a` to `b` (both YYYY-MM-DD). */
export function daysBetween(a: string, b: string): number {
  return Math.round((Date.parse(`${b}T00:00:00Z`) - Date.parse(`${a}T00:00:00Z`)) / 86_400_000);
}

export function readText(file: string): string {
  return readFileSync(file, "utf8");
}

export function loadYaml<T>(file: string): T {
  return YAML.parse(readText(file)) as T;
}

export function rel(root: string, file: string): string {
  return path.relative(root, file).split(path.sep).join("/");
}

export interface FrontMatter {
  readonly data: Record<string, unknown> | null;
  readonly body: string;
  /** 1-based line number where the body starts. */
  readonly bodyLine: number;
}

export function splitFrontMatter(text: string): FrontMatter {
  const lines = text.split("\n");
  if (lines[0]?.trim() !== "---") return { data: null, body: text, bodyLine: 1 };
  for (let index = 1; index < lines.length; index++) {
    if (lines[index]?.trim() === "---") {
      let data: unknown;
      try {
        data = YAML.parse(lines.slice(1, index).join("\n")) ?? {};
      } catch {
        return { data: null, body: text, bodyLine: 1 };
      }
      if (typeof data !== "object" || data === null || Array.isArray(data))
        return { data: null, body: text, bodyLine: 1 };
      return {
        data: data as Record<string, unknown>,
        body: lines.slice(index + 1).join("\n"),
        bodyLine: index + 2,
      };
    }
  }
  return { data: null, body: text, bodyLine: 1 };
}

export function joinFrontMatter(data: Record<string, unknown>, body: string): string {
  const dumped = YAML.stringify(data, { lineWidth: 0 }).trimEnd();
  return `---\n${dumped}\n---\n${body.replace(/^\n+/, "")}${body.endsWith("\n") ? "" : "\n"}`;
}

/** Run git in `root`; null when git is missing or the command fails. */
export function git(root: string, ...args: string[]): string | null {
  try {
    return execFileSync("git", args, {
      cwd: root,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
  } catch {
    return null;
  }
}

/** Minimal CLI parsing: positional args plus `--flag value` and boolean `--flag`. */
export function parseArgs(argv: readonly string[]): {
  positional: string[];
  flags: Map<string, string | true>;
} {
  const positional: string[] = [];
  const flags = new Map<string, string | true>();
  for (let index = 0; index < argv.length; index++) {
    const arg = argv[index] as string;
    if (arg.startsWith("--")) {
      const next = argv[index + 1];
      if (next !== undefined && !next.startsWith("--")) {
        flags.set(arg.slice(2), next);
        index++;
      } else {
        flags.set(arg.slice(2), true);
      }
    } else {
      positional.push(arg);
    }
  }
  return { positional, flags };
}

export function flag(flags: Map<string, string | true>, name: string): string | undefined {
  const value = flags.get(name);
  return typeof value === "string" ? value : undefined;
}

/** True when this module is the program entry point (works under tsx). */
export function isMain(metaUrl: string): boolean {
  const entry = process.argv[1];
  return entry !== undefined && path.resolve(entry) === path.resolve(new URL(metaUrl).pathname);
}
