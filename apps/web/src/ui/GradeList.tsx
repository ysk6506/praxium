import type { ReactNode } from "react";
import { cx } from "./cx";
import { GradeMark, type GradeMarkProps } from "./GradeMark";
import { Pagination } from "./Pagination";
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
                : "border-line-strong bg-surface",
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
}

export interface ListSection {
  readonly key: string;
  readonly mark: Omit<GradeMarkProps, "count">;
  /** How many rows the section has in all, of which `rows` are the page shown. */
  readonly total: number;
  readonly rows: readonly ListRow[];
}

export interface GradeListProps {
  readonly sections: readonly ListSection[];
  readonly columns: readonly ListColumn[];
  readonly onSort: (key: string) => void;
  readonly page: number;
  readonly pages: number;
  readonly pageSize: number;
  /** Where the page is among the answer, such as `1–50 / 312`. */
  readonly summary: string;
  readonly onPage: (page: number) => void;
  /** Shown when there is nothing to list. Nothing found is not the same as nothing affected, and the text says so. */
  readonly empty: ReactNode;
}

/**
 * The answer as tables, a section for each grade, the most severe first. This is the answer itself; the picture is
 * another way to look at it. Every column that can be sorted has a button in its head, and the pages are the same
 * number for every section.
 */
export function GradeList(props: GradeListProps) {
  if (props.sections.length === 0) return <>{props.empty}</>;
  return (
    <div className="flex flex-col gap-6">
      {props.sections.map((section) => (
        <section key={section.key} aria-label={`${section.mark.name} ${section.total}개`}>
          <h3 className="mb-2 text-sm font-semibold">
            <GradeMark {...section.mark} count={section.total} />
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
                    <Td key={props.columns[i]?.key ?? i}>{cell}</Td>
                  ))}
                </Tr>
              ))}
            </TBody>
          </Table>
        </section>
      ))}
      <Pagination
        page={props.page}
        hasPrevious={props.page > 1}
        hasNext={props.page < props.pages}
        onPrevious={() => props.onPage(props.page - 1)}
        onNext={() => props.onPage(props.page + 1)}
        pageSize={props.pageSize}
        pageSizes={[props.pageSize]}
        onPageSize={() => undefined}
        summary={props.summary}
        onFirst={() => props.onPage(1)}
      />
    </div>
  );
}
