/** The Go part of the architecture contract (ADR-0019) and the Go files it covers. */
import { readFileSync } from "node:fs";
import path from "node:path";
import { loadYaml } from "../common";
import { listFiles } from "../files";
import { CONFIG_PATH } from "./arch-model";
import {
  codeOf,
  type GoImport,
  type GoPackage,
  type GoTok,
  importsOf,
  lex,
  packageOf,
} from "./go-lex";

export { CONFIG_PATH };

export interface GoUnit {
  readonly name: string;
  /** Repo-relative folder of the unit's package. */
  readonly path: string;
  readonly mayImport: readonly string[];
}

export interface GoConfig {
  /** Repo-relative folder of the Go module (where go.mod lives). */
  readonly root: string;
  readonly module: string;
  readonly units: readonly GoUnit[];
  /** The unit that assembles the program: the only place for blank imports, `os.Exit` and `log.Fatal`. */
  readonly compositionRoot: string;
  /** Module path -> units allowed to import it. */
  readonly thirdParty: ReadonlyMap<string, readonly string[]>;
  /** Standard-library package -> units allowed to import it (the core knows no operating system or network). */
  readonly restrictedStdlib: ReadonlyMap<string, readonly string[]>;
  /** Files (exact) or folders (ending in `/`) that may read the clock, randomness and the environment. */
  readonly nondeterminismAllowed: readonly string[];
  /** The document holding the table of Go modules (REQ-239). */
  readonly dependencyTable: string;
}

interface RawGo {
  root: string;
  module: string;
  units: Record<string, { path: string; may_import: string[] }>;
  composition_root: string;
  third_party?: Record<string, string[]>;
  restricted_stdlib?: Record<string, string[]>;
  nondeterminism_allowed?: string[];
  dependency_table?: string;
}

const trimSlash = (p: string): string => p.replace(/\/$/, "");

/** The `go:` section of the architecture contract; undefined when the repository has no Go code. */
export function loadGoConfig(root: string): GoConfig | undefined {
  const raw = loadYaml<{ go?: RawGo }>(path.join(root, CONFIG_PATH)).go;
  if (!raw) return undefined;
  return {
    root: trimSlash(raw.root),
    module: raw.module,
    units: Object.entries(raw.units).map(([name, spec]) => ({
      name,
      path: trimSlash(spec.path),
      mayImport: spec.may_import,
    })),
    compositionRoot: raw.composition_root,
    thirdParty: new Map(Object.entries(raw.third_party ?? {})),
    restrictedStdlib: new Map(Object.entries(raw.restricted_stdlib ?? {})),
    nondeterminismAllowed: raw.nondeterminism_allowed ?? [],
    dependencyTable: raw.dependency_table ?? "ARCHITECTURE.md",
  };
}

export function unitOfPath(config: GoConfig, file: string): GoUnit | undefined {
  return config.units
    .filter((u) => file === u.path || file.startsWith(`${u.path}/`))
    .sort((a, b) => b.path.length - a.path.length)[0];
}

export interface GoFile {
  /** Repo-relative path, posix separators. */
  readonly path: string;
  readonly isTest: boolean;
  readonly text: string;
  readonly toks: readonly GoTok[];
  /** Tokens without comments. */
  readonly code: readonly GoTok[];
  readonly imports: readonly GoImport[];
  readonly pkg: GoPackage | undefined;
  readonly unit: GoUnit | undefined;
}

const cache = new Map<string, { text: string; file: Omit<GoFile, "unit"> }>();

function read(root: string, file: string): Omit<GoFile, "unit"> {
  const text = readFileSync(path.join(root, file), "utf8");
  const key = path.join(root, file);
  const hit = cache.get(key);
  if (hit?.text === text) return hit.file;
  const toks = lex(text);
  const code = codeOf(toks);
  const parsed = {
    path: file,
    isTest: file.endsWith("_test.go"),
    text,
    toks,
    code,
    imports: importsOf(code),
    pkg: packageOf(toks),
  };
  cache.set(key, { text, file: parsed });
  return parsed;
}

const skipped = (file: string): boolean => /(^|\/)(testdata|vendor)\//.test(file);

/** Every Go file of the module, parsed once while its text is unchanged. */
export function goFiles(root: string, config: GoConfig): GoFile[] {
  return listFiles(root, [config.root], (name) => name.endsWith(".go"))
    .filter((file) => !skipped(file))
    .map((file) => ({ ...read(root, file), unit: unitOfPath(config, file) }));
}

/** Repo-relative path of an import path inside the module, or undefined for any other import. */
export function moduleDir(config: GoConfig, importPath: string): string | undefined {
  if (importPath === config.module) return config.root;
  return importPath.startsWith(`${config.module}/`)
    ? `${config.root}/${importPath.slice(config.module.length + 1)}`
    : undefined;
}

export type ImportKind = "internal" | "stdlib" | "module";

/** Standard-library packages have no dot in the first path element; the module's own packages share its prefix. */
export function importKind(config: GoConfig, importPath: string): ImportKind {
  if (moduleDir(config, importPath) !== undefined) return "internal";
  return (importPath.split("/")[0] ?? "").includes(".") ? "module" : "stdlib";
}
