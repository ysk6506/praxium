/**
 * Licenses of what ships with Praxium (REQ-020 family, ADR-0185). Third-party material that is handed out must
 * be under a license on the allowlist (harness/config/licenses.yaml), and THIRD_PARTY_NOTICES.md lists it.
 * "Distributed" means: the npm packages the web bundle and the server run on (read from the installed tree, see
 * licenses-scan.ts) plus the non-npm assets in harness/config/third-party-assets.yaml.
 *
 * LIC001 a distributed component's license is not on the allowlist (or an unmodified-only license on a patched one)
 * LIC002 a distributed component has no determinable license (no field, not an SPDX expression, not installed)
 * LIC004 the asset manifest is broken (missing field, or a file it names does not exist)
 * LIC003 is a note, not a failure: THIRD_PARTY_NOTICES.md is behind the installed dependencies. The cleanup job
 * regenerates it with `pnpm tsx harness/src/checks/licenses.ts write`; ordinary changes are not blocked by it.
 * Exceptions in licenses.yaml carry a reason and are printed on every run.
 */
import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  isMain,
  loadYaml,
  parseArgs,
  type Violation,
  violation,
} from "../common";
import { type Component, fromPackage, type Reference, renderNotices } from "./licenses-render";
import { type Pkg, type ScanConfig, scan } from "./licenses-scan";
import { decide, type Expr, parseExpr, show } from "./licenses-spdx";

const CONFIG_PATH = "harness/config/licenses.yaml";

interface ExceptionEntry {
  package: string;
  version?: string;
  license: string;
  reason: string;
}

export interface LicensesConfig extends ScanConfig {
  notices_file: string;
  assets_manifest: string;
  allowed: string[];
  /** Allowed only while the package is unmodified (not patched); listed in their own part of the notices. */
  allowed_unmodified: string[];
  exceptions: ExceptionEntry[];
}

interface AssetEntry {
  id: string;
  name: string;
  version?: string;
  license: string;
  source: string;
  paths: string[];
  where: string;
  license_file?: string;
  copyright?: string;
}

export const loadLicensesConfig = (root: string): LicensesConfig =>
  loadYaml<LicensesConfig>(path.join(root, CONFIG_PATH));

interface Manifest {
  assets?: AssetEntry[];
  /** Data drawn by hand with outside material as a guide: nothing is copied, so no license applies. */
  references?: Reference[];
}

function loadManifest(root: string, config: LicensesConfig): Manifest {
  const file = path.join(root, config.assets_manifest);
  return existsSync(file) ? loadYaml<Manifest>(file) : {};
}

const loadAssets = (root: string, config: LicensesConfig): AssetEntry[] =>
  loadManifest(root, config).assets ?? [];

/** Packages that have a local patch (`pnpm.patchedDependencies`): a patch makes an unmodified-only license fail. */
function patchedNames(root: string): Set<string> {
  const file = path.join(root, "package.json");
  if (!existsSync(file)) return new Set();
  const pkg = JSON.parse(readFileSync(file, "utf8")) as {
    pnpm?: { patchedDependencies?: Record<string, string> };
  };
  return new Set(
    Object.keys(pkg.pnpm?.patchedDependencies ?? {}).map((k) => k.replace(/@[^@/]+$/, "")),
  );
}

export interface Evaluation {
  violations: Violation[];
  notes: string[];
  components: Component[];
  references: Reference[];
  fingerprint: string;
}

interface Judge {
  config: LicensesConfig;
  patched: Set<string>;
  /** Canonical spelling of an allowed identifier (case-insensitive), undefined when not allowed. */
  allowed: (id: string) => string | undefined;
}

function makeJudge(root: string, config: LicensesConfig): Judge {
  const table = new Map(
    [...config.allowed, ...config.allowed_unmodified].map((id) => [id.toLowerCase(), id]),
  );
  return { config, patched: patchedNames(root), allowed: (id) => table.get(id.toLowerCase()) };
}

const exceptionFor = (config: LicensesConfig, pkg: Pkg): ExceptionEntry | undefined =>
  config.exceptions.find(
    (e) => e.package === pkg.name && (e.version === undefined || e.version === pkg.version),
  );

/** Violations (and the identifiers that apply) for one declared license expression. */
function judge(
  j: Judge,
  where: string,
  label: string,
  declared: string | undefined,
  patched: boolean,
): { violations: Violation[]; applies: string[]; expression: string } {
  const none = (message: string, fix: string): ReturnType<typeof judge> => ({
    violations: [violation("LIC002", where, 0, `${label}: ${message}`, fix)],
    applies: [],
    expression: declared ?? "",
  });
  const hand =
    "Find the license by hand in the package, then record an exception with a reason in harness/config/licenses.yaml.";
  if (declared === undefined) return none("no license field", hand);
  const expr: Expr | undefined = parseExpr(declared);
  if (!expr || /^see license/i.test(declared) || /^unlicensed$/i.test(declared)) {
    if (/^unlicensed$/i.test(declared))
      return {
        violations: [
          violation(
            "LIC001",
            where,
            0,
            `${label}: license is UNLICENSED, which is not on the allowlist`,
            "Replace it, or record an exception with a reason in harness/config/licenses.yaml.",
          ),
        ],
        applies: [declared],
        expression: declared,
      };
    return none(`license field "${declared}" is not an SPDX expression`, hand);
  }
  const verdict = decide(expr, j.allowed);
  const unmodifiedOnly = verdict.applies.filter((id) =>
    j.config.allowed_unmodified.some((u) => u.toLowerCase() === id.toLowerCase()),
  );
  const bad = verdict.ok && patched && unmodifiedOnly.length > 0;
  const violations =
    verdict.ok && !bad
      ? []
      : [
          violation(
            "LIC001",
            where,
            0,
            bad
              ? `${label}: license ${show(expr)} is allowed only unmodified, but the package is patched`
              : `${label}: license ${show(expr)} is not on the allowlist`,
            "Replace it, or record an exception with a reason in harness/config/licenses.yaml.",
          ),
        ];
  return { violations, applies: [...verdict.applies], expression: show(expr) };
}

function evalPackages(root: string, j: Judge, out: Evaluation): void {
  const { packages, missing } = scan(root, j.config);
  for (const [name, wanted] of missing)
    out.violations.push(
      violation(
        "LIC002",
        "pnpm-lock.yaml",
        0,
        `${name} (needed by ${wanted}) is not installed, so its license cannot be read`,
        "Run pnpm install --frozen-lockfile, then run the check again.",
      ),
    );
  for (const pkg of packages) {
    const label = `${pkg.name}@${pkg.version}`;
    const exc = exceptionFor(j.config, pkg);
    if (exc) {
      out.notes.push(`NOTE licenses [exception] ${label}: ${exc.license} (${exc.reason})`);
      out.components.push(fromPackage(pkg, [exc.license], pkg.license ?? exc.license));
      continue;
    }
    const r = judge(j, "pnpm-lock.yaml", label, pkg.license, j.patched.has(pkg.name));
    out.violations.push(...r.violations);
    out.components.push(
      fromPackage(pkg, r.applies.length > 0 ? r.applies : ["(unknown)"], r.expression),
    );
  }
}

/** What is wrong with one manifest entry, as messages. */
function assetProblems(root: string, a: AssetEntry): string[] {
  const missing = (["id", "name", "license", "source", "where"] as const)
    .filter((key) => !a[key])
    .map((key) => `missing '${key}'`);
  if (!a.paths?.length) missing.push("missing 'paths'");
  const files = [...(a.paths ?? []), ...(a.license_file ? [a.license_file] : [])];
  const gone = files.filter((p) => !existsSync(path.join(root, p)));
  return [...missing, ...gone.map((p) => `file does not exist: ${p}`)];
}

function evalAssets(root: string, j: Judge, out: Evaluation): void {
  const manifest = j.config.assets_manifest;
  for (const a of loadAssets(root, j.config)) {
    for (const message of assetProblems(root, a))
      out.violations.push(
        violation(
          "LIC004",
          manifest,
          0,
          `${a.id ?? "(entry)"}: ${message}`,
          "Fix the entry in the manifest.",
        ),
      );
    const r = judge(j, manifest, a.id, a.license, false);
    out.violations.push(...r.violations);
    const licenseFile = a.license_file ? path.join(root, a.license_file) : undefined;
    const text =
      licenseFile && existsSync(licenseFile) ? readFileSync(licenseFile, "utf8") : undefined;
    out.components.push({
      name: a.name,
      version: a.version ?? "(no version)",
      licenses: r.applies.length > 0 ? r.applies : ["(unknown)"],
      declared: r.expression,
      source: a.source,
      where: a.where,
      texts: text ? [text] : [],
      notices: a.copyright ? [a.copyright.replace(/\s+/g, " ").trim()] : [],
      asset: true,
    });
  }
}

const fingerprintOf = (
  components: readonly Component[],
  references: readonly Reference[],
): string =>
  createHash("sha256")
    .update(
      JSON.stringify([
        components.map((c) => [
          c.name,
          c.version,
          c.licenses,
          c.texts.map((t) => t.length),
          c.notices,
        ]),
        references,
      ]),
    )
    .digest("hex")
    .slice(0, 16);

export function evaluate(root: string): Evaluation {
  const config = loadLicensesConfig(root);
  const j = makeJudge(root, config);
  const out: Evaluation = {
    violations: [],
    notes: [],
    components: [],
    references: loadManifest(root, config).references ?? [],
    fingerprint: "",
  };
  evalPackages(root, j, out);
  evalAssets(root, j, out);
  for (const r of out.references)
    for (const p of r.paths ?? [])
      if (!existsSync(path.join(root, p)))
        out.violations.push(
          violation(
            "LIC004",
            config.assets_manifest,
            0,
            `${r.id}: file does not exist: ${p}`,
            "Fix the entry in the manifest.",
          ),
        );
  out.fingerprint = fingerprintOf(out.components, out.references);
  return out;
}

export const render = (root: string): string => {
  const e = evaluate(root);
  return renderNotices(e.components, e.fingerprint, e.references);
};

export function report(root: string): { violations: Violation[]; notes: string[] } {
  const e = evaluate(root);
  const config = loadLicensesConfig(root);
  const file = path.join(root, config.notices_file);
  const current = existsSync(file) ? readFileSync(file, "utf8") : "";
  const stale = !current.includes(`licenses-fingerprint: ${e.fingerprint} `);
  const notes = [...e.notes];
  if (stale)
    notes.push(
      `NOTE licenses [LIC003] ${config.notices_file} is behind the dependencies: pnpm tsx harness/src/checks/licenses.ts write`,
    );
  return { violations: e.violations, notes };
}

export const run = (root: string): Violation[] => report(root).violations;

export function write(root: string): string {
  const config = loadLicensesConfig(root);
  writeFileSync(path.join(root, config.notices_file), render(root));
  return config.notices_file;
}

export function main(argv: readonly string[], out: (line: string) => void = console.log): number {
  const { positional, flags } = parseArgs(argv);
  const root = findRoot(flag(flags, "root"));
  if (positional[0] === "write") {
    out(`wrote ${write(root)}`);
    return 0;
  }
  const { violations, notes } = report(root);
  const code = finish("licenses", violations, out);
  for (const line of notes) out(line);
  return code;
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
