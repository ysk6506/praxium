import type { ReactNode } from "react";
import {
  channelName,
  EMPTY_CELL,
  gradeLabel,
  gradeStyle,
  type ImpactGroup,
  type ImpactNode,
  type ImpactTree,
  layerName,
  routeHash,
} from "../../data";
import { Badge, GradeMark, KeyValues } from "../../ui";

/** The most members of a group listed at once. */
export const MEMBER_LIMIT = 100;

const isReadable = (node: ImpactNode): boolean => node.kind !== "hidden";

const link = (kg: string, node: ImpactNode): ReactNode =>
  isReadable(node) ? (
    <a href={routeHash({ page: "object", kg, id: node.id, tab: "attributes" })}>{node.name}</a>
  ) : (
    "숨김 객체"
  );

/** The nodes from the start of the question to this one, the start first. */
export function pathTo(tree: ImpactTree, node: ImpactNode): ImpactNode[] {
  const path: ImpactNode[] = [];
  const seen = new Set<string>();
  for (let at: ImpactNode | undefined = node; at && !seen.has(at.id); ) {
    seen.add(at.id);
    path.push(at);
    at = at.parent === null ? undefined : tree.nodes.get(at.parent);
  }
  return path.reverse();
}

function evidenceText(node: ImpactNode): string {
  const parts: string[] = [];
  if (node.weakest !== undefined)
    parts.push(`경로에서 가장 약한 근거: ${gradeLabel(node.weakest)}`);
  else if (node.assumed) parts.push("가정에 근거합니다");
  if (node.viaHidden) parts.push("보이지 않는 객체를 거칩니다");
  return parts.length === 0 ? EMPTY_CELL : parts.join(". ");
}

export interface NodeDetailProps {
  readonly kg: string;
  readonly tree: ImpactTree;
  readonly node: ImpactNode;
}

/** One object of the answer: what it is, how it was reached from the start, what it lost, on what evidence. */
export function NodeDetail({ kg, tree, node }: NodeDetailProps) {
  const style = gradeStyle(node.grade);
  const upstream = tree.direction === "upstream";
  const path = pathTo(tree, node);
  return (
    <section aria-label="선택한 객체" className="rounded-md border border-line bg-surface p-3">
      <h3 className="mb-2 text-base font-semibold break-words">{link(kg, node)}</h3>
      <KeyValues
        items={[
          ["타입", `${node.type} (${layerName(node.layer)})`],
          [
            "등급",
            node.grade === undefined ? (
              "경유한 객체입니다(영향받는 항목이 아님)"
            ) : (
              <GradeMark key="g" {...style} />
            ),
          ],
          ["단계", `${node.hops}단계`],
          ["잃은 것", node.lost.length === 0 ? EMPTY_CELL : node.lost.map(channelName).join(", ")],
          [
            upstream ? "대상에서 여기까지" : "원인으로 가는 경로",
            <ol key="p" className="flex flex-wrap items-center gap-1">
              {path.map((step, i) => (
                <li key={step.id} className="flex items-center gap-1">
                  {i > 0 ? <span aria-hidden>→</span> : null}
                  {link(kg, step)}
                </li>
              ))}
            </ol>,
          ],
          ["근거", evidenceText(node)],
          [
            "표시",
            <span key="b" className="flex flex-wrap gap-1">
              {node.delayed ? <Badge tone="neutral">지연 있음</Badge> : null}
              {node.single ? <Badge tone="danger">단일 장애점</Badge> : null}
              {!node.delayed && !node.single ? EMPTY_CELL : null}
            </span>,
          ],
        ]}
      />
    </section>
  );
}

export interface GroupDetailProps {
  readonly kg: string;
  readonly tree: ImpactTree;
  readonly group: ImpactGroup;
}

/** A group of the overview: how many objects of one type it stands for, with which results, and the first of them. */
export function GroupDetail({ kg, tree, group }: GroupDetailProps) {
  const shown = group.members.slice(0, MEMBER_LIMIT);
  const grades = Object.entries(group.grades);
  return (
    <section aria-label="선택한 묶음" className="rounded-md border border-line bg-surface p-3">
      <h3 className="mb-1 text-base font-semibold">
        {group.type} {group.count}개
      </h3>
      <ul className="mb-2 flex flex-wrap gap-3 text-sm">
        {grades.map(([grade, n]) => (
          <li key={grade}>
            <GradeMark {...gradeStyle(grade)} count={n} />
          </li>
        ))}
      </ul>
      <p className="mb-1 text-sm text-muted">
        구성원 {shown.length}개를 심각한 순서로 보입니다
        {group.members.length > shown.length
          ? `(나머지 ${group.members.length - shown.length}개는 목록 탭에서 볼 수 있습니다)`
          : ""}
        .
      </p>
      <section aria-label="묶음의 구성원" className="max-h-72 overflow-auto">
        <ul className="flex flex-col gap-0.5 text-sm">
          {shown.map((id) => {
            const node = tree.nodes.get(id);
            if (node === undefined) return null;
            return (
              <li key={id} className="flex flex-wrap items-center gap-2">
                <GradeMark {...gradeStyle(node.grade)} />
                {link(kg, node)}
              </li>
            );
          })}
        </ul>
      </section>
    </section>
  );
}
