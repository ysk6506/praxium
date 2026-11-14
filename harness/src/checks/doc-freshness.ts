/**
 * Freshness per target (ADR-0194, which amends ADR-0180 and ADR-0002).
 *
 * A document is stale for target T when no record exists for (document, T) or the recorded hash differs from T's
 * hash now. A record whose target is no longer a target of the document (the file is gone, or the `covers` entry was
 * taken out) is stale too: the document was read against something that is not what it describes now.
 *
 * DOC010 stale   DOC011 never verified   DOC012 covers or follows match nothing   DOC020 too old   DOC021 future date
 * DOC013 stale for a `recheck: release` relation, in the release check only (ADR-0056); otherwise the notice DOC103
 * DOC100 the front matter still holds verification state (run `migrate`)   DOC101 a record file is unusable
 */
import { daysBetween, type Violation, violation } from "../common";
import type { DocsConfig } from "./doc-config";
import { latestDate, type RecordProblem, type VerificationRecord } from "./doc-records";
import { type DeclaredTargets, type DocContext, declaredTargets } from "./doc-targets";

/** What used to be kept in each document's front matter and now lives in `docs/verified/`. */
export const LEGACY_KEYS = [
  "last_verified",
  "verification_log",
  "covers_hash",
  "follows_hash",
  "verified_body",
  "unchanged_reverifies",
  "last_audited",
] as const;

export const MIGRATE_COMMAND = "pnpm tsx harness/src/checks/docs.ts migrate";

export const reverifyCommand = (file: string): string =>
  `pnpm tsx harness/src/checks/docs.ts reverify ${file} --note "<what you checked or changed>"`;

export const legacyKeysOf = (data: Record<string, unknown> | null): string[] =>
  data === null ? [] : LEGACY_KEYS.filter((key) => key in data);

export interface DocState extends DeclaredTargets {
  readonly records: ReadonlyMap<string, VerificationRecord>;
  /** Targets with no record, or a record whose hash is not the target's hash now. */
  readonly stale: string[];
  /** The part of `stale` that only `recheck: release` relations name: due at the next release, not at once. */
  readonly staleRelease: string[];
  /** Records of targets the document no longer has. */
  readonly gone: string[];
  /** Newest verification date among the records of the current targets. */
  readonly verified: string | null;
  readonly problems: readonly RecordProblem[];
}

/** Where a document stands against its targets. Reads the records and hashes of this run's context only once. */
export function docState(ctx: DocContext, file: string, data: Record<string, unknown>): DocState {
  const declared = declaredTargets(ctx.files, data);
  const { records, problems } = ctx.records.get(file);
  const stale = declared.targets.filter((t) => records.get(t)?.hash !== ctx.files.hash(t));
  const current = new Set(declared.targets);
  const gone = [...records.keys()].filter((t) => !current.has(t));
  const live = declared.targets.flatMap((t) =>
    records.has(t) ? [records.get(t) as VerificationRecord] : [],
  );
  const release = new Set(declared.release);
  const staleRelease = stale.filter((t) => release.has(t));
  return { ...declared, records, stale, staleRelease, gone, verified: latestDate(live), problems };
}

const sample = (items: readonly string[]): string =>
  items.length <= 3
    ? items.join(", ")
    : `${items.slice(0, 3).join(", ")} and ${items.length - 3} more`;

function emptyViolations(file: string, state: DocState): Violation[] {
  return (["covers", "follows"] as const).flatMap((key) =>
    state.empty[key].length === 0
      ? []
      : [
          violation(
            "DOC012",
            file,
            1,
            `'${key}' matches no files: ${state.empty[key].join(", ")}`,
            key === "covers"
              ? "Fix the globs (the code moved or was deleted) or remove the stale claim."
              : "Fix the paths (the doc moved or was deleted) or remove the stale claim.",
          ),
        ],
  );
}

function staleMessage(state: DocState, list: readonly string[]): string {
  const covered = state.covers.length > 0;
  const followed = state.follows.length > 0;
  const what =
    covered && followed
      ? "code or a doc this doc covers or follows changed"
      : covered
        ? "code covered by this doc changed"
        : "a doc this doc follows changed";
  return `${what} since it was last verified (${list.length} of ${state.targets.length + state.gone.length} targets: ${sample(list)})`;
}

/** The reverify command for one target; the note says what the document was read against and what came of it. */
export const reverifyTargetCommand = (file: string, target: string): string =>
  `pnpm tsx harness/src/checks/docs.ts reverify ${file} --target ${target} --note "<what you compared it with and what you changed, or why nothing needed changing>"`;

/** DOC013: in the release check, a `recheck: release` relation whose record is stale is a failure. */
function releaseViolations(file: string, state: DocState): Violation[] {
  return state.staleRelease.map((target) =>
    violation(
      "DOC013",
      file,
      1,
      `not re-verified for this release: ${file} against ${target} (a relation marked recheck: release)`,
      `Before the release, read ${file} against what changed in ${target}, fix it where it no longer fits, then run: ${reverifyTargetCommand(file, target)}`,
    ),
  );
}

/** The notice DOC103 for a document with release relations that are stale; null when there are none. */
export function releaseNote(file: string, state: DocState): string | null {
  if (state.staleRelease.length === 0) return null;
  const targets = state.staleRelease.join(", ");
  return `NOTE docs [DOC103] 다음 릴리스 전에 다시 확인해야 해요: ${file} (대상: ${targets}). 릴리스 검사(docs.ts --release)에서는 실패해요. 개요를 대상의 변경과 대조해 읽고 고친 뒤: ${reverifyTargetCommand(file, state.staleRelease[0] as string)}`;
}

/**
 * DOC010, DOC011, DOC012 and DOC101 for one document that keeps its state in records (`state` from `docState`).
 * A stale `recheck: release` relation is no DOC010; with `release` (the release check) it is DOC013.
 */
export function checkFreshness(file: string, state: DocState, release = false): Violation[] {
  const out = [
    ...emptyViolations(file, state),
    ...state.problems.map((p) =>
      violation(
        "DOC101",
        p.file,
        1,
        `verification record is unusable: ${p.message}`,
        `Delete the file and run: ${reverifyCommand(file)} (a conflict in a record is a real one: both sides verified the same target).`,
      ),
    ),
  ];
  if (state.targets.length === 0) return out;
  const fix = `Re-read the doc against what it covers or follows, update it, then run: ${reverifyCommand(file)}`;
  if (state.records.size === 0 && state.problems.length === 0) {
    const key = state.covers.length > 0 ? "covers" : "follows";
    return [
      ...out,
      violation("DOC011", file, 1, `doc declares '${key}' but was never verified`, fix),
    ];
  }
  const now = [...state.stale.filter((t) => !state.staleRelease.includes(t)), ...state.gone];
  if (now.length > 0) out.push(violation("DOC010", file, 1, staleMessage(state, now), fix));
  if (release) out.push(...releaseViolations(file, state));
  return out;
}

/** DOC020 and DOC021: the newest record of the document is the date someone last read it. */
export function checkAge(
  config: DocsConfig,
  file: string,
  data: Record<string, unknown>,
  state: DocState,
  now: string,
): Violation[] {
  if (state.verified === null) return [];
  const age = daysBetween(state.verified, now);
  if (age < 0)
    return [
      violation(
        "DOC021",
        file,
        1,
        "a verification record is dated in the future",
        "Use the real date you verified the doc.",
      ),
    ];
  const limit = config.max_age_days[String(data.kind)];
  if (limit === null || limit === undefined || data.status !== "active" || age <= limit) return [];
  return [
    violation(
      "DOC020",
      file,
      1,
      `last verified ${age} days ago (limit ${limit} for ${String(data.kind)})`,
      `Read the doc against the code, fix it, then run: ${reverifyCommand(file)}`,
    ),
  ];
}

/** One DOC100 for all documents that still keep verification state in their front matter. */
export function legacyViolation(legacy: ReadonlyMap<string, string[]>): Violation[] {
  const files = [...legacy.keys()].sort();
  const first = files[0];
  if (first === undefined) return [];
  const names =
    files.length <= 6
      ? files.join(", ")
      : `${files.slice(0, 6).join(", ")} and ${files.length - 6} more`;
  return [
    violation(
      "DOC100",
      first,
      1,
      `${files.length} document(s) still keep verification state in their front matter (${(legacy.get(first) ?? []).join(", ")}): ${names}`,
      `Verification state now lives in docs/verified/ (ADR-0194). Merge origin/main (in a conflicted front matter keep main's side), then run: ${MIGRATE_COMMAND} (safe to run again). Afterwards run reverify for the documents your change made stale.`,
    ),
  ];
}
