/**
 * Architecture rules for Go code (REQ-021, REQ-001, ADR-0019). The Go units are declared in the `go:` section of
 * harness/config/architecture.yaml. Imports are read from the source (go-lex.ts), so test files, build tags and
 * aliases are all seen.
 *
 * GOARCH001 Go file belongs to no unit                    GOARCH003 unit folder missing
 * GOARCH004 may_import names a unit that does not exist   GOARCH010 unit imports a unit it may not
 * GOARCH011 import reaches inside another unit's package  GOARCH013 production code imports `testing`
 * GOARCH014 import of a package of this module that has no Go files
 * GOARCH020 third-party module undeclared or out of scope GOARCH021 operating-system or network package in a unit
 *                                                         that may not know it
 * GOARCH030 import cycle between units                    GOARCH040 unit folder has no Go file of its own
 * GOARCH041 unit package has no doc comment               GOARCH050 banned construct (unsafe, cgo, plugin,
 *                                                         os.Exit or log.Fatal outside the composition root)
 * GOARCH051 dot import, or blank import outside the composition root     GOARCH052 clock, randomness or environment
 * GOARCH053 `func init`                                   read outside the designated files (REQ-001)
 * GOARCH054 import path written with an escape or with characters a package path never has (ADR-0028)
 * GOARCH055 build constraint on a test file: `//go:build`, `// +build`, an OS or architecture file suffix (ADR-0028)
 * GOARCH005 test_may_import repeats what may_import already allows (ADR-0028)
 * GOARCH006 restricted_stdlib or locked_support names a unit or file that does not exist (ADR-0028)
 *
 * GOARCH010 reads `may_import` for production files and `may_import` + `test_may_import` for `_test.go` files.
 */
import { existsSync, statSync } from "node:fs";
import path from "node:path";
import { type Violation, violation } from "../common";
import { allowedBy } from "./arch-model";
import {
  type GoConfig,
  type GoFile,
  type GoUnit,
  goFiles,
  importKind,
  moduleDir,
  unitOfPath,
} from "./go-config";
import { selectorsOf } from "./go-lex";
import { findCycles } from "./import-graph";

const HINT =
  "If the rule itself is wrong, write an ADR and change the `go:` section of harness/config/architecture.yaml; " +
  "otherwise change the code.";

/** Packages and calls that make a run depend on the outside world (REQ-001). */
const NONDETERMINISTIC_IMPORTS = new Set(["math/rand", "math/rand/v2", "crypto/rand"]);
const NONDETERMINISTIC_CALLS = new Set([
  "time.Now",
  "time.Since",
  "time.Until",
  "os.Getenv",
  "os.LookupEnv",
  "os.Environ",
]);
const EXIT_CALLS = new Set([
  "os.Exit",
  "log.Fatal",
  "log.Fatalf",
  "log.Fatalln",
  "log.Panic",
  "log.Panicf",
  "log.Panicln",
]);
const BANNED_IMPORTS = new Set(["unsafe", "plugin", "C"]);
/** What a package path is made of. A backslash, a space or anything else means an escape or a trick. */
const PLAIN_PATH = /^[A-Za-z0-9._~+/-]+$/;

/** Operating systems and architectures that Go reads from the end of a file name as a build constraint. */
const GOOS = new Set(
  "aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos".split(
    " ",
  ),
);
const GOARCH = new Set(
  "386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm".split(
    " ",
  ),
);
const CONSTRAINT_LINE = /^\/\/\s*(go:build\b|\+build\b)/;

export function checkStructure(
  root: string,
  config: GoConfig,
  files: readonly GoFile[],
): Violation[] {
  const out: Violation[] = [];
  const names = new Set(config.units.map((u) => u.name));
  for (const unit of config.units) {
    const full = path.join(root, unit.path);
    if (!existsSync(full) || !statSync(full).isDirectory()) {
      out.push(
        violation(
          "GOARCH003",
          "harness/config/architecture.yaml",
          0,
          `Go unit '${unit.name}' points at missing folder ${unit.path}`,
          `Create the folder or remove the unit. ${HINT}`,
        ),
      );
    }
    const listed = [...unit.mayImport, ...unit.testMayImport];
    for (const other of listed.filter((n) => !n.endsWith(".*") && !names.has(n))) {
      out.push(
        violation(
          "GOARCH004",
          "harness/config/architecture.yaml",
          0,
          `Go unit '${unit.name}' lists '${other}' as importable, but it is not a Go unit`,
          `Fix the name. ${HINT}`,
        ),
      );
    }
    for (const other of unit.testMayImport.filter((n) => allowedBy(unit.mayImport, n))) {
      out.push(
        violation(
          "GOARCH005",
          "harness/config/architecture.yaml",
          0,
          `Go unit '${unit.name}' lists '${other}' in test_may_import, but may_import already allows it`,
          `Remove it from test_may_import: that list is for what only tests may import. ${HINT}`,
        ),
      );
    }
    out.push(...checkUnitRoot(unit, files));
  }
  out.push(...checkNamedScopes(root, config, names));
  for (const file of files.filter((f) => !f.unit)) {
    out.push(
      violation(
        "GOARCH001",
        file.path,
        1,
        "Go file belongs to no unit",
        `Move it into a unit folder (see ARCHITECTURE.md) or register the unit. ${HINT}`,
      ),
    );
  }
  return out;
}

/** Units and files that `restricted_stdlib` and `locked_support` name must exist. */
function checkNamedScopes(root: string, config: GoConfig, names: ReadonlySet<string>): Violation[] {
  const out: Violation[] = [];
  const missing = (what: string, fix: string): number =>
    out.push(violation("GOARCH006", "harness/config/architecture.yaml", 0, what, `${fix} ${HINT}`));
  for (const [pkg, scope] of config.restrictedStdlib) {
    for (const entry of scope) {
      if (isFileEntry(entry)) {
        if (!existsSync(path.join(root, entry)))
          missing(
            `restricted_stdlib '${pkg}' names file ${entry}, which does not exist`,
            "Fix the path.",
          );
      } else if (!entry.endsWith(".*") && !names.has(entry)) {
        missing(
          `restricted_stdlib '${pkg}' names '${entry}', which is not a Go unit`,
          "Fix the name.",
        );
      }
    }
  }
  for (const entry of config.lockedSupport) {
    if (!existsSync(path.join(root, entry)))
      missing(
        `locked_support names ${entry}, which does not exist`,
        "Fix the path or remove the entry.",
      );
  }
  return out;
}

/** A restricted_stdlib entry that ends in `.go` is one file, anything else a unit name or pattern. */
const isFileEntry = (entry: string): boolean => entry.endsWith(".go");

/** A unit is a package: it needs Go code in its own folder, and a doc comment saying what it is for. */
function checkUnitRoot(unit: GoUnit, files: readonly GoFile[]): Violation[] {
  const own = files.filter((f) => path.posix.dirname(f.path) === unit.path && !f.isTest);
  if (own.length === 0) {
    const anyHere = files.some((f) => f.unit === unit);
    return anyHere
      ? [
          violation(
            "GOARCH040",
            unit.path,
            0,
            `unit '${unit.name}' has no Go file directly in its folder`,
            "A unit is one package: put its public surface in the unit's own folder, and sub-packages below it.",
          ),
        ]
      : [];
  }
  const documented = (f: GoFile): boolean =>
    f.pkg?.name === "main"
      ? f.pkg.doc.startsWith("Command ")
      : (f.pkg?.doc.startsWith(`Package ${f.pkg.name} `) ?? false);
  if (own.some(documented)) return [];
  return [
    violation(
      "GOARCH041",
      own[0]?.path ?? unit.path,
      1,
      `package of unit '${unit.name}' has no doc comment`,
      "Write `// Package <name> ...` (`// Command <name> ...` for main) right above the package clause, in doc.go: say what the unit is for.",
    ),
  ];
}

interface Site {
  readonly file: GoFile;
  readonly unit: GoUnit;
  readonly line: number;
}

function internalImport(
  root: string,
  config: GoConfig,
  site: Site,
  importPath: string,
): Violation[] {
  const dir = moduleDir(config, importPath) as string;
  const target = unitOfPath(config, dir);
  const full = path.join(root, dir);
  if (!existsSync(full) || !statSync(full).isDirectory()) {
    return [
      violation(
        "GOARCH014",
        site.file.path,
        site.line,
        `cannot find package '${importPath}' in this module`,
        "Fix the import path.",
      ),
    ];
  }
  if (!target || target === site.unit) return [];
  const out: Violation[] = [];
  const allowed =
    allowedBy(site.unit.mayImport, target.name) ||
    (site.file.isTest && allowedBy(site.unit.testMayImport, target.name));
  if (!allowed) {
    const onlyTests = allowedBy(site.unit.testMayImport, target.name);
    out.push(
      violation(
        "GOARCH010",
        site.file.path,
        site.line,
        onlyTests
          ? `${site.unit.name} may import ${target.name} in test files only (imports ${importPath} from production code)`
          : `${site.unit.name} must not import ${target.name} (imports ${importPath})`,
        `Depend downward only: move the shared code to a lower layer, or define a port (interface) lower down. ${HINT}`,
      ),
    );
  }
  if (dir !== target.path) {
    out.push(
      violation(
        "GOARCH011",
        site.file.path,
        site.line,
        `import of ${importPath} reaches inside unit '${target.name}'`,
        `Import the unit's package (${target.path}) and export the name there.`,
      ),
    );
  }
  return out;
}

function moduleImport(config: GoConfig, site: Site, importPath: string): Violation[] {
  const name = [...config.thirdParty.keys()].find(
    (m) => importPath === m || importPath.startsWith(`${m}/`),
  );
  const scope = name === undefined ? undefined : config.thirdParty.get(name);
  if (scope === undefined) {
    return [
      violation(
        "GOARCH020",
        site.file.path,
        site.line,
        `module for '${importPath}' is not declared in the architecture contract`,
        `New dependencies are decisions: write an ADR, add the module to \`go.third_party\` and to the table in ARCHITECTURE.md. ${HINT}`,
      ),
    ];
  }
  if (allowedBy(scope, site.unit.name)) return [];
  return [
    violation(
      "GOARCH020",
      site.file.path,
      site.line,
      `'${name}' may only be imported from: ${scope.join(", ") || "nowhere"} (found in ${site.unit.name})`,
      `Wrap it behind a port and implement the port in an allowed unit. ${HINT}`,
    ),
  ];
}

/** A file entry of the scope allows that one file; any other entry is a unit name or pattern. */
function stdlibAllowed(scope: readonly string[], site: Site): boolean {
  const units = scope.filter((entry) => !isFileEntry(entry));
  return allowedBy(units, site.unit.name) || scope.includes(site.file.path);
}

function stdlibImport(config: GoConfig, site: Site, importPath: string): Violation[] {
  const out: Violation[] = [];
  const scope = config.restrictedStdlib.get(importPath);
  if (scope && !stdlibAllowed(scope, site)) {
    out.push(
      violation(
        "GOARCH021",
        site.file.path,
        site.line,
        `'${importPath}' in ${site.file.path}: only ${scope.join(", ")} may know the operating system or the network`,
        `Put it behind a port defined in the core and implement the port in an outer unit. ${HINT}`,
      ),
    );
  }
  if (!site.file.isTest && (importPath === "testing" || importPath.startsWith("testing/"))) {
    out.push(
      violation(
        "GOARCH013",
        site.file.path,
        site.line,
        `production code imports '${importPath}'`,
        "Test helpers belong in _test.go files.",
      ),
    );
  }
  return out;
}

export function checkImports(
  root: string,
  config: GoConfig,
  files: readonly GoFile[],
): { violations: Violation[]; graph: Map<string, Set<string>> } {
  const violations: Violation[] = [];
  const graph = new Map<string, Set<string>>(config.units.map((u) => [u.name, new Set()]));
  for (const file of files) {
    if (!file.unit) continue;
    for (const imp of file.imports) {
      const site: Site = { file, unit: file.unit, line: imp.line };
      const kind = importKind(config, imp.path);
      if (kind === "internal") {
        violations.push(...internalImport(root, config, site, imp.path));
        const target = unitOfPath(config, moduleDir(config, imp.path) as string);
        if (target && target !== file.unit) graph.get(file.unit.name)?.add(target.name);
      } else if (kind === "module") {
        violations.push(...moduleImport(config, site, imp.path));
      } else {
        violations.push(...stdlibImport(config, site, imp.path));
      }
    }
  }
  return { violations, graph };
}

export function checkCycles(graph: Map<string, Set<string>>): Violation[] {
  return findCycles(graph).map((cycle) =>
    violation(
      "GOARCH030",
      "harness/config/architecture.yaml",
      0,
      `import cycle between units: ${cycle.join(" -> ")}`,
      "Break the cycle: move the shared piece into its own unit, or depend on an interface.",
    ),
  );
}

const designated = (config: GoConfig, file: string): boolean =>
  config.nondeterminismAllowed.some((entry) =>
    entry.endsWith("/") ? file.startsWith(entry) : file === entry,
  );

function importProblems(config: GoConfig, file: GoFile): Violation[] {
  const out: Violation[] = [];
  const inRoot = file.unit?.name === config.compositionRoot;
  for (const imp of file.imports) {
    const at = (code: string, message: string, fix: string): number =>
      out.push(violation(code, file.path, imp.line, message, fix));
    if (!PLAIN_PATH.test(imp.path))
      at(
        "GOARCH054",
        `import path ${JSON.stringify(imp.path)} is not written plainly`,
        "Write the import path as plain text: an escape such as \\x73 hides which package it is from every import rule.",
      );
    if (BANNED_IMPORTS.has(imp.path))
      at(
        "GOARCH050",
        `import of '${imp.path}'`,
        "Memory-unsafe, cgo and plugin code is not allowed; use the standard library's safe packages.",
      );
    if (imp.alias === ".")
      at(
        "GOARCH051",
        `dot import of '${imp.path}' hides where names come from`,
        "Import the package by name.",
      );
    if (imp.alias === "_" && !inRoot && imp.path !== "embed")
      at(
        "GOARCH051",
        `blank import of '${imp.path}' outside the composition root`,
        `Only ${config.compositionRoot} may import a package for its side effects.`,
      );
    if (NONDETERMINISTIC_IMPORTS.has(imp.path) && !designated(config, file.path))
      at(
        "GOARCH052",
        `import of '${imp.path}' makes results irreproducible (REQ-001)`,
        "Take a kernel.Rng as a parameter instead; only the kernel's entropy.go reads system randomness.",
      );
  }
  return out;
}

function callProblems(config: GoConfig, file: GoFile): Violation[] {
  const out: Violation[] = [];
  const inRoot = file.unit?.name === config.compositionRoot;
  for (const use of selectorsOf(file.code, file.imports)) {
    if (NONDETERMINISTIC_CALLS.has(use.qualified) && !designated(config, file.path)) {
      out.push(
        violation(
          "GOARCH052",
          file.path,
          use.line,
          `'${use.qualified}' makes results irreproducible (REQ-001)`,
          "Take a kernel.Clock (or the value itself) as a parameter instead; only the kernel's entropy.go reads the clock and the environment.",
        ),
      );
    }
    if (EXIT_CALLS.has(use.qualified) && !inRoot) {
      out.push(
        violation(
          "GOARCH050",
          file.path,
          use.line,
          `'${use.qualified}' ends the process from inside a unit`,
          "Return an error; only the composition root decides to exit.",
        ),
      );
    }
  }
  return out;
}

function initProblems(file: GoFile): Violation[] {
  const out: Violation[] = [];
  file.code.forEach((tok, k) => {
    const next = file.code[k + 1];
    const startsLine = tok.line > (file.code[k - 1]?.endLine ?? 0);
    if (
      tok.text === "func" &&
      startsLine &&
      next?.text === "init" &&
      file.code[k + 2]?.text === "("
    ) {
      out.push(
        violation(
          "GOARCH053",
          file.path,
          tok.line,
          "func init() wires behaviour in without anyone calling it",
          "Do the work in an explicit constructor that the composition root calls.",
        ),
      );
    }
  });
  return out;
}

/**
 * A test file that only some builds compile is counted by the lock and the tracer and never runs on the rest. Test
 * files take no build constraint: neither a `//go:build` or `// +build` line before the package clause, nor an
 * operating-system or architecture suffix in the file name (`x_linux_test.go`, `x_arm64_test.go`).
 */
function constraintProblems(file: GoFile): Violation[] {
  if (!file.isTest) return [];
  const out: Violation[] = [];
  const fix =
    "A test file must run everywhere: remove the constraint, or put the platform-specific part in production code behind an interface.";
  const clause = file.pkg?.line ?? Number.POSITIVE_INFINITY;
  for (const tok of file.toks) {
    if (tok.kind === "comment" && tok.line < clause && CONSTRAINT_LINE.test(tok.text))
      out.push(violation("GOARCH055", file.path, tok.line, "build constraint on a test file", fix));
  }
  const parts = path.posix
    .basename(file.path)
    .replace(/_test\.go$/, "")
    .split("_");
  const last = parts.at(-1) ?? "";
  if (parts.length > 1 && (GOOS.has(last) || GOARCH.has(last)))
    out.push(
      violation(
        "GOARCH055",
        file.path,
        1,
        `the file name ends in '_${last}', which is a build constraint on a test file`,
        fix,
      ),
    );
  return out;
}

export function checkConstructs(config: GoConfig, files: readonly GoFile[]): Violation[] {
  return files.flatMap((file) => [
    ...importProblems(config, file),
    ...callProblems(config, file),
    ...initProblems(file),
    ...constraintProblems(file),
  ]);
}

export function run(root: string, config: GoConfig): Violation[] {
  const files = goFiles(root, config);
  const { violations, graph } = checkImports(root, config, files);
  return [
    ...checkStructure(root, config, files),
    ...violations,
    ...checkCycles(graph),
    ...checkConstructs(config, files),
  ];
}
