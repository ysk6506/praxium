import type { KG } from "../data";
import {
  formatTimestamp,
  kgRoleName,
  kgStateName,
  routeHash,
  titleOf,
  useKgs,
  useScreenTitle,
} from "../data";
import { Badge, EmptyState, Mono, PageHeader, Table, TBody, Td, THead, Th, Tr } from "../ui";
import { Async } from "./Async";

function KgRow({ kg }: { readonly kg: KG }) {
  // The address and the API name a KG by its alias, or by its ID when it has none.
  const href = routeHash({ page: "types", kg: kg.alias ?? kg.kg_id });
  return (
    <Tr>
      <Td>
        {kg.alias ? (
          <a href={href} className="font-medium">
            {kg.alias}
          </a>
        ) : (
          "—"
        )}
      </Td>
      <Td>
        {kg.alias ? (
          kg.name
        ) : (
          <a href={href} className="font-medium">
            {kg.name}
          </a>
        )}
      </Td>
      <Td>
        <Mono>{kg.kg_id}</Mono>
      </Td>
      <Td>
        <Badge tone={kg.role === "production" ? "accent" : "neutral"} title={kg.role}>
          {kgRoleName(kg.role)}
        </Badge>
      </Td>
      <Td>
        <Badge tone={kg.state === "serving" ? "ok" : "warn"} title={kg.state}>
          {kgStateName(kg.state)}
        </Badge>
      </Td>
      <Td className="text-right tabular-nums">{kg.head_change_seq}</Td>
      <Td className="whitespace-nowrap">{formatTimestamp(kg.created_at)}</Td>
    </Tr>
  );
}

/** The KGs the token may read. Choosing one opens its object types. */
export function KgsScreen() {
  const kgs = useKgs();
  useScreenTitle(titleOf("KG 목록"));
  return (
    <>
      <PageHeader
        title="KG 목록"
        subtitle="읽을 KG(지식 그래프)를 선택하십시오. 별칭은 주소와 API에서 KG를 가리키는 이름입니다."
      />
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
                  <Th>별칭</Th>
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
