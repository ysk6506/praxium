/**
 * The list of documents in `docs/index.md` is made, not written (ADR-0194): one line per document from its front
 * matter `title`, grouped by folder, between two marker comments. The hand-written introduction (and anything after
 * the end marker) stays as it is. A change does not commit the list; the cleanup run regenerates it with
 * `docs.ts generate`, and a stale list is a notice (DOC102), never a failure.
 */
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { splitFrontMatter } from "../common";
import { type DocsConfig, discoverDocs } from "./doc-config";

export const LIST_BEGIN = "<!-- docs-list:begin -->";
export const LIST_END = "<!-- docs-list:end -->";
/** A notice, not a failure: the list in the index is behind the documents. */
export const INDEX_STALE_CODE = "DOC102";

export interface IndexGroup {
  readonly dir: string;
  readonly title: string;
}

/** The documents the generated list names: everything under docs/ except the index, decisions, generated and archive. */
export function listedDocs(root: string, config: DocsConfig): string[] {
  const skipped = [`${config.adr_dir}/`, `${config.generated_dir}/`, `${config.archive_dir}/`];
  return discoverDocs(root, config).filter(
    (f) =>
      f.startsWith(`${config.docs_root}/`) &&
      f !== config.index &&
      !skipped.some((s) => f.startsWith(s)),
  );
}

const linkText = (title: string): string => title.replace(/[[\]]/g, "").replace(/\s+/g, " ").trim();

function titleOf(root: string, file: string): string {
  const { data } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
  return linkText(String(data?.title ?? path.posix.basename(file, ".md")));
}

/** The lines between the markers. */
export function renderList(root: string, config: DocsConfig): string {
  const groups = config.index_groups ?? [];
  const rest = { dir: "", title: "그 밖의 문서" };
  const docs = listedDocs(root, config);
  const where = (file: string): IndexGroup =>
    groups.find((g) => file.startsWith(`${g.dir}/`)) ?? rest;
  const lines: string[] = [];
  for (const group of [...groups, rest]) {
    const files = docs.filter((f) => where(f) === group);
    if (files.length === 0) continue;
    lines.push(`### ${group.title}`, "");
    for (const file of files) {
      const rel = path.posix.relative(path.posix.dirname(config.index), file);
      lines.push(`- [${titleOf(root, file)}](${rel})`);
    }
    lines.push("");
  }
  return lines.join("\n").trimEnd();
}

/** Split the index text into what comes before, inside and after the markers; null when a marker is missing. */
export function splitIndex(text: string): { before: string; after: string } | null {
  const begin = text.indexOf(LIST_BEGIN);
  const end = text.indexOf(LIST_END);
  if (begin < 0 || end < begin) return null;
  return { before: text.slice(0, begin + LIST_BEGIN.length), after: text.slice(end) };
}

/** The text of the index with a fresh list, or null when the index has no markers. */
export function freshIndex(root: string, config: DocsConfig, text: string): string | null {
  const parts = splitIndex(text);
  return parts === null ? null : `${parts.before}\n\n${renderList(root, config)}\n\n${parts.after}`;
}

/** The list block of an index text (what a hand edit would change), or null without markers. */
export function listBlock(text: string): string | null {
  const parts = splitIndex(text);
  return parts === null
    ? null
    : text.slice(parts.before.length, text.length - parts.after.length).trim();
}

export function writeIndex(root: string, config: DocsConfig): boolean {
  const file = path.join(root, config.index);
  const text = readFileSync(file, "utf8");
  const fresh = freshIndex(root, config, text);
  if (fresh === null) return false;
  writeFileSync(file, fresh, "utf8");
  return true;
}

/** Notice text when the index list is behind the documents; null when current or when the index has no list. */
export function indexStale(root: string, config: DocsConfig): string | null {
  const text = readFileSync(path.join(root, config.index), "utf8");
  const fresh = freshIndex(root, config, text);
  return fresh === null || fresh === text
    ? null
    : `NOTE docs [${INDEX_STALE_CODE}] 색인의 문서 목록이 오래됨: pnpm tsx harness/src/checks/docs.ts generate`;
}
