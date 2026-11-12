/**
 * DOC096 (ADR-0180, notice only): a table of "시험" rows says what is tested; each row of a built feature should
 * carry the path of the test. Two kinds of tables count: any table under a heading that contains "시험", and any
 * table with a column headed "시험 파일". A row with a path to an existing test file passes; a row marked "예정"
 * (planned) is skipped. The notice says how many rows lack a path, per document.
 */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { splitFrontMatter } from "../common";
import { listFiles } from "../files";

export const TEST_ROWS_CODE = "DOC096";
const PATH_COLUMN = "시험 파일";

const cells = (line: string): string[] =>
  line
    .trim()
    .replace(/^\||\|$/g, "")
    .split(/(?<!\\)\|/)
    .map((c) => c.trim());

const isTable = (line: string): boolean => line.trimStart().startsWith("|");
const isSeparator = (line: string): boolean => /^\|?[\s:|-]+\|?$/.test(line.trim());

export interface TestRowReport {
  readonly file: string;
  readonly rows: number;
  readonly missing: number;
}

/** `a-{x,y}.test.ts` stands for `a-x.test.ts` and `a-y.test.ts` (one brace group). */
function expand(token: string): string[] {
  const m = /^(.*)\{([^}]*)\}(.*)$/.exec(token);
  return m ? (m[2] as string).split(",").map((alt) => `${m[1]}${alt}${m[3]}`) : [token];
}

/** Whether a row names an existing test file (`tests/...`, `*.test.ts`), by full path or unique base name. */
function hasTestPath(row: string, exists: (token: string) => boolean): boolean {
  return [...row.matchAll(/`([^`\n]+)`/g)].some((m) => {
    const token = (m[1] as string).trim().split(/\s/)[0] as string;
    return /\.test\.tsx?$|^tests\//.test(token) && expand(token).every(exists);
  });
}

interface TableRow {
  readonly header: string[];
  readonly line: string;
  readonly inTestSection: boolean;
}

/** Every body row of every table outside code fences, with its header and whether its section is about tests. */
function tableRows(body: string): TableRow[] {
  const rows: TableRow[] = [];
  let inTestSection = false;
  let header: string[] | null = null;
  let inFence = false;
  for (const line of body.split("\n")) {
    if (line.trimStart().startsWith("```")) inFence = !inFence;
    if (inFence) continue;
    if (/^#{1,6}\s/.test(line)) inTestSection = line.includes("시험");
    if (!isTable(line)) header = null;
    else if (header === null) header = cells(line);
    else if (!isSeparator(line)) rows.push({ header, line, inTestSection });
  }
  return rows;
}

export function testRowReport(
  root: string,
  file: string,
  exists: (t: string) => boolean,
): TestRowReport {
  const { body } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
  const counted = tableRows(body).filter(
    (r) => (r.inTestSection || r.header.includes(PATH_COLUMN)) && !r.line.includes("예정"),
  );
  const missing = counted.filter((r) => {
    const column = r.header.indexOf(PATH_COLUMN);
    return !hasTestPath(column >= 0 ? (cells(r.line)[column] ?? "") : r.line, exists);
  });
  return { file, rows: counted.length, missing: missing.length };
}

/** Notice lines for the documents (non-ADR) with rows lacking a test path. */
export function testRowNotes(root: string, docs: readonly string[]): string[] {
  const sources = listFiles(root, ["packages", "apps", "harness", "tests"], (n) =>
    /\.test\.tsx?$/.test(n),
  );
  const exists = (token: string): boolean =>
    token.includes("/")
      ? existsSync(path.join(root, token))
      : sources.some((f) => f.endsWith(`/${token}`));
  const reports = docs
    .map((file) => testRowReport(root, file, exists))
    .filter((r) => r.missing > 0);
  if (reports.length === 0) return [];
  const total = reports.reduce((sum, r) => sum + r.missing, 0);
  const list = reports.map((r) => `${r.file} (${r.missing}/${r.rows})`).join(", ");
  return [
    `NOTE docs [${TEST_ROWS_CODE}] 시험 표의 줄 ${total}개(문서 ${reports.length}개)에 시험 파일 경로가 없음: ${list}`,
  ];
}
