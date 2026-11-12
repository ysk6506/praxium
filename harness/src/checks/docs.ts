/**
 * Documentation quality (REQ-020, ADR-0002): docs must prove they are current.
 *
 * A doc declares `covers` (globs of the code it describes) and `follows` (docs it must be re-read against). Each
 * matched file is a target, and a record under docs/verified/ holds the target's hash at the last verification
 * (TypeScript by structure, so comment and format changes do not churn docs). When a target changes, the doc is
 * stale for it until someone re-reads the doc, fixes it and runs `reverify --note` (ADR-0194, doc-freshness.ts).
 *
 * DOC001 front matter   DOC002 bad kind/status   DOC010 stale   DOC011 never verified
 * DOC012 covers nothing DOC020 too old           DOC021 future date
 * DOC100 old-format front matter (run migrate)   DOC101 a verification record is unusable
 * DOC030 broken link    DOC031 dead path         DOC040 orphan  DOC050 too long
 * DOC060 wrong place    DOC070 sealed ADR edited DOC071 ADR not sealed  DOC080 ADR unenforced
 * DOC090 new ADR's updates_docs (doc-diff.ts)  DOC091 amends/amended_by  DOC092 no covers or follows
 * DOC093 goal sentence (doc-follow.ts, ADR-0180)  DOC094/DOC095 notices
 */
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  isMain,
  joinFrontMatter,
  loadYaml,
  parseArgs,
  splitFrontMatter,
  today,
  type Violation,
  violation,
} from "../common";
import { listFiles } from "../files";
import { checkAdr } from "./doc-adr";
import { audit, auditQueue, formatQueue, reverify } from "./doc-audit";
import { ClaimContext, checkClaims, proseLines } from "./doc-claims";
import { type DocsConfig, discoverDocs, loadDocsConfig } from "./doc-config";
import { linkAmends, writeDecisions } from "./doc-decisions";
import { checkFacts, FactCounter } from "./doc-facts";
import { checkAmendLinks, checkFollows, checkGoal } from "./doc-follow";
import { checkAge, checkFreshness, docState, legacyKeysOf, legacyViolation } from "./doc-freshness";
import { LIST_BEGIN, listedDocs, writeIndex } from "./doc-index";
import { migrateAll } from "./doc-legacy";
import { bodyFingerprint } from "./doc-print";
import { orphanRecordDirs } from "./doc-records";
import { DocContext } from "./doc-targets";

export { type DocsConfig, discoverDocs, loadDocsConfig, reverify };

const LINK_RE = /(?<!!)\[[^\]]*\]\(([^)\s]+)\)/g;
const INLINE_CODE_RE = /`([^`\n]+)`/g;
const PATH_LIKE_RE = /^[A-Za-z0-9_.\-/]+\.(?:ts|tsx|md|yaml|yml|json|txt|toml|css|html)$/;

function checkFrontMatter(
  config: DocsConfig,
  file: string,
  data: Record<string, unknown> | null,
): Violation[] {
  if (data === null) {
    return [
      violation(
        "DOC001",
        file,
        1,
        "missing YAML front matter",
        `Start the file with --- front matter containing: ${config.required_keys.join(", ")}.`,
      ),
    ];
  }
  const out: Violation[] = config.required_keys
    .filter((key) => !(key in data))
    .map((key) => violation("DOC001", file, 1, `front matter lacks '${key}'`, `Add '${key}'.`));
  for (const [key, allowed] of [
    ["kind", config.kinds],
    ["status", config.statuses],
  ] as const) {
    if (key in data && !allowed.includes(String(data[key]))) {
      out.push(
        violation(
          "DOC002",
          file,
          1,
          `${key} '${String(data[key])}' is invalid`,
          `Use one of: ${allowed.join(", ")}.`,
        ),
      );
    }
  }
  return out;
}

function checkLength(config: DocsConfig, file: string, text: string): Violation[] {
  const limit = config.max_lines[path.basename(file)] ?? config.max_lines.default ?? 400;
  const count = text.split("\n").length - (text.endsWith("\n") ? 1 : 0);
  return count <= limit
    ? []
    : [
        violation(
          "DOC050",
          file,
          1,
          `${count} lines exceeds the ${limit} line limit`,
          "Split the doc by topic and link the parts (progressive disclosure).",
        ),
      ];
}

function checkLinksAndPaths(
  root: string,
  config: DocsConfig,
  file: string,
  body: string,
  firstLine: number,
): { violations: Violation[]; links: string[] } {
  const violations: Violation[] = [];
  const links: string[] = [];
  for (const [line, text] of proseLines(body, firstLine)) {
    for (const match of text.matchAll(LINK_RE)) {
      const target = (match[1] as string).split("#")[0] as string;
      if (!target || target.includes("://") || target.startsWith("mailto:")) continue;
      const resolved = path.posix.normalize(path.posix.join(path.posix.dirname(file), target));
      if (existsSync(path.join(root, resolved))) links.push(resolved.replace(/\/$/, ""));
      else
        violations.push(
          violation(
            "DOC030",
            file,
            line,
            `broken link: ${match[1] as string}`,
            "Fix the path or delete the link.",
          ),
        );
    }
    for (const match of text.matchAll(INLINE_CODE_RE)) {
      const candidate = (match[1] as string).trim();
      if (!PATH_LIKE_RE.test(candidate)) continue;
      const checked =
        config.path_roots.some((p) => candidate.startsWith(p)) ||
        config.root_files.includes(candidate);
      if (checked && !existsSync(path.join(root, candidate))) {
        violations.push(
          violation(
            "DOC031",
            file,
            line,
            `\`${candidate}\` does not exist`,
            "Update the reference to the file's real location or remove it.",
          ),
        );
      }
    }
  }
  return { violations, links };
}

function checkOrphans(
  config: DocsConfig,
  docs: readonly string[],
  links: ReadonlyMap<string, readonly string[]>,
  listed: readonly string[],
): Violation[] {
  const reachable = new Set<string>();
  // The index lists every document by construction once it has the generated list (ADR-0194).
  const frontier = [config.index, ...config.root_docs, ...listed];
  while (frontier.length > 0) {
    const current = frontier.pop() as string;
    if (reachable.has(current)) continue;
    reachable.add(current);
    frontier.push(...(links.get(current) ?? []));
  }
  // The ADR folder is reached as a whole once something reachable links to it (ADR-0180).
  const adrFolderLinked = reachable.has(config.adr_dir);
  return docs
    .filter((doc) => doc.startsWith(`${config.docs_root}/`) && !reachable.has(doc))
    .filter((doc) => !(adrFolderLinked && doc.startsWith(`${config.adr_dir}/`)))
    .map((doc) =>
      violation(
        "DOC040",
        doc,
        1,
        "doc is not reachable from docs/index.md or a root doc",
        "Link it from docs/index.md (or a linked doc), or delete it.",
      ),
    );
}

function checkPlacement(
  config: DocsConfig,
  file: string,
  data: Record<string, unknown>,
): Violation[] {
  const out: Violation[] = [];
  const inAdr = file.startsWith(`${config.adr_dir}/`);
  if (data.kind === "decision" && !inAdr)
    out.push(
      violation(
        "DOC060",
        file,
        1,
        "decisions belong in the ADR directory",
        `Move it to ${config.adr_dir}/.`,
      ),
    );
  if (inAdr && data.kind !== "decision")
    out.push(
      violation(
        "DOC060",
        file,
        1,
        "only kind: decision docs live in the ADR directory",
        "Change the kind or move the file.",
      ),
    );
  if (
    data.kind === "plan" &&
    file.includes("exec-plans/completed/") &&
    data.status !== "archived"
  ) {
    out.push(
      violation(
        "DOC060",
        file,
        1,
        "completed plans must have status: archived",
        "Set status: archived (or move the plan back to active/).",
      ),
    );
  }
  if (data.kind === "plan" && file.includes("exec-plans/active/") && data.status !== "active") {
    out.push(
      violation(
        "DOC060",
        file,
        1,
        "plans in active/ must have status: active",
        "Move finished plans to exec-plans/completed/.",
      ),
    );
  }
  return out;
}

interface Scan {
  readonly ctx: DocContext;
  readonly legacy: Map<string, string[]>;
  readonly violations: Violation[];
}

/** Freshness and age of one non-decision document, or its place in the old-format list. */
function checkVerification(
  scan: Scan,
  config: DocsConfig,
  file: string,
  data: Record<string, unknown>,
  now: string,
): void {
  const legacy = legacyKeysOf(data);
  if (legacy.length > 0) scan.legacy.set(file, legacy);
  if (data.kind === "decision" || legacy.length > 0) return;
  const state = docState(scan.ctx, file, data);
  scan.violations.push(...checkFreshness(file, state), ...checkAge(config, file, data, state, now));
}

export function run(root: string, now: string = today()): Violation[] {
  const config = loadDocsConfig(root);
  const docs = discoverDocs(root, config);
  const links = new Map<string, string[]>();
  const adrs = new Map<string, Record<string, unknown>>();
  const claims = new ClaimContext(root, config.adr_dir);
  const facts = new FactCounter(root, claims);
  const scan: Scan = { ctx: new DocContext(root), legacy: new Map(), violations: [] };
  const violations = scan.violations;
  for (const file of docs) {
    const text = readFileSync(path.join(root, file), "utf8");
    const { data, body, bodyLine } = splitFrontMatter(text);
    violations.push(...checkFrontMatter(config, file, data), ...checkLength(config, file, text));
    const scanned = checkLinksAndPaths(root, config, file, body, bodyLine);
    violations.push(...scanned.violations);
    links.set(file, scanned.links);
    if (data === null) continue;
    checkVerification(scan, config, file, data, now);
    violations.push(...checkFollows(file, data), ...checkPlacement(config, file, data));
    if (data.kind === "decision") {
      violations.push(...checkAdr(root, file, data, body));
      adrs.set(file, data);
    } else {
      violations.push(
        ...checkClaims(claims, file, body, bodyLine),
        ...checkFacts(facts, file, body, bodyLine),
      );
    }
  }
  const listed = hasList(root, config) ? listedDocs(root, config) : [];
  return [
    ...violations,
    ...legacyViolation(scan.legacy),
    ...checkRecordFolders(root, docs),
    ...checkAmendLinks(config, adrs),
    ...checkGoal(root, config),
    ...checkOrphans(config, docs, links, listed),
  ];
}

const hasList = (root: string, config: DocsConfig): boolean =>
  existsSync(path.join(root, config.index)) &&
  readFileSync(path.join(root, config.index), "utf8").includes(LIST_BEGIN);

/** DOC101: a folder of records for a document that does not exist (deleted or moved without its records). */
function checkRecordFolders(root: string, docs: readonly string[]): Violation[] {
  return orphanRecordDirs(root, docs).map((dir) =>
    violation(
      "DOC101",
      dir,
      1,
      "verification records for a document that does not exist",
      `Delete the folder (git rm -r ${dir}), or move it with the document it belongs to.`,
    ),
  );
}

export function sealAdr(root: string, file: string): void {
  const full = path.join(root, file);
  const { data, body } = splitFrontMatter(readFileSync(full, "utf8"));
  if (data === null || data.kind !== "decision")
    throw new Error("only kind: decision docs can be sealed");
  data.decision_hash = bodyFingerprint(body);
  writeFileSync(full, joinFrontMatter(data, body), "utf8");
}

const relPath = (root: string, target: string): string =>
  path.relative(root, path.resolve(target)).split(path.sep).join("/");

/** Run a subcommand that takes a document; false when `command` is not one of them. */
function onDoc(
  root: string,
  command: string | undefined,
  target: string,
  flags: Map<string, string | true>,
): boolean {
  const file = relPath(root, target);
  const note = flag(flags, "note") ?? "";
  if (command === "reverify") {
    const only = flag(flags, "target");
    const done = reverify(root, file, note, today(), only && relPath(root, only));
    for (const t of done) console.log(`verified: ${t}`);
  } else if (command === "seal-adr") sealAdr(root, file);
  else if (command === "audit") audit(root, file, note);
  else if (command === "link-amends") {
    for (const changed of linkAmends(root, loadDocsConfig(root), file))
      console.log(`amended_by: ${changed}`);
  } else return false;
  return true;
}

/** Subcommands that take no document. Returns an exit code, or null when `command` is not one of them. */
function onRepo(root: string, command: string | undefined): number | null {
  const config = loadDocsConfig(root);
  if (command === "audit-queue") {
    const plain = discoverDocs(root, config).filter((f) => !f.startsWith(`${config.adr_dir}/`));
    console.log(formatQueue(auditQueue(root, plain)));
  } else if (command === "decisions") writeDecisions(root, config);
  else if (command === "index") writeIndex(root, config);
  else if (command === "generate") {
    writeDecisions(root, config);
    writeIndex(root, config);
  } else if (command === "migrate") {
    const report = migrateAll(root);
    console.log(
      `migrated ${report.converted.length} document(s), wrote ${report.records} verification record(s)`,
    );
  } else return null;
  return 0;
}

export function main(argv: readonly string[]): number {
  const { positional, flags } = parseArgs(argv);
  const root = findRoot(flag(flags, "root"));
  const [command, target] = positional;
  if (target && onDoc(root, command, target, flags)) return 0;
  return onRepo(root, command) ?? finish("docs", run(root));
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
