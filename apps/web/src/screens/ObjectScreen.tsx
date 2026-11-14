import { useState } from "react";
import {
  basisLabel,
  directionLabel,
  EMPTY_CELL,
  formatCount,
  formatQuantity,
  formatTimestamp,
  type GetData,
  gradeLabel,
  gradeTone,
  groupNeighbors,
  NEIGHBOR_STEPS,
  type ObjectDetail,
  type ObjectTab,
  routeHash,
  useNeighbors,
  useObject,
} from "../data";
import {
  Badge,
  Button,
  EmptyState,
  KeyValues,
  Mono,
  PageHeader,
  Section,
  Table,
  Tabs,
  TBody,
  Td,
  THead,
  Th,
  Tr,
} from "../ui";
import { Async } from "./Async";

function GradeBadge({ grade }: { readonly grade: string | null | undefined }) {
  return <Badge tone={gradeTone(grade)}>{gradeLabel(grade)}</Badge>;
}

function AttributesTab({ data }: { readonly data: GetData }) {
  const attributes = data.attributes ?? [];
  if (attributes.length === 0)
    return (
      <EmptyState title="속성이 없습니다">이 객체에는 읽을 수 있는 속성이 없습니다.</EmptyState>
    );
  return (
    <Table aria-label="속성">
      <THead>
        <Tr>
          <Th>속성</Th>
          <Th>값</Th>
          <Th>근거 등급</Th>
          <Th>근거 종류</Th>
          <Th>출처</Th>
          <Th>관측 시각</Th>
        </Tr>
      </THead>
      <TBody>
        {attributes.map((a) => (
          <Tr key={a.name}>
            <Td className="font-mono">{a.name}</Td>
            <Td>
              {a.masked ? (
                <Badge>가려짐</Badge>
              ) : (
                <span className="break-all">{formatQuantity(a.value, a.unit)}</span>
              )}
              {a.stale ? (
                <span className="ml-2">
                  <Badge tone="warn">오래됨</Badge>
                </span>
              ) : null}
            </Td>
            <Td>
              <GradeBadge grade={a.grade} />
            </Td>
            <Td>{basisLabel(a.basis)}</Td>
            <Td>{a.source_ref ? <Mono>{a.source_ref}</Mono> : EMPTY_CELL}</Td>
            <Td className="whitespace-nowrap">{formatTimestamp(a.seen_at)}</Td>
          </Tr>
        ))}
      </TBody>
    </Table>
  );
}

function IdentifiersTab({ data }: { readonly data: GetData }) {
  const ids = data.identifiers ?? [];
  if (ids.length === 0)
    return (
      <EmptyState title="식별자가 없습니다">
        이 객체에는 바깥 식별자가 등록되어 있지 않습니다.
      </EmptyState>
    );
  return (
    <Table aria-label="식별자">
      <THead>
        <Tr>
          <Th>체계</Th>
          <Th>값</Th>
          <Th>범위</Th>
        </Tr>
      </THead>
      <TBody>
        {ids.map((i) => (
          <Tr key={`${i.scheme}:${i.scope ?? ""}:${i.value}`}>
            <Td className="font-mono">{i.scheme}</Td>
            <Td>
              <Mono>{i.value}</Mono>
            </Td>
            <Td>{i.scope ? <Mono>{i.scope}</Mono> : EMPTY_CELL}</Td>
          </Tr>
        ))}
      </TBody>
    </Table>
  );
}

function EvidenceTab({ detail }: { readonly detail: ObjectDetail }) {
  const { data, meta } = detail;
  const evidence = data.evidence;
  const counts = evidence?.counts;
  const freshness = evidence?.worst_freshness;
  return (
    <div className="grid gap-6 md:grid-cols-2">
      <Section title="근거">
        <KeyValues
          items={[
            ["가장 낮은 근거 등급", <GradeBadge key="g" grade={evidence?.min_grade} />],
            ["사실인 값", counts ? formatCount(counts.fact) : EMPTY_CELL],
            ["가정인 값", counts ? formatCount(counts.assumed) : EMPTY_CELL],
            ["알 수 없는 값", counts ? formatCount(counts.unknown) : EMPTY_CELL],
            [
              "가장 오래된 값",
              freshness
                ? `${formatCount(freshness.oldest_age_seconds)}초 전 (${freshness.attribute})`
                : EMPTY_CELL,
            ],
          ]}
        />
      </Section>
      <Section title="읽은 시점">
        <KeyValues
          items={[
            ["KG", <Mono key="kg">{meta.as_of.kg}</Mono>],
            ["변경 번호", String(meta.as_of.change_seq)],
            ["세대", String(meta.as_of.generation)],
            ["신선도", meta.freshness.state],
            [
              "이 객체 상태의 유효 구간",
              `${data.valid.from_change_seq} ~ ${data.valid.to_change_seq ?? "현재"}`,
            ],
          ]}
        />
      </Section>
      <Section title="링크 수">
        <KeyValues
          items={[
            ["나가는 링크", formatCount(data.link_counts?.out ?? 0)],
            ["들어오는 링크", formatCount(data.link_counts?.in ?? 0)],
          ]}
        />
      </Section>
      <Section title="출처 참조">
        {(data.provenance_refs ?? []).length === 0 ? (
          <p className="text-sm text-muted">출처 참조가 없습니다.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {(data.provenance_refs ?? []).map((ref) => (
              <li key={ref}>
                <Mono>{ref}</Mono>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  );
}

function NeighborsTab({ kg, id }: { readonly kg: string; readonly id: string }) {
  const [step, setStep] = useState(0);
  const limit = NEIGHBOR_STEPS[step] ?? NEIGHBOR_STEPS[0];
  const neighbors = useNeighbors(kg, id, limit);
  return (
    <Async result={neighbors} loadingLabel="이웃을 불러오는 중">
      {({ data, page }) => {
        const groups = groupNeighbors(data);
        if (page.total === 0) {
          return <EmptyState title="이웃이 없습니다">이 객체에 이어진 링크가 없습니다.</EmptyState>;
        }
        const more = page.truncated && step < NEIGHBOR_STEPS.length - 1;
        return (
          <>
            <div className="mb-3 flex items-center justify-between gap-3 text-sm text-muted">
              <span>
                링크 {formatCount(page.returned)} / 전체 {formatCount(page.total)}개를 링크 타입별로
                봅니다.
              </span>
              {more ? <Button onClick={() => setStep(step + 1)}>더 보기</Button> : null}
            </div>
            {groups.map((group) => (
              <Section
                key={`${group.linkType}-${group.direction}`}
                title={`${group.linkType} · ${directionLabel(group.direction)}`}
                aside={`${group.items.length}개`}
              >
                <Table aria-label={`${group.linkType} ${directionLabel(group.direction)}`}>
                  <THead>
                    <Tr>
                      <Th>이웃</Th>
                      <Th>타입</Th>
                      <Th>근거 등급</Th>
                      <Th>근거 종류</Th>
                    </Tr>
                  </THead>
                  <TBody>
                    {group.items.map((item) => (
                      <Tr key={item.link_id}>
                        <Td>
                          <a
                            href={routeHash({
                              page: "object",
                              kg,
                              id: item.peer_object_id,
                              tab: "attributes",
                            })}
                            className="font-medium"
                          >
                            {item.peer_display_name}
                          </a>
                        </Td>
                        <Td>{item.peer_type}</Td>
                        <Td>
                          <GradeBadge grade={item.grade} />
                        </Td>
                        <Td>{basisLabel(item.basis)}</Td>
                      </Tr>
                    ))}
                  </TBody>
                </Table>
              </Section>
            ))}
          </>
        );
      }}
    </Async>
  );
}

export interface ObjectScreenProps {
  readonly kg: string;
  readonly id: string;
  readonly tab: ObjectTab;
  readonly onTab: (tab: ObjectTab) => void;
}

/** One object: its attributes and identifiers, the evidence behind them, and its neighbors by link type. */
export function ObjectScreen({ kg, id, tab, onTab }: ObjectScreenProps) {
  const object = useObject(kg, id);
  return (
    <Async result={object} loadingLabel="객체를 불러오는 중">
      {(detail) => {
        const { data } = detail;
        return (
          <>
            <PageHeader
              title={data.object.display_name}
              trail={[
                { label: "KG 선택", href: routeHash({ page: "kgs" }) },
                { label: kg, href: routeHash({ page: "types", kg }) },
                {
                  label: data.object.object_type,
                  href: routeHash({ page: "objects", kg, type: data.object.object_type }),
                },
              ]}
              subtitle={
                <span className="flex flex-wrap items-center gap-2">
                  <Badge tone="accent">{data.object.object_type}</Badge>
                  <Mono>{data.object.object_id}</Mono>
                  <GradeBadge grade={data.evidence?.min_grade} />
                </span>
              }
            />
            <Tabs
              value={tab}
              onChange={onTab}
              items={[
                {
                  value: "attributes",
                  label: `속성 (${formatCount((data.attributes ?? []).length)})`,
                  content: <AttributesTab data={data} />,
                },
                {
                  value: "identifiers",
                  label: `식별자 (${formatCount((data.identifiers ?? []).length)})`,
                  content: <IdentifiersTab data={data} />,
                },
                { value: "neighbors", label: "이웃", content: <NeighborsTab kg={kg} id={id} /> },
                {
                  value: "evidence",
                  label: "근거와 출처",
                  content: <EvidenceTab detail={detail} />,
                },
              ]}
            />
          </>
        );
      }}
    </Async>
  );
}
