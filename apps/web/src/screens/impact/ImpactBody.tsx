import {
  type ImpactAnswer,
  type ImpactLoad,
  type ImpactView,
  impactNotices,
  type NoticeAction,
  routeHash,
  titleOf,
  unfilledCount,
  useScreenTitle,
} from "../../data";
import { PageHeader, Section } from "../../ui";
import { Counts } from "./Counts";
import { Notices } from "./Notices";
import { QuestionHead } from "./QuestionHead";
import { ResultArea } from "./ResultArea";

export interface ImpactBodyProps {
  readonly kg: string;
  readonly view: ImpactView;
  readonly change: (patch: Partial<ImpactView>, how?: "push" | "replace") => void;
  readonly load: ImpactLoad;
  readonly answer: ImpactAnswer;
  readonly layerOf: (type: string) => string;
}

/** The answer of the question: the head, the counts, the notices, and under them the graph and the list. */
export function ImpactBody({ kg, view, change, load, answer, layerOf }: ImpactBodyProps) {
  const { data, meta } = answer;
  const { reading } = view;
  const { items } = load;
  const first = data.target ?? data.seeds?.[0];
  useScreenTitle(titleOf("영향 범위", first?.display_name ?? kg));

  const notices = impactNotices({
    computation: data.computation,
    warnings: meta.warnings,
    reading,
    unfilled: unfilledCount(data, reading),
    stopped: items.status === "stopped",
    received: items.items.length,
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
      <Counts
        answer={answer}
        reading={reading}
        grade={view.grade}
        received={items.items.length}
        receiving={items.status === "receiving"}
        onGrade={(grade) => change({ grade })}
      />
      <Notices notices={notices} onAction={onAction} limits={meta.model_limits} />
      <ResultArea
        kg={kg}
        view={view}
        change={change}
        load={load}
        answer={answer}
        layerOf={layerOf}
      />
    </>
  );
}
