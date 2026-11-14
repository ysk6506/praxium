/**
 * What the lane graph is given (ADR-0051): the tree, or the groups of an overview, as plain nodes, edges and lanes with
 * positions, steps, shapes and the words a screen reader reads. The shapes of these types are the props of the graph in
 * `ui`, which does not import this layer; the screen hands one to the other.
 */

import { layerName } from "./display";
import type { GradeStyle } from "./impact-grades";
import { gradeStyle, START_STYLE, WAYPOINT_STYLE } from "./impact-grades";
import type { ImpactGroup, ImpactOverview } from "./impact-group";
import { type LayoutInput, layoutLanes, OTHER_LANE } from "./impact-layout";
import { laneFirstStep, lastStep } from "./impact-stats";
import type { ImpactNode, ImpactTree } from "./impact-tree";

export interface PictureNode {
  readonly id: string;
  readonly label: string;
  readonly description: string;
  readonly lane: number;
  readonly row: number;
  readonly step: number;
  readonly parent: string | null;
  readonly shape: GradeStyle["shape"];
  readonly tone: GradeStyle["tone"];
  readonly dashed: boolean;
  readonly count?: number;
}

export interface PictureEdge {
  readonly from: string;
  readonly to: string;
  readonly step: number;
  readonly dashed: boolean;
  readonly group: boolean;
}

export interface PictureLane {
  readonly name: string;
  readonly firstStep: number;
}

export interface Picture {
  readonly lanes: readonly PictureLane[];
  readonly nodes: readonly PictureNode[];
  readonly edges: readonly PictureEdge[];
  /** The last step of the spread. */
  readonly last: number;
  /** One sentence for a screen reader: what the picture holds. */
  readonly summary: string;
  /** How many objects the picture stands for. */
  readonly objects: number;
}

const OTHER_NAME = "기타";
const HIDDEN_NAME = "숨김 객체";

const laneName = (layer: string): string => (layer === OTHER_LANE ? OTHER_NAME : layerName(layer));

function styleOf(node: ImpactNode): GradeStyle {
  if (node.kind === "root" && node.grade === undefined) return START_STYLE;
  if (node.kind === "hidden") return { ...WAYPOINT_STYLE, name: HIDDEN_NAME, dashed: true };
  return gradeStyle(node.grade);
}

function describe(node: ImpactNode, style: GradeStyle): string {
  if (node.kind === "hidden") return `${HIDDEN_NAME}, ${node.hops}단계`;
  const parts = [node.name, node.type, laneName(node.layer), style.name, `${node.hops}단계`];
  if (node.assumed) parts.push("가정에 근거");
  if (node.single) parts.push("단일 장애점");
  return parts.join(", ");
}

const words = (n: number, lanes: number, last: number): string =>
  `영향받는 객체 ${n}개, 열 ${lanes}개, 단계 ${last}까지`;

function lanesOf(layers: readonly string[], first: ReadonlyMap<string, number>): PictureLane[] {
  return layers.map((layer) => ({ name: laneName(layer), firstStep: first.get(layer) ?? 0 }));
}

/** The picture with a node for each object of the tree. */
export function nodePicture(tree: ImpactTree): Picture {
  const inputs: LayoutInput[] = tree.order.map((id) => {
    const n = tree.nodes.get(id);
    return { id, layer: n?.layer ?? "", parent: n?.parent ?? null };
  });
  const layout = layoutLanes(inputs);
  const first = laneFirstStep(tree);
  const nodes: PictureNode[] = [];
  const edges: PictureEdge[] = [];
  let objects = 0;
  for (const id of tree.order) {
    const node = tree.nodes.get(id);
    const placed = layout.placed.get(id);
    if (node === undefined || placed === undefined) continue;
    const style = styleOf(node);
    if (node.grade !== undefined) objects++;
    nodes.push({
      id,
      label: node.kind === "hidden" ? HIDDEN_NAME : node.name,
      description: describe(node, style),
      lane: placed.lane,
      row: placed.row,
      step: node.step,
      parent: node.parent,
      shape: style.shape,
      tone: style.tone,
      dashed: style.dashed,
    });
    if (node.parent !== null) {
      edges.push({
        from: node.parent,
        to: id,
        step: node.step,
        dashed: node.edge !== undefined && node.edge.grade !== "fact",
        group: node.edge?.viaGroup !== undefined,
      });
    }
  }
  const last = lastStep(tree);
  const layers = layout.lanes.map((l) => l.layer);
  return {
    lanes: lanesOf(layers, first),
    nodes,
    edges,
    last,
    summary: words(objects, layout.lanes.length, last),
    objects,
  };
}

function groupDescription(group: ImpactGroup): string {
  const grades = Object.entries(group.grades)
    .sort(([a], [b]) => (gradeStyle(a).name < gradeStyle(b).name ? -1 : 1))
    .map(([grade, n]) => `${gradeStyle(grade).name} ${n}`);
  const head = `${group.type} ${group.count}개, ${laneName(group.layer)}`;
  return grades.length === 0 ? head : `${head}, ${grades.join(", ")}`;
}

/** The picture with a node for each group of an overview, a line from a group to the groups below it. */
export function overviewPicture(tree: ImpactTree, overview: ImpactOverview): Picture {
  const inputs: LayoutInput[] = overview.order.map((key) => {
    const g = overview.groups.get(key);
    return { id: key, layer: g?.layer ?? "", parent: g?.parent ?? null };
  });
  const layout = layoutLanes(inputs);
  const first = new Map<string, number>();
  const nodes: PictureNode[] = [];
  const edges: PictureEdge[] = [];
  let objects = 0;
  let last = 0;
  for (const key of overview.order) {
    const group = overview.groups.get(key);
    const placed = layout.placed.get(key);
    if (group === undefined || placed === undefined) continue;
    const style = group.worst === undefined ? WAYPOINT_STYLE : gradeStyle(group.worst);
    objects += Object.values(group.grades).reduce((a, b) => a + b, 0);
    for (const id of group.members) last = Math.max(last, tree.nodes.get(id)?.step ?? 0);
    first.set(group.layer, Math.min(first.get(group.layer) ?? group.step, group.step));
    nodes.push({
      id: key,
      label: group.type,
      description: groupDescription(group),
      lane: placed.lane,
      row: placed.row,
      step: group.step,
      parent: group.parent,
      shape: style.shape,
      tone: style.tone,
      dashed: style.dashed,
      count: group.count,
    });
    if (group.parent !== null) {
      const members = group.members.map((id) => tree.nodes.get(id)?.edge);
      edges.push({
        from: group.parent,
        to: key,
        step: group.step,
        dashed: !members.some((e) => e?.grade === "fact"),
        group: members.some((e) => e?.viaGroup !== undefined),
      });
    }
  }
  return {
    lanes: lanesOf(
      layout.lanes.map((l) => l.layer),
      first,
    ),
    nodes,
    edges,
    last,
    summary: `${words(objects, layout.lanes.length, last)}. 타입별 묶음 ${nodes.length}개로 보입니다`,
    objects,
  };
}
