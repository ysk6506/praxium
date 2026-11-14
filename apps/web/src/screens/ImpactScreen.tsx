import {
  type AsyncResult,
  type ImpactAnswer,
  isAnswerable,
  routeHash,
  titleOf,
  useImpact,
  useImpactModel,
  useScreenTitle,
} from "../data";
import { ButtonLink, EmptyState, LoadingState, PageHeader } from "../ui";
import { Async } from "./Async";
import { ImpactBody } from "./impact/ImpactBody";
import { useImpactView } from "./impact/useImpactView";

const noLayer = (): string => "";

/** The read of the summary, as the state the screens draw reads are drawn in. */
function summaryResult(load: ReturnType<typeof useImpact>): AsyncResult<ImpactAnswer> {
  if (load.summaryError) {
    return {
      state: { status: "error", error: load.summaryError, data: undefined },
      reload: load.reload,
    };
  }
  if (load.summary) return { state: { status: "ready", data: load.summary }, reload: load.reload };
  return { state: { status: "loading", data: undefined, attempt: 0 }, reload: load.reload };
}

function Asked({ kg }: { readonly kg: string }) {
  const { view, change } = useImpactView();
  const model = useImpactModel();
  const load = useImpact(kg, view);
  const asked = view.question.target ?? view.question.dead[0] ?? view.question.site[0];
  return (
    <Async
      result={summaryResult(load)}
      loadingLabel="영향 범위를 계산하는 중"
      names={{ kg, object: asked }}
      back={{ label: "객체 타입 목록으로", href: routeHash({ page: "types", kg }) }}
    >
      {(answer) =>
        model.state.status === "loading" && model.state.data === undefined ? (
          <LoadingState label="모델을 불러오는 중" />
        ) : (
          <ImpactBody
            kg={kg}
            view={view}
            change={change}
            load={load}
            answer={answer}
            layerOf={model.state.data?.layerOf ?? noLayer}
          />
        )
      }
    </Async>
  );
}

function Unasked({ kg }: { readonly kg: string }) {
  useScreenTitle(titleOf("영향 범위", kg));
  return (
    <>
      <PageHeader
        title="영향 범위"
        trail={[{ label: "KG 목록", href: routeHash({ page: "kgs" }) }]}
      />
      <EmptyState
        title="무엇이 멈췄다고 가정할지 정하지 않았습니다"
        action={
          <ButtonLink href={routeHash({ page: "types", kg })}>객체 타입에서 객체 고르기</ButtonLink>
        }
      >
        영향 범위는 객체 하나를 정해서 묻습니다. 객체 상세에서 “이 객체가 멈추면”을 누르십시오.
      </EmptyState>
    </>
  );
}

/** What stops when something stops, or what a thing depends on: the question is in the address, the answer is read here. */
export function ImpactScreen({ kg }: { readonly kg: string }) {
  const { view } = useImpactView();
  return isAnswerable(view.question) ? <Asked kg={kg} /> : <Unasked kg={kg} />;
}
