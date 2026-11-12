/** Architecture contract model: config, units, and import resolution. */
import { existsSync } from "node:fs";
import { builtinModules } from "node:module";
import path from "node:path";
import { loadYaml } from "../common";

export const CONFIG_PATH = "harness/config/architecture.yaml";

export interface UnitSpec {
  readonly name: string;
  readonly path: string;
  readonly mayImport: readonly string[];
  readonly publicIndex: boolean;
}

export interface ArchConfig {
  readonly units: readonly UnitSpec[];
  readonly nodeBuiltins: readonly string[];
  readonly thirdParty: ReadonlyMap<string, readonly string[]>;
  readonly testOnly: readonly string[];
  readonly nondeterminismAllowed: readonly string[];
  readonly sourceRoots: readonly string[];
  readonly aliasPrefix: string;
  readonly aliasTarget: string;
  readonly ignoredFiles: readonly string[];
}

interface RawConfig {
  units: Record<string, { path: string; may_import: string[]; public_index?: boolean }>;
  node_builtins: string[];
  third_party: Record<string, string[]>;
  test_only: string[];
  nondeterminism_allowed: string[];
  source_roots: string[];
  alias_prefix: string;
  alias_target: string;
  ignored_files?: string[];
}

export function loadArchConfig(root: string): ArchConfig {
  const raw = loadYaml<RawConfig>(path.join(root, CONFIG_PATH));
  return {
    units: Object.entries(raw.units).map(([name, spec]) => ({
      name,
      path: spec.path.replace(/\/$/, ""),
      mayImport: spec.may_import,
      publicIndex: spec.public_index ?? true,
    })),
    nodeBuiltins: raw.node_builtins,
    thirdParty: new Map(Object.entries(raw.third_party)),
    testOnly: raw.test_only,
    nondeterminismAllowed: raw.nondeterminism_allowed,
    sourceRoots: raw.source_roots,
    aliasPrefix: raw.alias_prefix,
    aliasTarget: raw.alias_target.replace(/\/$/, ""),
    ignoredFiles: raw.ignored_files ?? [],
  };
}

/** `core.*` matches `core.world`; a plain name matches only itself. */
export function unitMatches(pattern: string, unit: string): boolean {
  return pattern.endsWith(".*") ? unit.startsWith(pattern.slice(0, -1)) : pattern === unit;
}

export function allowedBy(patterns: readonly string[], unit: string): boolean {
  return patterns.some((p) => unitMatches(p, unit));
}

export function unitOf(config: ArchConfig, file: string): UnitSpec | undefined {
  return config.units.find((u) => file === u.path || file.startsWith(`${u.path}/`));
}

export type Resolution =
  | { kind: "internal"; file: string }
  | { kind: "unresolved"; target: string }
  | { kind: "builtin"; name: string }
  | { kind: "package"; name: string };

const BUILTINS = new Set(builtinModules);
const CANDIDATES = ["", ".ts", ".tsx", "/index.ts", "/index.tsx"];

function firstExisting(root: string, base: string): string | undefined {
  for (const suffix of CANDIDATES) {
    const candidate = `${base}${suffix}`;
    const full = path.join(root, candidate);
    if (/\.(ts|tsx)$/.test(candidate) && existsSync(full)) return candidate;
  }
  return undefined;
}

export function resolveImport(
  root: string,
  config: ArchConfig,
  from: string,
  specifier: string,
): Resolution {
  if (specifier.startsWith(".")) {
    const base = path.posix.normalize(path.posix.join(path.posix.dirname(from), specifier));
    const found = firstExisting(root, base);
    return found ? { kind: "internal", file: found } : { kind: "unresolved", target: base };
  }
  if (specifier.startsWith(config.aliasPrefix)) {
    const base = `${config.aliasTarget}/${specifier.slice(config.aliasPrefix.length)}/index`;
    const found = firstExisting(root, base);
    return found ? { kind: "internal", file: found } : { kind: "unresolved", target: base };
  }
  const bare = specifier.startsWith("node:") ? specifier.slice(5) : specifier;
  const top = bare.split("/")[0] as string;
  if (specifier.startsWith("node:") || BUILTINS.has(bare) || BUILTINS.has(top))
    return { kind: "builtin", name: bare };
  const parts = specifier.split("/");
  const name = specifier.startsWith("@") ? parts.slice(0, 2).join("/") : (parts[0] as string);
  return { kind: "package", name };
}

export function isUnitIndex(unit: UnitSpec, file: string): boolean {
  return file === `${unit.path}/index.ts` || file === `${unit.path}/index.tsx`;
}
