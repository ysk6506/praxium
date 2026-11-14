import {
  type ImpactData,
  type ImpactReading,
  type ImpactView,
  type ObjectRef,
  routeHash,
} from "../../data";
import { Badge, Mono, Select } from "../../ui";

const READINGS = [
  { value: "facts_only", label: "사실만" },
  { value: "with_assumptions", label: "가정 포함" },
] as const;

const objectLink = (kg: string, ref: ObjectRef) => (
  <li key={ref.object_id}>
    <a href={routeHash({ page: "object", kg, id: ref.object_id, tab: "attributes" })}>
      {ref.display_name}
    </a>{" "}
    <Badge tone="accent">{ref.object_type}</Badge>
  </li>
);

/** The objects of the answer that the question names, by id, in the order the question gave; an id the answer does not know stays an id. */
function named(ids: readonly string[], refs: readonly ObjectRef[]): ObjectRef[] {
  const by = new Map(refs.map((r) => [r.object_id, r]));
  return ids.map(
    (id) => by.get(id) ?? { object_id: id, object_type: "", display_name: id.slice(0, 12) },
  );
}

function Group({
  title,
  refs,
  kg,
}: {
  readonly title: string;
  readonly refs: readonly ObjectRef[];
  readonly kg: string;
}) {
  if (refs.length === 0) return null;
  return (
    <div>
      <dt className="text-muted">{title}</dt>
      <dd>
        <ul className="flex flex-col gap-0.5">{refs.map((ref) => objectLink(kg, ref))}</ul>
      </dd>
    </div>
  );
}

export interface QuestionHeadProps {
  readonly kg: string;
  readonly view: ImpactView;
  readonly data: ImpactData;
  readonly onReading: (reading: ImpactReading) => void;
}

/**
 * The question the answer is for: what was assumed to have stopped (or whose dependencies are asked), the direction,
 * and the reading of the evidence (the facts only, or the facts with the assumptions) that can be switched.
 */
export function QuestionHead({ kg, view, data, onReading }: QuestionHeadProps) {
  const q = view.question;
  const upstream = q.target !== undefined;
  const refs = data.seeds ?? [];
  return (
    <div className="flex flex-col gap-3">
      <dl className="flex flex-col gap-2 text-sm">
        {upstream ? (
          <Group
            title="이 객체가 기대는 것을 묻습니다"
            kg={kg}
            refs={data.target ? [data.target] : []}
          />
        ) : (
          <>
            <Group title="멈췄다고 가정한 객체" kg={kg} refs={named(q.dead, refs)} />
            <Group title="안에 든 것까지 멈췄다고 가정한 객체" kg={kg} refs={named(q.site, refs)} />
            {q.cut.length > 0 ? (
              <div>
                <dt className="text-muted">끊겼다고 가정한 링크</dt>
                <dd>
                  <ul>
                    {q.cut.map((id) => (
                      <li key={id}>
                        <Mono>{id}</Mono>
                      </li>
                    ))}
                  </ul>
                </dd>
              </div>
            ) : null}
          </>
        )}
        <div>
          <dt className="text-muted">방향</dt>
          <dd>
            {upstream
              ? "위 방향: 이 객체가 의존하는 것과 단일 장애점"
              : "아래 방향: 멈추면 영향받는 것"}
          </dd>
        </div>
      </dl>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-muted">읽는 방식</span>
        <Select
          label="읽는 방식"
          value={view.reading}
          options={READINGS}
          onChange={(value) => onReading(value)}
        />
        <span className="text-muted">
          {view.reading === "facts_only"
            ? "근거가 사실인 링크만 장애를 넘깁니다."
            : "가정인 링크도 사실처럼 읽습니다."}
        </span>
      </div>
    </div>
  );
}
