import { Button } from "./Button";
import { Select } from "./Select";

export interface PaginationProps {
  readonly page: number;
  readonly hasPrevious: boolean;
  readonly hasNext: boolean;
  readonly onPrevious: () => void;
  readonly onNext: () => void;
  readonly pageSize: number;
  readonly pageSizes: readonly number[];
  readonly onPageSize: (size: number) => void;
  /** Where the page is among the whole result, such as `1–25 / 312`. */
  readonly summary: string;
}

export function Pagination(props: PaginationProps) {
  return (
    <div className="mt-3 flex flex-wrap items-center justify-between gap-3 text-sm">
      <span className="text-muted">{props.summary}</span>
      <div className="flex items-center gap-2">
        <Select
          label="페이지당 개수"
          value={props.pageSize}
          options={props.pageSizes.map((n) => ({ value: n, label: `${n}개씩` }))}
          onChange={props.onPageSize}
        />
        <Button onClick={props.onPrevious} disabled={!props.hasPrevious}>
          이전
        </Button>
        <span aria-live="polite" className="min-w-12 text-center text-muted">
          {props.page}쪽
        </span>
        <Button onClick={props.onNext} disabled={!props.hasNext}>
          다음
        </Button>
      </div>
    </div>
  );
}
