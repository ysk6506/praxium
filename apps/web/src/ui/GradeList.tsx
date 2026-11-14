import type { ReactNode } from "react";
import { Badge, type BadgeTone } from "./Badge";
import { cx } from "./cx";
import { GradeMark, type GradeMarkProps } from "./GradeMark";
import { SortTh, Table, TBody, Td, THead, Th, Tr } from "./Table";

export interface LegendItem {
  readonly key: string;
  readonly mark: Omit<GradeMarkProps, "count">;
  readonly count: number;
}

export interface GradeLegendProps {
  readonly items: readonly LegendItem[];
  /** The grade the list is narrowed to. */
  readonly active: string | undefined;
  readonly onToggle: (key: string) => void;
}

/** The grades of the answer with how many objects each has; pressing one narrows the list to it, pressing it again undoes that. */
export function GradeLegend({ items, active, onToggle }: GradeLegendProps) {
  return (
    <ul aria-label="등급별 개수" className="flex flex-wrap gap-2">
      {items.map((item) => (
        <li key={item.key}>
          <button
            type="button"
            aria-pressed={active === item.key}
            onClick={() => onToggle(item.key)}
            className={cx(
              "min-h-6 cursor-pointer rounded-md border px-2 py-1 text-sm hover:bg-sunken",
              active === item.key
                ? "border-accent bg-accent-soft"
                : "border-control-line bg-surface",
            )}
          >
            <GradeMark {...item.mark} count={item.count} />
          </button>
        </li>
      ))}
    </ul>
  );
}

export interface ListColumn {
  readonly key: string;
  readonly label: string;
  /** Present when the column can be sorted; it says how the list is sorted by it now. */
  readonly sort?: { readonly direction: "ascending" | "descending" | undefined };
}

export interface ListRow {
  readonly id: string;
  /** One cell for each column. A string is written as text; the screen puts a link in a cell where it needs one. */
  readonly cells: readonly ReactNode[];
  /** The weakest evidence on the path of the row, in the words and the tone the rest of the console uses for a grade. */
  readonly evidence?: { readonly label: string; readonly tone: BadgeTone };
  /** What more there is to say of the row, such as that the path went through an object that may not be read. */
  readonly notes?: readonly string[];
}

export interface ListSection {
  readonly key: string;
  readonly mark: Omit<GradeMarkProps, "count">;
  /** How many rows the section has in all, of which `rows` are shown. */
  readonly total: number;
  readonly rows: readonly ListRow[];
}

export interface GradeListProps {
  readonly sections: readonly ListSection[];
  readonly columns: readonly ListColumn[];
  readonly onSort: (key: string) => void;
  /** Shows more of a section: the next rows are added to those shown. */
  readonly onMore: (key: string) => void;
  /** Shown when there is nothing to list. Nothing found is not the same as nothing affected, and the text says so. */
  readonly empty: ReactNode;
}

/**
 * The answer as tables, a section for each grade, the most severe first. This is the answer itself; the picture is
 * another way to look at it. Every column that can be sorted has a button in its head, and a section shows its first rows, with
 * a button that shows more of it; there are no page numbers.
 */
export function GradeList(props: GradeListProps) {
  if (props.sections.length === 0) return <>{props.empty}</>;
  return (
    <div className="flex flex-col gap-6">
      {props.sections.map((section) => (
        <section key={section.key} aria-label={`${section.mark.name} ${section.total}개`}>
          <h3 className="mb-2 flex flex-wrap items-baseline gap-x-3 text-sm font-semibold">
            <GradeMark {...section.mark} count={section.total} />
            <span className="font-normal text-muted">
              전체 {section.total}개 중 {section.rows.length}개 표시
            </span>
          </h3>
          <Table aria-label={`${section.mark.name} 객체`}>
            <THead>
              <Tr>
                {props.columns.map((column) =>
                  column.sort ? (
                    <SortTh
                      key={column.key}
                      sort={column.sort.direction}
                      onSort={() => props.onSort(column.key)}
                    >
                      {column.label}
                    </SortTh>
                  ) : (
                    <Th key={column.key}>{column.label}</Th>
                  ),
                )}
              </Tr>
            </THead>
            <TBody>
              {section.rows.map((row) => (
                <Tr key={row.id}>
                  {row.cells.map((cell, i) => (
                    <Td key={props.columns[i]?.key ?? i}>
                      {cell}
                      {i === 0 ? <RowFacts row={row} /> : null}
                    </Td>
                  ))}
                </Tr>
              ))}
            </TBody>
          </Table>
          {section.rows.length < section.total ? (
            <button
              type="button"
              onClick={() => props.onMore(section.key)}
              className="mt-2 min-h-6 cursor-pointer rounded-md border border-control-line bg-surface px-3 py-1 text-sm hover:bg-sunken"
            >
              더 보기
              <span className="sr-only"> ({section.mark.name})</span>
            </button>
          ) : null}
        </section>
      ))}
    </div>
  );
}

/** The evidence and the notes of a row, written under its first cell, so they are in the row for a screen reader too. */
function RowFacts({ row }: { readonly row: ListRow }) {
  if (row.evidence === undefined && (row.notes === undefined || row.notes.length === 0))
    return null;
  return (
    <span className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
      {row.evidence ? <Badge tone={row.evidence.tone}>근거 {row.evidence.label}</Badge> : null}
      {row.notes?.map((note) => (
        <span key={note}>{note}</span>
      ))}
    </span>
  );
}
