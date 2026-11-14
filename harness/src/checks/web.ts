/**
 * Rules of the web UI in apps/web (ADR-0035, REQ-276, REQ-277). Rule codes are stable; tests and the ADR cite them.
 *
 * WEB001 a dependency of apps/web is not on the allow-list of harness/config/web.yaml
 * WEB010 a generated API file was made from another contract than the one in the repository
 * WEB011 a generated API file was edited by hand (its body does not match its recorded hash)
 * WEB012 a generated API file has no header, or the generated folder is missing
 * WEB013 the generator, run again, does not produce the files on disk
 * WEB020 a color literal (#hex, rgb(), hsl(), oklch() and the like) outside the token file
 * WEB021 a Tailwind color that is not a token: an arbitrary value, a default palette name, white or black
 * WEB030 the placeholder page of the embed folder is missing
 * WEB031 Go code or a module file names apps/web: the backend must build without it
 * WEB032 git does not ignore the real build output in the embed folder (or ignores the placeholder)
 * WEB040 the contrast of a pair of tokens is too low (web-colors.ts)
 * WEB041 an unsafe sink; WEB042 an absolute address; WEB043 the focus outline removed; WEB044 motion that does not
 *        stop for prefers-reduced-motion; WEB045 the build is over its size budget (web-rules.ts)
 *
 * That `@base-ui/react` is imported by the ui layer alone is ARCH020 (architecture.yaml, third_party).
 */
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  isMain,
  loadYaml,
  parseArgs,
  readText,
  type Violation,
  violation,
} from "../common";
import { budgetViolations, rulesViolations, type WebRules } from "./web-rules";

const CONFIG_PATH = "harness/config/web.yaml";

export interface WebConfig {
  readonly package: string;
  readonly dependencies: readonly string[];
  readonly dev_dependencies: readonly string[];
  readonly token_file: string;
  readonly scan: readonly string[];
  /** The rules of docs/design-docs/web-ui-rules.md that a machine checks (ADR-0045); see web-rules.ts. */
  readonly rules?: WebRules;
  readonly contract: string;
  readonly generated_dir: string;
  readonly generator_script: string;
  readonly backend: {
    readonly root: string;
    readonly embed_dir: string;
    readonly placeholder: string;
    readonly forbidden_reference: string;
    readonly gitignore: string;
    readonly ignore_lines: readonly string[];
  };
}

export const loadWebConfig = (root: string): WebConfig =>
  loadYaml<WebConfig>(path.join(root, CONFIG_PATH));

const sha256 = (text: string): string => createHash("sha256").update(text).digest("hex");

const DEPENDENCY_FIELDS = ["dependencies", "peerDependencies", "optionalDependencies"] as const;

export function dependencyViolations(root: string, config: WebConfig): Violation[] {
  const file = path.join(root, config.package);
  if (!existsSync(file)) return [];
  const pkg = JSON.parse(readText(file)) as Record<string, Record<string, string> | undefined>;
  const out: Violation[] = [];
  const check = (field: string, allowed: readonly string[]): void => {
    for (const name of Object.keys(pkg[field] ?? {})) {
      if (!allowed.includes(name)) {
        out.push(
          violation(
            "WEB001",
            config.package,
            1,
            `'${name}' (${field}) is not on the allow-list of apps/web`,
            `A dependency is a decision: write an ADR with its reason, license and transitive count, add a row to the dependency table of ARCHITECTURE.md, then list it in ${CONFIG_PATH}.`,
          ),
        );
      }
    }
  };
  for (const field of DEPENDENCY_FIELDS) check(field, config.dependencies);
  check("devDependencies", config.dev_dependencies);
  return out;
}

const HEADER_LINES = 4;

interface Header {
  readonly contract: string;
  readonly body: string;
  readonly text: string;
}

function parseHeader(text: string): Header | undefined {
  const lines = text.split("\n");
  const contract = /^\/\/ contract-sha256: ([0-9a-f]{64})$/.exec(lines[2] ?? "");
  const body = /^\/\/ body-sha256: ([0-9a-f]{64})$/.exec(lines[3] ?? "");
  if (!contract || !body || lines[HEADER_LINES] !== "") return undefined;
  return {
    contract: contract[1] as string,
    body: body[1] as string,
    text: lines.slice(HEADER_LINES + 1).join("\n"),
  };
}

export function generatedViolations(root: string, config: WebConfig): Violation[] {
  const dir = path.join(root, config.generated_dir);
  const contractFile = path.join(root, config.contract);
  if (!existsSync(dir)) {
    return existsSync(path.join(root, config.package))
      ? [
          violation(
            "WEB012",
            config.generated_dir,
            0,
            "the generated API folder is missing",
            "Run `pnpm web:gen-api`.",
          ),
        ]
      : [];
  }
  const contractHash = existsSync(contractFile) ? sha256(readText(contractFile)) : "";
  const out: Violation[] = [];
  for (const name of readdirSync(dir).sort()) {
    const rel = `${config.generated_dir}/${name}`;
    if (!statSync(path.join(dir, name)).isFile()) continue;
    const header = parseHeader(readText(path.join(dir, name)));
    if (!header) {
      out.push(
        violation(
          "WEB012",
          rel,
          1,
          "no generator header (contract-sha256, body-sha256)",
          "Generated files are written by `pnpm web:gen-api` only.",
        ),
      );
    } else if (header.contract !== contractHash) {
      out.push(
        violation(
          "WEB010",
          rel,
          3,
          "made from a contract that is not the one in the repository",
          "The REST contract changed: run `pnpm web:gen-api` and commit the result.",
        ),
      );
    } else if (sha256(header.text) !== header.body) {
      out.push(
        violation(
          "WEB011",
          rel,
          4,
          "the body was edited by hand (it does not match its recorded hash)",
          "Change the generator (apps/web/src/api/generator) and run `pnpm web:gen-api`.",
        ),
      );
    }
  }
  return out;
}

/** Run the generator again and compare (the hash rules above cannot see a generator that changed). */
export function regenerationViolations(root: string, config: WebConfig): Violation[] {
  const script = path.join(root, config.generator_script);
  const tsx = path.join(root, "node_modules/.bin/tsx");
  if (!existsSync(script) || !existsSync(tsx)) return [];
  try {
    execFileSync(tsx, [script, "--check"], { cwd: root, stdio: "pipe", encoding: "utf8" });
    return [];
  } catch (error) {
    const stderr = String((error as { stderr?: unknown }).stderr ?? "").trim();
    return [
      violation(
        "WEB013",
        config.generated_dir,
        0,
        `the generator does not produce the files on disk${stderr ? `: ${stderr.split("\n")[0]}` : ""}`,
        "Run `pnpm web:gen-api` and commit the result.",
      ),
    ];
  }
}

const COLOR_FUNCTION = /(?<![\w-])(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix)\(/;
const HEX_COLOR = /(?<![\w&/-])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})(?![\w-])/;
const COLOR_UTILITY =
  "(?:bg|text|border|border-[xytblrse]|fill|stroke|ring|ring-offset|outline|shadow|accent|caret|decoration|divide|placeholder|from|via|to)";
const ARBITRARY_COLOR = new RegExp(
  `(?<![\\w-])${COLOR_UTILITY}-\\[(?:color:|#|rgba?\\(|hsla?\\(|oklch\\(|oklab\\(|lab\\(|lch\\(|hwb\\()`,
);
const PALETTE_COLOR = new RegExp(
  `(?<![\\w-])${COLOR_UTILITY}-(?:(?:slate|gray|grey|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\\d{2,3}|white|black)(?![\\w-])`,
);

function scanFiles(root: string, entries: readonly string[]): string[] {
  const out: string[] = [];
  const visit = (rel: string): void => {
    const full = path.join(root, rel);
    if (!existsSync(full)) return;
    if (statSync(full).isFile()) {
      if (/\.(tsx?|css|html)$/.test(rel)) out.push(rel);
      return;
    }
    for (const name of readdirSync(full).sort()) {
      if (name === "node_modules" || name === "dist") continue;
      visit(`${rel}/${name}`);
    }
  };
  for (const entry of entries) visit(entry);
  return out;
}

export function colorViolations(root: string, config: WebConfig): Violation[] {
  const out: Violation[] = [];
  for (const file of scanFiles(root, config.scan)) {
    if (file === config.token_file || file.startsWith(`${config.generated_dir}/`)) continue;
    readText(path.join(root, file))
      .split("\n")
      .forEach((line, index) => {
        if (COLOR_FUNCTION.test(line) || HEX_COLOR.test(line)) {
          out.push(
            violation(
              "WEB020",
              file,
              index + 1,
              "a color literal outside the token file",
              `Name the color in ${config.token_file} and use the token (bg-surface, text-muted, border-line, ...).`,
            ),
          );
        } else if (ARBITRARY_COLOR.test(line) || PALETTE_COLOR.test(line)) {
          out.push(
            violation(
              "WEB021",
              file,
              index + 1,
              "a Tailwind color that is not a token (arbitrary value, default palette name, white or black)",
              `Only the colors named in ${config.token_file} exist; use one of them.`,
            ),
          );
        }
      });
  }
  return out;
}

function goFiles(dir: string): string[] {
  const out: string[] = [];
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) out.push(...goFiles(path.join(dir, entry.name)));
    else if (/\.go$|^go\.(mod|sum|work)$/.test(entry.name)) out.push(path.join(dir, entry.name));
  }
  return out;
}

export function backendViolations(root: string, config: WebConfig): Violation[] {
  const b = config.backend;
  const out: Violation[] = [];
  if (!existsSync(path.join(root, b.placeholder))) {
    out.push(
      violation(
        "WEB030",
        b.placeholder,
        0,
        "the placeholder page of the embed folder is missing",
        "The backend builds without apps/web only while this file is committed; restore it.",
      ),
    );
  }
  for (const file of goFiles(path.join(root, b.root))) {
    const rel = path.relative(root, file).split(path.sep).join("/");
    readText(file)
      .split("\n")
      .forEach((line, index) => {
        if (line.includes(b.forbidden_reference)) {
          out.push(
            violation(
              "WEB031",
              rel,
              index + 1,
              `names ${b.forbidden_reference}: the backend must build and test without the frontend`,
              "Let scripts/build-with-web.sh copy the build into the embed folder; Go code never refers to apps/web.",
            ),
          );
        }
      });
  }
  const ignorePath = path.join(root, b.gitignore);
  const lines = existsSync(ignorePath)
    ? readText(ignorePath)
        .split("\n")
        .map((l) => l.trim())
    : [];
  for (const wanted of b.ignore_lines) {
    if (!lines.includes(wanted)) {
      out.push(
        violation(
          "WEB032",
          b.gitignore,
          0,
          `the line '${wanted}' is missing: the real build output would be committed`,
          "Only the placeholder is committed in the embed folder.",
        ),
      );
    }
  }
  return out;
}

export function run(root: string): Violation[] {
  const config = loadWebConfig(root);
  return [
    ...dependencyViolations(root, config),
    ...generatedViolations(root, config),
    ...regenerationViolations(root, config),
    ...colorViolations(root, config),
    ...backendViolations(root, config),
    ...rulesViolations(root, config),
  ];
}

/** `--budget` measures the build output of apps/web (CI runs it after the web build); with none, it fails. */
export function main(argv: readonly string[]): number {
  const { flags } = parseArgs(argv);
  const root = findRoot(flag(flags, "root"));
  if (flags.has("budget")) {
    const rules = loadWebConfig(root).rules;
    return finish("web-budget", rules ? budgetViolations(root, rules, true) : []);
  }
  return finish("web", run(root));
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
