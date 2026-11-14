import { useMemo, useState } from "react";
import { formatCount, routeHash, type TypeEntry, useTypeCatalog } from "../data";
import {
  Badge,
  Checkbox,
  EmptyState,
  PageHeader,
  Table,
  TBody,
  Td,
  TextField,
  THead,
  Th,
  Tr,
} from "../ui";
import { Async } from "./Async";

function filterTypes(types: readonly TypeEntry[], text: string, onlyFilled: boolean): TypeEntry[] {
  const needle = text.trim().toLowerCase();
  return types.filter(
    (t) =>
      (!onlyFilled || t.count > 0) &&
      (needle === "" ||
        t.name.toLowerCase().includes(needle) ||
        t.description.toLowerCase().includes(needle)),
  );
}

/** The object types of the model with how many objects the chosen KG holds of each. */
export function TypesScreen({ kg }: { readonly kg: string }) {
  const catalog = useTypeCatalog(kg);
  const [text, setText] = useState("");
  const [onlyFilled, setOnlyFilled] = useState(true);
  const all = catalog.state.data;
  const shown = useMemo(
    () => (all ? filterTypes(all, text, onlyFilled) : []),
    [all, text, onlyFilled],
  );
  return (
    <>
      <PageHeader
        title="객체 타입"
        trail={[{ label: "KG 선택", href: routeHash({ page: "kgs" }) }, { label: kg }]}
        subtitle="타입을 선택하면 그 타입의 객체를 테이블로 봅니다."
      />
      <Async result={catalog} loadingLabel="객체 타입을 불러오는 중">
        {(types) => (
          <>
            <div className="mb-3 flex flex-wrap items-end gap-4">
              <div className="w-72">
                <TextField
                  label="타입 검색"
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  placeholder="이름이나 설명"
                />
              </div>
              <Checkbox checked={onlyFilled} onChange={setOnlyFilled}>
                객체가 있는 타입만
              </Checkbox>
              <span className="pb-1.5 text-sm text-muted">
                {shown.length} / {types.length}개 타입
              </span>
            </div>
            {shown.length === 0 ? (
              <EmptyState title="맞는 타입이 없습니다">
                {onlyFilled
                  ? "‘객체가 있는 타입만’을 끄거나 검색어를 바꿔 보십시오."
                  : "검색어를 바꿔 보십시오."}
              </EmptyState>
            ) : (
              <Table aria-label="객체 타입">
                <THead>
                  <Tr>
                    <Th>타입</Th>
                    <Th>계층</Th>
                    <Th>설명</Th>
                    <Th className="text-right">객체 수</Th>
                  </Tr>
                </THead>
                <TBody>
                  {shown.map((t) => (
                    <Tr key={t.name}>
                      <Td>
                        <a
                          href={routeHash({ page: "objects", kg, type: t.name })}
                          className="font-medium"
                        >
                          {t.name}
                        </a>
                      </Td>
                      <Td>
                        <Badge>{t.layer}</Badge>
                      </Td>
                      <Td className="text-muted">{t.description}</Td>
                      <Td className="text-right tabular-nums">{formatCount(t.count)}</Td>
                    </Tr>
                  ))}
                </TBody>
              </Table>
            )}
          </>
        )}
      </Async>
    </>
  );
}
