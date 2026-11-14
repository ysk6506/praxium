import { type ImpactAnswer, type ImpactLoad, type ImpactView, totalCount } from "../../data";
import { Button, EmptyState, ErrorState, LoadingState } from "../../ui";
import { ImpactResult } from "./ImpactResult";

const progressText = (received: number, expected: number): string =>
  `항목을 받는 중 ${received} / ${expected}`;

export interface ResultAreaProps {
  readonly kg: string;
  readonly view: ImpactView;
  readonly change: (patch: Partial<ImpactView>, how?: "push" | "replace") => void;
  readonly load: ImpactLoad;
  readonly answer: ImpactAnswer;
  readonly layerOf: (type: string) => string;
}

/**
 * What is under the notices: nothing found, or the progress of receiving the items with the button that stops it, an
 * error that came on the way (the items received stay), and the graph and the list.
 */
export function ResultArea({ kg, view, change, load, answer, layerOf }: ResultAreaProps) {
  const { items } = load;
  const received = items.items.length;
  if (totalCount(answer.data, view.reading) === 0) {
    return (
      <EmptyState title="영향받는 객체가 없습니다">
        이 질문으로는 영향받는 객체를 찾지 못했습니다. 없다는 것이 안전하다는 뜻은 아닙니다. 이
        계산이 보지 않는 것을 확인하십시오.
      </EmptyState>
    );
  }
  return (
    <div className="mt-4">
      {items.status === "receiving" ? (
        <div className="mb-3 flex flex-wrap items-center gap-3 text-sm">
          <output>{progressText(received, items.expected)}</output>
          <Button onClick={load.stop}>불러오기 중지</Button>
        </div>
      ) : null}
      {items.status === "error" && items.error ? (
        <div className="mb-3">
          <ErrorState
            title={items.error.title}
            message={`${items.error.message} 받은 ${received}개까지만 보입니다.`}
            hint={items.error.hint}
            action={<Button onClick={load.receiveAgain}>다시 불러오기</Button>}
          />
        </div>
      ) : null}
      {received === 0 && items.status === "receiving" ? (
        <LoadingState label={progressText(0, items.expected)} />
      ) : (
        <ImpactResult
          kg={kg}
          view={view}
          change={change}
          answer={answer}
          items={items}
          layerOf={layerOf}
        />
      )}
    </div>
  );
}
