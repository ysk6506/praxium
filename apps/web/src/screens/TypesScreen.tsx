import { useMemo } from "react";
import {
  formatCount,
  layerName,
  nextSort,
  readTypesView,
  routeHash,
  sortTypes,
  type TypeEntry,
  type TypeSortKey,
  type TypesView,
  typesViewParams,
  useHashParams,
  useTypeCatalog,
} from "../data";
import {
  Badge,
  Checkbox,
  EmptyState,
  PageHeader,
  SortTh,
  Table,
  TBody,
  Td,
  TextField,
  THead,
  Th,
  Tr,
} from "../ui";
import { Async } from "./Async";

function matches(t: TypeEntry, view: TypesView): boolean {
  const needle = view.text.trim().toLowerCase();
  return (
    (!view.onlyFilled || t.count > 0) &&
    (needle === "" ||
      t.name.toLowerCase().includes(needle) ||
      t.description.toLowerCase().includes(needle))
  );
}

/** What the number of types shown is out of: said in words, so "67" is not mistaken for a part of an unnamed whole. */
function countText(view: TypesView, total: number, filled: number, shown: number): string {
  if (view.text.trim() !== "") {
    const where = view.onlyFilled ? ", 객체가 있는 타입만" : "";
    return `${formatCount(total)}개 타입 중 검색어에 맞는 ${formatCount(shown)}개${where}`;
  }
  if (view.onlyFilled) return `${formatCount(total)}개 타입 중 객체가 있는 ${formatCount(shown)}개`;
  return `전체 ${formatCount(total)}개 타입(객체가 있는 타입 ${formatCount(filled)}개)`;
}

/** The object types of the model with how many objects the chosen KG holds of each. */
export function TypesScreen({ kg }: { readonly kg: string }) {
  const catalog = useTypeCatalog(kg);
  const { params, set } = useHashParams();
  const query = params.toString();
  const view = useMemo(() => readTypesView(new URLSearchParams(query)), [query]);
  const update = (next: TypesView) => set(typesViewParams(next));
  const all = catalog.state.data;
  const shown = useMemo(
    () =>
      all
        ? sortTypes(
            all.filter((t) => matches(t, view)),
            view.sort,
          )
        : [],
    [all, view],
  );
  const sortOf = (key: TypeSortKey) => (view.sort?.key === key ? view.sort.direction : undefined);
  const sortBy = (key: TypeSortKey) => () => update({ ...view, sort: nextSort(view.sort, key) });
  return (
    <>
      <PageHeader
        title="객체 타입"
        trail={[{ label: "KG 목록", href: routeHash({ page: "kgs" }) }, { label: kg }]}
        subtitle="타입을 선택하면 그 타입의 객체를 테이블로 봅니다."
      />
      <Async
        result={catalog}
        loadingLabel="객체 타입을 불러오는 중"
        names={{ kg }}
        back={{ label: "KG 목록으로", href: routeHash({ page: "kgs" }) }}
      >
        {(types) => (
          <>
            <div className="mb-3 flex flex-wrap items-end gap-4">
              <div className="w-72">
                <TextField
                  label="타입 검색"
                  value={view.text}
                  onChange={(e) => update({ ...view, text: e.target.value })}
                  placeholder="이름이나 설명"
                />
              </div>
              <Checkbox
                checked={view.onlyFilled}
                onChange={(on) => update({ ...view, onlyFilled: on })}
              >
                객체가 있는 타입만
              </Checkbox>
              <span className="pb-1.5 text-sm text-muted">
                {countText(
                  view,
                  types.length,
                  types.filter((t) => t.count > 0).length,
                  shown.length,
                )}
              </span>
            </div>
            {shown.length === 0 ? (
              <EmptyState title="맞는 타입이 없습니다">
                {view.onlyFilled
                  ? "‘객체가 있는 타입만’을 끄거나 검색어를 바꿔 보십시오."
                  : "검색어를 바꿔 보십시오."}
              </EmptyState>
            ) : (
              <Table aria-label="객체 타입">
                <THead>
                  <Tr>
                    <SortTh sort={sortOf("name")} onSort={sortBy("name")}>
                      타입
                    </SortTh>
                    <SortTh sort={sortOf("layer")} onSort={sortBy("layer")}>
                      계층
                    </SortTh>
                    <Th>설명</Th>
                    <SortTh className="text-right" sort={sortOf("count")} onSort={sortBy("count")}>
                      객체 수
                    </SortTh>
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
                        <Badge title={t.layer}>{layerName(t.layer)}</Badge>
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
