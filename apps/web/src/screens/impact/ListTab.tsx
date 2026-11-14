import { type ReactNode, useMemo, useState } from "react";
import {
  channelName,
  EMPTY_CELL,
  firstRows,
  type GroupTouched,
  gradeLabel,
  gradeStyle,
  type ImpactRow,
  type ImpactSortKey,
  type ImpactTree,
  impactGradeName,
  impactRows,
  ROWS_STEP,
  routeHash,
  sectionsByGrade,
  sortRows,
} from "../../data";
import {
  EmptyState,
  GradeList,
  type ListColumn,
  type ListRow,
  type ListSection,
  Section,
  Table,
  TBody,
  Td,
  THead,
  Th,
  Tr,
} from "../../ui";

const COLUMNS: readonly { key: ImpactSortKey; label: string }[] = [
  { key: "name", label: "객체" },
  { key: "type", label: "타입" },
  { key: "hops", label: "단계" },
  { key: "lost", label: "잃은 것" },
  { key: "via", label: "바로 앞 원인" },
  { key: "basis", label: "근거" },
];

type Sort = { readonly key: ImpactSortKey; readonly direction: "ascending" | "descending" };

const objectLink = (kg: string, id: string, name: string): ReactNode => (
  <a href={routeHash({ page: "object", kg, id, tab: "attributes" })}>{name}</a>
);

function notesOf(row: ImpactRow): string[] {
  const notes: string[] = [];
  if (row.viaHidden) notes.push("보이지 않는 객체를 거침");
  if (row.delayed) notes.push("지연 있음");
  if (row.single) notes.push("단일 장애점");
  return notes;
}

function rowOf(kg: string, row: ImpactRow): ListRow {
  return {
    id: row.id,
    cells: [
      objectLink(kg, row.id, row.name),
      row.type,
      `${row.hops}단계`,
      row.lost.length === 0 ? EMPTY_CELL : row.lost.map(channelName).join(", "),
      row.via !== undefined && row.viaId !== null ? objectLink(kg, row.viaId, row.via) : EMPTY_CELL,
      row.weakest !== undefined ? gradeLabel(row.weakest) : row.assumed ? "가정" : EMPTY_CELL,
    ],
    notes: notesOf(row),
  };
}

/** The next sort when a column head is pressed: the column ascending, then descending, then the order of the answer. */
export function nextSortOf(current: Sort | undefined, key: ImpactSortKey): Sort | undefined {
  if (current?.key !== key) return { key, direction: "ascending" };
  return current.direction === "ascending" ? { key, direction: "descending" } : undefined;
}

function GroupsTouched({ groups }: { readonly groups: readonly GroupTouched[] }) {
  if (groups.length === 0) return null;
  return (
    <Section
      title="영향받은 이중화 그룹"
      note="이중화를 잃었다는 말의 근거입니다. 정상 구성원이 최소 정상 수보다 많으면 그룹은 버티지만 여유가 줄어듭니다."
    >
      <Table aria-label="영향받은 이중화 그룹">
        <THead>
          <Tr>
            <Th>그룹</Th>
            <Th>타입</Th>
            <Th>정상 / 전체</Th>
            <Th>최소 정상 수</Th>
            <Th>여유</Th>
            <Th>그룹의 상태</Th>
          </Tr>
        </THead>
        <TBody>
          {groups.map((g) => (
            <Tr key={g.group_id}>
              <Td>{g.display_name}</Td>
              <Td>{g.group_type}</Td>
              <Td className="tabular-nums">
                {g.alive} / {g.n}
                {g.unknown > 0 ? ` (알 수 없음 ${g.unknown})` : ""}
              </Td>
              <Td className="tabular-nums">{g.min_alive ?? EMPTY_CELL}</Td>
              <Td className="tabular-nums">{g.slack ?? EMPTY_CELL}</Td>
              <Td>{impactGradeName(g.value)}</Td>
            </Tr>
          ))}
        </TBody>
      </Table>
    </Section>
  );
}

export interface ListTabProps {
  /** The id of the list, so a link in the graph can move focus here. */
  readonly id: string;
  readonly kg: string;
  readonly tree: ImpactTree;
  readonly sort: Sort | undefined;
  readonly onSort: (sort: Sort | undefined) => void;
  readonly groups: readonly GroupTouched[];
  /** How many objects the server counted that the tree does not hold (they were not received). */
  readonly missing: number;
  readonly hidden: number;
}

/** The answer as tables by result: every item received is a row. This is the answer; the graph is another way to see it. */
export function ListTab(props: ListTabProps) {
  const { kg, tree, sort, onSort, groups, missing, hidden } = props;
  const [counts, setCounts] = useState<Readonly<Record<string, number>>>({});
  const rows = useMemo(() => sortRows(impactRows(tree), sort), [tree, sort]);
  const sections: ListSection[] = sectionsByGrade(rows).map((section) => {
    const shown = firstRows(section.rows, counts[section.grade] ?? ROWS_STEP);
    return {
      key: section.grade,
      mark: gradeStyle(section.grade),
      total: shown.total,
      rows: shown.rows.map((row) => rowOf(kg, row)),
    };
  });
  const columns: ListColumn[] = COLUMNS.map((c) => ({
    key: c.key,
    label: c.label,
    sort: { direction: sort?.key === c.key ? sort.direction : undefined },
  }));
  return (
    <div className="flex flex-col gap-4" id={props.id} tabIndex={-1}>
      <p className="text-sm text-muted">
        받은 항목 {rows.length}개를 등급별로 나눴습니다.
        {missing > 0 ? ` 서버가 센 전체보다 ${missing}개 적습니다.` : ""}
        {hidden > 0
          ? ` 읽을 수 없는 객체를 거친 경로가 있어 숨김 객체 ${hidden}개는 이름 없이 셉니다.`
          : ""}
      </p>
      <GradeList
        sections={sections}
        columns={columns}
        onSort={(key) => onSort(nextSortOf(sort, key as ImpactSortKey))}
        onMore={(key) => setCounts((c) => ({ ...c, [key]: (c[key] ?? ROWS_STEP) + ROWS_STEP }))}
        empty={
          <EmptyState title="받은 항목이 없습니다">
            이 등급에는 받은 항목이 없습니다. 등급별 개수에서 다른 등급을 고르거나 거르기를 풀어
            보십시오.
          </EmptyState>
        }
      />
      <GroupsTouched groups={groups} />
    </div>
  );
}
