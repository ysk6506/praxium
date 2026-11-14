import { useState } from "react";
import {
  columnCandidates,
  defaultColumns,
  EMPTY_CELL,
  type FindItem,
  formatCount,
  formatQuantity,
  type GetData,
  gradeLabel,
  PAGE_SIZES,
  type RowDetail,
  routeHash,
  shortId,
  useObjectPage,
  useObjectTypeDetail,
  usePager,
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
      title="표에 보일 속성"
      description="고른 속성이 열로 나옵니다."
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
              <Td>
                <a
                  href={routeHash({
                    page: "object",
                    kg: props.kg,
                    id: item.object_id,
                    tab: "attributes",
                  })}
                  className="font-medium"
                >
                  {item.display_name}
                </a>
                <div className="text-xs text-subtle">
                  <Mono>{shortId(item.object_id, 16)}</Mono>
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
  const pager = usePager();
  const typeDetail = useObjectTypeDetail(type);
  const page = useObjectPage(kg, type, pager.state.limit, pager.cursor);
  const [picked, setPicked] = useState<readonly string[] | undefined>(undefined);
  const [pickerOpen, setPickerOpen] = useState(false);
  const properties = typeDetail.state.data?.effective_properties;

  const candidates = properties ? columnCandidates(properties) : [];
  const columns = picked ?? (properties ? defaultColumns(properties) : []);
  const toggle = (name: string, on: boolean) =>
    setPicked(candidates.map((c) => c.name).filter((n) => (n === name ? on : columns.includes(n))));

  return (
    <>
      <PageHeader
        title={type}
        trail={[
          { label: "KG 선택", href: routeHash({ page: "kgs" }) },
          { label: kg, href: routeHash({ page: "types", kg }) },
          { label: "객체 타입", href: routeHash({ page: "types", kg }) },
        ]}
        subtitle={typeDetail.state.data?.description}
        actions={
          candidates.length > 0 ? (
            <Button onClick={() => setPickerOpen(true)}>열 고르기 ({columns.length})</Button>
          ) : undefined
        }
      />
      <ColumnPicker
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        properties={candidates}
        selected={columns}
        onToggle={toggle}
      />
      <Async result={page} loadingLabel="객체를 불러오는 중">
        {({ items, page: info }) =>
          items.length === 0 ? (
            <EmptyState
              title="이 타입의 객체가 없습니다"
              action={
                pager.hasPrevious ? <Button onClick={pager.first}>첫 쪽으로</Button> : undefined
              }
            >
              KG에 {type} 객체가 아직 없습니다. 하위 타입의 객체는 표에 넣지 않습니다.
            </EmptyState>
          ) : (
            <>
              <ObjectRows kg={kg} items={items} columns={columns} />
              <Pagination
                page={pager.page}
                hasPrevious={pager.hasPrevious}
                hasNext={info.next_cursor != null}
                onPrevious={pager.previous}
                onNext={() => info.next_cursor != null && pager.next(info.next_cursor)}
                pageSize={pager.state.limit}
                pageSizes={PAGE_SIZES}
                onPageSize={pager.setLimit}
                summary={summaryOf(pager.page, pager.state.limit, items.length, info.total)}
              />
            </>
          )
        }
      </Async>
    </>
  );
}
