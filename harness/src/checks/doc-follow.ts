/**
 * Documents follow decisions, not only code (ADR-0180).
 *
 * DOC092 a non-ADR doc has neither `covers` nor `follows` (freshness itself is doc-freshness.ts, ADR-0194)
 * DOC091 `amends` and `amended_by` do not point at each other
 * DOC093 the product goal sentence differs in README.md or AGENTS.md
 */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { splitFrontMatter, type Violation, violation } from "../common";
import { amendEntries } from "./doc-amends";
import type { DocsConfig } from "./doc-config";

const strings = (value: unknown): string[] => (Array.isArray(value) ? value.map(String) : []);
const GOAL_HEADING = "## 제품 목표";

/** DOC092: a document that neither covers code nor follows other documents has nothing to go stale against. */
export function checkFollows(file: string, data: Record<string, unknown>): Violation[] {
  if (data.kind === "decision") return [];
  if (strings(data.covers).length > 0 || strings(data.follows).length > 0) return [];
  return [
    violation(
      "DOC092",
      file,
      1,
      "doc neither covers code nor follows other docs, so nothing tells when it goes out of date",
      "Add `covers:` (code it describes) or `follows:` (docs, ADRs or globs whose changes should make it be re-read), then run reverify.",
    ),
  ];
}

/** DOC091: every `amends` entry exists and lists this ADR in its `amended_by`; and the other way round. */
export function checkAmendLinks(
  config: DocsConfig,
  adrs: ReadonlyMap<string, Record<string, unknown>>,
): Violation[] {
  const out: Violation[] = [];
  const names = [...adrs.keys()].map((f) => path.posix.basename(f));
  const entries = (value: unknown): string[] => amendEntries(value, names).map((e) => e.name);
  for (const [file, data] of adrs) {
    const self = path.posix.basename(file);
    for (const name of entries(data.amends)) {
      const target = adrs.get(`${config.adr_dir}/${name}`);
      if (!target) {
        out.push(
          violation(
            "DOC091",
            file,
            1,
            `amends '${name}', which is not an ADR file`,
            `Use the file name, or { adr: "NNNN", what: "..." } for an ADR in ${config.adr_dir}/.`,
          ),
        );
      } else if (!entries(target.amended_by).includes(self)) {
        out.push(
          violation(
            "DOC091",
            file,
            1,
            `amends ${name}, but ${name} does not list this ADR in amended_by`,
            `Run: pnpm tsx harness/src/checks/docs.ts link-amends ${file}`,
          ),
        );
      }
    }
    for (const name of entries(data.amended_by)) {
      if (!entries(adrs.get(`${config.adr_dir}/${name}`)?.amends).includes(self)) {
        out.push(
          violation(
            "DOC091",
            file,
            1,
            `amended_by lists ${name}, but ${name} does not name this ADR in amends`,
            "Add this ADR to that ADR's amends, or remove the stale amended_by entry.",
          ),
        );
      }
    }
  }
  return out;
}

const plain = (text: string): string => text.replace(/\*\*/g, "").trim();

/** The first paragraph after the `# ` title. */
function leadParagraph(body: string): string | null {
  const lines = body.split("\n");
  const title = lines.findIndex((l) => l.startsWith("# "));
  if (title < 0) return null;
  const paragraph: string[] = [];
  for (const line of lines.slice(title + 1)) {
    if (line.trim() === "") {
      if (paragraph.length > 0) break;
      continue;
    }
    paragraph.push(line);
  }
  return paragraph.join(" ");
}

/** DOC093: the bold sentence under the goal heading of the spec leads README.md and AGENTS.md. */
export function checkGoal(root: string, config: DocsConfig): Violation[] {
  const source = config.goal_source;
  if (!source || !existsSync(path.join(root, source))) return [];
  const lines = readFileSync(path.join(root, source), "utf8").split("\n");
  const start = lines.findIndex((l) => l.startsWith(GOAL_HEADING));
  const sentence =
    start < 0
      ? undefined
      : lines
          .slice(start + 1)
          .map((l) => l.trim())
          .find((l) => /^\*\*.+\*\*$/.test(l));
  if (!sentence) {
    return [
      violation(
        "DOC093",
        source,
        1,
        `no bold goal sentence under '${GOAL_HEADING}'`,
        "Keep the product goal as one bold line right under that heading.",
      ),
    ];
  }
  const goal = plain(sentence);
  const out: Violation[] = [];
  for (const doc of config.goal_docs ?? []) {
    if (!existsSync(path.join(root, doc))) continue;
    const lead = leadParagraph(splitFrontMatter(readFileSync(path.join(root, doc), "utf8")).body);
    if (lead === null || !plain(lead).includes(goal)) {
      out.push(
        violation(
          "DOC093",
          doc,
          1,
          `the first paragraph after the title lacks the product goal sentence from ${source}`,
          `Put exactly this sentence in the first paragraph: ${goal}`,
        ),
      );
    }
  }
  return out;
}
