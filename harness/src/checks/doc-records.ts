/**
 * Verification records (ADR-0194): what used to live in each document's front matter lives in one small file per
 * (document, target) pair, so two changes that re-verify the same document against different targets touch
 * different files and merge cleanly.
 *
 *   docs/verified/<document slug>/<target slug>.yaml   one target of one document
 *   docs/verified/<document slug>/_audit.yaml          the last independent reading of the document
 *
 * A slug is the repository path with `/` written as `__` (a document's `.md` is dropped). The record itself carries
 * the full paths, so the slug only has to be stable and readable.
 */
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import YAML from "yaml";
import { isIsoDate } from "../common";

export const RECORDS_DIR = "docs/verified";
export const AUDIT_FILE = "_audit.yaml";
/** Written by `migrate` for a target whose document was already out of date: it never matches a real hash. */
export const STALE_MARK = "migrated-stale";

export interface VerificationRecord {
  /** The document (repository path). */
  readonly doc: string;
  /** The covered or followed file (repository path). */
  readonly target: string;
  /** Content hash of the target when the document was verified against it. */
  readonly hash: string;
  readonly date: string;
  readonly note: string;
  /** Whether the document's text differed from its text at the previous verification (false: re-read only). */
  readonly text_changed: boolean;
  /** Hash of the document's text (without front matter) at this verification. */
  readonly doc_hash: string;
}

export interface AuditRecord {
  readonly doc: string;
  readonly date: string;
  readonly note: string;
  readonly doc_hash: string;
}

export interface RecordProblem {
  readonly file: string;
  readonly message: string;
}

export const docSlug = (doc: string): string => doc.replace(/\.md$/, "").replace(/\//g, "__");
export const targetSlug = (target: string): string => target.replace(/\//g, "__");
export const docDir = (doc: string): string => `${RECORDS_DIR}/${docSlug(doc)}`;
export const recordPath = (doc: string, target: string): string =>
  `${docDir(doc)}/${targetSlug(target)}.yaml`;
export const auditPath = (doc: string): string => `${docDir(doc)}/${AUDIT_FILE}`;

/**
 * One `key: value` line per field, each value written as a JSON scalar (which is also a YAML double-quoted scalar or
 * a YAML boolean), so any YAML reader reads the file and ours reads it without a YAML parser.
 */
const dump = (value: object): string =>
  `${Object.entries(value)
    .map(([key, v]) => `${key}: ${JSON.stringify(v)}`)
    .join("\n")}\n`;

const SIMPLE_LINE = /^([a-z_]+): ("(?:[^"\\]|\\.)*"|true|false)$/;

/** The strict reader for what `dump` writes; null when the text is anything else (then YAML decides). */
function parseSimple(text: string): Record<string, unknown> | null {
  const value: Record<string, unknown> = {};
  for (const line of text.split("\n")) {
    if (line === "") continue;
    const m = SIMPLE_LINE.exec(line);
    if (!m) return null;
    value[m[1] as string] = JSON.parse(m[2] as string);
  }
  return value;
}

function writeText(root: string, file: string, text: string): void {
  const full = path.join(root, file);
  mkdirSync(path.dirname(full), { recursive: true });
  writeFileSync(full, text, "utf8");
}

export function writeRecord(root: string, record: VerificationRecord): string {
  const file = recordPath(record.doc, record.target);
  writeText(root, file, dump(record));
  return file;
}

export function writeAudit(root: string, record: AuditRecord): string {
  const file = auditPath(record.doc);
  writeText(root, file, dump(record));
  return file;
}

/** Remove the record of one target, and the document's folder when that was its last file. */
export function removeRecord(root: string, doc: string, target: string): void {
  rmSync(path.join(root, recordPath(doc, target)), { force: true });
  const dir = path.join(root, docDir(doc));
  if (existsSync(dir) && readdirSync(dir).length === 0) rmSync(dir, { recursive: true });
}

const isText = (value: unknown): value is string => typeof value === "string" && value.length > 0;

function parseYaml(
  root: string,
  file: string,
): { value?: Record<string, unknown>; error?: string } {
  try {
    const text = readFileSync(path.join(root, file), "utf8");
    const simple = parseSimple(text);
    if (simple) return { value: simple };
    const value = YAML.parse(text);
    if (value === null || typeof value !== "object" || Array.isArray(value))
      return { error: "is not a YAML mapping" };
    return { value: value as Record<string, unknown> };
  } catch {
    return { error: "cannot be read as YAML (a merge conflict left markers in it?)" };
  }
}

function asRecord(value: Record<string, unknown>): VerificationRecord | null {
  const { doc, target, hash, date, note, text_changed, doc_hash } = value;
  if (!isText(doc) || !isText(target) || !isText(hash) || !isText(note) || !isText(doc_hash))
    return null;
  if (!isIsoDate(date) || typeof text_changed !== "boolean") return null;
  return { doc, target, hash, date, note, text_changed, doc_hash };
}

/** One record file, or null when it is missing or unusable. */
export function readRecordFile(root: string, file: string): VerificationRecord | null {
  if (!existsSync(path.join(root, file))) return null;
  const parsed = parseYaml(root, file);
  return parsed.value ? asRecord(parsed.value) : null;
}

export interface DocRecords {
  readonly records: ReadonlyMap<string, VerificationRecord>;
  readonly audit: AuditRecord | null;
  readonly problems: readonly RecordProblem[];
}

function readAudit(root: string, doc: string, problems: RecordProblem[]): AuditRecord | null {
  const file = auditPath(doc);
  if (!existsSync(path.join(root, file))) return null;
  const parsed = parseYaml(root, file);
  const { doc: owner, date, note, doc_hash } = parsed.value ?? {};
  if (!parsed.value || owner !== doc || !isIsoDate(date) || !isText(note) || !isText(doc_hash)) {
    problems.push({ file, message: parsed.error ?? "audit record is incomplete" });
    return null;
  }
  return { doc, date, note, doc_hash };
}

/** Every record of one document, plus whatever is wrong with the files (unreadable, misplaced, incomplete). */
export function readDocRecords(root: string, doc: string): DocRecords {
  const records = new Map<string, VerificationRecord>();
  const problems: RecordProblem[] = [];
  const dir = path.join(root, docDir(doc));
  const names = existsSync(dir) ? readdirSync(dir).sort() : [];
  for (const name of names.filter((n) => n.endsWith(".yaml") && n !== AUDIT_FILE)) {
    const file = `${docDir(doc)}/${name}`;
    const parsed = parseYaml(root, file);
    const record = parsed.value ? asRecord(parsed.value) : null;
    if (!record) {
      problems.push({ file, message: parsed.error ?? "record lacks a required field" });
    } else if (record.doc !== doc || `${targetSlug(record.target)}.yaml` !== name) {
      problems.push({
        file,
        message: `record is in the wrong place (it says ${record.doc} / ${record.target})`,
      });
    } else {
      records.set(record.target, record);
    }
  }
  return { records, audit: readAudit(root, doc, problems), problems };
}

/** Records read once per run, per document. */
export class RecordStore {
  private readonly cache = new Map<string, DocRecords>();
  constructor(private readonly root: string) {}

  get(doc: string): DocRecords {
    let hit = this.cache.get(doc);
    if (!hit) {
      hit = readDocRecords(this.root, doc);
      this.cache.set(doc, hit);
    }
    return hit;
  }
}

/** Folders of `docs/verified/` that belong to no existing document. */
export function orphanRecordDirs(root: string, docs: readonly string[]): string[] {
  const dir = path.join(root, RECORDS_DIR);
  if (!existsSync(dir)) return [];
  const known = new Set(docs.map(docSlug));
  return readdirSync(dir)
    .filter((name) => !known.has(name))
    .sort()
    .map((name) => `${RECORDS_DIR}/${name}`);
}

/** The newest verification date among records (null when there are none). */
export const latestDate = (records: Iterable<VerificationRecord>): string | null => {
  let best: string | null = null;
  for (const r of records) if (best === null || r.date > best) best = r.date;
  return best;
};
