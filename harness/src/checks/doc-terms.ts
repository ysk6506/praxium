/**
 * The document-terminology check (ADR-0029). Documents must use the standard terms of the glossary, so the old
 * spellings it lists never come back.
 *
 * WORD003 an old spelling of the glossary stands in the prose of a document
 * WORD004 the dictionary is unusable (no table found, an entry that is one ambiguous word, a spelling that is also a
 *         standard term, the same spelling with two standard terms)
 *
 * The dictionary is the glossary itself: the tables whose header is `| 표준 용어 | 옛 표기(쓰지 않음) | 뜻 | 영어 대응 |`.
 * One source means no second list that can drift from what people read. Cells of the second column hold the old
 * spellings, separated by commas, each matched as plain text. Everyday words with many meanings (자리, 길, 묶음, 칸, 쪽,
 * 뜻, 줄, 판 ...) are never entries; only their unambiguous compounds are (읽는 자리, 쪽 넘김). `ambiguous_words` in the
 * config names the words that may not be entries, and WORD004 refuses one.
 *
 * Not read: code blocks, inline code, link targets, URLs, HTML comments, quoted blocks (lines starting with `>`),
 * front matter except the text keys (`title`, `what`, `*_reason`), the reference documents named in `skip`, and the
 * old-spelling column of the glossary tables (a glossary has to name what it retires).
 */
import path from "node:path";
import { readText, type Violation, violation } from "../common";
import { listFiles, matchesGlob } from "../files";

export const TERM_CODE = "WORD003";
export const DICT_CODE = "WORD004";
const HOLE = "";

export interface DocumentsConfig {
  /** `report` counts and never fails; `fail` makes WORD003 a failure. Switched to `fail` in the last stage (ADR-0029). */
  readonly mode: "report" | "fail";
  readonly scan: readonly string[];
  readonly skip: readonly string[];
  readonly dictionary: readonly string[];
  readonly ambiguous_words: readonly string[];
}

export interface Term {
  readonly old: string;
  readonly standard: string;
  readonly source: string;
}

export interface Hit {
  readonly file: string;
  readonly line: number;
  readonly term: Term;
}

const cells = (line: string): string[] =>
  line
    .trim()
    .replace(/^\||\|$/g, "")
    .split(/(?<!\\)\|/)
    .map((c) => c.trim());

const isSeparator = (line: string): boolean => /^\|?[\s:|-]+\|?$/.test(line.trim());
const DICT_HEADER = ["표준 용어", "옛 표기(쓰지 않음)", "뜻", "영어 대응"];
const LIST_HEADER = "옛 표기";

type Table = "dictionary" | "list" | null;
const tableKind = (header: readonly string[]): Table =>
  DICT_HEADER.every((h, i) => header[i] === h) ? "dictionary" : header[0] === LIST_HEADER ? "list" : null;

/** The text of each line with the parts that are not prose replaced by a placeholder of the same length. */
export function proseLines(text: string, hideListColumns = false): string[] {
  const lines = text.split("\n");
  const out: string[] = [];
  let i = 0;
  const hole = (s: string): string => HOLE.repeat(s.length);
  if (lines[0]?.trim() === "---") {
    let end = 1;
    while (end < lines.length && lines[end]?.trim() !== "---") end++;
    let block = false;
    for (let k = 0; k <= Math.min(end, lines.length - 1); k++) {
      const ln = lines[k] as string;
      const m = /^(\s*(?:-\s+)?(?:title|what|[a-z_]*_reason):\s*)(.*)$/.exec(ln);
      if (k === 0 || k === end) out.push(hole(ln));
      else if (m) {
        block = /^[>|][-+]?$/.test((m[2] as string).trim());
        out.push(block ? hole(ln) : hole(m[1] as string) + inline(m[2] as string));
      } else if (block && /^\s+\S/.test(ln)) out.push(inline(ln));
      else {
        block = false;
        out.push(hole(ln));
      }
    }
    i = end + 1;
  }
  let fence = false;
  let comment = false;
  let table: Table = null;
  let prevTable = false;
  for (; i < lines.length; i++) {
    const ln = lines[i] as string;
    if (/^\s*(```|~~~)/.test(ln)) {
      fence = !fence;
      out.push(hole(ln));
    } else if (fence || ln.trimStart().startsWith(">")) out.push(hole(ln));
    else if (comment) {
      const k = ln.indexOf("-->");
      if (k < 0) out.push(hole(ln));
      else {
        comment = false;
        out.push(hole(ln.slice(0, k + 3)) + inline(ln.slice(k + 3)));
      }
    } else {
      const k = ln.indexOf("<!--");
      if (k >= 0 && !ln.includes("-->", k)) {
        comment = true;
        out.push(inline(ln.slice(0, k)) + hole(ln.slice(k)));
        continue;
      }
      let masked = inline(ln);
      if (!ln.trimStart().startsWith("|")) table = null;
      else if (hideListColumns) {
        if (!prevTable) table = tableKind(cells(ln));
        else if (table !== null && !isSeparator(ln)) masked = hideColumn(ln, masked, table === "dictionary" ? 1 : 0);
      }
      prevTable = ln.trimStart().startsWith("|");
      out.push(masked);
    }
  }
  return out;
}

/** Hide the n-th cell of a table row. */
function hideColumn(line: string, masked: string, n: number): string {
  let start = 0;
  let seen = -1;
  for (let k = 0; k <= line.length; k++) {
    if (k === line.length || (line[k] === "|" && line[k - 1] !== "\\")) {
      if (seen === n && k > start) return masked.slice(0, start) + HOLE.repeat(k - start) + masked.slice(k);
      seen++;
      start = k + 1;
    }
  }
  return masked;
}

function inline(line: string): string {
  let s = "";
  let k = 0;
  while (k < line.length) {
    if (line[k] === "`") {
      let j = k;
      while (line[j] === "`") j++;
      const ticks = line.slice(k, j);
      const close = line.indexOf(ticks, j);
      if (close < 0) {
        s += ticks;
        k = j;
      } else {
        s += HOLE.repeat(close + ticks.length - k);
        k = close + ticks.length;
      }
    } else s += line[k++];
  }
  return s
    .replace(/\]\(([^)]*)\)/g, (_m, t: string) => `](${HOLE.repeat(t.length)})`)
    .replace(/https?:\/\/[^\s)>\]]+/g, (m) => HOLE.repeat(m.length))
    .replace(/<!--.*?-->/g, (m) => HOLE.repeat(m.length));
}

/** The dictionary of one glossary document: old spelling to standard term. */
export function parseDictionary(file: string, text: string): Term[] {
  const terms: Term[] = [];
  let header: string[] | null = null;
  let fence = false;
  for (const line of text.split("\n")) {
    if (/^\s*```/.test(line)) fence = !fence;
    if (fence || !line.trimStart().startsWith("|")) {
      header = null;
      continue;
    }
    const row = cells(line);
    if (header === null) header = row;
    else if (!isSeparator(line) && tableKind(header) === "dictionary") {
      const standard = (row[0] ?? "").replace(/\*\*/g, "").trim();
      for (const old of (row[1] ?? "").split(",").map((s) => s.trim().replace(/\*\*/g, ""))) {
        if (old && old !== "—" && old !== "-") terms.push({ old, standard, source: file });
      }
    }
  }
  return terms;
}

export function dictionaryProblems(terms: readonly Term[], config: DocumentsConfig): Violation[] {
  const out: Violation[] = [];
  const bad = (file: string, message: string): number =>
    out.push(violation(DICT_CODE, file, 0, message, "Fix the glossary table (ADR-0029)."));
  const standards = new Set(terms.map((t) => t.standard.replace(/\s*\(.*\)\s*$/, "").trim()));
  const seen = new Map<string, Term>();
  for (const t of terms) {
    if (config.ambiguous_words.includes(t.old))
      bad(t.source, `"${t.old}" is an everyday word with many meanings: list only its compounds`);
    else if ([...t.old.replace(/\s/g, "")].length < 2) bad(t.source, `"${t.old}" is too short to be an entry`);
    if (standards.has(t.old)) bad(t.source, `"${t.old}" is an old spelling here and a standard term elsewhere`);
    const before = seen.get(t.old);
    if (before && before.standard !== t.standard)
      bad(t.source, `"${t.old}" has two standard terms: "${before.standard}" and "${t.standard}"`);
    seen.set(t.old, t);
  }
  return out;
}

export function loadDictionary(root: string, config: DocumentsConfig): { terms: Term[]; problems: Violation[] } {
  const terms: Term[] = [];
  const problems: Violation[] = [];
  for (const file of config.dictionary) {
    const found = parseDictionary(file, readText(path.join(root, file)));
    if (found.length === 0) problems.push(violation(DICT_CODE, file, 0, "no dictionary table found", "Keep the glossary tables with the header `표준 용어 | 옛 표기(쓰지 않음) | 뜻 | 영어 대응`."));
    terms.push(...found);
  }
  return { terms, problems: [...problems, ...dictionaryProblems(terms, config)] };
}

/** Every old spelling in the prose of one document. */
export function findOld(file: string, text: string, terms: readonly Term[], isDictionary = false): Hit[] {
  const hits: Hit[] = [];
  const sorted = [...terms].sort((a, b) => b.old.length - a.old.length);
  proseLines(text, isDictionary).forEach((masked, index) => {
    const taken = new Array<boolean>(masked.length).fill(false);
    for (const term of sorted) {
      for (let at = masked.indexOf(term.old); at >= 0; at = masked.indexOf(term.old, at + 1)) {
        if (taken.slice(at, at + term.old.length).some(Boolean)) continue;
        for (let k = at; k < at + term.old.length; k++) taken[k] = true;
        hits.push({ file, line: index + 1, term });
      }
    }
  });
  return hits;
}

export function documentFiles(root: string, config: DocumentsConfig): string[] {
  return listFiles(root, ["."], (n) => n.endsWith(".md")).filter(
    (f) => config.scan.some((g) => matchesGlob(f, g)) && !config.skip.some((g) => matchesGlob(f, g)),
  );
}

export interface DocumentsReport {
  readonly hits: Hit[];
  readonly problems: Violation[];
}

export function scanDocuments(root: string, config: DocumentsConfig): DocumentsReport {
  const { terms, problems } = loadDictionary(root, config);
  if (problems.length > 0) return { hits: [], problems };
  const hits: Hit[] = [];
  for (const file of documentFiles(root, config))
    hits.push(...findOld(file, readText(path.join(root, file)), terms, config.dictionary.includes(file)));
  return { hits, problems: [] };
}

/** Counts per document, worst first. */
export function perDocument(hits: readonly Hit[]): Array<[string, number]> {
  const counts = new Map<string, number>();
  for (const h of hits) counts.set(h.file, (counts.get(h.file) ?? 0) + 1);
  return [...counts].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
}

export function hitViolation(h: Hit): Violation {
  return violation(
    TERM_CODE,
    h.file,
    h.line,
    `"${h.term.old}" is an old spelling (glossary: ${h.term.standard})`,
    `Write the standard term "${h.term.standard}". The dictionary is the glossary (${h.term.source}); the rule is ADR-0029.`,
  );
}

/** Violations (fail mode) or a notice (report mode) for the documents. */
export function documentsReport(
  root: string,
  config: DocumentsConfig,
): { violations: Violation[]; notes: string[] } {
  const { hits, problems } = scanDocuments(root, config);
  if (problems.length > 0) return { violations: problems, notes: [] };
  if (config.mode === "fail") return { violations: hits.map(hitViolation), notes: [] };
  const docs = perDocument(hits);
  const notes =
    hits.length === 0
      ? []
      : [
          `NOTE ui-words [${TERM_CODE}] 문서에 옛 표기 ${hits.length}곳(문서 ${docs.length}개, 보고 모드): pnpm tsx harness/src/checks/ui-words.ts --documents`,
        ];
  return { violations: [], notes };
}
