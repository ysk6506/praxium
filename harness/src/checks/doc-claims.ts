/**
 * DOC094 (ADR-0180): what a document names must exist. Only forms that cannot be mistaken for prose are checked:
 * PRAXIUM_ environment variables, `pnpm <script>`, `METHOD /api/...` routes, rule codes, a source file plus a
 * camelCase name on one line, and ADR-NNNN / REQ-NNN references. ADRs are sealed and are not scanned.
 */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { type Violation, violation } from "../common";
import { listFiles } from "../files";
import { harnessText } from "./doc-adr";

const BUILTIN = new Set([
  "install",
  "i",
  "add",
  "remove",
  "update",
  "up",
  "run",
  "exec",
  "dlx",
  "tsx",
  "vitest",
  "playwright",
  "biome",
  "tsc",
  "store",
  "rebuild",
  "approve-builds",
  "create",
  "init",
  "outdated",
  "list",
  "ls",
  "why",
  "prune",
  "dedupe",
  "test",
  "start",
  "build",
  "audit",
  "link",
  "unlink",
  "publish",
  "pack",
  "env",
  "config",
  "fetch",
  "import",
  "patch",
  "root",
  "bin",
]);
const ENV_RE = /\bPRAXIUM_[A-Z0-9_]*[A-Z0-9]\b/g;
const ROUTE_RE = /\b(GET|POST|PUT|PATCH|DELETE) (\/api\/[A-Za-z0-9_\-/:{}.]*[A-Za-z0-9_}])/g;
const CODE_RE = /^[A-Z]{2,6}\d{3}$/;
/** `name`(`file.ts`) and `file.ts`의 `name`: a name written right next to the one file it lives in. */
const NAME_THEN_FILE = /`([A-Za-z_]\w*)(?:\([^`]*\))?`\s*\(`([\w./-]+\.tsx?)`\)/g;
const FILE_THEN_NAME =
  /`([\w./-]+\.tsx?)`(?:의|에서|안의|의 함수|의 타입)?\s*`([A-Za-z_]\w*)(?:\([^`]*\))?`/g;
const PENDING_RE = /만드는 중|예정|제안/;
const INLINE_RE = /`([^`\n]+)`/g;

/** Lines outside fenced code blocks, with their 1-based line numbers. */
export function proseLines(body: string, firstLine: number): Array<[number, string]> {
  const out: Array<[number, string]> = [];
  let fenced = false;
  body.split("\n").forEach((line, offset) => {
    if (line.trimStart().startsWith("```")) {
      fenced = !fenced;
      return;
    }
    if (!fenced) out.push([firstLine + offset, line]);
  });
  return out;
}

export const routeShape = (route: string): string =>
  route
    .replace(/\$\{[^}]*\}/g, ":p")
    .replace(/[?#].*$/, "")
    .replace(/(:\w+|\{\w+\})/g, ":p")
    .replace(/\/$/, "");

/** `METHOD path` of every route in the contract's route tables. */
export function contractRoutes(root: string): string[] {
  const files = listFiles(root, ["packages/core/src/contract"], (n) => /routes\.ts$/.test(n));
  return files.flatMap((file) =>
    [
      ...readFileSync(path.join(root, file), "utf8").matchAll(
        /method:\s*"(GET|POST|PUT|PATCH|DELETE)",\s*path:\s*"([^"]+)"/g,
      ),
    ].map((m) => `${m[1]} ${routeShape(m[2] as string)}`),
  );
}

/** `METHOD path` of the vendor-API routes the facades answer (`.on("GET", "/api/vcenter/vm", ...)`). */
export function facadeRoutes(root: string): string[] {
  return listFiles(root, ["packages/core/src/facades"], (n) => /\.ts$/.test(n)).flatMap((file) =>
    [
      ...readFileSync(path.join(root, file), "utf8").matchAll(
        /\.on\(\s*"(GET|POST|PUT|PATCH|DELETE)",\s*"(\/api\/[^"]*)"/g,
      ),
    ].map((m) => `${m[1]} ${routeShape(m[2] as string)}`),
  );
}

/** What the repository can prove, read once per run and only as far as a document needs it. */
export class ClaimContext {
  private memo = new Map<string, unknown>();
  constructor(
    private readonly root: string,
    private readonly adrDir: string,
  ) {}

  private once<T>(key: string, make: () => T): T {
    if (!this.memo.has(key)) this.memo.set(key, make());
    return this.memo.get(key) as T;
  }

  read(file: string): string {
    return this.once(`read:${file}`, () => readFileSync(path.join(this.root, file), "utf8"));
  }
  code = (): string => harnessText(this.root);
  /** Rule codes the harness defines: written as "CODE" or as [CODE] in its sources. */
  codes = (): Set<string> =>
    this.once(
      "codes",
      () =>
        new Set(
          [...this.code().matchAll(/["[]([A-Z]{2,6}\d{3})["\]]/g)].map((m) => m[1] as string),
        ),
    );
  envText = (): string =>
    this.once("env", () =>
      listFiles(
        this.root,
        ["packages", "apps", "deploy", ".github", "scripts", "tests", "harness"],
        (n) => /\.(tsx?|mjs|js|json|ya?ml|sh|env|example)$/.test(n),
      )
        .map((f) => this.read(f))
        .join("\n"),
    );
  scripts = (): Set<string> =>
    this.once("scripts", () => {
      const names = new Set<string>();
      for (const f of listFiles(this.root, ["."], (n) => n === "package.json")) {
        const scripts = (JSON.parse(this.read(f)) as { scripts?: Record<string, string> }).scripts;
        for (const key of Object.keys(scripts ?? {})) names.add(key);
      }
      return names;
    });
  routes = (): Set<string> =>
    this.once("routes", () => new Set([...contractRoutes(this.root), ...facadeRoutes(this.root)]));
  /** Paths of `"/api/..."` literals in the server, for routes it registers outside the contract (facades). */
  serverPaths = (): Set<string> =>
    this.once("serverPaths", () => {
      const paths = new Set<string>();
      for (const f of listFiles(this.root, ["apps/server/src"], (n) => /\.ts$/.test(n))) {
        for (const m of this.read(f).matchAll(/["'`](\/api\/[^"'`\s]*)["'`]/g))
          paths.add(routeShape(m[1] as string));
      }
      return paths;
    });
  hasRoutes = (): boolean => this.routes().size > 0;
  sources = (): string[] =>
    this.once("sources", () =>
      listFiles(this.root, ["packages", "apps", "harness", "tests"], (n) => /\.tsx?$/.test(n)),
    );
  adrNumbers = (): Set<string> =>
    this.once(
      "adrs",
      () =>
        new Set(
          listFiles(this.root, [this.adrDir], (n) => n.endsWith(".md")).map(
            (f) => /(\d{4})-/.exec(path.posix.basename(f))?.[1] ?? "",
          ),
        ),
    );
  reqText = (): string =>
    this.once("req", () => {
      const file = "docs/product-specs/requirements.md";
      return existsSync(path.join(this.root, file)) ? this.read(file) : "";
    });
  /** The one source file a token names, or null when it is not a file or is ambiguous. */
  resolve(token: string): string | null {
    if (token.includes("/")) return existsSync(path.join(this.root, token)) ? token : null;
    const hits = this.sources().filter((f) => f.endsWith(`/${token}`));
    return hits.length === 1 ? (hits[0] as string) : null;
  }
}

function bad(file: string, line: number, problem: string, fix: string): Violation {
  return violation("DOC094", file, line, problem, fix);
}

function inlineTokens(text: string): string[] {
  return [...text.matchAll(INLINE_RE)].map((m) => (m[1] as string).trim());
}

function pnpmScripts(token: string): string[] {
  const out: string[] = [];
  for (const m of token.matchAll(/\bpnpm\s+(?:run\s+)?([a-z][\w:.-]*)/g)) {
    const name = m[1] as string;
    if (token.includes("pnpm run ") || !BUILTIN.has(name)) out.push(name);
  }
  return out;
}

/** A route in the contract must match by method too; one the contract lacks may be a server-registered facade. */
function routeKnown(ctx: ClaimContext, method: string, shape: string): boolean {
  if (ctx.routes().has(`${method} ${shape}`)) return true;
  const inContract = [...ctx.routes()].some((r) => r.slice(r.indexOf(" ") + 1) === shape);
  return !inContract && ctx.serverPaths().has(shape);
}

function checkEnv(ctx: ClaimContext, file: string, line: number, text: string): Violation[] {
  return [...new Set(text.match(ENV_RE) ?? [])]
    .filter((env) => !ctx.envText().includes(env))
    .map((env) =>
      bad(
        file,
        line,
        `environment variable ${env} is not used by any code or config`,
        "Use the name the code reads, or remove the sentence.",
      ),
    );
}

function checkToken(ctx: ClaimContext, file: string, line: number, token: string): Violation[] {
  const out: Violation[] = [];
  const scripts = token.startsWith("pnpm ") ? pnpmScripts(token) : [];
  for (const script of scripts.filter((name) => !ctx.scripts().has(name)))
    out.push(
      bad(
        file,
        line,
        `\`pnpm ${script}\`: no package.json defines a script '${script}'`,
        "Use a script that exists (see the scripts in package.json) or remove the command.",
      ),
    );
  if (CODE_RE.test(token) && !ctx.codes().has(token))
    out.push(
      bad(
        file,
        line,
        `rule code ${token} is not defined by any harness check`,
        "Use a code that exists in harness/src/checks, or remove it.",
      ),
    );
  return out;
}

function checkRoutes(ctx: ClaimContext, file: string, line: number, text: string): Violation[] {
  if (!ctx.hasRoutes()) return [];
  return [...text.matchAll(ROUTE_RE)]
    .filter((m) => !routeKnown(ctx, m[1] as string, routeShape(m[2] as string)))
    .map((m) =>
      bad(
        file,
        line,
        `route ${m[1]} ${m[2]} is neither in the contract's route tables nor registered by the server`,
        "Use a route from packages/core/src/contract/*routes*.ts, or remove it.",
      ),
    );
}

function checkLine(ctx: ClaimContext, file: string, line: number, text: string): Violation[] {
  return [
    ...checkEnv(ctx, file, line, text),
    ...inlineTokens(text).flatMap((token) => checkToken(ctx, file, line, token)),
    ...checkRoutes(ctx, file, line, text),
    ...checkSymbols(ctx, file, line, text),
  ];
}

function checkSymbols(ctx: ClaimContext, file: string, line: number, text: string): Violation[] {
  const pairs: Array<[string, string]> = [
    ...[...text.matchAll(NAME_THEN_FILE)].map((m) => [m[1], m[2]] as [string, string]),
    ...[...text.matchAll(FILE_THEN_NAME)].map((m) => [m[2], m[1]] as [string, string]),
  ];
  const out: Violation[] = [];
  for (const [name, token] of pairs) {
    const resolved = ctx.resolve(token);
    if (resolved !== null && !new RegExp(`\\b${name}\\b`).test(ctx.read(resolved)))
      out.push(
        bad(
          file,
          line,
          `\`${name}\` is written next to ${resolved}, but that file has no such name`,
          "Use the name the file defines now, or point at the file that has it.",
        ),
      );
  }
  return out;
}

function checkRefs(ctx: ClaimContext, file: string, line: number, text: string): Violation[] {
  const out: Violation[] = [];
  for (const m of text.matchAll(/\bADR-(\d{4})\b/g)) {
    if (!ctx.adrNumbers().has(m[1] as string) && !PENDING_RE.test(text))
      out.push(
        bad(
          file,
          line,
          `${m[0]} does not exist`,
          "Use the number of an existing ADR. For a decision not merged yet, say so on the same line (만드는 중 or 예정).",
        ),
      );
  }
  if (ctx.reqText() !== "") {
    for (const m of text.matchAll(/\bREQ-(\d{3})\b/g)) {
      if (!ctx.reqText().includes(m[0]))
        out.push(
          bad(file, line, `${m[0]} is not in the requirements`, "Use an existing REQ number."),
        );
    }
  }
  return out;
}

export function checkClaims(
  ctx: ClaimContext,
  file: string,
  body: string,
  firstLine: number,
): Violation[] {
  return proseLines(body, firstLine).flatMap(([line, text]) => [
    ...checkLine(ctx, file, line, text),
    ...checkRefs(ctx, file, line, text),
  ]);
}
