/**
 * The checks of the REST contract (ADR-0024), run as part of the architecture check:
 * lint of the assembled OpenAPI, the committed document against its parts, breaking changes against the base,
 * the table of routes against the contract and the planned registry, and the server against the contract.
 * Codes API001-API099; see the api-*.ts files for each.
 */
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { git, type Violation, violation } from "../common";
import { type Assembler, checkAssembled, checkKeywords } from "./api-assembled";
import { type BaseReader, checkBreaking, type NewAdr, readDeclarations } from "./api-breaking";
import { type ApiConfig, loadApiConfig } from "./api-config";
import { parseObject } from "./api-json";
import { lintDocument } from "./api-lint";
import { checkRoutes } from "./api-routes";
import { checkServer, type RouteLister, readServed } from "./api-server";
import { runTool } from "./go-run";

/** Everything the checks take from outside the files of the repository, so tests can give fixed answers. */
export interface ApiIo {
  readonly assemble: Assembler;
  readonly listRoutes: RouteLister;
  /** The repository at the base ref; null when there is no base to compare with. */
  readonly base: BaseReader | null;
  /** Remove what the io made (the built program). */
  readonly dispose?: () => void;
}

export function defaultIo(root: string, config: ApiConfig, base: string | undefined): ApiIo {
  const cwd = path.join(root, config.go_root);
  const dir = mkdtempSync(path.join(tmpdir(), "praxium-api-"));
  const program = path.join(dir, "praxium");
  let built: string | null | undefined;
  // One build serves both commands: the first call compiles, the rest only run the program.
  const exec = (args: readonly string[]): { ok: boolean; stdout: string; stderr: string } => {
    if (built === undefined) {
      const build = runTool(cwd, "go", ["build", "-o", program, config.program]);
      built = build.ok ? program : build.stderr || "go is not installed";
    }
    return built === program
      ? runTool(cwd, program, args)
      : { ok: false, stdout: "", stderr: built ?? "" };
  };
  return {
    assemble: () => {
      const result = exec(config.assemble_args);
      return result.ok ? { text: result.stdout } : { error: result.stderr.trim() };
    },
    listRoutes: () => {
      const result = exec(config.routes_args);
      const routes = result.ok ? readServed(result.stdout) : null;
      return routes ? { routes } : { error: (result.stderr || result.stdout).trim() };
    },
    base: base ? gitBase(root, base) : null,
    dispose: () => rmSync(dir, { recursive: true, force: true }),
  };
}

function gitBase(root: string, base: string): BaseReader | null {
  if (git(root, "rev-parse", "--verify", "--quiet", base) === null) return null;
  return {
    read: (file) => git(root, "show", `${base}:${file}`),
    has: (file) => git(root, "cat-file", "-e", `${base}:${file}`) !== null,
  };
}

/** ADR files that are not at the base: the only ones whose declarations count. */
function newAdrs(root: string, config: ApiConfig, base: BaseReader): NewAdr[] {
  const dir = path.join(root, config.adr_dir);
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .filter((name) => name.endsWith(".md") && !base.has(`${config.adr_dir}/${name}`))
    .map((file) => ({ file, text: readFileSync(path.join(dir, file), "utf8") }));
}

export function run(root: string, base?: string, io?: ApiIo): Violation[] {
  const config = loadApiConfig(root);
  if (!config) return [];
  const use = io ?? defaultIo(root, config, base);
  try {
    return runChecks(root, config, use);
  } finally {
    use.dispose?.();
  }
}

function runChecks(root: string, config: ApiConfig, use: ApiIo): Violation[] {
  const out = [...checkAssembled(root, config, use.assemble), ...checkKeywords(root, config)];
  const file = path.join(root, config.openapi);
  const doc = existsSync(file) ? parseObject(readFileSync(file, "utf8")) : null;
  if (!doc) {
    return [
      ...out,
      ...(existsSync(file)
        ? [
            violation(
              "API001",
              config.openapi,
              0,
              "the contract is not a JSON object",
              "Assemble it again from the parts.",
            ),
          ]
        : []),
    ];
  }
  out.push(...lintDocument(doc, config, config.openapi));
  out.push(...checkRoutes(root, config, doc), ...checkServer(config, doc, use.listRoutes));
  if (use.base) {
    const declarations = readDeclarations(config, newAdrs(root, config, use.base));
    out.push(...checkBreaking(config, doc, use.base.read(config.openapi), declarations));
  }
  return out;
}
