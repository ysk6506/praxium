/**
 * Moving verification state out of the front matter (ADR-0194): `docs.ts migrate`.
 *
 * Before ADR-0194 a document kept `last_verified`, `verification_log`, `covers_hash`, `follows_hash`, `verified_body`,
 * `unchanged_reverifies` and `last_audited` in its own front matter. `migrate` reads that state the way the old check
 * did (so a document that was fresh stays fresh and a stale one stays stale), writes the records, and strips the keys.
 * It only touches documents that still have one of the keys, so running it again changes nothing.
 *
 * What is lost: the old `verification_log` lines beyond the newest one (the history is in git), and for a document that
 * was already stale, the knowledge of which target changed (every target gets a record that never matches).
 */
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { isIsoDate, splitFrontMatter, today } from "../common";
import { discoverDocs, loadDocsConfig } from "./doc-config";
import { LEGACY_KEYS, legacyKeysOf } from "./doc-freshness";
import { bodyFingerprint, coversFingerprint, followsFingerprint } from "./doc-print";
import { STALE_MARK, writeAudit, writeRecord } from "./doc-records";
import { DocContext, declaredTargets } from "./doc-targets";

const MIGRATED_NOTE = "옛 형식에서 옮김";

/** Whether each section was fresh by the old rules: null when the document has no such section. */
export function legacyFreshness(
  root: string,
  data: Record<string, unknown>,
): { covers: boolean | null; follows: boolean | null } {
  const strings = (value: unknown): string[] => (Array.isArray(value) ? value.map(String) : []);
  const [covers, follows] = [strings(data.covers), strings(data.follows)];
  const fresh = (globs: string[], hash: unknown, make: () => string): boolean | null =>
    globs.length === 0 ? null : typeof hash === "string" && hash === make();
  return {
    covers: fresh(covers, data.covers_hash, () => coversFingerprint(root, covers)),
    follows: fresh(follows, data.follows_hash, () => followsFingerprint(root, follows)),
  };
}

/** Remove top-level keys from the front matter text, leaving every other line as it was. */
export function stripKeys(text: string, keys: readonly string[]): string {
  const lines = text.split("\n");
  if (lines[0]?.trim() !== "---") return text;
  const end = lines.findIndex((l, i) => i > 0 && l.trim() === "---");
  if (end < 0) return text;
  const kept: string[] = [];
  let dropping = false;
  for (const line of lines.slice(1, end)) {
    const top = /^([A-Za-z_][\w-]*):/.exec(line);
    if (top) dropping = keys.includes(top[1] as string);
    else if (!/^(\s|-)/.test(line) && line.trim() !== "") dropping = false;
    if (!dropping) kept.push(line);
  }
  return ["---", ...kept, ...lines.slice(end)].join("\n");
}

const noteOf = (log: unknown): string => {
  const last = Array.isArray(log) ? String(log[log.length - 1] ?? "") : "";
  return last.replace(/^\d{4}-\d{2}-\d{2}:\s*/, "").trim() || MIGRATED_NOTE;
};

/** Whether every old section that names `target` was fresh (a target in no section counts as fresh). */
function freshForTarget(
  ctx: DocContext,
  data: Record<string, unknown>,
  target: string,
  fresh: ReturnType<typeof legacyFreshness>,
): boolean {
  const { covers, follows } = declaredTargets(ctx.files, data);
  const inside = (globs: string[]): boolean =>
    globs.some((g) => ctx.files.expand(g).includes(target));
  return (!inside(covers) || fresh.covers === true) && (!inside(follows) || fresh.follows === true);
}

/** The records one old-format document turns into. Existing fresh records are kept as they are. */
function recordsFor(
  ctx: DocContext,
  file: string,
  data: Record<string, unknown>,
  body: string,
): number {
  const { targets } = declaredTargets(ctx.files, data);
  const fresh = legacyFreshness(ctx.root, data);
  const date = isIsoDate(data.last_verified) ? data.last_verified : today();
  const unchanged = Number(data.unchanged_reverifies ?? 0) > 0;
  const docHash =
    typeof data.verified_body === "string" ? data.verified_body : bodyFingerprint(body);
  const existing = ctx.records.get(file).records;
  let written = 0;
  for (const target of targets) {
    const isFresh = freshForTarget(ctx, data, target, fresh);
    const now = ctx.files.hash(target);
    const known = existing.get(target);
    if (known && (known.hash === now || !isFresh)) continue;
    writeRecord(ctx.root, {
      doc: file,
      target,
      hash: isFresh ? now : STALE_MARK,
      date,
      note: isFresh ? noteOf(data.verification_log) : `${MIGRATED_NOTE}: 옮기기 전에 이미 낡았음`,
      text_changed: !unchanged,
      doc_hash: docHash,
    });
    written++;
  }
  if (isIsoDate(data.last_audited))
    writeAudit(ctx.root, {
      doc: file,
      date: data.last_audited,
      note: MIGRATED_NOTE,
      doc_hash: docHash,
    });
  return written;
}

export interface MigrateReport {
  /** Documents whose front matter was rewritten. */
  readonly converted: string[];
  readonly records: number;
}

/** Convert every document that still keeps its verification state in the front matter. */
export function migrateAll(root: string): MigrateReport {
  const config = loadDocsConfig(root);
  const ctx = new DocContext(root);
  const converted: string[] = [];
  let records = 0;
  for (const file of discoverDocs(root, config)) {
    const text = readFileSync(path.join(root, file), "utf8");
    const { data, body } = splitFrontMatter(text);
    if (data === null || legacyKeysOf(data).length === 0) continue;
    if (data.kind !== "decision") records += recordsFor(ctx, file, data, body);
    writeFileSync(path.join(root, file), stripKeys(text, LEGACY_KEYS), "utf8");
    converted.push(file);
  }
  return { converted, records };
}
