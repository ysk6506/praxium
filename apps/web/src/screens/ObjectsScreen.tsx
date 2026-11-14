import { type ReactNode, useMemo, useState } from "react";
import {
  columnCandidates,
  createTrail,
  defaultColumns,
  EMPTY_CELL,
  type ErrorView,
  type FindItem,
  formatCount,
  formatQuantity,
  type GetData,
  gradeLabel,
  type ObjectsView,
  objectsViewParams,
  orderedColumns,
  PAGE_SIZES,
  type RowDetail,
  readObjectsView,
  routeHash,
  shortId,
  tabStorage,
  useHashParams,
  useObjectPage,
  useObjectTypeDetail,
  useRowDetails,
} from "../data";
import {
  Button,
  Checkbox,
  Dialog,
  EmptyState,
  Mono,
  PageHeader,
  Pagination,
  Table,
  TBody,
  Td,
  THead,
  Th,
  Tr,
} from "../ui";
import { Async } from "./Async";

const SUBTYPE_NOTE =
  "하위 타입(이 타입을 상속해 더 구체적으로 정의한 타입)의 객체는 이 테이블에 포함하지 않습니다. 하위 타입의 객체는 객체 타입 목록에서 그 타입을 선택해 보십시오.";

function identifierText(detail: RowDetail | undefined): string {
  if (detail?.status !== "ready") return EMPTY_CELL;
  const first = detail.data.identifiers?.[0];
  return first ? `${first.scheme}:${first.value}` : EMPTY_CELL;
}

function attributeCell(detail: RowDetail | undefined, name: string) {
  if (detail === undefined) return <span className="text-subtle">…</span>;
  if (detail.status === "error") return <span className="text-danger">읽지 못함</span>;
  const attribute = (detail.data as GetData).attributes?.find((a) => a.name === name);
  if (!attribute) return <span className="text-subtle">{EMPTY_CELL}</span>;
  return (
    <span
      title={`근거 등급: ${gradeLabel(attribute.grade)}`}
      className={attribute.stale ? "text-muted" : undefined}
    >
      {formatQuantity(attribute.value, attribute.unit)}
    </span>
  );
}

function summaryOf(page: number, size: number, returned: number, total: number): string {
  if (returned === 0) return `0 / ${formatCount(total)}`;
  const from = (page - 1) * size + 1;
  return `${formatCount(from)}–${formatCount(from + returned - 1)} / ${formatCount(total)}`;
}

function ColumnPicker(props: {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly properties: readonly { name: string; description: string }[];
  readonly selected: readonly string[];
  readonly onToggle: (name: string, on: boolean) => void;
}) {
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title="컬럼 선택"
      description="선택한 속성이 테이블의 컬럼으로 표시됩니다. 목록의 순서가 테이블의 컬럼 순서입니다."
    >
      <div className="max-h-80 overflow-auto pr-1">
        {props.properties.map((p) => (
          <Checkbox
            key={p.name}
            checked={props.selected.includes(p.name)}
            onChange={(on) => props.onToggle(p.name, on)}
          >
            <span className="font-mono">{p.name}</span>
          </Checkbox>
        ))}
      </div>
    </Dialog>
  );
}

function ObjectRows(props: {
  readonly kg: string;
  readonly items: readonly FindItem[];
  readonly columns: readonly string[];
}) {
  const details = useRowDetails(props.kg, props.items);
  return (
    <Table aria-label="객체 목록">
      <THead>
        <Tr>
          <Th>이름</Th>
          <Th>식별자</Th>
          {props.columns.map((c) => (
            <Th key={c} className="font-mono">
              {c}
            </Th>
          ))}
        </Tr>
      </THead>
      <TBody>
        {props.items.map((item) => {
          const detail = details.get(item.object_id);
          return (
            <Tr key={item.object_id}>
              <Td className="min-w-56">
                <a
                  href={routeHash({
                    page: "object",
                    kg: props.kg,
                    id: item.object_id,
                    tab: "attributes",
                  })}
                  className="font-medium whitespace-nowrap"
                >
                  {item.display_name}
                </a>
                <div className="text-xs whitespace-nowrap text-subtle">
                  <Mono className="break-normal">{shortId(item.object_id, 16)}</Mono>
                </div>
              </Td>
              <Td>
                <Mono className="whitespace-nowrap break-normal">{identifierText(detail)}</Mono>
              </Td>
              {props.columns.map((c) => (
                <Td key={c}>{attributeCell(detail, c)}</Td>
              ))}
            </Tr>
          );
        })}
      </TBody>
    </Table>
  );
}

/** The objects of one type as a table: a page at a time, with the attributes the person picked as columns. */
export function ObjectsScreen({ kg, type }: { readonly kg: string; readonly type: string }) {
  const { params, set } = useHashParams();
  const view = readObjectsView(params);
  const typeDetail = useObjectTypeDetail(type);
  const page = useObjectPage(kg, type, view.size, view.cursor);
  const [pickerOpen, setPickerOpen] = useState(false);
  const trail = useMemo(
    () => createTrail(tabStorage(), `praxium.pages:${kg}/${type}/${view.size}`),
    [kg, type, view.size],
  );
  const properties = typeDetail.state.data?.effective_properties;

  const ordered = properties ? orderedColumns(properties) : [];
  const candidates = properties ? columnCandidates(properties) : [];
  const columns = view.columns
    ? ordered.map((c) => c.name).filter((n) => view.columns?.includes(n))
    : properties
      ? defaultColumns(properties)
      : [];
  const toggle = (name: string, on: boolean) =>
    set(
      objectsViewParams({
        ...view,
        columns: ordered.map((c) => c.name).filter((n) => (n === name ? on : columns.includes(n))),
      }),
    );

  const show = (next: Partial<ObjectsView>): void => {
    set(objectsViewParams({ ...view, ...next }), "push");
    globalThis.scrollTo(0, 0);
  };
  const previousPage = view.page - 1;
  const previousCursor = previousPage > 1 ? trail.get(previousPage) : undefined;
  const hasPrevious = view.page > 1 && (previousPage === 1 || previousCursor !== undefined);
  const toFirst = (): void => show({ page: 1, cursor: undefined });

  const recover = (error: ErrorView): ReactNode =>
    error.code === "invalid_input.cursor_invalid" ? (
      <Button onClick={toFirst}>첫 페이지로</Button>
    ) : undefined;

  return (
    <>
      <PageHeader
        title={type}
        trail={[
          { label: "KG 목록", href: routeHash({ page: "kgs" }) },
          { label: kg, href: routeHash({ page: "types", kg }) },
          { label: "객체 타입", href: routeHash({ page: "types", kg }) },
        ]}
        subtitle={typeDetail.state.data?.description}
        actions={
          candidates.length > 0 ? (
            <Button onClick={() => setPickerOpen(true)}>컬럼 선택 ({columns.length})</Button>
          ) : undefined
        }
      />
      <ColumnPicker
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        properties={ordered}
        selected={columns}
        onToggle={toggle}
      />
      <Async
        result={page}
        loadingLabel="객체를 불러오는 중"
        names={{ kg, object_type: type }}
        back={{ label: "객체 타입 목록으로", href: routeHash({ page: "types", kg }) }}
        recover={recover}
      >
        {({ items, page: info }) =>
          items.length === 0 ? (
            <EmptyState
              title="이 타입의 객체가 없습니다"
              action={view.page > 1 ? <Button onClick={toFirst}>첫 페이지로</Button> : undefined}
            >
              KG에 {type} 객체가 아직 없습니다. {SUBTYPE_NOTE}
            </EmptyState>
          ) : (
            <>
              <ObjectRows kg={kg} items={items} columns={columns} />
              <Pagination
                page={view.page}
                hasPrevious={hasPrevious}
                hasNext={info.next_cursor != null}
                onPrevious={() => show({ page: previousPage, cursor: previousCursor })}
                onNext={() => {
                  if (info.next_cursor == null) return;
                  if (view.cursor !== undefined) trail.set(view.page, view.cursor);
                  trail.set(view.page + 1, info.next_cursor);
                  show({ page: view.page + 1, cursor: info.next_cursor });
                }}
                onFirst={toFirst}
                pageSize={view.size}
                pageSizes={PAGE_SIZES}
                onPageSize={(size) => show({ size, page: 1, cursor: undefined })}
                summary={summaryOf(view.page, view.size, items.length, info.total)}
              />
              <p className="mt-2 text-xs text-muted">{SUBTYPE_NOTE}</p>
            </>
          )
        }
      </Async>
    </>
  );
}
