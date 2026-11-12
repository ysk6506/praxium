/**
 * The quality table (REQ-024, ADR-0148): docs/generated/quality.md, one line per architecture unit with a grade
 * A-D that follows from measured values by the rule printed above the table. The harness writes the file
 * (`pnpm quality write`); this check fails when the committed file differs from what the repository now produces.
 *
 * QUAL002 the coverage record is malformed   QUAL003 a debt item names a unit that does not exist
 * QUAL001 is a note, not a failure: the committed table is missing or stale. Nearly every change moves a cell, so
 * a red check would make every change regenerate it and every open PR conflict on it. The check fails only when
 * the table cannot be generated; the cleanup job regenerates and commits it (ADR-0148).
 */
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import {
  findRoot,
  finish,
  flag,
  isMain,
  parseArgs,
  today,
  type Violation,
  violation,
} from "../common";
import { loadArchConfig } from "./arch-model";
import { coverageByUnit } from "./coverage";
import { loadDebtConfig, loadRegister } from "./debt";
import { collect, counts, loadIgnoresConfig } from "./ignores";
import { KINDS } from "./ignores-scan";
import { gatherFacts, NO_UNIT, RECORD_FILE, readRecord, type UnitFacts } from "./quality-facts";
import { loadTestsConfig } from "./testguard";

export const TABLE_FILE = "docs/generated/quality.md";

export type Grade = "A" | "B" | "C" | "D";

type Reasons = ReadonlyArray<readonly [boolean, string]>;

const reasonsOf = (rules: Reasons): string[] => rules.filter(([on]) => on).map(([, why]) => why);

function belowFloor(f: UnitFacts): boolean {
  return f.percent !== undefined && f.floor !== undefined && f.percent + 1e-9 < f.floor;
}

/** Grade and the measured reasons behind anything below A, from one unit's facts. No judgement enters. */
export function gradeOf(f: UnitFacts): { grade: Grade; why: string[] } {
  const d = reasonsOf([
    [belowFloor(f), "커버리지가 하한 아래"],
    [f.over > 0, `한도 초과 ${f.over}`],
    [f.debtHigh > 0, `열린 고위험 부채 ${f.debtHigh}`],
  ]);
  if (d.length > 0) return { grade: "D", why: d };
  const c = reasonsOf([
    [f.debtOpen >= 2, `열린 부채 ${f.debtOpen}`],
    [f.near >= 5, `한도 90% 넘음 ${f.near}`],
    [f.ignores >= 10, `끄기 ${f.ignores}`],
  ]);
  if (c.length > 0) return { grade: "C", why: c };
  const b = reasonsOf([
    [f.debtOpen === 1, "열린 부채 1"],
    [f.near >= 1, `한도 90% 넘음 ${f.near}`],
    [f.ignores >= 1, `끄기 ${f.ignores}`],
    [f.floor !== undefined && f.percent === undefined, "커버리지 모름"],
  ]);
  return b.length > 0 ? { grade: "B", why: b } : { grade: "A", why: [] };
}

const RANK: Record<Grade, number> = { D: 0, C: 1, B: 2, A: 3 };

const RULES = `## 등급 규칙

위에서부터 처음 맞는 줄이 그 단위의 등급이에요. 값은 모두 재서 나오고, 사람이 등급을 고칠 수 없어요.

| 등급 | 조건 |
|---|---|
| D | 커버리지가 하한 아래이거나, 한도를 넘은 파일·함수가 있거나, 열린 고위험(high) 부채가 있어요 |
| C | D가 아니고, 열린 부채가 2건 이상이거나, 한도 90%를 넘은 파일·함수가 5개 이상이거나, 끄기가 10곳 이상이에요 |
| B | C가 아니고, 열린 부채가 1건이거나, 한도 90%를 넘은 것이 1개 이상이거나, 끄기가 1곳 이상이거나, 커버리지가 "모름"이에요 |
| A | 위에 하나도 걸리지 않아요: 열린 부채 0건, 한도 90%를 넘은 것 0개, 끄기 0곳, 커버리지가 하한 이상으로 기록되어 있어요 |

- **모름**: 커버리지는 CI의 coverage 작업에서만 재서, 마지막으로 기록한 값(\`${RECORD_FILE}\`)을 읽어요. 그 단위의 기록이 없으면 "모름"이고, 지어내지 않아요. 모름인 단위는 A가 될 수 없고 B가 가장 좋아요. 한도 아래로 알려진 값이 아니면 D로 내리지도 않아요.
- **—**: 커버리지를 재지 않는 단위예요(하한이 없어요). 커버리지는 등급에 들어가지 않아요.
- **끄기**: \`biome-ignore\` 같은 검사 끄기 주석, 건너뛴 시험, CI의 \`continue-on-error\`예요 (ADR-0148, \`IGN*\`).
- **한도 90%를 넘음**: 파일 500줄, 함수 80줄, 복잡도 12 한도의 90%(450줄, 72줄, 10.8)를 넘은 파일·함수 수예요. 괄호는 그중 한도 자체를 넘은 것이에요(매개변수 6개 한도 포함).
- **열린 부채**: 장부의 열린 항목이에요. 항목이 \`units\`로 단위를 적었거나, 그 단위의 코드 주석이 \`TD-NNNN\`으로 가리키면 그 단위의 부채예요. 어느 단위에도 속하지 않는 항목은 "(단위 밖)" 줄에 있어요. 괄호는 그중 가장 이른 기한이에요.
- **문서**: 그 단위의 코드를 \`covers\`로 다루는 활성 문서 중 가장 오래된 마지막 확인일이에요(문서마다 확인 기록 중 가장 새 날짜, ADR-0194) (결정 문서 ADR은 빼요).
- 오늘 날짜를 쓰지 않아요. 기한이 지난 부채는 \`DEBT011\`이, 너무 오래된 문서는 \`DOC020\`이 따로 빨갛게 해요.`;

const fixed = (value: number): string => value.toFixed(1);

function coverageCell(f: UnitFacts): string {
  if (f.floor === undefined) return "—";
  return `${f.percent === undefined ? "모름" : fixed(f.percent)} / ${fixed(f.floor)}`;
}

function row(f: UnitFacts): string {
  const { grade, why } = gradeOf(f);
  const debt = f.debtOpen === 0 ? "0" : `${f.debtOpen} (${f.earliestDue})`;
  const doc = f.oldestDoc ? `${f.oldestDoc.date} (${path.basename(f.oldestDoc.file)})` : "—";
  return `| ${f.unit} | ${grade} | ${coverageCell(f)} | ${f.ignores} | ${f.near} (${f.over}) | ${debt} | ${doc} | ${why.join(", ") || "—"} |`;
}

/** Worst grade first; units of the same grade keep the order of architecture.yaml. */
export function sorted(facts: readonly UnitFacts[]): UnitFacts[] {
  return facts
    .map((f, index) => ({ f, index, rank: RANK[gradeOf(f).grade] }))
    .sort((a, b) => a.rank - b.rank || a.index - b.index)
    .map((x) => x.f);
}

export function renderTable(
  facts: readonly UnitFacts[],
  ignoreCounts: Readonly<Record<string, number>>,
  measuredOn: string | undefined,
): string {
  const totals = KINDS.map((k) => `${k} ${ignoreCounts[k] ?? 0}`).join(", ");
  const record = measuredOn === undefined ? '없음 (모든 단위가 "모름")' : measuredOn;
  const grades = (["D", "C", "B", "A"] as const)
    .map((g) => `${g} ${facts.filter((f) => gradeOf(f).grade === g).length}`)
    .join(", ");
  return [
    "# 품질 등급표",
    "",
    "이 문서는 하네스가 만들어요(`pnpm quality write`). 손으로 고치지 않아요. 5시간마다 도는 정리 작업이 새로 만들어 올려요. 일반 변경은 건드리지 않아도 되고, 충돌하면 고치지 말고 다시 만들어요. 낡으면 구조 검사가 알림 한 줄(`QUAL001`)만 내고 빨갛게 하지는 않아요. 변경마다 자기 등급은 CI의 job 요약에 나와요.",
    '정리 작업은 이 표의 위쪽, 등급이 가장 낮은 단위부터 시작해요. 한 줄의 "이유" 칸이 무엇을 고치면 등급이 오르는지 알려줘요.',
    "",
    RULES,
    "",
    "## 단위별",
    "",
    `단위 수 등급별: ${grades}. 커버리지 기록: ${record}. 끄기 종류별: ${totals}.`,
    "",
    "| 단위 | 등급 | 커버리지 (값 / 하한) | 끄기 | 한도 90% 넘음 (초과) | 열린 부채 (가장 이른 기한) | 가장 오래된 문서 | 이유 |",
    "|---|---|---|---|---|---|---|---|",
    ...sorted(facts).map(row),
    "",
  ].join("\n");
}

export function render(root: string): string {
  const found = collect(root, loadIgnoresConfig(root));
  return renderTable(gatherFacts(root, found), counts(found), readRecord(root)?.measuredOn);
}

function unitNameProblems(root: string): Violation[] {
  const known = new Set(loadArchConfig(root).units.map((u) => u.name));
  const config = loadDebtConfig(root);
  return loadRegister(root, config).flatMap((item) =>
    (Array.isArray(item.units) ? item.units : [])
      .filter((unit) => !known.has(String(unit)) && unit !== NO_UNIT)
      .map((unit) =>
        violation(
          "QUAL003",
          config.register,
          0,
          `${String(item.id)} names unit '${String(unit)}', which is not in architecture.yaml`,
          "Use a unit name from harness/config/architecture.yaml, or drop the `units` field.",
        ),
      ),
  );
}

export function report(root: string): { violations: Violation[]; notes: string[] } {
  try {
    readRecord(root);
  } catch (error) {
    const bad = violation(
      "QUAL002",
      RECORD_FILE,
      0,
      (error as Error).message,
      "Regenerate it from a coverage run: pnpm tsx harness/src/checks/quality.ts record",
    );
    return { violations: [bad], notes: [] };
  }
  const file = path.join(root, TABLE_FILE);
  const fresh = render(root); // generating must work even when nothing is committed yet
  const stale = !existsSync(file) || readFileSync(file, "utf8") !== fresh;
  return {
    violations: unitNameProblems(root),
    notes: stale ? ["NOTE quality [QUAL001] 낡음: pnpm quality write"] : [],
  };
}

export const run = (root: string): Violation[] => report(root).violations;

export function write(root: string): void {
  const file = path.join(root, TABLE_FILE);
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, render(root));
}

/** Record this run's per-unit coverage (from coverage/coverage-summary.json) for the table to read. */
export function recordCoverage(root: string): string {
  const config = loadTestsConfig(root);
  const summaryFile = path.join(root, config.coverage_summary);
  if (!existsSync(summaryFile))
    throw new Error("coverage summary missing: run the coverage job first");
  const summary = JSON.parse(readFileSync(summaryFile, "utf8")) as Parameters<
    typeof coverageByUnit
  >[1];
  const units = Object.fromEntries(
    [...coverageByUnit(root, summary)]
      .sort(([a], [b]) => Number(a > b) - Number(a < b))
      .map(([unit, c]) => [unit, Math.round(c.percent * 100) / 100]),
  );
  const text = `${JSON.stringify({ measuredOn: today(), units }, null, 2)}\n`;
  mkdirSync(path.dirname(path.join(root, RECORD_FILE)), { recursive: true });
  writeFileSync(path.join(root, RECORD_FILE), text);
  return text;
}

export function main(argv: readonly string[], out: (line: string) => void = console.log): number {
  const { positional, flags } = parseArgs(argv);
  const root = findRoot(flag(flags, "root"));
  if (positional[0] === "write") {
    write(root);
    out(`wrote ${TABLE_FILE}`);
    return 0;
  }
  if (positional[0] === "print") {
    out(render(root));
    return 0;
  }
  if (positional[0] === "record") {
    out(recordCoverage(root));
    return 0;
  }
  const { violations, notes } = report(root);
  const code = finish("quality", violations, out);
  for (const line of notes) out(line);
  return code;
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
