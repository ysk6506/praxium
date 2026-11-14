import { useMemo } from "react";
import {
  buildOverview,
  countByStep,
  describeStep,
  type ImpactTree,
  needsOverview,
  nodePicture,
  overviewPicture,
  type Spread,
  spreadPlan,
} from "../../data";
import { LaneGraph, SpreadControls } from "../../ui";
import { GroupDetail, NodeDetail } from "./NodeDetail";

export interface GraphTabProps {
  readonly kg: string;
  readonly tree: ImpactTree;
  readonly spread: Spread;
  /** The object selected in a picture of objects. */
  readonly selected: string | undefined;
  /** The group selected in the overview. */
  readonly focus: string | undefined;
  readonly onSelect: (selection: { selected?: string; focus?: string }) => void;
  readonly onSkip: () => void;
}

/**
 * The picture: a column for each layer, the spread played step by step. A result of more than 300 objects opens as an
 * overview of groups (one for each type below one cause), because a node for each object would not be readable; the
 * list holds every object. What is selected is told below the picture.
 */
export function GraphTab(props: GraphTabProps) {
  const { kg, tree, spread, selected, focus } = props;
  const overview = useMemo(() => (needsOverview(tree) ? buildOverview(tree) : undefined), [tree]);
  const picture = useMemo(
    () => (overview ? overviewPicture(tree, overview) : nodePicture(tree)),
    [tree, overview],
  );
  const counts = useMemo(() => countByStep(tree), [tree]);
  const group = overview && focus !== undefined ? overview.groups.get(focus) : undefined;
  const node = !overview && selected !== undefined ? tree.nodes.get(selected) : undefined;
  return (
    <div className="flex flex-col gap-3">
      <SpreadControls
        step={spread.state.step}
        last={spread.state.plan.last}
        playing={spread.state.playing}
        caption={describeStep(counts, spread.state.step)}
        onPlay={spread.play}
        onPause={spread.pause}
        onReplay={spread.replay}
        onSeek={spread.seek}
      />
      <LaneGraph
        name="영향받는 객체의 그래프"
        summary={picture.summary}
        lanes={picture.lanes}
        nodes={picture.nodes}
        edges={picture.edges}
        step={spread.state.step}
        stepMs={spreadPlan(picture.last).beatMs}
        playing={spread.state.playing}
        selected={overview ? focus : selected}
        onSelect={(id) => {
          if (id === undefined) props.onSelect({});
          else if (overview) props.onSelect({ focus: id });
          else if (!id.startsWith("hidden:")) props.onSelect({ selected: id });
        }}
        onSkip={props.onSkip}
      />
      {overview ? (
        <p className="text-sm text-muted">
          결과가 많아 타입별 묶음으로 보입니다. 묶음을 고르면 구성원을 볼 수 있고, 모든 객체는 목록
          탭에 있습니다.
        </p>
      ) : null}
      {group ? <GroupDetail kg={kg} tree={tree} group={group} /> : null}
      {node ? <NodeDetail kg={kg} tree={tree} node={node} /> : null}
    </div>
  );
}
