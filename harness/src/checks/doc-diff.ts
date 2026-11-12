/**
 * Checks that compare this change with its base (ADR-0180).
 *
 * DOC090 a new ADR does not name the docs it updates, or a named doc is unchanged in the same change
 * DOC097 (notice) docs whose records in this change say the text is unchanged: re-read, not edited (ADR-0194)
 * DOC098 (notice) the generated decisions table is behind the ADR front matter
 * DOC099 a generated file was edited by hand (ADR-0194)   DOC102 (notice) the document list in the index is behind
 */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { git, splitFrontMatter, type Violation, violation } from "../common";
import { amendEntries } from "./doc-amends";
import { type DocsConfig, discoverDocs, loadDocsConfig } from "./doc-config";
import { decisionsStale, decisionsTable } from "./doc-decisions";
import { freshIndex, indexStale, listBlock } from "./doc-index";
import { bodyFingerprint } from "./doc-print";
import { AUDIT_FILE, RECORDS_DIR, readRecordFile, type VerificationRecord } from "./doc-records";
import { testRowNotes } from "./doc-tests";
import { changedFiles } from "./protected";
import { render as renderQuality, TABLE_FILE } from "./quality";

const REVERIFY_ONLY_CODE = "DOC097";

/** DOC091: a new ADR says, for each ADR it amends, in a few words what it changed there. */
function checkWhat(file: string, data: Record<string, unknown>): Violation[] {
  if (!Array.isArray(data.amends)) return [];
  return data.amends.flatMap((item) => {
    const [entry] = amendEntries([item], []);
    return entry?.what
      ? []
      : [
          violation(
            "DOC091",
            file,
            1,
            `amends ${entry?.name ?? String(item)} without saying what it changed`,
            'Write the entry as { adr: "NNNN", what: "<a short plain phrase for the part that changed>" }.',
          ),
        ];
  });
}

function checkAddedAdr(root: string, file: string, changed: ReadonlySet<string>): Violation[] {
  const { data } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
  if (data === null) return [];
  return [...checkWhat(file, data), ...checkUpdatesDocs(root, file, data, changed)];
}

function checkUpdatesDocs(
  root: string,
  file: string,
  data: Record<string, unknown>,
  changed: ReadonlySet<string>,
): Violation[] {
  const listed = data.updates_docs;
  if (!Array.isArray(listed)) {
    return [
      violation(
        "DOC090",
        file,
        1,
        "new ADR does not say which docs it updates",
        "Add `updates_docs: [paths]` (the docs this decision changes, edited in this same change), or `updates_docs: []` plus `updates_docs_reason`.",
      ),
    ];
  }
  if (listed.length === 0) {
    return typeof data.updates_docs_reason === "string" && data.updates_docs_reason.trim()
      ? []
      : [
          violation(
            "DOC090",
            file,
            1,
            "updates_docs is empty without updates_docs_reason",
            "Say why no doc changes, or list the docs this decision changes.",
          ),
        ];
  }
  return listed.map(String).flatMap((doc) => {
    if (!existsSync(path.join(root, doc)))
      return [
        violation(
          "DOC090",
          file,
          1,
          `updates_docs names ${doc}, which does not exist`,
          "Fix the path to the doc this decision changes.",
        ),
      ];
    return changed.has(doc)
      ? []
      : [
          violation(
            "DOC090",
            file,
            1,
            `updates_docs names ${doc}, but this change does not edit it`,
            `Update ${doc} in this same change to follow the decision, or take it off the list.`,
          ),
        ];
  });
}

/** The document's text is not the text the record was written for (it was edited after the verification). */
function editedSince(root: string, record: VerificationRecord): boolean {
  const full = path.join(root, record.doc);
  return (
    existsSync(full) &&
    bodyFingerprint(splitFrontMatter(readFileSync(full, "utf8")).body) !== record.doc_hash
  );
}

/**
 * The documents whose verification records this change adds or rewrites, each with whether any of those records says
 * the document's text changed (ADR-0194). Read from the records, so no git call per document.
 */
export function reverifiedDocs(
  root: string,
  changes: ReadonlyArray<[string, string]>,
): Map<string, boolean> {
  const edited = new Map<string, boolean>();
  for (const [status, file] of changes) {
    if (status === "D" || !file.startsWith(`${RECORDS_DIR}/`) || !file.endsWith(".yaml")) continue;
    if (file.endsWith(`/${AUDIT_FILE}`)) continue;
    const record = readRecordFile(root, file);
    if (record)
      edited.set(
        record.doc,
        (edited.get(record.doc) ?? false) || record.text_changed || editedSince(root, record),
      );
  }
  return edited;
}

/**
 * DOC099: a generated file that differs from the base and from what the generator makes now was edited by hand.
 * A file that equals a fresh build is the cleanup run's own work and passes.
 */
function generatedEdits(
  root: string,
  config: DocsConfig,
  base: string,
  changed: ReadonlySet<string>,
): Violation[] {
  const indexText = (): string => readFileSync(path.join(root, config.index), "utf8");
  const artifacts: Array<{
    file: string;
    fix: string;
    fresh: () => string | null;
    part: (text: string) => string | null;
  }> = [
    {
      file: config.decisions_file ?? `${config.generated_dir}/decisions.md`,
      fix: "pnpm tsx harness/src/checks/docs.ts decisions",
      fresh: () => decisionsTable(root, config),
      part: (text) => text,
    },
    {
      file: TABLE_FILE,
      fix: "pnpm quality write",
      fresh: () => renderQuality(root),
      part: (text) => text,
    },
    {
      file: config.index,
      fix: "pnpm tsx harness/src/checks/docs.ts generate",
      fresh: () => {
        const text = freshIndex(root, config, indexText());
        return text === null ? null : listBlock(text);
      },
      part: listBlock,
    },
  ];
  return artifacts.flatMap(({ file, fix, fresh, part }) => {
    if (!changed.has(file) || !existsSync(path.join(root, file))) return [];
    const mine = part(readFileSync(path.join(root, file), "utf8"));
    const before = git(root, "show", `${base}:${file}`);
    if (mine === null || (before !== null && part(before) === mine) || fresh() === mine) return [];
    return [
      violation(
        "DOC099",
        file,
        1,
        "a generated file was edited by hand (it matches neither the base nor what the generator makes now)",
        `Changes do not commit generated files; the cleanup run regenerates them. Restore the base version (git checkout ${base} -- ${file}), or regenerate it: ${fix}`,
      ),
    ];
  });
}

export function report(
  root: string,
  base: string | undefined,
): { violations: Violation[]; notes: string[] } {
  const config = loadDocsConfig(root);
  const notes: string[] = [];
  for (const stale of [decisionsStale(root, config), indexStale(root, config)])
    if (stale) notes.push(stale);
  notes.push(
    ...testRowNotes(
      root,
      discoverDocs(root, config).filter((f) => !f.startsWith(`${config.adr_dir}/`)),
    ),
  );
  if (!base) return { violations: [], notes };
  const changes = changedFiles(root, base);
  const changed = new Set(changes.filter(([status]) => status !== "D").map(([, file]) => file));
  const reread = reverifiedDocs(root, changes);
  // A document re-verified in this change counts as touched by it (the record changed, the text may not have).
  for (const doc of reread.keys()) changed.add(doc);
  const violations = changes
    .filter(
      ([status, file]) =>
        status === "A" && file.startsWith(`${config.adr_dir}/`) && file.endsWith(".md"),
    )
    .filter(([, file]) => existsSync(path.join(root, file)))
    .flatMap(([, file]) => checkAddedAdr(root, file, changed));
  violations.push(...generatedEdits(root, config, base, changed));
  const stamped = [...reread]
    .filter(([, textChanged]) => !textChanged)
    .map(([doc]) => doc)
    .sort();
  if (stamped.length > 0)
    notes.push(
      `NOTE docs [${REVERIFY_ONLY_CODE}] 본문은 그대로이고 다시 확인만 된 문서 ${stamped.length}개: ${stamped.join(", ")}. 정말 읽고 맞는지 확인했는지 봐 주세요.`,
    );
  return { violations, notes };
}

export const run = (root: string, base: string | undefined): Violation[] =>
  report(root, base).violations;
