/**
 * Finding the third-party npm packages that ship with Praxium, from the installed tree. A package is
 * distributed when a distributed workspace (or a listed runtime root) reaches it through `dependencies`,
 * `optionalDependencies` or an installed peer. `devDependencies` are never followed.
 */
import { existsSync, readdirSync, readFileSync, realpathSync } from "node:fs";
import path from "node:path";

export interface ScanConfig {
  /** Workspaces whose `dependencies` end up in what is handed out (web bundle, server runtime). */
  distributed_workspaces: string[];
  /** Packages declared as devDependencies at the root that the shipped server still runs on. */
  extra_runtime_roots?: Array<{ package: string; reason: string }>;
  /** Package name -> license file inside the package, for packages that keep it outside the top folder. */
  license_files?: Record<string, string>;
}

export interface TextFile {
  readonly file: string;
  readonly text: string;
}

export interface Pkg {
  readonly name: string;
  readonly version: string;
  /** The `license` (or `licenses`) field as text; undefined when the package has none. */
  readonly license: string | undefined;
  readonly sourceUrl: string;
  readonly author: string | undefined;
  readonly licenseFiles: readonly TextFile[];
  readonly noticeFiles: readonly TextFile[];
  /** Short names of what reaches it: web, server, core, runtime. */
  readonly via: readonly string[];
}

export interface Scan {
  readonly packages: readonly Pkg[];
  /** Dependencies that are declared but not installed (name, wanted by). */
  readonly missing: ReadonlyArray<readonly [string, string]>;
}

type Json = Record<string, unknown>;

const readJson = (file: string): Json => JSON.parse(readFileSync(file, "utf8")) as Json;
const asRecord = (v: unknown): Record<string, string> =>
  typeof v === "object" && v !== null ? (v as Record<string, string>) : {};

/** Node's lookup: `<ancestor>/node_modules/<name>` for every ancestor, without doubling `node_modules`. */
function resolveFrom(dir: string, name: string): string | undefined {
  let at = dir;
  for (;;) {
    const base = path.basename(at) === "node_modules" ? at : path.join(at, "node_modules");
    const candidate = path.join(base, name);
    if (existsSync(path.join(candidate, "package.json"))) return realpathSync(candidate);
    const up = path.dirname(at);
    if (up === at) return undefined;
    at = up;
  }
}

function licenseText(meta: Json): string | undefined {
  const one = (v: unknown): string | undefined => {
    if (typeof v === "string") return v.trim() || undefined;
    const type = (v as { type?: unknown } | null)?.type;
    return typeof type === "string" ? type : undefined;
  };
  if (Array.isArray(meta.licenses)) {
    const all = meta.licenses.map(one).filter((x): x is string => x !== undefined);
    if (all.length > 0 && meta.license === undefined) return all.join(" OR ");
  }
  return one(meta.license);
}

function sourceUrl(meta: Json, name: string): string {
  const repo = meta.repository;
  let url = typeof repo === "string" ? repo : (repo as { url?: string } | undefined)?.url;
  if (url) {
    url = url
      .replace(/^git\+/, "")
      .replace(/\.git$/, "")
      .replace(/^git:\/\//, "https://");
    url = url.replace(/^ssh:\/\/git@github\.com/, "https://github.com");
    if (/^github:/.test(url)) url = `https://github.com/${url.slice(7)}`;
    else if (/^[\w.-]+\/[\w.-]+$/.test(url)) url = `https://github.com/${url}`;
    if (/^https?:\/\//.test(url)) return url;
  }
  if (typeof meta.homepage === "string" && /^https?:\/\//.test(meta.homepage)) return meta.homepage;
  return `https://www.npmjs.com/package/${name}`;
}

function authorOf(meta: Json): string | undefined {
  const a = meta.author;
  if (typeof a === "string") return a.replace(/\s*<[^>]*>/, "").replace(/\s*\([^)]*\)/, "");
  const n = (a as { name?: unknown } | undefined)?.name;
  return typeof n === "string" ? n : undefined;
}

const LICENSE_NAME = /^(licen[cs]e|licen[cs]es|copying|unlicense)([._-].*)?$/i;
const NOTICE_NAME = /^notice([._-].*)?$/i;

function filesMatching(dir: string, test: RegExp): TextFile[] {
  return readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isFile() && test.test(e.name))
    .map((e) => ({ file: e.name, text: readFileSync(path.join(dir, e.name), "utf8") }))
    .sort((a, b) => a.file.localeCompare(b.file));
}

function readPackage(dir: string, config: ScanConfig, via: string[]): Pkg {
  const meta = readJson(path.join(dir, "package.json"));
  const name = String(meta.name);
  let licenseFiles = filesMatching(dir, LICENSE_NAME);
  const hint = config.license_files?.[name];
  if (licenseFiles.length === 0 && hint !== undefined && existsSync(path.join(dir, hint)))
    licenseFiles = [{ file: hint, text: readFileSync(path.join(dir, hint), "utf8") }];
  return {
    name,
    version: String(meta.version),
    license: licenseText(meta),
    sourceUrl: sourceUrl(meta, name),
    author: authorOf(meta),
    licenseFiles,
    noticeFiles: filesMatching(dir, NOTICE_NAME),
    via,
  };
}

/** Walk the dependency closure of every root; roots are (label, directory to resolve from, names to start at). */
export function scan(root: string, config: ScanConfig): Scan {
  const seen = new Map<string, { dir: string; via: Set<string> }>();
  const missing: Array<readonly [string, string]> = [];
  const visit = (dir: string, label: string): void => {
    const entry = seen.get(dir);
    if (entry?.via.has(label)) return;
    if (entry) entry.via.add(label);
    else seen.set(dir, { dir, via: new Set([label]) });
    const meta = readJson(path.join(dir, "package.json"));
    const required = { ...asRecord(meta.dependencies) };
    const soft = { ...asRecord(meta.optionalDependencies), ...asRecord(meta.peerDependencies) };
    for (const name of Object.keys(required)) {
      const found = resolveFrom(dir, name);
      if (found) visit(found, label);
      else missing.push([name, String(meta.name)]);
    }
    for (const name of Object.keys(soft)) {
      const found = name in required ? undefined : resolveFrom(dir, name);
      if (found) visit(found, label);
    }
  };
  for (const ws of config.distributed_workspaces) {
    const wsDir = path.join(root, ws);
    const meta = readJson(path.join(wsDir, "package.json"));
    const label = ws.split("/").pop() as string;
    for (const [name, wanted] of Object.entries(asRecord(meta.dependencies))) {
      if (wanted.startsWith("workspace:")) continue; // our own code
      const found = resolveFrom(wsDir, name);
      if (found) visit(found, label);
      else missing.push([name, String(meta.name)]);
    }
  }
  for (const extra of config.extra_runtime_roots ?? []) {
    const found = resolveFrom(root, extra.package);
    if (found) visit(found, "runtime");
    else missing.push([extra.package, "root"]);
  }
  const packages = [...seen.values()]
    .map((s) => readPackage(s.dir, config, [...s.via].sort()))
    .sort((a, b) => a.name.localeCompare(b.name) || a.version.localeCompare(b.version));
  return { packages, missing };
}
