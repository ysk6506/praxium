import {
  gradeStyle,
  type ImpactAnswer,
  type ImpactLoad,
  type ImpactView,
  impactNotices,
  type NoticeAction,
  routeHash,
  serverCounts,
  singlePoints,
  titleOf,
  totalCount,
  unfilledCount,
  useScreenTitle,
} from "../../data";
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  GradeLegend,
  LoadingState,
  PageHeader,
  Section,
} from "../../ui";
import { ImpactResult } from "./ImpactResult";
import { Notices } from "./Notices";
import { QuestionHead } from "./QuestionHead";

export interface ImpactBodyProps {
  readonly kg: string;
  readonly view: ImpactView;
  readonly change: (patch: Partial<ImpactView>, how?: "push" | "replace") => void;
  readonly load: ImpactLoad;
  readonly answer: ImpactAnswer;
  readonly layerOf: (type: string) => string;
}

/** The facts the screen counts over the answer's items: how many came, how many were expected. */
function progressText(received: number, expected: number): string {
  return `항목을 받는 중 ${received} / ${expected}`;
}

export function ImpactBody({ kg, view, change, load, answer, layerOf }: ImpactBodyProps) {
  const { data, meta } = answer;
  const { reading } = view;
  const { items } = load;
  const first = data.target ?? data.seeds?.[0];
  useScreenTitle(titleOf("영향 범위", first?.display_name ?? kg));

  const counts = serverCounts(data, reading);
  const total = totalCount(data, reading);
  const spofs = singlePoints(data);
  const received = items.items.length;
  const notices = impactNotices({
    computation: data.computation,
    warnings: meta.warnings,
    reading,
    unfilled: unfilledCount(data, reading),
    stopped: items.status === "stopped",
    received,
    expected: items.expected,
  });

  const onAction = (action: NoticeAction): void => {
    if (action.kind === "raise") change({ max: action.max }, "push");
    else if (action.kind === "unlimited-hops") change({ hops: undefined }, "push");
    else load.receiveAgain();
  };

  const trail = [
    { label: "KG 목록", href: routeHash({ page: "kgs" }) },
    { label: kg, href: routeHash({ page: "types", kg }) },
    ...(first
      ? [
          {
            label: first.display_name,
            href: routeHash({ page: "object", kg, id: first.object_id, tab: "attributes" }),
          },
        ]
      : []),
  ];
  const limits = meta.model_limits;

  return (
    <>
      <PageHeader title="영향 범위" trail={trail} />
      <Section title="질문">
        <QuestionHead
          kg={kg}
          view={view}
          data={data}
          onReading={(next) => change({ reading: next, grade: undefined }, "push")}
        />
      </Section>
      <Section
        title="등급별 개수"
        aside={`서버가 센 전체 ${total}개`}
        note={
          received < total && items.status !== "receiving"
            ? `받은 항목은 ${received}개입니다. 개수는 전체를 센 값입니다.`
            : undefined
        }
      >
        {total === 0 ? null : (
          <GradeLegend
            items={counts.map((c) => ({
              key: c.grade,
              mark: gradeStyle(c.grade),
              count: c.count,
            }))}
            active={view.grade}
            onToggle={(grade) => change({ grade: view.grade === grade ? undefined : grade })}
          />
        )}
        {spofs === undefined ? null : (
          <p className="mt-2 text-sm">
            <Badge tone={spofs > 0 ? "danger" : "neutral"}>단일 장애점 {spofs}개</Badge>{" "}
            <span className="text-muted">
              그 객체 하나만 멈춰도 대상이 멈추는 의존 대상입니다. 목록과 그래프에 표시합니다.
            </span>
          </p>
        )}
      </Section>
      <Notices notices={notices} onAction={onAction} limits={total === 0 ? [] : limits} />
      {total === 0 ? (
        <EmptyState title="영향받는 객체가 없습니다">
          이 질문으로는 영향받는 객체를 찾지 못했습니다. 없다는 것이 안전하다는 뜻은 아닙니다. 이
          계산이 보지 않는 것을 확인하십시오.
        </EmptyState>
      ) : (
        <div className="mt-4">
          {items.status === "receiving" ? (
            <div className="mb-3 flex flex-wrap items-center gap-3 text-sm">
              <output>{progressText(received, items.expected)}</output>
              <Button onClick={load.stop}>받기 중지</Button>
            </div>
          ) : null}
          {items.status === "error" && items.error ? (
            <div className="mb-3">
              <ErrorState
                title={items.error.title}
                message={`${items.error.message} 받은 ${received}개까지만 보입니다.`}
                hint={items.error.hint}
                action={<Button onClick={load.receiveAgain}>다시 받기</Button>}
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
      )}
    </>
  );
}
