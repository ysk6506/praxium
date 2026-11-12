/**
 * Re-verifying and auditing, from records (ADR-0194, which amends ADR-0180).
 *
 * `reverify` writes one record for every target the document is stale for (or for the one named by `--target`),
 * and removes the records of targets the document no longer has. Each record says whether the document's text differed
 * from its text at the previous verification. A record that says "no" is a re-read without a change: the notice
 * DOC097 names such documents in a change, and `audit-queue` lists the documents that have such records newer than
 * their last audit, the longest unaudited first. `audit` records an independent reading in `_audit.yaml`.
 */
import { readFileSync } from "node:fs";
import path from "node:path";
import { splitFrontMatter, today } from "../common";
import { docState } from "./doc-freshness";
import { bodyFingerprint } from "./doc-print";
import {
  type AuditRecord,
  latestDate,
  readDocRecords,
  removeRecord,
  type VerificationRecord,
  writeAudit,
  writeRecord,
} from "./doc-records";
import { DocContext } from "./doc-targets";

function readDoc(root: string, file: string): { data: Record<string, unknown>; body: string } {
  const { data, body } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
  if (data === null) throw new Error(`${file} has no front matter`);
  return { data, body };
}

/** Targets `reverify` writes: the named one, else every stale one, else (nothing stale) all of them again. */
function chosenTargets(
  state: ReturnType<typeof docState>,
  file: string,
  only: string | undefined,
): string[] {
  if (only !== undefined) {
    if (!state.targets.includes(only) && !state.gone.includes(only))
      throw new Error(
        `${only} is not a target of ${file} (its covers and follows match: ${state.targets.join(", ")})`,
      );
    return [only];
  }
  return state.stale.length > 0 || state.gone.length > 0 ? state.stale : state.targets;
}

/**
 * Record that someone re-read `file` against its targets. Returns the targets that got a record.
 * `only` limits it to one target (a repository path).
 */
export function reverify(
  root: string,
  file: string,
  note: string,
  now: string = today(),
  only?: string,
): string[] {
  if (!note.trim()) throw new Error("--note is required: say what you checked or changed");
  const { data, body } = readDoc(root, file);
  const ctx = new DocContext(root);
  const state = docState(ctx, file, data);
  if (state.targets.length === 0 && state.gone.length === 0)
    throw new Error(
      `${file} has no covers or follows that match a file, so there is nothing to verify`,
    );
  const docHash = bodyFingerprint(body);
  const previous = [...state.records.values()].sort((a, b) => b.date.localeCompare(a.date))[0];
  const changed = previous === undefined || previous.doc_hash !== docHash;
  const chosen = chosenTargets(state, file, only);
  for (const target of chosen.filter((t) => state.targets.includes(t))) {
    writeRecord(root, {
      doc: file,
      target,
      hash: ctx.files.hash(target),
      date: now,
      note: note.trim(),
      text_changed: changed,
      doc_hash: docHash,
    });
  }
  for (const target of state.gone.filter((t) => only === undefined || only === t))
    removeRecord(root, file, target);
  return chosen;
}

/** Record that another reader checked `file` against what it covers and follows. */
export function audit(root: string, file: string, note: string, now: string = today()): void {
  if (!note.trim()) throw new Error("--note is required: one line saying what the audit found");
  const { body } = readDoc(root, file);
  writeAudit(root, { doc: file, date: now, note: note.trim(), doc_hash: bodyFingerprint(body) });
}

export interface AuditEntry {
  readonly file: string;
  readonly lastAudited: string;
  readonly lastVerified: string;
  /** Records newer than the last audit that say the text did not change. */
  readonly unchanged: number;
}

/** Records that still wait for an independent reading: re-reads without a change, newer than the last audit. */
export const unaudited = (
  records: Iterable<VerificationRecord>,
  audited: AuditRecord | null,
): VerificationRecord[] =>
  [...records].filter((r) => !r.text_changed && (audited === null || r.date > audited.date));

/** `docs` are the non-ADR docs (repo-relative). */
export function auditQueue(root: string, docs: readonly string[]): AuditEntry[] {
  return docs
    .flatMap((file) => {
      const { records, audit: last } = readDocRecords(root, file);
      const waiting = unaudited(records.values(), last);
      if (waiting.length === 0) return [];
      return [
        {
          file,
          lastAudited: last?.date ?? "",
          lastVerified: latestDate(records.values()) ?? "",
          unchanged: waiting.length,
        },
      ];
    })
    .sort(
      (a, b) =>
        a.lastAudited.localeCompare(b.lastAudited) ||
        a.lastVerified.localeCompare(b.lastVerified) ||
        a.file.localeCompare(b.file),
    );
}

export const formatQueue = (queue: readonly AuditEntry[]): string =>
  queue.length === 0
    ? "audit queue is empty"
    : queue
        .map(
          (e) =>
            `${e.file}  last_audited=${e.lastAudited || "never"}  last_verified=${e.lastVerified}  unchanged_reverifies=${e.unchanged}`,
        )
        .join("\n");
