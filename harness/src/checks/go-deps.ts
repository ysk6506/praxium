/**
 * The modules of the Go backend (REQ-239, ADR-0019): backend/go.mod is compared with the table in ARCHITECTURE.md
 * and with the `go.third_party` entries of the architecture contract. Adding a module is a decision, and the
 * decision has a licence and a reason.
 *
 * GODEP001 go.mod missing, or its module line differs from the contract   GODEP010 module required but not in the table
 * GODEP011 table lists a module go.mod does not require                   GODEP012 table row without a reason, or a
 * GODEP013 module required but not declared in `go.third_party`           licence that is not on the allowlist
 * GODEP014 `go.third_party` names a module go.mod does not require        GODEP020 replace or exclude directive
 */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { loadYaml, type Violation, violation } from "../common";
import type { GoConfig } from "./go-config";
import type { LicensesConfig } from "./licenses";
import { decide, parseExpr } from "./licenses-spdx";

export interface GoMod {
  readonly module: string | undefined;
  readonly go: string | undefined;
  readonly requires: ReadonlyArray<{ path: string; indirect: boolean; line: number }>;
  /** Lines of `replace` and `exclude` directives. */
  readonly rewrites: readonly number[];
}

const stripComment = (line: string): string => line.replace(/\/\/.*$/, "").trim();

type Requires = Array<{ path: string; indirect: boolean; line: number }>;

/** One directive: a line of a block, or the rest of a single-line directive. */
function directive(
  word: string,
  body: string,
  raw: string,
  line: number,
  into: { requires: Requires; rewrites: number[] },
): void {
  if (word === "require") {
    const modulePath = body.split(/\s+/)[0] ?? "";
    if (modulePath)
      into.requires.push({ path: modulePath, indirect: /\/\/\s*indirect/.test(raw), line });
  } else if (word === "replace" || word === "exclude") {
    into.rewrites.push(line);
  }
}

/** The parts of go.mod the checks need: module, go version, requires (also in blocks) and rewrites. */
export function parseGoMod(text: string): GoMod {
  let module: string | undefined;
  let go: string | undefined;
  const found = { requires: [] as Requires, rewrites: [] as number[] };
  let block: string | undefined;
  text.split("\n").forEach((raw, index) => {
    const line = stripComment(raw);
    if (line === "") return;
    if (block !== undefined) {
      if (line === ")") block = undefined;
      else directive(block, line, raw, index + 1, found);
      return;
    }
    const [word = "", ...rest] = line.split(/\s+/);
    if (word === "module") module = rest[0]?.replace(/^"|"$/g, "");
    else if (word === "go") go = rest[0];
    else if (rest[0] === "(") block = word;
    else directive(word, rest.join(" "), raw, index + 1, found);
  });
  return { module, go, ...found };
}

export interface TableRow {
  readonly module: string;
  readonly license: string;
  readonly reason: string;
  readonly line: number;
}

const cells = (line: string): string[] =>
  line
    .replace(/^\s*\|/, "")
    .replace(/\|\s*$/, "")
    .split("|")
    .map((c) => c.trim());

/** Rows of the table whose header has the columns module, licence and reason (모듈, 라이선스, 까닭). */
export function dependencyRows(text: string): TableRow[] {
  const rows: TableRow[] = [];
  let inTable = false;
  text.split("\n").forEach((raw, index) => {
    if (!raw.trimStart().startsWith("|")) {
      inTable = false;
      return;
    }
    const row = cells(raw);
    if (row[0] === "모듈" && row[1] === "라이선스" && row[2] === "까닭") {
      inTable = true;
      return;
    }
    const module = /^`([^`]+)`$/.exec(row[0] ?? "")?.[1];
    if (inTable && module)
      rows.push({ module, license: row[1] ?? "", reason: row[2] ?? "", line: index + 1 });
  });
  return rows;
}

function licenseProblem(root: string, row: TableRow): string | undefined {
  const config = loadYaml<LicensesConfig>(path.join(root, "harness/config/licenses.yaml"));
  const allowed = new Set([...config.allowed, ...config.allowed_unmodified]);
  const expr = parseExpr(row.license);
  if (expr === undefined) return `licence '${row.license}' is not an SPDX expression`;
  const verdict = decide(expr, (id) => (allowed.has(id) ? id : undefined));
  return verdict.ok
    ? undefined
    : `licence '${row.license}' is not on the allowlist (harness/config/licenses.yaml)`;
}

export function run(root: string, config: GoConfig): Violation[] {
  const modFile = `${config.root}/go.mod`;
  if (!existsSync(path.join(root, modFile))) {
    return [
      violation(
        "GODEP001",
        modFile,
        0,
        "go.mod is missing",
        `Create ${modFile} with \`module ${config.module}\`.`,
      ),
    ];
  }
  const mod = parseGoMod(readFileSync(path.join(root, modFile), "utf8"));
  const out: Violation[] = [];
  if (mod.module !== config.module || mod.go === undefined) {
    out.push(
      violation(
        "GODEP001",
        modFile,
        1,
        `go.mod must start with \`module ${config.module}\` and name a go version (found module '${mod.module ?? "none"}')`,
        "Fix go.mod, or the `go.module` entry of the architecture contract (needs an ADR).",
      ),
    );
  }
  for (const line of mod.rewrites) {
    out.push(
      violation(
        "GODEP020",
        modFile,
        line,
        "replace and exclude directives bend what a module means",
        "Remove it. A module is used as published; write an ADR if a fork is unavoidable.",
      ),
    );
  }
  out.push(...compare(root, config, mod, modFile));
  return out;
}

function compare(root: string, config: GoConfig, mod: GoMod, modFile: string): Violation[] {
  const tableFile = config.dependencyTable;
  const rows = existsSync(path.join(root, tableFile))
    ? dependencyRows(readFileSync(path.join(root, tableFile), "utf8"))
    : [];
  const listed = new Map(rows.map((r) => [r.module, r]));
  const required = new Set(mod.requires.map((r) => r.path));
  const out: Violation[] = [];
  for (const req of mod.requires) {
    if (!listed.has(req.path)) {
      out.push(
        violation(
          "GODEP010",
          modFile,
          req.line,
          `${req.path} is required but not in the dependency table of ${tableFile} (REQ-239)`,
          "Add a row: module, licence (SPDX), the reason it is not written by hand, and the units that use it. Needs an ADR.",
        ),
      );
    }
    if (!config.thirdParty.has(req.path) && !req.indirect) {
      out.push(
        violation(
          "GODEP013",
          modFile,
          req.line,
          `${req.path} is required but not declared in \`go.third_party\` of the architecture contract`,
          "Declare the units that may import it (needs an ADR).",
        ),
      );
    }
  }
  for (const row of rows) {
    if (!required.has(row.module)) {
      out.push(
        violation(
          "GODEP011",
          tableFile,
          row.line,
          `the table lists ${row.module}, which go.mod does not require`,
          "Remove the row, or add the module.",
        ),
      );
    }
    const problem = row.reason === "" ? "the row has no reason" : licenseProblem(root, row);
    if (problem)
      out.push(
        violation(
          "GODEP012",
          tableFile,
          row.line,
          `${row.module}: ${problem}`,
          "Every module needs an allowed licence and a reason.",
        ),
      );
  }
  for (const name of config.thirdParty.keys()) {
    if (!required.has(name)) {
      out.push(
        violation(
          "GODEP014",
          "harness/config/architecture.yaml",
          0,
          `\`go.third_party\` names ${name}, which go.mod does not require`,
          "Remove the entry, or add the module.",
        ),
      );
    }
  }
  return out;
}
