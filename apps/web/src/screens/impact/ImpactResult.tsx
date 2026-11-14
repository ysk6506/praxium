import { useEffect, useId, useMemo, useRef } from "react";
import {
  buildImpactTree,
  type ImpactAnswer,
  type ImpactItems,
  type ImpactView,
  impactSource,
  lastStep,
  totalCount,
  useSpread,
  withItems,
} from "../../data";
import { Tabs } from "../../ui";
import { GraphTab } from "./GraphTab";
import { ListTab } from "./ListTab";

export interface ImpactResultProps {
  readonly kg: string;
  readonly view: ImpactView;
  readonly change: (patch: Partial<ImpactView>, how?: "push" | "replace") => void;
  readonly answer: ImpactAnswer;
  readonly items: ImpactItems;
  readonly layerOf: (type: string) => string;
}

/**
 * The graph and the list of the items received. It is made when the first items are there, so the spread that plays once
 * when the screen opens has the steps of the answer to play (the pages that follow only add to them).
 */
export function ImpactResult({ kg, view, change, answer, items, layerOf }: ImpactResultProps) {
  const { data, meta } = answer;
  const { reading } = view;
  const tree = useMemo(() => {
    const source = impactSource(withItems(data, reading, items.items), reading);
    const records =
      view.grade === undefined
        ? source.records
        : source.records.filter((r) => r.grade === view.grade);
    return buildImpactTree({ ...source, records }, layerOf);
  }, [data, reading, items.items, view.grade, layerOf]);
  const spread = useSpread(lastStep(tree));

  const listId = useId();
  const skip = useRef(false);
  useEffect(() => {
    if (!skip.current || view.view !== "list") return;
    skip.current = false;
    document.getElementById(listId)?.focus();
  });

  return (
    <Tabs
      value={view.view}
      onChange={(next) => change({ view: next })}
      items={[
        {
          value: "graph",
          label: "그래프",
          content: (
            <GraphTab
              kg={kg}
              tree={tree}
              spread={spread}
              selected={view.selected}
              focus={view.focus}
              onSelect={(next) => change({ selected: next.selected, focus: next.focus })}
              onSkip={() => {
                skip.current = true;
                change({ view: "list" });
              }}
            />
          ),
        },
        {
          value: "list",
          label: "목록",
          content: (
            <ListTab
              id={listId}
              kg={kg}
              tree={tree}
              sort={view.sort}
              onSort={(sort) => change({ sort })}
              groups={data[reading]?.groups_touched ?? []}
              missing={Math.max(0, totalCount(data, reading) - items.items.length)}
              hidden={
                (meta.hidden.hidden_passed_count ?? 0) + (meta.hidden.hidden_reached_count ?? 0)
              }
            />
          ),
        },
      ]}
    />
  );
}
