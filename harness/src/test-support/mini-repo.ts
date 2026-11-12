/** Throwaway repositories for harness meta-tests. */
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

export const DEMO_ARCHITECTURE = `
units:
  core.kernel: {path: packages/core/src/kernel, may_import: []}
  core.world: {path: packages/core/src/world, may_import: [core.kernel]}
  core.sources.gen: {path: packages/core/src/sources/gen, may_import: [core.kernel, core.sources]}
  core.sources.imp: {path: packages/core/src/sources/imp, may_import: [core.kernel, core.sources]}
  core.sources: {path: packages/core/src/sources, may_import: [core.kernel]}
  server.http: {path: apps/server/src/http, may_import: ["core.*"]}
  server.main: {path: apps/server/src, may_import: ["core.*", "server.*"], public_index: false}
node_builtins: ["server.*"]
third_party:
  zod: ["core.*"]
  fastify: [server.http]
test_only: [vitest]
nondeterminism_allowed: [packages/core/src/kernel/entropy.ts, apps/]
source_roots: [packages, apps]
alias_prefix: "@praxium/core/"
alias_target: packages/core/src
ignored_files: [apps/server/tool.config.ts]
`;

const BASE: Record<string, string> = {
  "package.json": "{}\n",
  "harness/config/architecture.yaml": DEMO_ARCHITECTURE,
  "packages/core/src/kernel/index.ts": "export const k = 1;\n",
  "packages/core/src/world/index.ts": "export const w = 1;\n",
  "packages/core/src/sources/index.ts": "export const s = 1;\n",
  "packages/core/src/sources/gen/index.ts": "export const g = 1;\n",
  "packages/core/src/sources/imp/index.ts": "export const i = 1;\n",
  "apps/server/src/http/index.ts": "export const h = 1;\n",
  "apps/server/src/main.ts": "export const m = 1;\n",
};

/** Write `files` (over a small healthy base) into a fresh temp dir and return its path. */
export function miniRepo(
  files: Record<string, string> = {},
  base: Record<string, string> = BASE,
): string {
  const root = mkdtempSync(path.join(tmpdir(), "praxium-mini-"));
  writeFiles(root, { ...base, ...files });
  return root;
}

export function writeFiles(root: string, files: Record<string, string>): void {
  for (const [name, content] of Object.entries(files)) {
    const target = path.join(root, name);
    mkdirSync(path.dirname(target), { recursive: true });
    writeFileSync(target, content, "utf8");
  }
}

/** Initialise git and commit everything (for checks that compare with a base ref). */
export function commitAll(root: string, message = "base"): void {
  const git = (...args: string[]) =>
    execFileSync("git", ["-c", "user.email=t@t", "-c", "user.name=t", ...args], {
      cwd: root,
      stdio: "ignore",
    });
  git("init", "-q");
  git("add", "-A");
  git("commit", "-qm", message);
}
