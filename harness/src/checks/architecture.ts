/**
 * Architecture enforcement (REQ-021, ADR-0011). Rule codes are stable; tests and ADRs cite them.
 *
 * ARCH001 file under a source root belongs to no unit      ARCH003 unit folder missing
 * ARCH010 unit imports a unit it may not                   ARCH011 import bypasses a unit's index.ts
 * ARCH012 sibling units import each other                  ARCH013 production code imports test-support
 * ARCH014 import cannot be resolved
 * ARCH020 third-party package undeclared or out of scope   ARCH021 Node built-in in browser-safe code
 * ARCH022 test-only package in production code
 * ARCH030 import cycle                                     ARCH040 unit has no index.ts
 * ARCH050 banned construct (eval, new Function, debugger)  ARCH051 `export *` or @ts-expect-error/@ts-nocheck
 * ARCH052 nondeterminism (Date.now, Math.random, ...) outside allowed paths
 */
import { existsSync, statSync } from "node:fs";
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  isMain,
  parseArgs,
  readText,
  type Violation,
  violation,
} from "../common";
import { isSourceFile, isTestFile, listFiles } from "../files";
import { comments, dotted, lineOf, moduleRefs, parse, ts, walk } from "../ts-ast";
import {
  type ArchConfig,
  allowedBy,
  CONFIG_PATH,
  isUnitIndex,
  loadArchConfig,
  resolveImport,
  type UnitSpec,
  unitOf,
} from "./arch-model";
import { findCycles } from "./import-graph";

const CONFIG_HINT =
  "If the rule itself is wrong, write an ADR (docs/design-docs/adr/) and change " +
  "harness/config/architecture.yaml; otherwise change the code.";

const NONDETERMINISTIC_CALLS = new Set([
  "Date.now",
  "Math.random",
  "performance.now",
  "crypto.randomUUID",
  "crypto.getRandomValues",
  "process.hrtime",
  "process.hrtime.bigint",
]);

export function checkStructure(
  root: string,
  config: ArchConfig,
  files: readonly string[],
): Violation[] {
  const out: Violation[] = [];
  for (const unit of config.units) {
    const full = path.join(root, unit.path);
    if (!existsSync(full) || !statSync(full).isDirectory()) {
      out.push(
        violation(
          "ARCH003",
          CONFIG_PATH,
          0,
          `unit '${unit.name}' points at missing folder ${unit.path}`,
          `Create the folder or remove the unit. ${CONFIG_HINT}`,
        ),
      );
    } else if (
      unit.publicIndex &&
      !existsSync(path.join(full, "index.ts")) &&
      !existsSync(path.join(full, "index.tsx"))
    ) {
      out.push(
        violation(
          "ARCH040",
          unit.path,
          0,
          `unit '${unit.name}' has no index.ts`,
          "Add index.ts exporting the unit's public API; other units import only from it.",
        ),
      );
    }
  }
  for (const file of files) {
    if (!unitOf(config, file) && !config.ignoredFiles.includes(file)) {
      out.push(
        violation(
          "ARCH001",
          file,
          1,
          "file belongs to no unit",
          `Move it into a unit folder (see ARCHITECTURE.md). ${CONFIG_HINT}`,
        ),
      );
    }
  }
  return out;
}

interface ImportContext {
  readonly root: string;
  readonly config: ArchConfig;
  readonly file: string;
  readonly unit: UnitSpec;
  readonly isTest: boolean;
}

/** The closest unit whose folder contains this unit's folder, if any. */
function parentUnit(config: ArchConfig, unit: UnitSpec): UnitSpec | undefined {
  return config.units
    .filter((u) => u !== unit && unit.path.startsWith(`${u.path}/`))
    .sort((a, b) => b.path.length - a.path.length)[0];
}

function checkInternal(ctx: ImportContext, line: number, target: string): Violation[] {
  const targetUnit = unitOf(ctx.config, target);
  if (!targetUnit) return [];
  const out: Violation[] = [];
  const isSupport = target.includes("/test-support/");
  if (isSupport && !ctx.isTest) {
    out.push(
      violation(
        "ARCH013",
        ctx.file,
        line,
        `production code imports test helper ${target}`,
        "Test support is for tests only. Move what production needs into the unit's real code.",
      ),
    );
  }
  if (targetUnit.name === ctx.unit.name) return out;
  if (!allowedBy(ctx.unit.mayImport, targetUnit.name)) {
    const parent = parentUnit(ctx.config, ctx.unit);
    const siblings = parent !== undefined && parent === parentUnit(ctx.config, targetUnit);
    out.push(
      violation(
        siblings ? "ARCH012" : "ARCH010",
        ctx.file,
        line,
        `${ctx.unit.name} must not import ${targetUnit.name} (imports ${target})`,
        siblings
          ? "Sibling units stay independent. Move shared code into their common parent unit or a lower layer."
          : `Depend downward only: move the shared code to a lower layer, or define a port (interface) lower down. ${CONFIG_HINT}`,
      ),
    );
  }
  if (!isSupport && !isUnitIndex(targetUnit, target)) {
    out.push(
      violation(
        "ARCH011",
        ctx.file,
        line,
        `import of ${target} reaches inside unit '${targetUnit.name}'`,
        `Import from the unit's index (${targetUnit.path}/index.ts or its @praxium/core alias) and export the name there.`,
      ),
    );
  }
  return out;
}

function checkExternal(
  ctx: ImportContext,
  line: number,
  kind: "builtin" | "package",
  name: string,
): Violation[] {
  if (kind === "builtin") {
    return allowedBy(ctx.config.nodeBuiltins, ctx.unit.name)
      ? []
      : [
          violation(
            "ARCH021",
            ctx.file,
            line,
            `Node built-in '${name}' in ${ctx.unit.name}`,
            "core and web run in browsers too. Put platform code in apps/server behind a port defined in core.",
          ),
        ];
  }
  if (ctx.config.testOnly.includes(name)) {
    return ctx.isTest
      ? []
      : [
          violation(
            "ARCH022",
            ctx.file,
            line,
            `test-only package '${name}' in production code`,
            "Only *.test.ts files and test-support/ may use it.",
          ),
        ];
  }
  const scope = ctx.config.thirdParty.get(name);
  if (scope === undefined) {
    return [
      violation(
        "ARCH020",
        ctx.file,
        line,
        `package '${name}' is not declared in the architecture contract`,
        `New dependencies are decisions: write an ADR and add '${name}' with its allowed units. ${CONFIG_HINT}`,
      ),
    ];
  }
  if (!allowedBy(scope, ctx.unit.name)) {
    return [
      violation(
        "ARCH020",
        ctx.file,
        line,
        `'${name}' may only be imported from: ${scope.join(", ") || "nowhere"} (found in ${ctx.unit.name})`,
        `Wrap it behind a port and implement the port in an allowed unit. ${CONFIG_HINT}`,
      ),
    ];
  }
  return [];
}

export interface ImportScan {
  readonly violations: Violation[];
  readonly graph: Map<string, Set<string>>;
}

export function checkImports(
  root: string,
  config: ArchConfig,
  files: readonly string[],
): ImportScan {
  const violations: Violation[] = [];
  const graph = new Map<string, Set<string>>();
  for (const file of files) {
    const unit = unitOf(config, file);
    graph.set(file, new Set());
    if (!unit) continue;
    const ctx: ImportContext = { root, config, file, unit, isTest: isTestFile(file) };
    const sf = parse(file, readText(path.join(root, file)));
    for (const ref of moduleRefs(sf)) {
      const resolved = resolveImport(root, config, file, ref.specifier);
      if (resolved.kind === "internal") {
        if (!ref.typeOnly) graph.get(file)?.add(resolved.file);
        violations.push(...checkInternal(ctx, ref.line, resolved.file));
      } else if (resolved.kind === "unresolved") {
        violations.push(
          violation(
            "ARCH014",
            file,
            ref.line,
            `cannot resolve '${ref.specifier}'`,
            "Fix the path. Imports are extensionless and resolve to .ts, .tsx or a folder index.ts.",
          ),
        );
      } else {
        violations.push(...checkExternal(ctx, ref.line, resolved.kind, resolved.name));
      }
    }
  }
  return { violations, graph };
}

export function checkCycles(graph: Map<string, Set<string>>): Violation[] {
  return findCycles(graph).map((cycle) =>
    violation(
      "ARCH030",
      cycle[0] as string,
      1,
      `import cycle: ${cycle.join(" -> ")}`,
      "Break the cycle: move the shared piece into its own module, or depend on an interface.",
    ),
  );
}

function nondeterminismAllowed(config: ArchConfig, file: string): boolean {
  return config.nondeterminismAllowed.some((prefix) => file === prefix || file.startsWith(prefix));
}

function bannedCode(file: string, line: number, node: ts.Node): Violation | undefined {
  if (node.kind === ts.SyntaxKind.DebuggerStatement)
    return violation("ARCH050", file, line, "debugger statement", "Remove it.");
  if (!ts.isCallExpression(node) && !ts.isNewExpression(node)) return undefined;
  const name = dotted(node.expression);
  if (name === "eval" || (name === "Function" && ts.isNewExpression(node))) {
    return violation(
      "ARCH050",
      file,
      line,
      `'${name}' executes code from strings`,
      "Use real functions or a parser.",
    );
  }
  return undefined;
}

function nondeterminism(file: string, line: number, node: ts.Node): Violation | undefined {
  if (ts.isCallExpression(node) && NONDETERMINISTIC_CALLS.has(dotted(node.expression))) {
    return violation(
      "ARCH052",
      file,
      line,
      `'${dotted(node.expression)}()' makes results irreproducible (REQ-001)`,
      "Take a Clock or Rng from @praxium/core/kernel as a parameter instead.",
    );
  }
  if (
    ts.isNewExpression(node) &&
    dotted(node.expression) === "Date" &&
    (node.arguments?.length ?? 0) === 0
  ) {
    return violation(
      "ARCH052",
      file,
      line,
      "'new Date()' reads the wall clock (REQ-001)",
      "Take a Clock from @praxium/core/kernel as a parameter instead.",
    );
  }
  return undefined;
}

function constructViolation(
  file: string,
  sf: ts.SourceFile,
  node: ts.Node,
  deterministic: boolean,
): Violation | undefined {
  const line = lineOf(sf, node.getStart(sf));
  if (ts.isExportDeclaration(node) && node.moduleSpecifier && !node.exportClause) {
    return violation(
      "ARCH051",
      file,
      line,
      "'export *' hides what a module's public API is",
      "Export the names explicitly.",
    );
  }
  return (
    bannedCode(file, line, node) ?? (deterministic ? nondeterminism(file, line, node) : undefined)
  );
}

export function checkConstructs(
  root: string,
  config: ArchConfig,
  files: readonly string[],
): Violation[] {
  const out: Violation[] = [];
  for (const file of files) {
    if (!file.startsWith("packages/") && !file.startsWith("apps/")) continue;
    const text = readText(path.join(root, file));
    const sf = parse(file, text);
    const deterministic = !nondeterminismAllowed(config, file);
    walk(sf, (node) => {
      const found = constructViolation(file, sf, node, deterministic);
      if (found) out.push(found);
    });
    for (const comment of comments(file, text)) {
      if (/@ts-(ignore|nocheck)\b/.test(comment.text)) {
        out.push(
          violation(
            "ARCH051",
            file,
            comment.line,
            "@ts-ignore / @ts-nocheck silences the type checker",
            "Fix the type error, or use @ts-expect-error with a reason in a test.",
          ),
        );
      }
    }
  }
  return out;
}

export function sourceFiles(root: string, config: ArchConfig): string[] {
  return listFiles(root, config.sourceRoots, isSourceFile);
}

export function run(root: string): Violation[] {
  const config = loadArchConfig(root);
  const files = sourceFiles(root, config);
  const { violations, graph } = checkImports(root, config, files);
  return [
    ...checkStructure(root, config, files),
    ...violations,
    ...checkCycles(graph),
    ...checkConstructs(root, config, files),
  ];
}

export function main(argv: readonly string[]): number {
  const { flags } = parseArgs(argv);
  return finish("architecture", run(findRoot(flag(flags, "root"))));
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
