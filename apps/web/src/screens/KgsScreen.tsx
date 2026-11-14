import type { KG } from "../data";
import { formatTimestamp, routeHash, useKgs } from "../data";
import { Badge, EmptyState, Mono, PageHeader, Table, TBody, Td, THead, Th, Tr } from "../ui";
import { Async } from "./Async";

const ROLE_LABELS: Readonly<Record<string, string>> = {
  production: "운영",
  candidate: "후보",
  test: "테스트",
  system: "시스템",
};

function KgRow({ kg }: { readonly kg: KG }) {
  const key = kg.alias ?? kg.kg_id;
  return (
    <Tr>
      <Td>
        <a href={routeHash({ page: "types", kg: key })} className="font-medium">
          {kg.alias ?? kg.name}
        </a>
        {kg.alias ? <div className="text-xs text-muted">{kg.name}</div> : null}
      </Td>
      <Td>
        <Mono>{kg.kg_id}</Mono>
      </Td>
      <Td>
        <Badge tone={kg.role === "production" ? "accent" : "neutral"}>
          {ROLE_LABELS[kg.role] ?? kg.role}
        </Badge>
      </Td>
      <Td>
        <Badge tone={kg.state === "serving" ? "ok" : "warn"}>{kg.state}</Badge>
      </Td>
      <Td className="text-right tabular-nums">{kg.head_change_seq}</Td>
      <Td className="whitespace-nowrap">{formatTimestamp(kg.created_at)}</Td>
    </Tr>
  );
}

/** The KGs the token may read. Choosing one opens its object types. */
export function KgsScreen() {
  const kgs = useKgs();
  return (
    <>
      <PageHeader title="KG 선택" subtitle="읽을 지식 그래프(KG)를 선택하십시오." />
      <Async result={kgs} loadingLabel="KG를 불러오는 중">
        {(list) =>
          list.length === 0 ? (
            <EmptyState title="읽을 수 있는 KG가 없습니다">
              서버에 KG가 없거나 이 토큰의 역할로는 볼 수 있는 KG가 없습니다.
            </EmptyState>
          ) : (
            <Table aria-label="KG 목록">
              <THead>
                <Tr>
                  <Th>이름</Th>
                  <Th>KG ID</Th>
                  <Th>역할</Th>
                  <Th>상태</Th>
                  <Th className="text-right">변경 번호</Th>
                  <Th>만든 시각</Th>
                </Tr>
              </THead>
              <TBody>
                {list.map((kg) => (
                  <KgRow key={kg.kg_id} kg={kg} />
                ))}
              </TBody>
            </Table>
          )
        }
      </Async>
    </>
  );
}
