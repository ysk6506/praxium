/** File discovery shared by the checks (no globbing library needed). */
import { existsSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { rel } from "./common";

const SKIP_DIRS = new Set([
  "node_modules",
  "dist",
  "coverage",
  ".git",
  ".vite",
  "test-results",
  "playwright-report",
]);

/** Repo-relative posix paths of files under `dirs` whose name matches `accept`. */
export function listFiles(
  root: string,
  dirs: readonly string[],
  accept: (name: string) => boolean,
): string[] {
  const out: string[] = [];
  const visit = (dir: string): void => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        if (!SKIP_DIRS.has(entry.name)) visit(path.join(dir, entry.name));
      } else if (accept(entry.name)) {
        out.push(rel(root, path.join(dir, entry.name)));
      }
    }
  };
  for (const dir of dirs) {
    const full = path.join(root, dir);
    if (existsSync(full) && statSync(full).isDirectory()) visit(full);
  }
  return out.sort();
}

export const isSourceFile = (name: string): boolean =>
  /\.(ts|tsx)$/.test(name) && !name.endsWith(".d.ts");

export function isTestFile(file: string): boolean {
  return /\.test\.tsx?$/.test(file) || file.includes("/test-support/");
}

const compiled = new Map<string, RegExp>();

/** Convert a simple glob (`*`, `**`, `?`) into a RegExp over posix paths (compiled once per glob). */
export function globToRegExp(glob: string): RegExp {
  const hit = compiled.get(glob);
  if (hit) return hit;
  const regexp = compileGlob(glob);
  compiled.set(glob, regexp);
  return regexp;
}

function compileGlob(glob: string): RegExp {
  let pattern = "";
  for (let index = 0; index < glob.length; index++) {
    const ch = glob[index] as string;
    if (ch === "*") {
      if (glob[index + 1] === "*") {
        const slash = glob[index + 2] === "/";
        pattern += slash ? "(?:.*/)?" : ".*";
        index += slash ? 2 : 1;
      } else {
        pattern += "[^/]*";
      }
    } else if (ch === "?") {
      pattern += "[^/]";
    } else {
      pattern += /[.+^${}()|[\]\\]/.test(ch) ? `\\${ch}` : ch;
    }
  }
  return new RegExp(`^${pattern}$`);
}

export function matchesGlob(file: string, glob: string): boolean {
  return globToRegExp(glob).test(file);
}

/** Files (repo-relative) matching any of the globs. Directories match everything inside them. */
export function expandGlobs(root: string, globs: readonly string[]): string[] {
  const all = listFiles(root, ["."], () => true).map((f) => f.replace(/^\.\//, ""));
  const out = new Set<string>();
  for (const glob of globs) {
    const isDir =
      existsSync(path.join(root, glob)) && statSync(path.join(root, glob)).isDirectory();
    for (const file of all) {
      if (isDir ? file.startsWith(`${glob.replace(/\/$/, "")}/`) : matchesGlob(file, glob))
        out.add(file);
    }
  }
  return [...out].sort();
}
