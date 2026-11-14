import {
  gradeStyle,
  type ImpactAnswer,
  type ImpactReading,
  serverCounts,
  singlePoints,
  totalCount,
} from "../../data";
import { Badge, GradeLegend, Section } from "../../ui";

export interface CountsProps {
  readonly answer: ImpactAnswer;
  readonly reading: ImpactReading;
  /** The grade the list and the graph are narrowed to. */
  readonly grade: string | undefined;
  /** How many items have been received, and whether more are on the way. */
  readonly received: number;
  readonly receiving: boolean;
  readonly onGrade: (grade: string | undefined) => void;
}

/**
 * How many objects have each result, as the server counted them over the whole answer; pressing one narrows the list and
 * the graph to it. Upstream the single points of failure are counted too. When fewer items came than the server counted
 * (the limit, a stop) it says so, because the counts are then more than what the graph and the list hold.
 */
export function Counts({ answer, reading, grade, received, receiving, onGrade }: CountsProps) {
  const { data } = answer;
  const total = totalCount(data, reading);
  const spofs = singlePoints(data);
  return (
    <Section
      title="등급별 개수"
      aside={`서버가 센 전체 ${total}개`}
      note={
        received < total && !receiving
          ? `받은 항목은 ${received}개입니다. 개수는 전체를 센 값입니다.`
          : undefined
      }
    >
      {total === 0 ? null : (
        <GradeLegend
          items={serverCounts(data, reading).map((c) => ({
            key: c.grade,
            mark: gradeStyle(c.grade),
            count: c.count,
          }))}
          active={grade}
          onToggle={(next) => onGrade(grade === next ? undefined : next)}
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
  );
}
