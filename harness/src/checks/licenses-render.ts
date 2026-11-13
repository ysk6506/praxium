/** Writing THIRD_PARTY_NOTICES.md from the entries the check found. The file is made, never edited by hand. */
import type { Pkg } from "./licenses-scan";

/** What the notices file needs to know about one component, whether it came from npm or the manifest. */
export interface Component {
  readonly name: string;
  readonly version: string;
  /** SPDX identifiers that apply (an OR expression has been resolved to the alternative taken). */
  readonly licenses: readonly string[];
  /** The expression as declared, shown when it differs from what applies. */
  readonly declared: string;
  readonly source: string;
  readonly where: string;
  /** License texts shipped with the component, as found. */
  readonly texts: readonly string[];
  readonly notices: readonly string[];
  readonly author?: string;
  /** From the asset manifest rather than the installed packages. */
  readonly asset?: boolean;
}

// A copyright line, not a license sentence that happens to start with the word ("Copyright Holder ...").
const COPYRIGHT =
  /^\s*[*#/;>\s-]*(copyright\b(?!\s*$|\s+(holder|statement|notice|owner|law|license|and|in|of|or|for))|©|\(c\)\s+\d)/i;
const STOP = /^\s*(redistribution|permission|all rights reserved|licensed under|this software)/i;

/** Split a license text into its copyright paragraphs and the rest (the part that is the same for everyone). */
export function splitCopyright(text: string): { copyright: string[]; body: string } {
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  const copyright: string[] = [];
  const keep: string[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] as string;
    if (!COPYRIGHT.test(line) || /yyyy/i.test(line)) {
      keep.push(line);
      continue;
    }
    const para = [line.trim()];
    while (
      para.length < 4 &&
      (lines[i + 1] ?? "").trim() !== "" &&
      (!STOP.test(lines[i + 1] as string) ||
        /^\s*all rights reserved/i.test(lines[i + 1] as string))
    ) {
      para.push((lines[++i] as string).trim());
      if (/all rights reserved/i.test(para[para.length - 1] as string)) break;
    }
    copyright.push(para.join(" "));
  }
  return {
    copyright,
    body: keep
      .join("\n")
      .replace(/\n{3,}/g, "\n\n")
      .trim(),
  };
}

const squash = (s: string): string => s.replace(/\s+/g, " ").trim().toLowerCase();

export function fromPackage(pkg: Pkg, licenses: readonly string[], declared: string): Component {
  return {
    name: pkg.name,
    version: pkg.version,
    licenses,
    declared,
    source: pkg.sourceUrl,
    where: pkg.via
      .map((v) => (v === "web" ? "web 번들" : v === "runtime" ? "서버 실행" : `${v} 코드`))
      .join(", "),
    texts: pkg.licenseFiles.map((f) => f.text),
    notices: pkg.noticeFiles.map((f) => f.text.trim()),
    ...(pkg.author ? { author: pkg.author } : {}),
  };
}

interface Group {
  id: string;
  license: string;
  body: string;
  members: string[];
}

function groupTexts(components: readonly Component[]): {
  groups: Group[];
  of: Map<string, string>;
} {
  const byKey = new Map<string, Group>();
  const of = new Map<string, string>();
  const counters = new Map<string, number>();
  const label = (c: Component): string => c.licenses.join(" + ");
  for (const c of components) {
    const text = c.texts[0];
    if (text === undefined) continue;
    const { body } = splitCopyright(text);
    const key = `${label(c)}\n${squash(body)}`;
    let group = byKey.get(key);
    if (!group) {
      const n = (counters.get(label(c)) ?? 0) + 1;
      counters.set(label(c), n);
      group = { id: `${label(c)}-${n}`, license: label(c), body, members: [] };
      byKey.set(key, group);
    }
    group.members.push(`${c.name}@${c.version}`);
    of.set(`${c.name}@${c.version}`, group.id);
  }
  const groups = [...byKey.values()].sort(
    (a, b) => a.license.localeCompare(b.license) || a.id.localeCompare(b.id),
  );
  return { groups, of };
}

function section(c: Component, groupId: string | undefined): string[] {
  const out = [`### ${c.asset ? `${c.name} (${c.version})` : `${c.name}@${c.version}`}`, ""];
  const license = c.licenses.join(" + ");
  out.push(`- 라이선스: ${license}${c.declared !== license ? ` (선언: \`${c.declared}\`)` : ""}`);
  out.push(`- 출처: ${c.source}`);
  out.push(`- 들어가는 곳: ${c.where}`);
  const copyright = c.texts.flatMap((t) => splitCopyright(t).copyright);
  const unique = [...new Set(copyright)];
  if (unique.length > 0) for (const line of unique) out.push(`- 저작권: ${line}`);
  else
    out.push(
      `- 저작권: 라이선스 파일에 저작권 줄이 없어요${c.author ? ` (package.json의 author: ${c.author})` : ""}`,
    );
  if (groupId)
    out.push(`- 라이선스 전문: [${groupId}](#${groupId.toLowerCase().replace(/[^a-z0-9-]/g, "")})`);
  else out.push("- 라이선스 전문: 패키지가 라이선스 파일을 싣지 않았어요 (확인 못 함)");
  for (const extra of c.texts.slice(1))
    out.push("", "라이선스 파일(추가):", "", "```text", extra.trim(), "```");
  for (const notice of c.notices)
    out.push("", "고지 문구 또는 NOTICE 파일:", "", "```text", notice, "```");
  out.push("");
  return out;
}

/** Data in the repository drawn by hand with outside material as a guide; nothing was copied. */
export interface Reference {
  readonly id: string;
  readonly name: string;
  readonly paths?: readonly string[];
  readonly text: string;
}

export function renderNotices(
  components: readonly Component[],
  fingerprint: string,
  references: readonly Reference[] = [],
): string {
  const sorted = [...components].sort(
    (a, b) => a.name.localeCompare(b.name) || a.version.localeCompare(b.version),
  );
  const { groups, of } = groupTexts(sorted);
  const counts = new Map<string, number>();
  for (const c of sorted)
    counts.set(c.licenses.join(" + "), (counts.get(c.licenses.join(" + ")) ?? 0) + 1);
  const out = [
    "# Third-Party Notices",
    "",
    "Praxium은 Apache License 2.0이에요 ([LICENSE](LICENSE)). 이 파일은 Praxium과 함께 나가는 제3자 구성요소와 그 라이선스 고지예요.",
    "`pnpm tsx harness/src/checks/licenses.ts write`가 만들어요. 손으로 수정하지 않아요. npm 의존성은 설치된 패키지에서, 그 밖의 자료는 `harness/config/third-party-assets.yaml`에서 와요.",
    "",
    "- 이 목록은 웹·서버에서 도달할 수 있는 모든 패키지를 담아 실제 번들보다 넓어요.",
    "- 소스 zip과 컨테이너 이미지에 이 파일이 들어 있어요.",
    "",
    `<!-- licenses-fingerprint: ${fingerprint} -->`,
    "",
    "## 요약",
    "",
    "| 라이선스 | 구성요소 수 |",
    "|---|---|",
    ...[...counts].sort(([a], [b]) => a.localeCompare(b)).map(([l, n]) => `| ${l} | ${n} |`),
    "",
    "## 구성요소",
    "",
  ];
  for (const c of sorted) out.push(...section(c, of.get(`${c.name}@${c.version}`)));
  out.push(
    "## 라이선스 전문",
    "",
    "같은 전문은 한 번만 적고, 구성요소의 저작권 줄은 위 각 항목에 있어요.",
    "",
  );
  for (const g of groups) {
    out.push(
      `### ${g.id}`,
      "",
      `쓰는 구성요소: ${g.members.join(", ")}`,
      "",
      "```text",
      g.body,
      "```",
      "",
    );
  }
  if (references.length > 0) {
    out.push("## 바깥 자료를 참고한 데이터", "");
    for (const r of references) {
      out.push(`### ${r.name}`, "", r.text.replace(/\s+/g, " ").trim(), "");
      if (r.paths?.length) out.push(`위치: ${r.paths.map((p) => `\`${p}\``).join(", ")}`, "");
    }
  }
  return `${out.join("\n").trimEnd()}\n`;
}
