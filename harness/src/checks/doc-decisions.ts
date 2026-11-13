/**
 * The decisions table is made, not written (ADR-0180): one line per ADR from its front matter
 * (title, status, amends, amended_by, superseded_by). `link-amends` writes the `amended_by` side.
 */
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { joinFrontMatter, splitFrontMatter } from "../common";
import { listFiles } from "../files";
import { adrNumber, amendEntries, backEntry } from "./doc-amends";
import type { DocsConfig } from "./doc-config";

/** A notice, not a failure: the committed decisions file is behind the ADR front matter. */
export const STALE_CODE = "DOC098";
const numberOf = adrNumber;

interface AdrEntry {
  readonly name: string;
  readonly data: Record<string, unknown>;
}

function loadAdrs(root: string, config: DocsConfig): AdrEntry[] {
  return listFiles(root, [config.adr_dir], (name) => name.endsWith(".md"))
    .sort()
    .flatMap((file) => {
      const { data } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
      return data === null ? [] : [{ name: path.posix.basename(file), data }];
    });
}

const decisionsPath = (config: DocsConfig): string =>
  config.decisions_file ?? `${config.generated_dir}/decisions.md`;

/** Repo-relative link from the decisions file to an ADR file. */
const linkTo = (config: DocsConfig, name: string): string =>
  path.posix.relative(path.posix.dirname(decisionsPath(config)), `${config.adr_dir}/${name}`);

const cell = (text: string): string => text.replace(/\|/g, "\\|").replace(/\s+/g, " ").trim();

function row(config: DocsConfig, adrNames: readonly string[], { name, data }: AdrEntry): string {
  const title = cell(String(data.title ?? name));
  const label = title.startsWith("ADR-") ? title : `ADR-${numberOf(name)} ${title}`;
  const status = data.status === "superseded" ? "대체됨" : "유효";
  const names = adrNames;
  const ref = (n: string, what: string | null): string =>
    `[${numberOf(n)}](${linkTo(config, n)})${what ? ` (${cell(what)})` : ""}`;
  const replaced =
    typeof data.superseded_by === "string" ? [`대체: ${ref(data.superseded_by, null)}`] : [];
  const amended = amendEntries(data.amended_by, names);
  const parts = [
    ...replaced,
    ...(amended.length > 0
      ? [`일부 수정: ${amended.map((e) => ref(e.name, e.what)).join(", ")}`]
      : []),
  ];
  return `| [${label}](${linkTo(config, name)}) | ${status} | ${parts.join("; ")} |`;
}

/** The whole text of the decisions file, from the ADR front matter alone. */
export function decisionsTable(root: string, config: DocsConfig): string {
  const adrs = loadAdrs(root, config);
  return [
    "# 결정 목록",
    "",
    "이 문서는 하네스가 만들어요(`pnpm tsx harness/src/checks/docs.ts decisions`). 손으로 수정하지 않아요. 결정 기록의 앞부분(제목, 상태, `amends`, `amended_by`, `superseded_by`)에서 번호 순으로 한 줄씩 만들고, 하루에 한 번 실행되는 정리 작업이 main에서 새로 만들어 올려요. 결정마다 커밋하지 않아요. 오래되면 구조 검사가 알림 한 줄만 내요.",
    "",
    "'일부 수정'이 적힌 결정은 뒤의 결정이 그 일부를 바꿨다는 의미예요. 본문은 확정되어 그대로이니, 거기 적힌 결정 가운데 무엇이 지금 유효한지는 수정한 결정을 읽어 확인해요.",
    "",
    "| 결정 | 상태 | 수정 내용 |",
    "|---|---|---|",
    ...adrs.map((adr) =>
      row(
        config,
        adrs.map((a) => a.name),
        adr,
      ),
    ),
    "",
  ].join("\n");
}

export function writeDecisions(root: string, config: DocsConfig): void {
  const file = path.join(root, decisionsPath(config));
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, decisionsTable(root, config), "utf8");
}

/** Notice text when the committed decisions file is missing or differs from a fresh one; else null. */
export function decisionsStale(root: string, config: DocsConfig): string | null {
  const file = path.join(root, decisionsPath(config));
  const fresh = decisionsTable(root, config);
  return existsSync(file) && readFileSync(file, "utf8") === fresh
    ? null
    : `NOTE docs [${STALE_CODE}] 결정 목록이 오래됨: pnpm tsx harness/src/checks/docs.ts decisions`;
}

/** Add `amended_by: [<this ADR>]` to every ADR the given ADR amends (front matter only). Returns the files changed. */
export function linkAmends(root: string, config: DocsConfig, file: string): string[] {
  const { data } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
  if (data === null || data.kind !== "decision") throw new Error(`${file} is not an ADR`);
  const self = path.posix.basename(file);
  const changed: string[] = [];
  const names = loadAdrs(root, config).map((a) => a.name);
  for (const { name, what } of amendEntries(data.amends, names)) {
    const target = `${config.adr_dir}/${name}`;
    if (!existsSync(path.join(root, target))) throw new Error(`amends '${name}': no such ADR file`);
    const parsed = splitFrontMatter(readFileSync(path.join(root, target), "utf8"));
    if (parsed.data === null) throw new Error(`${target} has no front matter`);
    const already = Array.isArray(parsed.data.amended_by) ? parsed.data.amended_by : [];
    const wanted = backEntry(self, what);
    const at = amendEntries(already, names).findIndex((e) => e.name === self);
    if (at >= 0 && JSON.stringify(already[at]) === JSON.stringify(wanted)) continue;
    parsed.data.amended_by =
      at >= 0 ? already.map((e, i) => (i === at ? wanted : e)) : [...already, wanted];
    writeFileSync(path.join(root, target), joinFrontMatter(parsed.data, parsed.body), "utf8");
    changed.push(target);
  }
  return changed;
}
