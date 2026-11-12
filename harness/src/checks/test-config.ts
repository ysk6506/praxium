/**
 * Rules on the configuration that decides which tests run and what coverage counts (ADR-0004).
 *
 * TCFG001 test script or forbidden vitest option   TCFG002 test include list changed
 * TCFG003 coverage include/exclude changed          TCFG004 extra test configuration file
 */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { type Violation, violation } from "../common";
import { listFiles } from "../files";
import { parse, ts, walk } from "../ts-ast";

export interface TestConfigRules {
  readonly vitest_config: string;
  readonly test_script: string;
  readonly vitest_include: readonly string[];
  readonly coverage_include: readonly string[];
  readonly coverage_exclude_allowed: readonly string[];
  readonly forbidden_test_options: readonly string[];
  /** `setupFiles` is forbidden unless it lists exactly these files (ADR-0140). */
  readonly allowed_setup_files?: readonly string[];
}

/** The object literal assigned to property `name` anywhere in the file. */
function findObject(sf: ts.SourceFile, name: string): ts.ObjectLiteralExpression | undefined {
  let found: ts.ObjectLiteralExpression | undefined;
  walk(sf, (node) => {
    if (
      !found &&
      ts.isPropertyAssignment(node) &&
      node.name.getText(sf) === name &&
      ts.isObjectLiteralExpression(node.initializer)
    ) {
      found = node.initializer;
    }
  });
  return found;
}

function property(
  obj: ts.ObjectLiteralExpression | undefined,
  name: string,
): ts.Expression | undefined {
  const prop = obj?.properties.find((p) => ts.isPropertyAssignment(p) && p.name.getText() === name);
  return prop && ts.isPropertyAssignment(prop) ? prop.initializer : undefined;
}

/** Every assignment of `name` in the object literal (a repeated key counts each time). */
function properties(obj: ts.ObjectLiteralExpression | undefined, name: string): ts.Expression[] {
  const found: ts.Expression[] = [];
  for (const p of obj?.properties ?? [])
    if (ts.isPropertyAssignment(p) && p.name.getText() === name) found.push(p.initializer);
  return found;
}

function stringArray(node: ts.Expression | undefined): string[] | undefined {
  if (!node || !ts.isArrayLiteralExpression(node)) return undefined;
  const values = node.elements.map((e) => (ts.isStringLiteralLike(e) ? e.text : undefined));
  return values.every((v) => v !== undefined) ? (values as string[]) : undefined;
}

const sameSet = (a: readonly string[], b: readonly string[]) =>
  a.length === b.length && [...a].sort().join("\n") === [...b].sort().join("\n");

function checkVitestConfig(root: string, rules: TestConfigRules): Violation[] {
  const file = rules.vitest_config;
  if (!existsSync(path.join(root, file)))
    return [violation("TCFG004", file, 0, "vitest config is missing", "Restore it from git.")];
  const sf = parse(file, readFileSync(path.join(root, file), "utf8"));
  const testObj = findObject(sf, "test");
  const coverageObj = findObject(sf, "coverage");
  const out: Violation[] = [];
  for (const option of rules.forbidden_test_options) {
    const found = properties(testObj, option);
    const allowed = option === "setupFiles" ? (rules.allowed_setup_files ?? []) : undefined;
    const permitted = (node: ts.Expression): boolean => {
      const list = stringArray(node);
      return !!allowed && !!list && sameSet(list, allowed);
    };
    if (found.some((node) => !permitted(node))) {
      out.push(
        violation(
          "TCFG001",
          file,
          0,
          `vitest option '${option}' changes which tests run or what counts as passing`,
          "Remove it. Every test runs on every run.",
        ),
      );
    }
  }
  const include = stringArray(property(testObj, "include"));
  if (!include || !sameSet(include, rules.vitest_include)) {
    out.push(
      violation(
        "TCFG002",
        file,
        0,
        "test.include differs from the contract",
        `Use exactly: ${rules.vitest_include.join(", ")}`,
      ),
    );
  }
  return [...out, ...checkCoverageOptions(file, coverageObj, rules)];
}

function checkCoverageOptions(
  file: string,
  coverageObj: ts.ObjectLiteralExpression | undefined,
  rules: TestConfigRules,
): Violation[] {
  const out: Violation[] = [];
  const covInclude = stringArray(property(coverageObj, "include"));
  if (!covInclude || !sameSet(covInclude, rules.coverage_include)) {
    out.push(
      violation(
        "TCFG003",
        file,
        0,
        "coverage.include differs from the contract",
        `Use exactly: ${rules.coverage_include.join(", ")}`,
      ),
    );
  }
  const rawExclude = property(coverageObj, "exclude");
  const covExclude = stringArray(rawExclude);
  const extra = (covExclude ?? []).filter((e) => !rules.coverage_exclude_allowed.includes(e));
  if (extra.length > 0 || (rawExclude && !covExclude)) {
    out.push(
      violation(
        "TCFG003",
        file,
        0,
        `coverage.exclude hides code: ${extra.join(", ") || "(computed value)"}`,
        "Cover the code or delete it; only test files may be excluded.",
      ),
    );
  }
  return out;
}

function checkScript(root: string, rules: TestConfigRules): Violation[] {
  const pkg = JSON.parse(readFileSync(path.join(root, "package.json"), "utf8")) as {
    scripts?: Record<string, string>;
  };
  const script = pkg.scripts?.test;
  return script === rules.test_script
    ? []
    : [
        violation(
          "TCFG001",
          "package.json",
          0,
          `scripts.test is '${script ?? ""}'`,
          `It must be exactly '${rules.test_script}'.`,
        ),
      ];
}

function checkStrayConfigs(root: string, rules: TestConfigRules): Violation[] {
  const out: Violation[] = [];
  for (const file of listFiles(
    root,
    ["."],
    (name) =>
      /^(vitest|vite)\.(config|workspace)\.[cm]?[jt]s$/.test(name) ||
      /^vitest\.workspace/.test(name),
  )) {
    const clean = file.replace(/^\.\//, "");
    if (clean === rules.vitest_config) continue;
    const sf = parse(clean, readFileSync(path.join(root, clean), "utf8"));
    if (clean.includes("vitest") || findObject(sf, "test")) {
      out.push(
        violation(
          "TCFG004",
          clean,
          0,
          "extra test configuration",
          `All test configuration lives in ${rules.vitest_config} (it is protected).`,
        ),
      );
    }
  }
  return out;
}

export function configViolations(root: string, rules: TestConfigRules): Violation[] {
  return [
    ...checkVitestConfig(root, rules),
    ...checkScript(root, rules),
    ...checkStrayConfigs(root, rules),
  ];
}
